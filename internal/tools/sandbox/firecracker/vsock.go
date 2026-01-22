package firecracker

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

// GuestAgentPort is the default vsock port for guest agent communication.
const GuestAgentPort = 52

// VsockConnection manages communication between host and guest via vsock.
type VsockConnection struct {
	socketPath string
	cid        uint32
	port       uint32
	conn       net.Conn
	mu         sync.Mutex
	reader     *bufio.Reader
	writer     *bufio.Writer
	closed     bool

	// requestID is used for correlating requests and responses.
	requestID uint64
	reqMu     sync.Mutex

	// pending tracks pending requests waiting for responses.
	pending   map[uint64]chan *GuestResponse
	pendingMu sync.Mutex
}

// GuestRequest represents a request sent to the guest agent.
type GuestRequest struct {
	ID        uint64            `json:"id"`
	Type      RequestType       `json:"type"`
	Command   string            `json:"command,omitempty"`
	Code      string            `json:"code,omitempty"`
	Language  string            `json:"language,omitempty"`
	Stdin     string            `json:"stdin,omitempty"`
	Files     map[string]string `json:"files,omitempty"`
	Timeout   int               `json:"timeout,omitempty"`
	CPULimit  int               `json:"cpu_limit,omitempty"`
	MemLimit  int               `json:"mem_limit,omitempty"`
	Workspace string            `json:"workspace,omitempty"`
}

// RequestType identifies the type of guest request.
type RequestType string

const (
	RequestTypeExecute  RequestType = "execute"
	RequestTypeHealth   RequestType = "health"
	RequestTypeShutdown RequestType = "shutdown"
	RequestTypeReset    RequestType = "reset"
	RequestTypeFileSync RequestType = "file_sync"
)

// GuestResponse represents a response from the guest agent.
type GuestResponse struct {
	ID       uint64 `json:"id"`
	Success  bool   `json:"success"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
	Timeout  bool   `json:"timeout,omitempty"`
	Duration int64  `json:"duration_ms,omitempty"`
}

// NewVsockConnection creates a new vsock connection to a guest.
func NewVsockConnection(socketPath string, cid, port uint32) (*VsockConnection, error) {
	vc := &VsockConnection{
		socketPath: socketPath,
		cid:        cid,
		port:       port,
		pending:    make(map[uint64]chan *GuestResponse),
	}

	return vc, nil
}

// Connect establishes the vsock connection.
func (vc *VsockConnection) Connect(ctx context.Context) error {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	if vc.conn != nil {
		return nil // Already connected
	}

	// Connect to the vsock socket
	// The vsock socket is created by Firecracker and we connect to it via Unix socket
	// then send data to the guest via the virtio-vsock device
	conn, err := vc.dialVsock(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to vsock: %w", err)
	}

	vc.conn = conn
	vc.reader = bufio.NewReader(conn)
	vc.writer = bufio.NewWriter(conn)
	vc.closed = false

	// Start response reader
	go vc.readResponses()

	return nil
}

// dialVsock connects to the vsock socket.
func (vc *VsockConnection) dialVsock(ctx context.Context) (net.Conn, error) {
	// Firecracker exposes vsock via a Unix socket
	// We connect to it and then communicate with the guest
	vsockPath := vc.socketPath + "_vsock"

	// Check if vsock socket exists
	if _, err := os.Stat(vsockPath); os.IsNotExist(err) {
		// Try alternative path
		vsockPath = vc.socketPath + ".vsock"
	}

	// Use dialer with context
	dialer := net.Dialer{
		Timeout: 10 * time.Second,
	}

	conn, err := dialer.DialContext(ctx, "unix", vsockPath)
	if err != nil {
		return nil, fmt.Errorf("failed to dial vsock socket: %w", err)
	}

	// Send connection request to guest
	// Format: [CID:4bytes][Port:4bytes]
	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:4], vc.cid)
	binary.LittleEndian.PutUint32(header[4:8], vc.port)

	if _, err := conn.Write(header); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to send vsock header: %w", err)
	}

	return conn, nil
}

// readResponses continuously reads responses from the guest.
func (vc *VsockConnection) readResponses() {
	for {
		vc.mu.Lock()
		if vc.closed || vc.reader == nil {
			vc.mu.Unlock()
			return
		}
		reader := vc.reader
		vc.mu.Unlock()

		// Read message length (4 bytes)
		lengthBuf := make([]byte, 4)
		if _, err := io.ReadFull(reader, lengthBuf); err != nil {
			if vc.isClosed() {
				return
			}
			// Connection error, try to reconnect
			time.Sleep(100 * time.Millisecond)
			continue
		}

		length := binary.LittleEndian.Uint32(lengthBuf)
		if length > 10*1024*1024 { // 10MB max message size
			continue
		}

		// Read message body
		body := make([]byte, length)
		if _, err := io.ReadFull(reader, body); err != nil {
			continue
		}

		// Parse response
		var resp GuestResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			continue
		}

		// Dispatch to waiting request
		vc.pendingMu.Lock()
		if ch, ok := vc.pending[resp.ID]; ok {
			delete(vc.pending, resp.ID)
			ch <- &resp
		}
		vc.pendingMu.Unlock()
	}
}

// Send sends a request to the guest and waits for a response.
func (vc *VsockConnection) Send(ctx context.Context, req *GuestRequest) (*GuestResponse, error) {
	if err := vc.ensureConnected(ctx); err != nil {
		return nil, err
	}

	// Assign request ID
	vc.reqMu.Lock()
	vc.requestID++
	req.ID = vc.requestID
	vc.reqMu.Unlock()

	// Create response channel
	respCh := make(chan *GuestResponse, 1)
	vc.pendingMu.Lock()
	vc.pending[req.ID] = respCh
	vc.pendingMu.Unlock()

	// Cleanup on exit
	defer func() {
		vc.pendingMu.Lock()
		delete(vc.pending, req.ID)
		vc.pendingMu.Unlock()
	}()

	// Serialize request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Send message with length prefix
	vc.mu.Lock()
	if vc.writer == nil {
		vc.mu.Unlock()
		return nil, fmt.Errorf("connection not established")
	}

	lengthBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(lengthBuf, uint32(len(data)))

	if _, err := vc.writer.Write(lengthBuf); err != nil {
		vc.mu.Unlock()
		return nil, fmt.Errorf("failed to write message length: %w", err)
	}

	if _, err := vc.writer.Write(data); err != nil {
		vc.mu.Unlock()
		return nil, fmt.Errorf("failed to write message body: %w", err)
	}

	if err := vc.writer.Flush(); err != nil {
		vc.mu.Unlock()
		return nil, fmt.Errorf("failed to flush message: %w", err)
	}
	vc.mu.Unlock()

	// Wait for response
	select {
	case resp := <-respCh:
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Execute sends an execution request to the guest.
func (vc *VsockConnection) Execute(ctx context.Context, code, language, stdin string, files map[string]string, timeout int) (*GuestResponse, error) {
	req := &GuestRequest{
		Type:     RequestTypeExecute,
		Code:     code,
		Language: language,
		Stdin:    stdin,
		Files:    files,
		Timeout:  timeout,
	}

	return vc.Send(ctx, req)
}

// Health checks if the guest agent is responsive.
func (vc *VsockConnection) Health(ctx context.Context) error {
	req := &GuestRequest{
		Type: RequestTypeHealth,
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := vc.Send(ctx, req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("health check returned failure: %s", resp.Error)
	}

	return nil
}

// Reset tells the guest to reset its state.
func (vc *VsockConnection) Reset(ctx context.Context) error {
	req := &GuestRequest{
		Type: RequestTypeReset,
	}

	resp, err := vc.Send(ctx, req)
	if err != nil {
		return fmt.Errorf("reset failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("reset returned failure: %s", resp.Error)
	}

	return nil
}

// Shutdown tells the guest to shut down gracefully.
func (vc *VsockConnection) Shutdown(ctx context.Context) error {
	req := &GuestRequest{
		Type: RequestTypeShutdown,
	}

	// Don't wait for response since guest will shut down
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	_, _ = vc.Send(ctx, req)
	return nil
}

// SyncFiles sends files to the guest.
func (vc *VsockConnection) SyncFiles(ctx context.Context, files map[string]string, workspace string) error {
	req := &GuestRequest{
		Type:      RequestTypeFileSync,
		Files:     files,
		Workspace: workspace,
	}

	resp, err := vc.Send(ctx, req)
	if err != nil {
		return fmt.Errorf("file sync failed: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("file sync returned failure: %s", resp.Error)
	}

	return nil
}

// Close closes the vsock connection.
func (vc *VsockConnection) Close() error {
	vc.mu.Lock()
	defer vc.mu.Unlock()

	if vc.closed {
		return nil
	}
	vc.closed = true

	// Cancel all pending requests
	vc.pendingMu.Lock()
	for id, ch := range vc.pending {
		delete(vc.pending, id)
		close(ch)
	}
	vc.pendingMu.Unlock()

	if vc.conn != nil {
		return vc.conn.Close()
	}

	return nil
}

// isClosed checks if the connection is closed.
func (vc *VsockConnection) isClosed() bool {
	vc.mu.Lock()
	defer vc.mu.Unlock()
	return vc.closed
}

// ensureConnected ensures the connection is established.
func (vc *VsockConnection) ensureConnected(ctx context.Context) error {
	vc.mu.Lock()
	connected := vc.conn != nil && !vc.closed
	vc.mu.Unlock()

	if connected {
		return nil
	}

	return vc.Connect(ctx)
}

// VsockListener listens for incoming vsock connections (used in guest).
type VsockListener struct {
	port     uint32
	listener net.Listener
	mu       sync.Mutex
	closed   bool
}

// NewVsockListener creates a listener for vsock connections in the guest.
func NewVsockListener(port uint32) (*VsockListener, error) {
	// In the guest, we listen on the virtio-vsock device
	// This is typically exposed as /dev/vsock or via a file descriptor

	// For Linux guests, we use the vsock address family
	// AF_VSOCK = 40
	addr := fmt.Sprintf("/dev/vsock:%d", port)

	// Try to create vsock listener
	// In practice, this uses the linux vsock socket API
	listener, err := net.Listen("unix", addr)
	if err != nil {
		// Fall back to standard approach
		return nil, fmt.Errorf("failed to create vsock listener: %w", err)
	}

	return &VsockListener{
		port:     port,
		listener: listener,
	}, nil
}

// Accept waits for and returns the next connection.
func (vl *VsockListener) Accept() (net.Conn, error) {
	vl.mu.Lock()
	if vl.closed {
		vl.mu.Unlock()
		return nil, fmt.Errorf("listener closed")
	}
	listener := vl.listener
	vl.mu.Unlock()

	return listener.Accept()
}

// Close closes the listener.
func (vl *VsockListener) Close() error {
	vl.mu.Lock()
	defer vl.mu.Unlock()

	if vl.closed {
		return nil
	}
	vl.closed = true

	if vl.listener != nil {
		return vl.listener.Close()
	}
	return nil
}

// Port returns the port this listener is bound to.
func (vl *VsockListener) Port() uint32 {
	return vl.port
}
