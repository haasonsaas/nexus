// Package edge provides the edge daemon client for connecting to Nexus gateway.
package edge

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"

	edgepb "github.com/haasonsaas/nexus/pkg/proto/edge"
)

// Client connects to the Nexus gateway as an edge daemon.
type Client struct {
	config  ClientConfig
	logger  *slog.Logger
	conn    *grpc.ClientConn
	service edgepb.EdgeServiceClient
	stream  edgepb.EdgeService_ConnectClient

	mu           sync.RWMutex
	sessionToken string
	tools        map[string]*Tool
	handlers     map[string]ToolHandler
	connected    bool

	// Channels for coordination
	done     chan struct{}
	requests chan *edgepb.ToolExecutionRequest
}

// ClientConfig configures the edge client.
type ClientConfig struct {
	// GatewayAddr is the address of the Nexus gateway (e.g., "localhost:50051").
	GatewayAddr string

	// EdgeID is the unique identifier for this edge daemon.
	EdgeID string

	// EdgeName is a human-readable name for this edge daemon.
	EdgeName string

	// AuthMethod determines how to authenticate with the gateway.
	AuthMethod edgepb.AuthMethod

	// SharedSecret is the pre-shared key (for AuthMethodSharedSecret).
	SharedSecret string

	// PrivateKey is the ed25519 private key (for AuthMethodTOFU).
	PrivateKey ed25519.PrivateKey

	// HeartbeatInterval is how often to send heartbeats (default 30s).
	HeartbeatInterval time.Duration

	// ReconnectDelay is how long to wait before reconnecting (default 5s).
	ReconnectDelay time.Duration

	// MaxConcurrentExecutions limits parallel tool executions.
	MaxConcurrentExecutions int
}

// Tool defines a tool provided by this edge daemon.
type Tool struct {
	Name              string
	Description       string
	InputSchema       json.RawMessage
	Category          edgepb.ToolCategory
	RequiresApproval  bool
	RiskLevel         edgepb.RiskLevel
	SupportsStreaming bool
	Metadata          map[string]string
}

// ToolHandler handles execution of a tool.
type ToolHandler func(ctx context.Context, req *ToolExecutionRequest) (*ClientToolResult, error)

// ToolExecutionRequest represents a tool execution request.
type ToolExecutionRequest struct {
	RequestID string
	ToolName  string
	Input     json.RawMessage
	SessionID string
	UserID    string
	AgentID   string
	MessageID string
	Metadata  map[string]string
	Timeout   time.Duration
}

// ClientToolResult represents a tool execution result.
type ClientToolResult struct {
	Success      bool
	Output       interface{}
	ErrorMessage string
	DurationMS   int32
	Attachments  []*edgepb.ToolAttachment
}

// NewClient creates a new edge client.
func NewClient(config ClientConfig, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = 30 * time.Second
	}
	if config.ReconnectDelay == 0 {
		config.ReconnectDelay = 5 * time.Second
	}
	if config.MaxConcurrentExecutions == 0 {
		config.MaxConcurrentExecutions = 10
	}

	return &Client{
		config:   config,
		logger:   logger,
		tools:    make(map[string]*Tool),
		handlers: make(map[string]ToolHandler),
		requests: make(chan *edgepb.ToolExecutionRequest, config.MaxConcurrentExecutions),
		done:     make(chan struct{}),
	}
}

// RegisterTool registers a tool with a handler.
func (c *Client) RegisterTool(tool *Tool, handler ToolHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.tools[tool.Name] = tool
	c.handlers[tool.Name] = handler
}

// Connect establishes connection to the gateway and starts the event loop.
func (c *Client) Connect(ctx context.Context) error {
	// Connect to gateway
	conn, err := grpc.NewClient(
		c.config.GatewayAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return fmt.Errorf("dial gateway: %w", err)
	}
	c.conn = conn
	c.service = edgepb.NewEdgeServiceClient(conn)

	// Start bidirectional stream
	stream, err := c.service.Connect(ctx)
	if err != nil {
		c.conn.Close()
		return fmt.Errorf("connect stream: %w", err)
	}
	c.stream = stream

	// Authenticate
	if err := c.authenticate(ctx); err != nil {
		_ = c.stream.CloseSend() //nolint:errcheck // best-effort cleanup
		_ = c.conn.Close()       //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("authenticate: %w", err)
	}

	// Register tools
	if err := c.registerTools(ctx); err != nil {
		_ = c.stream.CloseSend() //nolint:errcheck // best-effort cleanup
		_ = c.conn.Close()       //nolint:errcheck // best-effort cleanup
		return fmt.Errorf("register tools: %w", err)
	}

	c.mu.Lock()
	c.connected = true
	c.mu.Unlock()

	// Start background goroutines
	go c.receiveLoop(ctx)
	go c.heartbeatLoop(ctx)
	go c.executionLoop(ctx)

	return nil
}

// Disconnect gracefully disconnects from the gateway.
func (c *Client) Disconnect() {
	close(c.done)

	c.mu.Lock()
	c.connected = false
	c.mu.Unlock()

	if c.stream != nil {
		_ = c.stream.CloseSend() //nolint:errcheck // best-effort cleanup
	}
	if c.conn != nil {
		_ = c.conn.Close() //nolint:errcheck // best-effort cleanup
	}
}

// IsConnected returns whether the client is connected.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

func (c *Client) authenticate(ctx context.Context) error {
	var publicKey []byte
	if c.config.AuthMethod == edgepb.AuthMethod_AUTH_METHOD_TOFU {
		publicKey = c.config.PrivateKey.Public().(ed25519.PublicKey) //nolint:errcheck // type is guaranteed by ed25519
	}

	authReq := &edgepb.AuthenticateRequest{
		EdgeId:          c.config.EdgeID,
		EdgeName:        c.config.EdgeName,
		AuthMethod:      c.config.AuthMethod,
		SharedSecret:    c.config.SharedSecret,
		PublicKey:       publicKey,
		ProtocolVersion: "1.0",
		Capabilities: &edgepb.EdgeCapabilities{
			MaxConcurrentExecutions: int32(c.config.MaxConcurrentExecutions),
			SupportsStreaming:       true,
		},
	}

	// Send auth request
	if err := c.stream.Send(&edgepb.EdgeMessage{
		Message: &edgepb.EdgeMessage_Authenticate{
			Authenticate: authReq,
		},
	}); err != nil {
		return fmt.Errorf("send auth: %w", err)
	}

	// Wait for response
	msg, err := c.stream.Recv()
	if err != nil {
		return fmt.Errorf("recv auth response: %w", err)
	}

	authResp := msg.GetAuthenticate()
	if authResp == nil {
		return errors.New("expected auth response")
	}

	// Handle TOFU challenge
	if !authResp.Success && len(authResp.Challenge) > 0 && c.config.AuthMethod == edgepb.AuthMethod_AUTH_METHOD_TOFU {
		c.logger.Info("TOFU challenge received, signing...")

		// Sign the challenge
		signature := ed25519.Sign(c.config.PrivateKey, authResp.Challenge)

		// Send signed challenge
		authReq.Signature = signature
		if err := c.stream.Send(&edgepb.EdgeMessage{
			Message: &edgepb.EdgeMessage_Authenticate{
				Authenticate: authReq,
			},
		}); err != nil {
			return fmt.Errorf("send signed challenge: %w", err)
		}

		// Wait for final response
		msg, err = c.stream.Recv()
		if err != nil {
			return fmt.Errorf("recv auth response: %w", err)
		}
		authResp = msg.GetAuthenticate()
		if authResp == nil {
			return errors.New("expected auth response")
		}
	}

	if !authResp.Success {
		return fmt.Errorf("auth failed: %s", authResp.ErrorMessage)
	}

	c.mu.Lock()
	c.sessionToken = authResp.SessionToken
	c.mu.Unlock()

	c.logger.Info("authenticated with gateway",
		"trust_level", authResp.TrustLevel.String(),
		"session_token", authResp.SessionToken[:min(8, len(authResp.SessionToken))]+"...",
	)

	return nil
}

func (c *Client) registerTools(ctx context.Context) error {
	c.mu.RLock()
	tools := make([]*edgepb.EdgeTool, 0, len(c.tools))
	for _, tool := range c.tools {
		tools = append(tools, &edgepb.EdgeTool{
			Name:              tool.Name,
			Description:       tool.Description,
			InputSchema:       string(tool.InputSchema),
			Category:          tool.Category,
			RequiresApproval:  tool.RequiresApproval,
			RiskLevel:         tool.RiskLevel,
			SupportsStreaming: tool.SupportsStreaming,
			Metadata:          tool.Metadata,
		})
	}
	c.mu.RUnlock()

	if len(tools) == 0 {
		return nil
	}

	// Send registration request
	if err := c.stream.Send(&edgepb.EdgeMessage{
		Message: &edgepb.EdgeMessage_RegisterTools{
			RegisterTools: &edgepb.RegisterToolsRequest{
				EdgeId:     c.config.EdgeID,
				Tools:      tools,
				ReplaceAll: true,
			},
		},
	}); err != nil {
		return fmt.Errorf("send registration: %w", err)
	}

	// Wait for ack
	msg, err := c.stream.Recv()
	if err != nil {
		return fmt.Errorf("recv registration ack: %w", err)
	}

	regResp := msg.GetRegisterAck()
	if regResp == nil {
		return errors.New("expected registration ack")
	}

	if !regResp.Success {
		for _, e := range regResp.Errors {
			c.logger.Warn("tool registration error",
				"tool", e.ToolName,
				"error", e.ErrorMessage,
			)
		}
	}

	c.logger.Info("tools registered",
		"count", regResp.RegisteredCount,
		"canonical_names", regResp.CanonicalNames,
	)

	return nil
}

func (c *Client) receiveLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		default:
		}

		msg, err := c.stream.Recv()
		if err != nil {
			if err == io.EOF || errors.Is(err, context.Canceled) {
				c.logger.Info("stream closed")
				return
			}
			c.logger.Error("receive error", "error", err)
			c.handleDisconnect()
			return
		}

		c.handleMessage(ctx, msg)
	}
}

func (c *Client) handleMessage(ctx context.Context, msg *edgepb.GatewayMessage) {
	switch m := msg.Message.(type) {
	case *edgepb.GatewayMessage_ToolRequest:
		c.logger.Debug("received tool request",
			"request_id", m.ToolRequest.RequestId,
			"tool", m.ToolRequest.ToolName,
		)
		select {
		case c.requests <- m.ToolRequest:
		default:
			c.logger.Warn("execution queue full, rejecting request",
				"request_id", m.ToolRequest.RequestId,
			)
			c.sendToolResult(ctx, m.ToolRequest.RequestId, &ClientToolResult{
				Success:      false,
				ErrorMessage: "edge daemon overloaded",
			})
		}

	case *edgepb.GatewayMessage_ToolCancel:
		c.logger.Info("tool cancellation requested",
			"request_id", m.ToolCancel.RequestId,
			"reason", m.ToolCancel.Reason,
		)
		// TODO: implement cancellation

	case *edgepb.GatewayMessage_Heartbeat:
		c.logger.Debug("heartbeat ack received")

	case *edgepb.GatewayMessage_StatusUpdate:
		if !m.StatusUpdate.AcceptingRequests {
			c.logger.Warn("gateway not accepting requests")
		}

	case *edgepb.GatewayMessage_Error:
		c.logger.Error("gateway error",
			"code", m.Error.Code,
			"message", m.Error.Message,
		)
	}
}

func (c *Client) executionLoop(ctx context.Context) {
	sem := make(chan struct{}, c.config.MaxConcurrentExecutions)

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case req := <-c.requests:
			sem <- struct{}{} // acquire
			go func(r *edgepb.ToolExecutionRequest) {
				defer func() { <-sem }() // release
				c.executeToolRequest(ctx, r)
			}(req)
		}
	}
}

func (c *Client) executeToolRequest(ctx context.Context, req *edgepb.ToolExecutionRequest) {
	start := time.Now()

	c.mu.RLock()
	handler, ok := c.handlers[req.ToolName]
	c.mu.RUnlock()

	if !ok {
		c.sendToolResult(ctx, req.RequestId, &ClientToolResult{
			Success:      false,
			ErrorMessage: fmt.Sprintf("tool not found: %s", req.ToolName),
		})
		return
	}

	// Set up timeout context
	execCtx := ctx
	if req.TimeoutMs > 0 {
		var cancel context.CancelFunc
		execCtx, cancel = context.WithTimeout(ctx, time.Duration(req.TimeoutMs)*time.Millisecond)
		defer cancel()
	}

	// Build request
	execReq := &ToolExecutionRequest{
		RequestID: req.RequestId,
		ToolName:  req.ToolName,
		Input:     json.RawMessage(req.Input),
		Timeout:   time.Duration(req.TimeoutMs) * time.Millisecond,
	}
	if req.Context != nil {
		execReq.SessionID = req.Context.SessionId
		execReq.UserID = req.Context.UserId
		execReq.AgentID = req.Context.AgentId
		execReq.MessageID = req.Context.MessageId
		execReq.Metadata = req.Context.Metadata
	}

	// Execute
	result, err := handler(execCtx, execReq)
	if err != nil {
		result = &ClientToolResult{
			Success:      false,
			ErrorMessage: err.Error(),
		}
	}

	result.DurationMS = int32(time.Since(start).Milliseconds())
	c.sendToolResult(ctx, req.RequestId, result)
}

func (c *Client) sendToolResult(ctx context.Context, requestID string, result *ClientToolResult) {
	var output string
	if result.Output != nil {
		data, err := json.Marshal(result.Output)
		if err != nil {
			output = fmt.Sprintf("%v", result.Output)
		} else {
			output = string(data)
		}
	}

	var errorCode edgepb.ToolErrorCode
	if !result.Success {
		errorCode = edgepb.ToolErrorCode_TOOL_ERROR_CODE_INTERNAL
	}

	if err := c.stream.Send(&edgepb.EdgeMessage{
		Message: &edgepb.EdgeMessage_ToolResult{
			ToolResult: &edgepb.ToolExecutionResult{
				RequestId:    requestID,
				Success:      result.Success,
				Output:       output,
				ErrorMessage: result.ErrorMessage,
				ErrorCode:    errorCode,
				DurationMs:   result.DurationMS,
				Attachments:  result.Attachments,
			},
		},
	}); err != nil {
		c.logger.Error("failed to send tool result",
			"request_id", requestID,
			"error", err,
		)
	}
}

func (c *Client) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(c.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-ticker.C:
			c.sendHeartbeat(ctx)
		}
	}
}

func (c *Client) sendHeartbeat(ctx context.Context) {
	if err := c.stream.Send(&edgepb.EdgeMessage{
		Message: &edgepb.EdgeMessage_Heartbeat{
			Heartbeat: &edgepb.HeartbeatRequest{
				EdgeId:    c.config.EdgeID,
				Timestamp: timestamppb.Now(),
				Status: &edgepb.EdgeStatusUpdate{
					Status:           edgepb.EdgeStatus_EDGE_STATUS_READY,
					ActiveExecutions: 0,
				},
			},
		},
	}); err != nil {
		c.logger.Error("heartbeat failed", "error", err)
	}
}

func (c *Client) handleDisconnect() {
	c.mu.Lock()
	c.connected = false
	c.mu.Unlock()

	c.logger.Warn("disconnected from gateway")
	// Reconnection would be handled by the main Run loop
}

// Run runs the edge daemon, handling reconnection.
func (c *Client) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			c.Disconnect()
			return ctx.Err()
		default:
		}

		c.logger.Info("connecting to gateway", "addr", c.config.GatewayAddr)

		if err := c.Connect(ctx); err != nil {
			c.logger.Error("connection failed", "error", err)
			time.Sleep(c.config.ReconnectDelay)
			continue
		}

		// Wait for disconnect
		select {
		case <-ctx.Done():
			c.Disconnect()
			return ctx.Err()
		case <-c.done:
			// Reconnect
			c.done = make(chan struct{})
			time.Sleep(c.config.ReconnectDelay)
		}
	}
}

// SetMetadata sets metadata for gRPC calls.
func (c *Client) SetMetadata(ctx context.Context) context.Context {
	c.mu.RLock()
	token := c.sessionToken
	c.mu.RUnlock()

	md := metadata.New(map[string]string{
		"x-edge-id":       c.config.EdgeID,
		"x-session-token": token,
	})
	return metadata.NewOutgoingContext(ctx, md)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
