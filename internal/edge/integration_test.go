package edge

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/haasonsaas/nexus/pkg/proto"
)

// TestEdgeIntegration tests the full edge protocol flow:
// 1. Start an edge service
// 2. Connect an edge daemon
// 3. Register tools
// 4. Execute a tool
// 5. Verify the result
func TestEdgeIntegration(t *testing.T) {
	// Create manager with dev authenticator (accepts all)
	config := DefaultManagerConfig()
	auth := NewDevAuthenticator()
	manager := NewManager(config, auth, nil)
	defer manager.Close()

	service := NewService(manager)

	// Create gRPC server
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	grpcServer := grpc.NewServer()
	pb.RegisterEdgeServiceServer(grpcServer, service)

	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			// Expected when we stop the server
		}
	}()
	defer grpcServer.Stop()

	// Create client connection
	conn, err := grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewEdgeServiceClient(conn)

	// Start stream
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}

	// Send registration
	err = stream.Send(&pb.EdgeMessage{
		Message: &pb.EdgeMessage_Register{
			Register: &pb.EdgeRegister{
				EdgeId:    "test-edge",
				Name:      "Test Edge",
				AuthToken: "test-token",
				Tools: []*pb.EdgeToolDefinition{
					{
						Name:        "echo",
						Description: "Echo the input back",
						InputSchema: `{"type":"object","properties":{"message":{"type":"string"}},"required":["message"]}`,
					},
				},
				Capabilities: &pb.BasicEdgeCapabilities{
					Tools:    true,
					Channels: false,
				},
				Version: "test",
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to send register: %v", err)
	}

	// Receive registration response
	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("failed to receive: %v", err)
	}

	registered := msg.GetRegistered()
	if registered == nil {
		t.Fatalf("expected registered message, got %T", msg.Message)
	}
	if !registered.Success {
		t.Fatalf("registration failed: %s", registered.Error)
	}
	if registered.EdgeId != "test-edge" {
		t.Errorf("expected edge_id test-edge, got %s", registered.EdgeId)
	}

	// Verify edge is connected
	edges := manager.ListEdges()
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}
	if edges[0].EdgeId != "test-edge" {
		t.Errorf("expected edge_id test-edge, got %s", edges[0].EdgeId)
	}
	if edges[0].ConnectionStatus != pb.EdgeConnectionStatus_EDGE_CONNECTION_STATUS_CONNECTED {
		t.Errorf("expected status connected, got %s", edges[0].ConnectionStatus)
	}

	// Verify tools are registered
	tools := manager.GetTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "echo" {
		t.Errorf("expected tool name echo, got %s", tools[0].Name)
	}

	// Execute tool in background goroutine and handle tool request
	toolResultCh := make(chan *ToolExecutionResult, 1)
	toolErrCh := make(chan error, 1)

	go func() {
		result, err := manager.ExecuteTool(ctx, "test-edge", "echo", `{"message":"hello"}`, ExecuteOptions{
			Timeout: 5 * time.Second,
		})
		if err != nil {
			toolErrCh <- err
			return
		}
		toolResultCh <- result
	}()

	// Handle tool request on the edge side
	msg, err = stream.Recv()
	if err != nil {
		t.Fatalf("failed to receive tool request: %v", err)
	}

	toolReq := msg.GetToolRequest()
	if toolReq == nil {
		t.Fatalf("expected tool request, got %T", msg.Message)
	}
	if toolReq.ToolName != "echo" {
		t.Errorf("expected tool name echo, got %s", toolReq.ToolName)
	}

	// Send tool result
	err = stream.Send(&pb.EdgeMessage{
		Message: &pb.EdgeMessage_ToolResult{
			ToolResult: &pb.ToolExecutionResult{
				ExecutionId: toolReq.ExecutionId,
				Content:     "Echo: hello",
				IsError:     false,
				DurationMs:  10,
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to send tool result: %v", err)
	}

	// Wait for tool execution to complete
	select {
	case result := <-toolResultCh:
		if result.IsError {
			t.Errorf("expected success, got error: %s", result.Content)
		}
		if result.Content != "Echo: hello" {
			t.Errorf("expected content 'Echo: hello', got %s", result.Content)
		}
	case err := <-toolErrCh:
		t.Fatalf("tool execution failed: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for tool result")
	}

	// Test ListEdges RPC
	listResp, err := client.ListEdges(ctx, &pb.ListEdgesRequest{})
	if err != nil {
		t.Fatalf("ListEdges failed: %v", err)
	}
	if len(listResp.Edges) != 1 {
		t.Fatalf("expected 1 edge in list, got %d", len(listResp.Edges))
	}

	// Test GetEdgeStatus RPC
	statusResp, err := client.GetEdgeStatus(ctx, &pb.GetEdgeStatusRequest{EdgeId: "test-edge"})
	if err != nil {
		t.Fatalf("GetEdgeStatus failed: %v", err)
	}
	if statusResp.Status == nil {
		t.Fatal("expected status in response")
	}
	if statusResp.Status.ConnectionStatus != pb.EdgeConnectionStatus_EDGE_CONNECTION_STATUS_CONNECTED {
		t.Error("expected edge to be connected")
	}
	if len(statusResp.Status.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(statusResp.Status.Tools))
	}
}

// TestEdgeHeartbeat tests the heartbeat mechanism.
func TestEdgeHeartbeat(t *testing.T) {
	config := ManagerConfig{
		HeartbeatInterval:  100 * time.Millisecond,
		HeartbeatTimeout:   300 * time.Millisecond,
		DefaultToolTimeout: 5 * time.Second,
		MaxConcurrentTools: 5,
		EventBufferSize:    100,
	}
	auth := NewDevAuthenticator()
	manager := NewManager(config, auth, nil)
	defer manager.Close()

	service := NewService(manager)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	grpcServer := grpc.NewServer()
	pb.RegisterEdgeServiceServer(grpcServer, service)

	go func() {
		_ = grpcServer.Serve(listener)
	}()
	defer grpcServer.Stop()

	conn, err := grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewEdgeServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}

	// Register
	err = stream.Send(&pb.EdgeMessage{
		Message: &pb.EdgeMessage_Register{
			Register: &pb.EdgeRegister{
				EdgeId:    "heartbeat-edge",
				AuthToken: "token",
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("failed to receive: %v", err)
	}
	if msg.GetRegistered() == nil || !msg.GetRegistered().Success {
		t.Fatal("registration failed")
	}

	// Send heartbeats
	for i := 0; i < 3; i++ {
		err = stream.Send(&pb.EdgeMessage{
			Message: &pb.EdgeMessage_Heartbeat{
				Heartbeat: &pb.EdgeHeartbeat{
					EdgeId:    "heartbeat-edge",
					Timestamp: timestamppb.Now(),
				},
			},
		})
		if err != nil {
			t.Fatalf("failed to send heartbeat: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Verify edge is still connected
	status, ok := manager.GetEdge("heartbeat-edge")
	if !ok {
		t.Fatal("edge not found")
	}
	if status.ConnectionStatus != pb.EdgeConnectionStatus_EDGE_CONNECTION_STATUS_CONNECTED {
		t.Errorf("expected connected, got %s", status.ConnectionStatus)
	}
}

// TestEdgeDisconnect tests edge disconnection handling.
func TestEdgeDisconnect(t *testing.T) {
	config := DefaultManagerConfig()
	auth := NewDevAuthenticator()
	manager := NewManager(config, auth, nil)
	defer manager.Close()

	service := NewService(manager)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	grpcServer := grpc.NewServer()
	pb.RegisterEdgeServiceServer(grpcServer, service)

	go func() {
		_ = grpcServer.Serve(listener)
	}()
	defer grpcServer.Stop()

	conn, err := grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	client := pb.NewEdgeServiceClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}

	// Register
	err = stream.Send(&pb.EdgeMessage{
		Message: &pb.EdgeMessage_Register{
			Register: &pb.EdgeRegister{
				EdgeId:    "disconnect-edge",
				AuthToken: "token",
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	_, err = stream.Recv()
	if err != nil {
		t.Fatalf("failed to receive: %v", err)
	}

	// Verify connected
	if len(manager.ListEdges()) != 1 {
		t.Fatal("expected 1 edge")
	}

	// Close stream (simulates disconnect)
	err = stream.CloseSend()
	if err != nil {
		t.Fatalf("failed to close stream: %v", err)
	}
	conn.Close()

	// Wait for disconnect to be processed
	time.Sleep(100 * time.Millisecond)

	// Verify disconnected
	edges := manager.ListEdges()
	if len(edges) != 0 {
		t.Errorf("expected 0 edges after disconnect, got %d", len(edges))
	}
}

// TestToolCancellation tests tool execution cancellation.
func TestToolCancellation(t *testing.T) {
	config := DefaultManagerConfig()
	auth := NewDevAuthenticator()
	manager := NewManager(config, auth, nil)
	defer manager.Close()

	service := NewService(manager)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	grpcServer := grpc.NewServer()
	pb.RegisterEdgeServiceServer(grpcServer, service)

	go func() {
		_ = grpcServer.Serve(listener)
	}()
	defer grpcServer.Stop()

	conn, err := grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewEdgeServiceClient(conn)

	ctx := context.Background()
	stream, err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("failed to open stream: %v", err)
	}

	// Register with a slow tool
	err = stream.Send(&pb.EdgeMessage{
		Message: &pb.EdgeMessage_Register{
			Register: &pb.EdgeRegister{
				EdgeId:    "cancel-edge",
				AuthToken: "token",
				Tools: []*pb.EdgeToolDefinition{
					{
						Name:           "slow_task",
						Description:    "A slow task",
						TimeoutSeconds: 30,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("failed to register: %v", err)
	}

	_, err = stream.Recv()
	if err != nil {
		t.Fatalf("failed to receive: %v", err)
	}

	// Start tool execution
	execDone := make(chan error, 1)
	go func() {
		_, err := manager.ExecuteTool(ctx, "cancel-edge", "slow_task", "{}", ExecuteOptions{
			Timeout: 10 * time.Second,
		})
		execDone <- err
	}()

	// Give the goroutine time to register the pending execution
	time.Sleep(50 * time.Millisecond)

	// Receive tool request
	msg, err := stream.Recv()
	if err != nil {
		t.Fatalf("failed to receive tool request: %v", err)
	}

	toolReq := msg.GetToolRequest()
	if toolReq == nil {
		t.Fatalf("expected tool request, got %T", msg.Message)
	}
	// Cancel the tool (after receiving the request, so it's registered)
	err = manager.CancelTool(toolReq.ExecutionId, "test cancellation")
	if err != nil {
		t.Fatalf("failed to cancel tool: %v", err)
	}

	// The execution should complete (with error or cancellation)
	select {
	case <-execDone:
		// Success - execution completed (cancelled)
	case <-time.After(2 * time.Second):
		t.Error("timeout waiting for execution to complete")
	}
}

// TestMultipleEdges tests multiple edges connecting simultaneously.
func TestMultipleEdges(t *testing.T) {
	config := DefaultManagerConfig()
	auth := NewDevAuthenticator()
	manager := NewManager(config, auth, nil)
	defer manager.Close()

	service := NewService(manager)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	grpcServer := grpc.NewServer()
	pb.RegisterEdgeServiceServer(grpcServer, service)

	go func() {
		_ = grpcServer.Serve(listener)
	}()
	defer grpcServer.Stop()

	// Connect multiple edges
	numEdges := 3
	streams := make([]pb.EdgeService_ConnectClient, numEdges)

	for i := 0; i < numEdges; i++ {
		conn, err := grpc.NewClient(
			listener.Addr().String(),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			t.Fatalf("failed to connect edge %d: %v", i, err)
		}
		defer conn.Close()

		client := pb.NewEdgeServiceClient(conn)
		stream, err := client.Connect(context.Background())
		if err != nil {
			t.Fatalf("failed to open stream for edge %d: %v", i, err)
		}
		streams[i] = stream

		// Register
		err = stream.Send(&pb.EdgeMessage{
			Message: &pb.EdgeMessage_Register{
				Register: &pb.EdgeRegister{
					EdgeId:    fmt.Sprintf("edge-%d", i),
					AuthToken: "token",
					Tools: []*pb.EdgeToolDefinition{
						{
							Name:        fmt.Sprintf("tool_%d", i),
							Description: fmt.Sprintf("Tool for edge %d", i),
						},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("failed to register edge %d: %v", i, err)
		}

		// Receive response
		msg, err := stream.Recv()
		if err != nil {
			t.Fatalf("failed to receive for edge %d: %v", i, err)
		}
		if !msg.GetRegistered().Success {
			t.Fatalf("registration failed for edge %d", i)
		}
	}

	// Verify all edges are connected
	edges := manager.ListEdges()
	if len(edges) != numEdges {
		t.Errorf("expected %d edges, got %d", numEdges, len(edges))
	}

	// Verify all tools are registered
	tools := manager.GetTools()
	if len(tools) != numEdges {
		t.Errorf("expected %d tools, got %d", numEdges, len(tools))
	}

	// Verify metrics
	metrics := manager.Metrics()
	if int(metrics.ConnectedEdges) != numEdges {
		t.Errorf("expected %d connected edges in metrics, got %d", numEdges, metrics.ConnectedEdges)
	}
}
