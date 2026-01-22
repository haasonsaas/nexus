package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// StdioTransport implements the MCP stdio transport.
type StdioTransport struct {
	config *ServerConfig
	logger *slog.Logger

	process *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Scanner
	stderr  io.ReadCloser

	pending   map[int64]chan *JSONRPCResponse
	pendingMu sync.Mutex
	events    chan *JSONRPCNotification
	nextID    atomic.Int64

	connected atomic.Bool
	stopChan  chan struct{}
	wg        sync.WaitGroup
}

// NewStdioTransport creates a new stdio transport.
func NewStdioTransport(cfg *ServerConfig) *StdioTransport {
	return &StdioTransport{
		config:   cfg,
		logger:   slog.Default().With("mcp_server", cfg.ID, "transport", "stdio"),
		pending:  make(map[int64]chan *JSONRPCResponse),
		events:   make(chan *JSONRPCNotification, 100),
		stopChan: make(chan struct{}),
	}
}

// Connect starts the subprocess and establishes the connection.
func (t *StdioTransport) Connect(ctx context.Context) error {
	if t.config.Command == "" {
		return fmt.Errorf("command is required for stdio transport")
	}

	// Build command
	t.process = exec.CommandContext(ctx, t.config.Command, t.config.Args...)

	// Set environment
	t.process.Env = os.Environ()
	for k, v := range t.config.Env {
		t.process.Env = append(t.process.Env, fmt.Sprintf("%s=%s", k, v))
	}

	if t.config.WorkDir != "" {
		t.process.Dir = t.config.WorkDir
	}

	// Set up pipes
	var err error
	t.stdin, err = t.process.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := t.process.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	t.stdout = bufio.NewScanner(stdout)
	t.stdout.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	t.stderr, _ = t.process.StderrPipe()

	// Start process
	if err := t.process.Start(); err != nil {
		return fmt.Errorf("start process: %w", err)
	}

	t.connected.Store(true)
	t.logger.Info("started MCP server process",
		"command", t.config.Command,
		"pid", t.process.Process.Pid)

	// Start reader goroutine
	t.wg.Add(1)
	go t.readLoop()

	// Log stderr
	if t.stderr != nil {
		t.wg.Add(1)
		go t.logStderr()
	}

	return nil
}

// Close stops the subprocess.
func (t *StdioTransport) Close() error {
	t.connected.Store(false)
	close(t.stopChan)

	if t.stdin != nil {
		t.stdin.Close()
	}

	if t.process != nil && t.process.Process != nil {
		t.process.Process.Kill()
	}

	t.wg.Wait()
	return nil
}

// Call sends a request and waits for a response.
func (t *StdioTransport) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if !t.connected.Load() {
		return nil, fmt.Errorf("not connected")
	}

	id := t.nextID.Add(1)

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
	}

	if params != nil {
		paramsJSON, err := json.Marshal(params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		req.Params = paramsJSON
	}

	// Create response channel
	respChan := make(chan *JSONRPCResponse, 1)
	t.pendingMu.Lock()
	t.pending[id] = respChan
	t.pendingMu.Unlock()

	defer func() {
		t.pendingMu.Lock()
		delete(t.pending, id)
		t.pendingMu.Unlock()
	}()

	// Send request
	data, _ := json.Marshal(req)
	if _, err := t.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("write request: %w", err)
	}

	// Wait for response
	timeout := t.config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	select {
	case resp := <-respChan:
		if resp.Error != nil {
			return nil, fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, fmt.Errorf("request timeout after %v", timeout)
	case <-t.stopChan:
		return nil, fmt.Errorf("transport closed")
	}
}

// Notify sends a notification (no response expected).
func (t *StdioTransport) Notify(ctx context.Context, method string, params any) error {
	if !t.connected.Load() {
		return fmt.Errorf("not connected")
	}

	notif := JSONRPCNotification{
		JSONRPC: "2.0",
		Method:  method,
	}

	if params != nil {
		paramsJSON, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("marshal params: %w", err)
		}
		notif.Params = paramsJSON
	}

	data, _ := json.Marshal(notif)
	if _, err := t.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write notification: %w", err)
	}

	return nil
}

// Events returns the notification channel.
func (t *StdioTransport) Events() <-chan *JSONRPCNotification {
	return t.events
}

// Connected returns whether the transport is connected.
func (t *StdioTransport) Connected() bool {
	return t.connected.Load()
}

// readLoop reads messages from stdout.
func (t *StdioTransport) readLoop() {
	defer t.wg.Done()
	defer t.connected.Store(false)

	for t.stdout.Scan() {
		select {
		case <-t.stopChan:
			return
		default:
		}

		line := t.stdout.Text()
		if line == "" {
			continue
		}

		t.processLine(line)
	}

	if err := t.stdout.Err(); err != nil {
		t.logger.Error("stdout scanner error", "error", err)
	}
}

// processLine handles a single JSON-RPC message.
func (t *StdioTransport) processLine(line string) {
	// Try to parse as response (has ID)
	var resp JSONRPCResponse
	if err := json.Unmarshal([]byte(line), &resp); err == nil && resp.ID != nil {
		// Convert ID to int64 for lookup
		var id int64
		switch v := resp.ID.(type) {
		case float64:
			id = int64(v)
		case int64:
			id = v
		case int:
			id = int64(v)
		default:
			t.logger.Warn("unexpected response ID type", "id", resp.ID)
			return
		}

		t.pendingMu.Lock()
		if ch, ok := t.pending[id]; ok {
			select {
			case ch <- &resp:
			default:
			}
			delete(t.pending, id)
		}
		t.pendingMu.Unlock()
		return
	}

	// Try to parse as notification (no ID)
	var notif JSONRPCNotification
	if err := json.Unmarshal([]byte(line), &notif); err == nil && notif.Method != "" {
		select {
		case t.events <- &notif:
		default:
			t.logger.Warn("notification channel full, dropping")
		}
	}
}

// logStderr logs stderr output from the subprocess.
func (t *StdioTransport) logStderr() {
	defer t.wg.Done()

	scanner := bufio.NewScanner(t.stderr)
	for scanner.Scan() {
		select {
		case <-t.stopChan:
			return
		default:
		}

		line := scanner.Text()
		if line != "" {
			t.logger.Debug("server stderr", "message", line)
		}
	}
}
