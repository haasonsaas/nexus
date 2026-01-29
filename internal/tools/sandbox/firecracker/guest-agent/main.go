//go:build linux

// Package main implements the guest agent that runs inside Firecracker microVMs.
// It listens for execution requests via vsock and executes code in isolation.
package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	// VsockPort is the port to listen on for host connections.
	VsockPort = 52

	// WorkspaceDir is the directory for code execution.
	WorkspaceDir = "/workspace"

	// MaxMessageSize is the maximum message size (10MB).
	MaxMessageSize = 10 * 1024 * 1024
)

// RequestType identifies the type of guest request.
type RequestType string

const (
	RequestTypeExecute  RequestType = "execute"
	RequestTypeHealth   RequestType = "health"
	RequestTypeShutdown RequestType = "shutdown"
	RequestTypeReset    RequestType = "reset"
	RequestTypeFileSync RequestType = "file_sync"
)

// GuestRequest represents a request from the host.
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
	// WorkspaceAccess controls workspace permissions: ro, rw, none.
	WorkspaceAccess string `json:"workspace_access,omitempty"`
}

// GuestResponse represents a response to the host.
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

// Agent handles requests from the host.
type Agent struct {
	listener   net.Listener
	shutdownCh chan struct{}
	wg         sync.WaitGroup
	mu         sync.Mutex
}

func main() {
	agent := &Agent{
		shutdownCh: make(chan struct{}),
	}

	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigCh
		fmt.Println("Received shutdown signal")
		agent.Shutdown()
	}()

	// Create workspace directory
	if err := os.MkdirAll(WorkspaceDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create workspace: %v\n", err)
		os.Exit(1)
	}

	// Start the agent
	if err := agent.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Agent failed: %v\n", err)
		os.Exit(1)
	}
}

// Start begins listening for host connections.
func (a *Agent) Start() error {
	// Create vsock listener
	// In Linux guests, vsock is accessed via /dev/vsock
	listener, err := createVsockListener(VsockPort)
	if err != nil {
		return fmt.Errorf("failed to create vsock listener: %w", err)
	}
	a.listener = listener

	fmt.Printf("Guest agent listening on vsock port %d\n", VsockPort)

	// Accept connections
	for {
		select {
		case <-a.shutdownCh:
			return nil
		default:
		}

		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-a.shutdownCh:
				return nil
			default:
				fmt.Fprintf(os.Stderr, "Accept error: %v\n", err)
				continue
			}
		}

		a.wg.Add(1)
		go a.handleConnection(conn)
	}
}

// createVsockListener creates a vsock listener.
func createVsockListener(port uint32) (net.Listener, error) {
	// Try virtio-vsock first
	// AF_VSOCK = 40 on Linux
	fd, err := syscall.Socket(40, syscall.SOCK_STREAM, 0)
	if err != nil {
		// Fall back to Unix socket for testing
		return net.Listen("unix", fmt.Sprintf("/tmp/vsock-%d.sock", port))
	}

	// Bind to vsock address
	// sockaddr_vm: family (2 bytes), reserved1 (2 bytes), port (4 bytes), cid (4 bytes)
	addr := make([]byte, 16)
	binary.LittleEndian.PutUint16(addr[0:2], 40) // AF_VSOCK
	binary.LittleEndian.PutUint32(addr[4:8], port)
	binary.LittleEndian.PutUint32(addr[8:12], 0xFFFFFFFF) // VMADDR_CID_ANY

	_, _, errno := syscall.Syscall(syscall.SYS_BIND, uintptr(fd), uintptr(unsafePointer(addr)), 16)
	if errno != 0 {
		syscall.Close(fd)
		return net.Listen("unix", fmt.Sprintf("/tmp/vsock-%d.sock", port))
	}

	_, _, errno = syscall.Syscall(syscall.SYS_LISTEN, uintptr(fd), 5, 0)
	if errno != 0 {
		syscall.Close(fd)
		return nil, fmt.Errorf("listen failed: %w", errno)
	}

	file := os.NewFile(uintptr(fd), "vsock")
	return net.FileListener(file)
}

// unsafePointer returns an unsafe pointer to the byte slice.
// This is needed for syscall operations.
func unsafePointer(b []byte) uintptr {
	if len(b) == 0 {
		return 0
	}
	return uintptr(unsafe.Pointer(&b[0]))
}

// handleConnection processes requests from a host connection.
func (a *Agent) handleConnection(conn net.Conn) {
	defer a.wg.Done()
	defer conn.Close()

	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)

	for {
		select {
		case <-a.shutdownCh:
			return
		default:
		}

		// Read message length
		lengthBuf := make([]byte, 4)
		if _, err := io.ReadFull(reader, lengthBuf); err != nil {
			if err != io.EOF {
				fmt.Fprintf(os.Stderr, "Read error: %v\n", err)
			}
			return
		}

		length := binary.LittleEndian.Uint32(lengthBuf)
		if length > MaxMessageSize {
			fmt.Fprintf(os.Stderr, "Message too large: %d\n", length)
			continue
		}

		// Read message body
		body := make([]byte, length)
		if _, err := io.ReadFull(reader, body); err != nil {
			fmt.Fprintf(os.Stderr, "Read body error: %v\n", err)
			return
		}

		// Parse request
		var req GuestRequest
		if err := json.Unmarshal(body, &req); err != nil {
			if sendErr := a.sendError(writer, 0, fmt.Sprintf("Invalid request: %v", err)); sendErr != nil {
				fmt.Fprintf(os.Stderr, "Send error response failed: %v\n", sendErr)
			}
			continue
		}

		// Handle request
		resp := a.handleRequest(&req)

		// Send response
		if err := a.sendResponse(writer, resp); err != nil {
			fmt.Fprintf(os.Stderr, "Send error: %v\n", err)
			return
		}

		// Check for shutdown request
		if req.Type == RequestTypeShutdown {
			a.Shutdown()
			return
		}
	}
}

// handleRequest processes a single request.
func (a *Agent) handleRequest(req *GuestRequest) *GuestResponse {
	switch req.Type {
	case RequestTypeExecute:
		return a.handleExecute(req)
	case RequestTypeHealth:
		return a.handleHealth(req)
	case RequestTypeReset:
		return a.handleReset(req)
	case RequestTypeShutdown:
		return &GuestResponse{ID: req.ID, Success: true}
	case RequestTypeFileSync:
		return a.handleFileSync(req)
	default:
		return &GuestResponse{
			ID:    req.ID,
			Error: fmt.Sprintf("Unknown request type: %s", req.Type),
		}
	}
}

// handleExecute executes code.
func (a *Agent) handleExecute(req *GuestRequest) *GuestResponse {
	start := time.Now()

	// Prepare workspace
	workspace := req.Workspace
	if workspace == "" {
		workspace = WorkspaceDir
	}

	if err := os.MkdirAll(workspace, 0755); err != nil {
		return &GuestResponse{
			ID:    req.ID,
			Error: fmt.Sprintf("Failed to create workspace: %v", err),
		}
	}

	// Write code file
	mainFile := getMainFilename(req.Language)
	codePath := filepath.Join(workspace, mainFile)
	if err := os.WriteFile(codePath, []byte(req.Code), 0644); err != nil {
		return &GuestResponse{
			ID:    req.ID,
			Error: fmt.Sprintf("Failed to write code: %v", err),
		}
	}

	// Write additional files
	writtenFiles := []string{codePath}
	for name, content := range req.Files {
		filePath, err := safeWorkspacePath(workspace, name)
		if err != nil {
			return &GuestResponse{
				ID:    req.ID,
				Error: fmt.Sprintf("Failed to write file %s: %v", name, err),
			}
		}
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return &GuestResponse{
				ID:    req.ID,
				Error: fmt.Sprintf("Failed to write file %s: %v", name, err),
			}
		}
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return &GuestResponse{
				ID:    req.ID,
				Error: fmt.Sprintf("Failed to write file %s: %v", name, err),
			}
		}
		writtenFiles = append(writtenFiles, filePath)
	}

	if isReadOnlyAccess(req.WorkspaceAccess) {
		if err := applyReadOnlyAccess(workspace, writtenFiles); err != nil {
			return &GuestResponse{
				ID:    req.ID,
				Error: fmt.Sprintf("Failed to apply read-only workspace: %v", err),
			}
		}
	}

	// Set timeout
	timeout := time.Duration(req.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Build command
	cmd := buildCommand(ctx, req.Language, workspace)
	if cmd == nil {
		return &GuestResponse{
			ID:    req.ID,
			Error: fmt.Sprintf("Unsupported language: %s", req.Language),
		}
	}

	// Set resource limits
	setResourceLimits(cmd, req.CPULimit, req.MemLimit)

	// Set up stdin
	if req.Stdin != "" {
		cmd.Stdin = strings.NewReader(req.Stdin)
	}

	// Capture output
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run command
	err := cmd.Run()

	duration := time.Since(start).Milliseconds()

	resp := &GuestResponse{
		ID:       req.ID,
		Success:  true,
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: duration,
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			resp.Timeout = true
			resp.Error = "Execution timeout"
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			resp.ExitCode = exitErr.ExitCode()
		} else {
			resp.Error = err.Error()
		}
	}

	return resp
}

// handleHealth responds to health checks.
func (a *Agent) handleHealth(req *GuestRequest) *GuestResponse {
	return &GuestResponse{
		ID:      req.ID,
		Success: true,
	}
}

// handleReset cleans up the workspace.
func (a *Agent) handleReset(req *GuestRequest) *GuestResponse {
	// Clean workspace
	if err := os.RemoveAll(WorkspaceDir); err != nil {
		return &GuestResponse{
			ID:    req.ID,
			Error: fmt.Sprintf("Failed to clean workspace: %v", err),
		}
	}

	if err := os.MkdirAll(WorkspaceDir, 0755); err != nil {
		return &GuestResponse{
			ID:    req.ID,
			Error: fmt.Sprintf("Failed to recreate workspace: %v", err),
		}
	}

	return &GuestResponse{
		ID:      req.ID,
		Success: true,
	}
}

// handleFileSync writes files to the workspace.
func (a *Agent) handleFileSync(req *GuestRequest) *GuestResponse {
	workspace := req.Workspace
	if workspace == "" {
		workspace = WorkspaceDir
	}

	if err := os.MkdirAll(workspace, 0755); err != nil {
		return &GuestResponse{
			ID:    req.ID,
			Error: fmt.Sprintf("Failed to create workspace: %v", err),
		}
	}

	writtenFiles := []string{}
	for name, content := range req.Files {
		filePath, err := safeWorkspacePath(workspace, name)
		if err != nil {
			return &GuestResponse{
				ID:    req.ID,
				Error: fmt.Sprintf("Failed to write file %s: %v", name, err),
			}
		}
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			return &GuestResponse{
				ID:    req.ID,
				Error: fmt.Sprintf("Failed to write file %s: %v", name, err),
			}
		}
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return &GuestResponse{
				ID:    req.ID,
				Error: fmt.Sprintf("Failed to write file %s: %v", name, err),
			}
		}
		writtenFiles = append(writtenFiles, filePath)
	}

	if isReadOnlyAccess(req.WorkspaceAccess) {
		if err := applyReadOnlyAccess(workspace, writtenFiles); err != nil {
			return &GuestResponse{
				ID:    req.ID,
				Error: fmt.Sprintf("Failed to apply read-only workspace: %v", err),
			}
		}
	}

	return &GuestResponse{
		ID:      req.ID,
		Success: true,
	}
}

func safeWorkspacePath(baseDir, name string) (string, error) {
	normalized := strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	cleaned := filepath.Clean(filepath.FromSlash(normalized))
	if cleaned == "" || cleaned == "." {
		return "", fmt.Errorf("invalid filename: %q", name)
	}
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		cleaned = filepath.Base(cleaned)
	}
	target := filepath.Join(baseDir, cleaned)
	rel, err := filepath.Rel(baseDir, target)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid filename: %q", name)
	}
	return target, nil
}

// sendResponse sends a response to the host.
func (a *Agent) sendResponse(writer *bufio.Writer, resp *GuestResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}

	lengthBuf := make([]byte, 4)
	dataLen := uint64(len(data))
	if dataLen > math.MaxUint32 {
		return fmt.Errorf("message too large: %d bytes", dataLen)
	}
	// #nosec G115 -- bounded by check above
	binary.LittleEndian.PutUint32(lengthBuf, uint32(dataLen))

	if _, err := writer.Write(lengthBuf); err != nil {
		return err
	}

	if _, err := writer.Write(data); err != nil {
		return err
	}

	return writer.Flush()
}

// sendError sends an error response.
func (a *Agent) sendError(writer *bufio.Writer, id uint64, message string) error {
	return a.sendResponse(writer, &GuestResponse{
		ID:    id,
		Error: message,
	})
}

// Shutdown gracefully shuts down the agent.
func (a *Agent) Shutdown() {
	a.mu.Lock()
	defer a.mu.Unlock()

	select {
	case <-a.shutdownCh:
		return // Already shutting down
	default:
		close(a.shutdownCh)
	}

	if a.listener != nil {
		a.listener.Close()
	}

	// Wait for connections to finish
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		fmt.Println("Shutdown timeout, forcing exit")
	}

	// Clean shutdown
	syscall.Sync()
	if err := syscall.Reboot(syscall.LINUX_REBOOT_CMD_POWER_OFF); err != nil {
		fmt.Fprintf(os.Stderr, "Reboot failed: %v\n", err)
	}
}

// getMainFilename returns the filename for the code based on language.
func getMainFilename(language string) string {
	switch language {
	case "python":
		return "main.py"
	case "nodejs":
		return "main.js"
	case "go":
		return "main.go"
	case "bash":
		return "main.sh"
	default:
		return "main.txt"
	}
}

// buildCommand creates the execution command for a language.
func buildCommand(ctx context.Context, language, workspace string) *exec.Cmd {
	var cmd *exec.Cmd

	switch language {
	case "python":
		cmd = exec.CommandContext(ctx, "python3", "main.py")
	case "nodejs":
		cmd = exec.CommandContext(ctx, "node", "main.js")
	case "go":
		cmd = exec.CommandContext(ctx, "sh", "-c", "go run main.go")
	case "bash":
		cmd = exec.CommandContext(ctx, "bash", "main.sh")
	default:
		return nil
	}

	cmd.Dir = workspace
	cmd.Env = []string{
		"PATH=/usr/local/bin:/usr/bin:/bin",
		"HOME=/root",
		"LANG=C.UTF-8",
	}

	return cmd
}

func isReadOnlyAccess(mode string) bool {
	value := strings.ToLower(strings.TrimSpace(mode))
	switch value {
	case "", "ro", "readonly", "read-only":
		return true
	case "rw", "readwrite", "read-write", "none":
		return false
	default:
		return true
	}
}

func applyReadOnlyAccess(workspace string, files []string) error {
	for _, file := range files {
		if err := os.Chmod(file, 0444); err != nil {
			return err
		}
	}
	return os.Chmod(workspace, 0555)
}

// setResourceLimits sets CPU and memory limits for the command.
func setResourceLimits(cmd *exec.Cmd, cpuLimit, memLimit int) {
	// Set process attributes
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// Create new process group for easy killing
		Setpgid: true,
		Pgid:    0,
	}

	// Set resource limits via rlimit
	// These will be inherited by the child process

	// Memory limit (in bytes)
	if memLimit > 0 {
		memBytes := uint64(memLimit) * 1024 * 1024
		// Set via cgroup or rlimit
		// For now, we rely on the VM's memory limit
		_ = memBytes
	}

	// CPU limit (millicores)
	if cpuLimit > 0 {
		// Use cgroup cpu.cfs_quota_us and cpu.cfs_period_us
		// For now, we rely on the VM's CPU limit
		_ = cpuLimit
	}
}
