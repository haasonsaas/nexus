// Package edge provides the edge daemon management system for Nexus.
//
// The edge system enables local/privileged capabilities to be executed on user
// machines while the core handles orchestration. This includes:
//   - Device access (camera, screen, location)
//   - Browser relay (attached Chrome sessions)
//   - Edge-only channels (iMessage, local Signal bridges)
//   - Local filesystem and command execution
//
// # Architecture
//
//	┌─────────────────────────────────────────────────────────────────┐
//	│                        Nexus Core                                │
//	│  ┌─────────────────────────────────────────────────────────────┐│
//	│  │                     Edge Manager                             ││
//	│  │  - Connection registry                                       ││
//	│  │  - Tool routing                                              ││
//	│  │  - Event dispatch                                            ││
//	│  └─────────────────────────────────────────────────────────────┘│
//	└───────────────────────────┬─────────────────────────────────────┘
//	                            │ gRPC streaming
//	            ┌───────────────┼───────────────┐
//	            │               │               │
//	      ┌─────▼─────┐   ┌─────▼─────┐   ┌─────▼─────┐
//	      │   Edge    │   │   Edge    │   │   Edge    │
//	      │  MacBook  │   │  iPhone   │   │  Server   │
//	      └───────────┘   └───────────┘   └───────────┘
//
// # Security Model
//
// Edges are semi-trusted: they can execute privileged actions but are subject to:
//   - Authentication (pre-shared tokens or TOFU with approval)
//   - Tool policies (allow/deny lists, approval requirements)
//   - Rate limiting and quotas
//   - Audit logging for all actions
package edge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	pb "github.com/haasonsaas/nexus/pkg/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Manager coordinates edge daemon connections and tool execution.
type Manager struct {
	mu sync.RWMutex

	// edges maps edge_id to connection state
	edges map[string]*EdgeConnection

	// pendingTools maps execution_id to pending tool calls
	pendingTools map[string]*PendingTool

	// config holds manager configuration
	config ManagerConfig

	// auth handles edge authentication
	auth Authenticator

	// events receives edge events for forwarding
	events chan EdgeEvent

	// logger for structured logging
	logger *slog.Logger

	// metrics for observability
	metrics *Metrics
}

// ManagerConfig configures the edge manager.
type ManagerConfig struct {
	// HeartbeatInterval is how often edges should send heartbeats.
	HeartbeatInterval time.Duration

	// HeartbeatTimeout is how long to wait before considering an edge dead.
	HeartbeatTimeout time.Duration

	// DefaultToolTimeout is the default timeout for tool execution.
	DefaultToolTimeout time.Duration

	// MaxConcurrentTools is the max tools executing per edge.
	MaxConcurrentTools int

	// EventBufferSize is the size of the event channel buffer.
	EventBufferSize int
}

// DefaultManagerConfig returns sensible defaults.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		HeartbeatInterval:  30 * time.Second,
		HeartbeatTimeout:   90 * time.Second,
		DefaultToolTimeout: 60 * time.Second,
		MaxConcurrentTools: 10,
		EventBufferSize:    1000,
	}
}

// EdgeConnection represents a connected edge daemon.
type EdgeConnection struct {
	mu sync.RWMutex

	// ID is the unique edge identifier.
	ID string

	// Name is the human-readable name.
	Name string

	// Status is the current connection status.
	Status pb.EdgeConnectionStatus

	// ConnectedAt is when the edge connected.
	ConnectedAt time.Time

	// LastHeartbeat is when we last heard from the edge.
	LastHeartbeat time.Time

	// Tools registered by this edge.
	Tools map[string]*EdgeTool

	// ChannelTypes this edge can host.
	ChannelTypes []string

	// Capabilities of this edge.
	Capabilities *pb.BasicEdgeCapabilities

	// Version of the edge daemon.
	Version string

	// Metadata about the edge environment.
	Metadata map[string]string

	// Metrics from the edge.
	Metrics *pb.EdgeMetrics

	// stream is the gRPC stream for sending messages to the edge.
	stream pb.EdgeService_ConnectServer

	// activeTools tracks currently executing tools.
	activeTools map[string]*PendingTool

	// cancel cancels the connection context.
	cancel context.CancelFunc
}

// EdgeTool represents a tool provided by an edge.
type EdgeTool struct {
	// Name is the tool name (without edge: prefix).
	Name string

	// Description for the LLM.
	Description string

	// InputSchema is the JSON Schema for parameters.
	InputSchema string

	// RequiresApproval before execution.
	RequiresApproval bool

	// TimeoutSeconds for execution (0 = default).
	TimeoutSeconds int

	// ProducesArtifacts like screenshots or files.
	ProducesArtifacts bool

	// EdgeID is the edge providing this tool.
	EdgeID string
}

// PendingTool tracks an in-flight tool execution.
type PendingTool struct {
	// ExecutionID is the unique execution identifier.
	ExecutionID string

	// RunID for tracing.
	RunID string

	// SessionID for context.
	SessionID string

	// ToolName being executed.
	ToolName string

	// EdgeID executing the tool.
	EdgeID string

	// StartedAt is when execution started.
	StartedAt time.Time

	// Timeout for this execution.
	Timeout time.Duration

	// Result channel for the execution result.
	Result chan *ToolExecutionResult

	// Cancelled indicates if the execution was cancelled.
	Cancelled bool
}

// ToolExecutionResult is the result of a tool execution.
type ToolExecutionResult struct {
	// Content is the tool output.
	Content string

	// IsError indicates an error result.
	IsError bool

	// DurationMs is the execution duration.
	DurationMs int64

	// Artifacts produced by the tool.
	Artifacts []*pb.Artifact

	// ErrorDetails if IsError is true.
	ErrorDetails string
}

// EdgeEvent is an event from an edge for external consumption.
type EdgeEvent struct {
	// EdgeID is the source edge.
	EdgeID string

	// Type of event.
	Type pb.EdgeEventType

	// Timestamp of the event.
	Timestamp time.Time

	// Data is event-specific payload.
	Data map[string]interface{}
}

// Authenticator validates edge connections.
type Authenticator interface {
	// Authenticate validates an edge registration request.
	// Returns the approved edge ID (may differ from requested) or error.
	Authenticate(ctx context.Context, reg *pb.EdgeRegister) (string, error)
}

// Metrics tracks edge manager metrics.
type Metrics struct {
	ConnectedEdges    int64
	TotalToolCalls    int64
	FailedToolCalls   int64
	ActiveToolCalls   int64
	TotalConnections  int64
	FailedConnections int64
}

// NewManager creates a new edge manager.
func NewManager(config ManagerConfig, auth Authenticator, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		edges:        make(map[string]*EdgeConnection),
		pendingTools: make(map[string]*PendingTool),
		config:       config,
		auth:         auth,
		events:       make(chan EdgeEvent, config.EventBufferSize),
		logger:       logger.With("component", "edge.manager"),
		metrics:      &Metrics{},
	}
}

// HandleConnect handles a new edge connection stream.
func (m *Manager) HandleConnect(stream pb.EdgeService_ConnectServer) error {
	ctx := stream.Context()

	// Wait for registration message
	msg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("failed to receive registration: %w", err)
	}

	reg := msg.GetRegister()
	if reg == nil {
		return errors.New("first message must be registration")
	}

	// Authenticate the edge
	edgeID, err := m.auth.Authenticate(ctx, reg)
	if err != nil {
		// Send failure response
		_ = stream.Send(&pb.CoreMessage{
			Message: &pb.CoreMessage_Registered{
				Registered: &pb.EdgeRegistered{
					Success: false,
					Error:   err.Error(),
				},
			},
		})
		m.metrics.FailedConnections++
		return fmt.Errorf("authentication failed: %w", err)
	}

	// Create connection with cancellable context
	connCtx, cancel := context.WithCancel(ctx)

	conn := &EdgeConnection{
		ID:            edgeID,
		Name:          reg.Name,
		Status:        pb.EdgeConnectionStatus_EDGE_CONNECTION_STATUS_CONNECTED,
		ConnectedAt:   time.Now(),
		LastHeartbeat: time.Now(),
		Tools:         make(map[string]*EdgeTool),
		ChannelTypes:  reg.ChannelTypes,
		Capabilities:  reg.Capabilities,
		Version:       reg.Version,
		Metadata:      reg.Metadata,
		stream:        stream,
		activeTools:   make(map[string]*PendingTool),
		cancel:        cancel,
	}

	// Register tools
	for _, td := range reg.Tools {
		conn.Tools[td.Name] = &EdgeTool{
			Name:              td.Name,
			Description:       td.Description,
			InputSchema:       td.InputSchema,
			RequiresApproval:  td.RequiresApproval,
			TimeoutSeconds:    int(td.TimeoutSeconds),
			ProducesArtifacts: td.ProducesArtifacts,
			EdgeID:            edgeID,
		}
	}

	// Register connection
	m.mu.Lock()
	// Check for existing connection with same ID
	if existing, ok := m.edges[edgeID]; ok {
		existing.cancel() // Cancel old connection
	}
	m.edges[edgeID] = conn
	m.metrics.ConnectedEdges++
	m.metrics.TotalConnections++
	m.mu.Unlock()

	// Send success response
	if err := stream.Send(&pb.CoreMessage{
		Message: &pb.CoreMessage_Registered{
			Registered: &pb.EdgeRegistered{
				Success:                  true,
				EdgeId:                   edgeID,
				HeartbeatIntervalSeconds: int32(m.config.HeartbeatInterval.Seconds()),
				CoreVersion:              "1.0.0",
			},
		},
	}); err != nil {
		m.removeEdge(edgeID)
		return fmt.Errorf("failed to send registration response: %w", err)
	}

	m.logger.Info("edge connected",
		"edge_id", edgeID,
		"name", reg.Name,
		"tools", len(reg.Tools),
		"version", reg.Version,
	)

	// Handle incoming messages
	return m.handleMessages(connCtx, conn)
}

// handleMessages processes incoming messages from an edge.
func (m *Manager) handleMessages(ctx context.Context, conn *EdgeConnection) error {
	defer m.removeEdge(conn.ID)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		msg, err := conn.stream.Recv()
		if err != nil {
			m.logger.Warn("edge stream error",
				"edge_id", conn.ID,
				"error", err,
			)
			return err
		}

		switch payload := msg.Message.(type) {
		case *pb.EdgeMessage_Heartbeat:
			m.handleHeartbeat(conn, payload.Heartbeat)

		case *pb.EdgeMessage_ToolResult:
			m.handleToolResult(conn, payload.ToolResult)

		case *pb.EdgeMessage_Event:
			m.handleEdgeEvent(conn, payload.Event)
		}
	}
}

// handleHeartbeat processes a heartbeat from an edge.
func (m *Manager) handleHeartbeat(conn *EdgeConnection, hb *pb.EdgeHeartbeat) {
	conn.mu.Lock()
	conn.LastHeartbeat = time.Now()
	conn.Metrics = hb.Metrics
	conn.mu.Unlock()
}

// handleToolResult processes a tool execution result.
func (m *Manager) handleToolResult(conn *EdgeConnection, result *pb.ToolExecutionResult) {
	m.mu.Lock()
	pending, ok := m.pendingTools[result.ExecutionId]
	if ok {
		delete(m.pendingTools, result.ExecutionId)
	}
	m.mu.Unlock()

	if !ok {
		m.logger.Warn("received result for unknown execution",
			"execution_id", result.ExecutionId,
			"edge_id", conn.ID,
		)
		return
	}

	// Remove from edge's active tools
	conn.mu.Lock()
	delete(conn.activeTools, result.ExecutionId)
	conn.mu.Unlock()

	// Send result to waiting caller
	select {
	case pending.Result <- &ToolExecutionResult{
		Content:      result.Content,
		IsError:      result.IsError,
		DurationMs:   result.DurationMs,
		Artifacts:    result.Artifacts,
		ErrorDetails: result.ErrorDetails,
	}:
	default:
		m.logger.Warn("result channel full or closed",
			"execution_id", result.ExecutionId,
		)
	}

	m.logger.Debug("tool execution completed",
		"execution_id", result.ExecutionId,
		"tool", pending.ToolName,
		"edge_id", conn.ID,
		"duration_ms", result.DurationMs,
		"is_error", result.IsError,
	)
}

// handleEdgeEvent processes an event from an edge.
func (m *Manager) handleEdgeEvent(conn *EdgeConnection, event *pb.EdgeEvent) {
	// Forward to event channel
	select {
	case m.events <- EdgeEvent{
		EdgeID:    conn.ID,
		Type:      event.Type,
		Timestamp: event.Timestamp.AsTime(),
		Data:      nil, // TODO: convert struct to map
	}:
	default:
		m.logger.Warn("event channel full, dropping event",
			"edge_id", conn.ID,
			"event_type", event.Type,
		)
	}
}

// removeEdge removes an edge from the registry.
func (m *Manager) removeEdge(edgeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	conn, ok := m.edges[edgeID]
	if !ok {
		return
	}

	// Cancel any pending tools
	for execID, pending := range conn.activeTools {
		delete(m.pendingTools, execID)
		close(pending.Result)
	}

	delete(m.edges, edgeID)
	m.metrics.ConnectedEdges--

	m.logger.Info("edge disconnected", "edge_id", edgeID)
}

// ExecuteTool sends a tool execution request to an edge.
func (m *Manager) ExecuteTool(ctx context.Context, edgeID, toolName, input string, opts ExecuteOptions) (*ToolExecutionResult, error) {
	m.mu.RLock()
	conn, ok := m.edges[edgeID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("edge not connected: %s", edgeID)
	}

	// Check if tool exists
	conn.mu.RLock()
	tool, ok := conn.Tools[toolName]
	conn.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("tool not found on edge %s: %s", edgeID, toolName)
	}

	// Determine timeout
	timeout := m.config.DefaultToolTimeout
	if tool.TimeoutSeconds > 0 {
		timeout = time.Duration(tool.TimeoutSeconds) * time.Second
	}
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}

	// Create execution ID
	execID := uuid.New().String()

	// Create pending tool tracker
	pending := &PendingTool{
		ExecutionID: execID,
		RunID:       opts.RunID,
		SessionID:   opts.SessionID,
		ToolName:    toolName,
		EdgeID:      edgeID,
		StartedAt:   time.Now(),
		Timeout:     timeout,
		Result:      make(chan *ToolExecutionResult, 1),
	}

	// Register pending tool
	m.mu.Lock()
	m.pendingTools[execID] = pending
	m.metrics.TotalToolCalls++
	m.metrics.ActiveToolCalls++
	m.mu.Unlock()

	conn.mu.Lock()
	conn.activeTools[execID] = pending
	conn.mu.Unlock()

	// Send execution request
	if err := conn.stream.Send(&pb.CoreMessage{
		Message: &pb.CoreMessage_ToolRequest{
			ToolRequest: &pb.ToolExecutionRequest{
				ExecutionId:    execID,
				RunId:          opts.RunID,
				SessionId:      opts.SessionID,
				ToolName:       toolName,
				Input:          input,
				TimeoutSeconds: int32(timeout.Seconds()),
				Approved:       opts.Approved,
				Metadata:       opts.Metadata,
			},
		},
	}); err != nil {
		m.cleanupPending(execID)
		return nil, fmt.Errorf("failed to send tool request: %w", err)
	}

	m.logger.Debug("tool execution started",
		"execution_id", execID,
		"tool", toolName,
		"edge_id", edgeID,
		"timeout", timeout,
	)

	// Wait for result with timeout
	select {
	case result := <-pending.Result:
		m.mu.Lock()
		m.metrics.ActiveToolCalls--
		if result != nil && result.IsError {
			m.metrics.FailedToolCalls++
		}
		m.mu.Unlock()
		if result == nil {
			return nil, fmt.Errorf("tool execution failed: nil result")
		}
		return result, nil

	case <-time.After(timeout):
		m.cleanupPending(execID)
		// Send cancellation
		_ = conn.stream.Send(&pb.CoreMessage{
			Message: &pb.CoreMessage_ToolCancel{
				ToolCancel: &pb.ToolCancellation{
					ExecutionId: execID,
					Reason:      "timeout",
				},
			},
		})
		return nil, fmt.Errorf("tool execution timed out after %v", timeout)

	case <-ctx.Done():
		m.cleanupPending(execID)
		// Send cancellation
		_ = conn.stream.Send(&pb.CoreMessage{
			Message: &pb.CoreMessage_ToolCancel{
				ToolCancel: &pb.ToolCancellation{
					ExecutionId: execID,
					Reason:      "context cancelled",
				},
			},
		})
		return nil, ctx.Err()
	}
}

// ExecuteOptions configures a tool execution.
type ExecuteOptions struct {
	RunID     string
	SessionID string
	Timeout   time.Duration
	Approved  bool
	Metadata  map[string]string
}

// cleanupPending removes a pending tool from tracking.
func (m *Manager) cleanupPending(execID string) {
	m.mu.Lock()
	if pending, ok := m.pendingTools[execID]; ok {
		delete(m.pendingTools, execID)
		m.metrics.ActiveToolCalls--
		m.metrics.FailedToolCalls++

		// Also remove from edge
		if conn, ok := m.edges[pending.EdgeID]; ok {
			conn.mu.Lock()
			delete(conn.activeTools, execID)
			conn.mu.Unlock()
		}
	}
	m.mu.Unlock()
}

// CancelTool cancels a running tool execution.
func (m *Manager) CancelTool(execID, reason string) error {
	m.mu.RLock()
	pending, ok := m.pendingTools[execID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("execution not found: %s", execID)
	}

	m.mu.RLock()
	conn, ok := m.edges[pending.EdgeID]
	m.mu.RUnlock()

	if !ok {
		return fmt.Errorf("edge not connected: %s", pending.EdgeID)
	}

	pending.Cancelled = true

	// Send cancellation result to unblock waiting goroutine
	select {
	case pending.Result <- &ToolExecutionResult{
		Content: fmt.Sprintf("cancelled: %s", reason),
		IsError: true,
	}:
	default:
		// Result already received or channel closed
	}

	// Clean up pending
	m.cleanupPending(execID)

	// Notify edge of cancellation
	return conn.stream.Send(&pb.CoreMessage{
		Message: &pb.CoreMessage_ToolCancel{
			ToolCancel: &pb.ToolCancellation{
				ExecutionId: execID,
				Reason:      reason,
			},
		},
	})
}

// GetEdge returns the status of an edge.
func (m *Manager) GetEdge(edgeID string) (*pb.EdgeStatusInfo, bool) {
	m.mu.RLock()
	conn, ok := m.edges[edgeID]
	m.mu.RUnlock()

	if !ok {
		return nil, false
	}

	return m.edgeToStatus(conn), true
}

// ListEdges returns all connected edges.
func (m *Manager) ListEdges() []*pb.EdgeStatusInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*pb.EdgeStatusInfo, 0, len(m.edges))
	for _, conn := range m.edges {
		result = append(result, m.edgeToStatus(conn))
	}
	return result
}

// GetTools returns all tools from all connected edges.
func (m *Manager) GetTools() []*EdgeTool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var tools []*EdgeTool
	for _, conn := range m.edges {
		conn.mu.RLock()
		for _, tool := range conn.Tools {
			tools = append(tools, tool)
		}
		conn.mu.RUnlock()
	}
	return tools
}

// GetTool returns a specific tool from a specific edge.
func (m *Manager) GetTool(edgeID, toolName string) (*EdgeTool, bool) {
	m.mu.RLock()
	conn, ok := m.edges[edgeID]
	m.mu.RUnlock()

	if !ok {
		return nil, false
	}

	conn.mu.RLock()
	defer conn.mu.RUnlock()
	tool, ok := conn.Tools[toolName]
	return tool, ok
}

// Events returns the event channel for consuming edge events.
func (m *Manager) Events() <-chan EdgeEvent {
	return m.events
}

// Metrics returns current metrics.
func (m *Manager) Metrics() Metrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return *m.metrics
}

// edgeToStatus converts an EdgeConnection to a pb.EdgeStatusInfo.
func (m *Manager) edgeToStatus(conn *EdgeConnection) *pb.EdgeStatusInfo {
	conn.mu.RLock()
	defer conn.mu.RUnlock()

	tools := make([]string, 0, len(conn.Tools))
	for name := range conn.Tools {
		tools = append(tools, name)
	}

	return &pb.EdgeStatusInfo{
		EdgeId:           conn.ID,
		Name:             conn.Name,
		ConnectionStatus: conn.Status,
		ConnectedAt:      timestamppb.New(conn.ConnectedAt),
		LastHeartbeat:    timestamppb.New(conn.LastHeartbeat),
		Tools:            tools,
		ChannelTypes:     conn.ChannelTypes,
		Metrics:          conn.Metrics,
		Version:          conn.Version,
		Metadata:         conn.Metadata,
	}
}

// Close shuts down the manager.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Cancel all connections
	for _, conn := range m.edges {
		conn.cancel()
	}

	close(m.events)
	return nil
}
