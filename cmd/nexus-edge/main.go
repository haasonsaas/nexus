// Package main provides the nexus-edge daemon that connects to a Nexus core
// and executes tools locally on the user's machine.
//
// The edge daemon enables local/privileged capabilities:
//   - Device access (camera, screen, location)
//   - Browser relay (attached Chrome sessions)
//   - Edge-only channels (iMessage, local Signal bridges)
//   - Local filesystem and command execution
//
// Usage:
//
//	nexus-edge --core-url localhost:9090 --edge-id macbook --token secret
//
// Configuration can also be provided via config file or environment variables.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/haasonsaas/nexus/pkg/proto"
)

// Version is set at build time.
var Version = "dev"

// Config holds edge daemon configuration.
type Config struct {
	// CoreURL is the address of the Nexus core.
	CoreURL string `json:"core_url"`

	// EdgeID is the unique identifier for this edge.
	EdgeID string `json:"edge_id"`

	// Name is the human-readable name for this edge.
	Name string `json:"name"`

	// AuthToken is the authentication token.
	AuthToken string `json:"auth_token"`

	// ReconnectDelay is the delay between reconnection attempts.
	ReconnectDelay time.Duration `json:"reconnect_delay"`

	// HeartbeatInterval is how often to send heartbeats.
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`

	// LogLevel is the logging level.
	LogLevel string `json:"log_level"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	hostname, _ := os.Hostname()
	return Config{
		CoreURL:           "localhost:9090",
		EdgeID:            hostname,
		Name:              hostname,
		ReconnectDelay:    5 * time.Second,
		HeartbeatInterval: 30 * time.Second,
		LogLevel:          "info",
	}
}

// EdgeDaemon manages the connection to Nexus core.
type EdgeDaemon struct {
	config Config
	logger *slog.Logger
	tools  []*Tool

	// Runtime state
	conn        *grpc.ClientConn
	client      pb.EdgeServiceClient
	stream      pb.EdgeService_ConnectClient
	startTime   time.Time
	activeCalls map[string]context.CancelFunc
}

// Tool represents a tool provided by this edge.
type Tool struct {
	Name              string
	Description       string
	InputSchema       string
	RequiresApproval  bool
	TimeoutSeconds    int
	ProducesArtifacts bool
	Handler           ToolHandler
}

// ToolHandler executes a tool.
type ToolHandler func(ctx context.Context, input string) (*ToolResult, error)

// ToolResult is the result of a tool execution.
type ToolResult struct {
	Content   string
	IsError   bool
	Artifacts []*pb.Artifact
}

// NewEdgeDaemon creates a new edge daemon.
func NewEdgeDaemon(config Config, logger *slog.Logger) *EdgeDaemon {
	return &EdgeDaemon{
		config:      config,
		logger:      logger.With("component", "edge-daemon"),
		tools:       make([]*Tool, 0),
		activeCalls: make(map[string]context.CancelFunc),
		startTime:   time.Now(),
	}
}

// RegisterTool adds a tool to this edge.
func (d *EdgeDaemon) RegisterTool(tool *Tool) {
	d.tools = append(d.tools, tool)
}

// Run starts the edge daemon and blocks until stopped.
func (d *EdgeDaemon) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := d.connect(ctx)
		if err != nil {
			d.logger.Error("connection failed", "error", err)
		}

		// Wait before reconnecting
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(d.config.ReconnectDelay):
			d.logger.Info("reconnecting...")
		}
	}
}

// connect establishes a connection to the core.
func (d *EdgeDaemon) connect(ctx context.Context) error {
	d.logger.Info("connecting to core", "url", d.config.CoreURL)

	// Create gRPC connection
	conn, err := grpc.NewClient(
		d.config.CoreURL,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer conn.Close()

	d.conn = conn
	d.client = pb.NewEdgeServiceClient(conn)

	// Open stream
	stream, err := d.client.Connect(ctx)
	if err != nil {
		return fmt.Errorf("failed to open stream: %w", err)
	}
	d.stream = stream

	// Send registration
	if err := d.register(); err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}

	// Wait for registration response
	msg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("failed to receive registration response: %w", err)
	}

	registered := msg.GetRegistered()
	if registered == nil {
		return fmt.Errorf("unexpected response: %T", msg.Message)
	}

	if !registered.Success {
		return fmt.Errorf("registration rejected: %s", registered.Error)
	}

	d.logger.Info("connected to core",
		"edge_id", registered.EdgeId,
		"heartbeat_interval", registered.HeartbeatIntervalSeconds,
	)

	// Update heartbeat interval if specified by core
	if registered.HeartbeatIntervalSeconds > 0 {
		d.config.HeartbeatInterval = time.Duration(registered.HeartbeatIntervalSeconds) * time.Second
	}

	// Start heartbeat goroutine
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	defer cancelHeartbeat()
	go d.heartbeatLoop(heartbeatCtx)

	// Handle incoming messages
	return d.handleMessages(ctx)
}

// register sends the registration message.
func (d *EdgeDaemon) register() error {
	toolDefs := make([]*pb.EdgeToolDefinition, len(d.tools))
	for i, t := range d.tools {
		toolDefs[i] = &pb.EdgeToolDefinition{
			Name:              t.Name,
			Description:       t.Description,
			InputSchema:       t.InputSchema,
			RequiresApproval:  t.RequiresApproval,
			TimeoutSeconds:    int32(t.TimeoutSeconds),
			ProducesArtifacts: t.ProducesArtifacts,
		}
	}

	return d.stream.Send(&pb.EdgeMessage{
		Message: &pb.EdgeMessage_Register{
			Register: &pb.EdgeRegister{
				EdgeId:       d.config.EdgeID,
				Name:         d.config.Name,
				AuthToken:    d.config.AuthToken,
				Tools:        toolDefs,
				ChannelTypes: []string{}, // TODO: add channel support
				Capabilities: &pb.BasicEdgeCapabilities{
					Tools:     true,
					Channels:  false,
					Streaming: true,
					Artifacts: true,
				},
				Version: Version,
				Metadata: map[string]string{
					"os":       runtime.GOOS,
					"arch":     runtime.GOARCH,
					"hostname": d.config.Name,
				},
			},
		},
	})
}

// heartbeatLoop sends periodic heartbeats.
func (d *EdgeDaemon) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(d.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := d.sendHeartbeat(); err != nil {
				d.logger.Warn("failed to send heartbeat", "error", err)
			}
		}
	}
}

// sendHeartbeat sends a heartbeat message.
func (d *EdgeDaemon) sendHeartbeat() error {
	activeTools := make([]string, 0, len(d.activeCalls))
	for name := range d.activeCalls {
		activeTools = append(activeTools, name)
	}

	return d.stream.Send(&pb.EdgeMessage{
		Message: &pb.EdgeMessage_Heartbeat{
			Heartbeat: &pb.EdgeHeartbeat{
				EdgeId:    d.config.EdgeID,
				Timestamp: timestamppb.Now(),
				Metrics: &pb.EdgeMetrics{
					ActiveToolCount: int32(len(d.activeCalls)),
					UptimeSeconds:   int64(time.Since(d.startTime).Seconds()),
				},
				ActiveTools: activeTools,
			},
		},
	})
}

// handleMessages processes incoming messages from the core.
func (d *EdgeDaemon) handleMessages(ctx context.Context) error {
	for {
		msg, err := d.stream.Recv()
		if err != nil {
			return fmt.Errorf("stream error: %w", err)
		}

		switch payload := msg.Message.(type) {
		case *pb.CoreMessage_ToolRequest:
			go d.handleToolRequest(ctx, payload.ToolRequest)

		case *pb.CoreMessage_ToolCancel:
			d.handleToolCancel(payload.ToolCancel)

		case *pb.CoreMessage_Event:
			d.handleCoreEvent(payload.Event)
		}
	}
}

// handleToolRequest executes a tool request.
func (d *EdgeDaemon) handleToolRequest(ctx context.Context, req *pb.ToolExecutionRequest) {
	startTime := time.Now()

	// Find the tool
	var tool *Tool
	for _, t := range d.tools {
		if t.Name == req.ToolName {
			tool = t
			break
		}
	}

	if tool == nil {
		d.sendToolResult(req.ExecutionId, &ToolResult{
			Content: fmt.Sprintf("tool not found: %s", req.ToolName),
			IsError: true,
		}, time.Since(startTime))
		return
	}

	// Create cancellable context
	toolCtx, cancel := context.WithCancel(ctx)
	if req.TimeoutSeconds > 0 {
		toolCtx, cancel = context.WithTimeout(ctx, time.Duration(req.TimeoutSeconds)*time.Second)
	}
	defer cancel()

	// Track active call
	d.activeCalls[req.ExecutionId] = cancel
	defer delete(d.activeCalls, req.ExecutionId)

	// Send started event
	_ = d.sendEvent(pb.EdgeEventType_EDGE_EVENT_TYPE_TOOL_STARTED, map[string]interface{}{
		"execution_id": req.ExecutionId,
		"tool_name":    req.ToolName,
	})

	d.logger.Info("executing tool",
		"execution_id", req.ExecutionId,
		"tool", req.ToolName,
	)

	// Execute the tool
	result, err := tool.Handler(toolCtx, req.Input)
	if err != nil {
		result = &ToolResult{
			Content: fmt.Sprintf("tool execution error: %v", err),
			IsError: true,
		}
	}

	// Send result
	d.sendToolResult(req.ExecutionId, result, time.Since(startTime))

	// Send completed event
	eventType := pb.EdgeEventType_EDGE_EVENT_TYPE_TOOL_COMPLETED
	if result.IsError {
		eventType = pb.EdgeEventType_EDGE_EVENT_TYPE_TOOL_FAILED
	}
	_ = d.sendEvent(eventType, map[string]interface{}{
		"execution_id": req.ExecutionId,
		"tool_name":    req.ToolName,
		"duration_ms":  time.Since(startTime).Milliseconds(),
	})
}

// sendToolResult sends the tool result back to the core.
func (d *EdgeDaemon) sendToolResult(execID string, result *ToolResult, duration time.Duration) {
	if err := d.stream.Send(&pb.EdgeMessage{
		Message: &pb.EdgeMessage_ToolResult{
			ToolResult: &pb.ToolExecutionResult{
				ExecutionId: execID,
				Content:     result.Content,
				IsError:     result.IsError,
				DurationMs:  duration.Milliseconds(),
				Artifacts:   result.Artifacts,
			},
		},
	}); err != nil {
		d.logger.Error("failed to send tool result", "error", err)
	}
}

// handleToolCancel cancels a running tool.
func (d *EdgeDaemon) handleToolCancel(cancel *pb.ToolCancellation) {
	if cancelFn, ok := d.activeCalls[cancel.ExecutionId]; ok {
		cancelFn()
		d.logger.Info("tool cancelled",
			"execution_id", cancel.ExecutionId,
			"reason", cancel.Reason,
		)

		_ = d.sendEvent(pb.EdgeEventType_EDGE_EVENT_TYPE_TOOL_CANCELLED, map[string]interface{}{
			"execution_id": cancel.ExecutionId,
			"reason":       cancel.Reason,
		})
	}
}

// handleCoreEvent processes an event from the core.
func (d *EdgeDaemon) handleCoreEvent(event *pb.CoreEvent) {
	switch event.Type {
	case pb.CoreEventType_CORE_EVENT_TYPE_SHUTDOWN:
		d.logger.Info("core requested shutdown")
		// The stream will close and we'll reconnect
	}
}

// sendEvent sends an event to the core.
func (d *EdgeDaemon) sendEvent(eventType pb.EdgeEventType, _ map[string]interface{}) error {
	return d.stream.Send(&pb.EdgeMessage{
		Message: &pb.EdgeMessage_Event{
			Event: &pb.EdgeEvent{
				EdgeId:    d.config.EdgeID,
				Type:      eventType,
				Timestamp: timestamppb.Now(),
				// TODO: convert data to Struct
			},
		},
	})
}

func main() {
	config := DefaultConfig()

	rootCmd := &cobra.Command{
		Use:   "nexus-edge",
		Short: "Nexus Edge Daemon - local tool execution for Nexus",
		Long: `The Nexus Edge Daemon connects to a Nexus core and provides
local capabilities like device access, browser relay, and edge-only channels.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Set up logging
			var level slog.Level
			switch config.LogLevel {
			case "debug":
				level = slog.LevelDebug
			case "warn":
				level = slog.LevelWarn
			case "error":
				level = slog.LevelError
			default:
				level = slog.LevelInfo
			}

			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
				Level: level,
			}))

			// Create daemon
			daemon := NewEdgeDaemon(config, logger)

			// Register example echo tool
			daemon.RegisterTool(&Tool{
				Name:        "echo",
				Description: "Echo the input back (for testing)",
				InputSchema: `{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`,
				Handler: func(ctx context.Context, input string) (*ToolResult, error) {
					var params struct {
						Message string `json:"message"`
					}
					if err := json.Unmarshal([]byte(input), &params); err != nil {
						return nil, err
					}
					return &ToolResult{
						Content: fmt.Sprintf("Echo: %s", params.Message),
					}, nil
				},
			})

			// Set up signal handling
			ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()

			logger.Info("starting nexus-edge",
				"version", Version,
				"edge_id", config.EdgeID,
				"core_url", config.CoreURL,
			)

			return daemon.Run(ctx)
		},
	}

	rootCmd.Flags().StringVar(&config.CoreURL, "core-url", config.CoreURL, "Nexus core URL")
	rootCmd.Flags().StringVar(&config.EdgeID, "edge-id", config.EdgeID, "Unique edge identifier")
	rootCmd.Flags().StringVar(&config.Name, "name", config.Name, "Human-readable edge name")
	rootCmd.Flags().StringVar(&config.AuthToken, "token", "", "Authentication token")
	rootCmd.Flags().DurationVar(&config.ReconnectDelay, "reconnect-delay", config.ReconnectDelay, "Delay between reconnection attempts")
	rootCmd.Flags().StringVar(&config.LogLevel, "log-level", config.LogLevel, "Log level (debug, info, warn, error)")

	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("nexus-edge %s\n", Version)
		},
	})

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
