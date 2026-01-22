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

// maxPendingRequests limits the number of concurrent pending requests
// to prevent memory exhaustion from unresponsive servers.
const maxPendingRequests = 1000

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
	requests  chan *JSONRPCRequest
	nextID    atomic.Int64

	connected atomic.Bool
	stopChan  chan struct{}
	wg        sync.WaitGroup
	writeMu   sync.Mutex
	closeOnce sync.Once
}

// NewStdioTransport creates a new stdio transport.
func NewStdioTransport(cfg *ServerConfig) *StdioTransport {
	return &StdioTransport{
		config:   cfg,
		logger:   slog.Default().With("mcp_server", cfg.ID, "transport", "stdio"),
		pending:  make(map[int64]chan *JSONRPCResponse),
		events:   make(chan *JSONRPCNotification, 100),
		requests: make(chan *JSONRPCRequest, 100),
		stopChan: make(chan struct{}),
	}
}

// Connect starts the subprocess and establishes the connection.
func (t *StdioTransport) Connect(ctx context.Context) error {
	if t.config.Command == "" {
		return fmt.Errorf("command is required for stdio transport")
	}

	// Validate configuration before executing
	if err := t.config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
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

	t.stderr, err = t.process.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

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

// Close stops the subprocess. Safe to call multiple times.
func (t *StdioTransport) Close() error {
	var closeErr error
	t.closeOnce.Do(func() {
		t.connected.Store(false)
		close(t.stopChan)

		if t.stdin != nil {
			t.stdin.Close()
		}

		if t.process != nil && t.process.Process != nil {
			if err := t.process.Process.Kill(); err != nil {
				closeErr = err
			}
		}

		t.wg.Wait()
	})
	return closeErr
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

	// Create response channel with pending limit check
	respChan := make(chan *JSONRPCResponse, 1)
	t.pendingMu.Lock()
	if len(t.pending) >= maxPendingRequests {
		t.pendingMu.Unlock()
		return nil, fmt.Errorf("too many pending requests (limit: %d)", maxPendingRequests)
	}
	t.pending[id] = respChan
	t.pendingMu.Unlock()

	defer func() {
		t.pendingMu.Lock()
		delete(t.pending, id)
		t.pendingMu.Unlock()
	}()

	// Send request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	t.writeMu.Lock()
	_, err = t.stdin.Write(append(data, '\n'))
	t.writeMu.Unlock()
	if err != nil {
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

	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}
	t.writeMu.Lock()
	_, err = t.stdin.Write(append(data, '\n'))
	t.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("write notification: %w", err)
	}

	return nil
}

// Events returns the notification channel.
func (t *StdioTransport) Events() <-chan *JSONRPCNotification {
	return t.events
}

// Requests returns the request channel.
func (t *StdioTransport) Requests() <-chan *JSONRPCRequest {
	return t.requests
}

// Respond sends a response to a server request.
func (t *StdioTransport) Respond(ctx context.Context, id any, result any, rpcErr *JSONRPCError) error {
	if !t.connected.Load() {
		return fmt.Errorf("not connected")
	}
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   rpcErr,
	}
	if rpcErr == nil && result != nil {
		data, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("marshal result: %w", err)
		}
		resp.Result = data
	}
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	t.writeMu.Lock()
	_, err = t.stdin.Write(append(data, '\n'))
	t.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
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
	var envelope struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      any             `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
		Result  json.RawMessage `json:"result,omitempty"`
		Error   *JSONRPCError   `json:"error,omitempty"`
	}
	if err := json.Unmarshal([]byte(line), &envelope); err != nil {
		t.logger.Debug("failed to parse line", "error", err)
		return
	}

	if envelope.Method != "" {
		if envelope.ID != nil {
			req := &JSONRPCRequest{
				JSONRPC: envelope.JSONRPC,
				ID:      envelope.ID,
				Method:  envelope.Method,
				Params:  envelope.Params,
			}
			select {
			case t.requests <- req:
			default:
				t.logger.Warn("request channel full, dropping")
			}
			return
		}
		notif := &JSONRPCNotification{
			JSONRPC: envelope.JSONRPC,
			Method:  envelope.Method,
			Params:  envelope.Params,
		}
		select {
		case t.events <- notif:
		default:
			t.logger.Warn("notification channel full, dropping")
		}
		return
	}

	if envelope.ID != nil {
		resp := JSONRPCResponse{
			JSONRPC: envelope.JSONRPC,
			ID:      envelope.ID,
			Result:  envelope.Result,
			Error:   envelope.Error,
		}
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
