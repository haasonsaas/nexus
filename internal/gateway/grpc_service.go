package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/auth"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/internal/storage"
	"github.com/haasonsaas/nexus/pkg/models"
	proto "github.com/haasonsaas/nexus/pkg/proto"
)

type grpcService struct {
	proto.UnimplementedNexusGatewayServer
	proto.UnimplementedSessionServiceServer
	proto.UnimplementedAgentServiceServer
	proto.UnimplementedChannelServiceServer
	proto.UnimplementedHealthServiceServer
	proto.UnimplementedArtifactServiceServer

	server       *Server
	agentStore   storage.AgentStore
	channelStore storage.ChannelConnectionStore
}

func newGRPCService(server *Server) *grpcService {
	var agentStore storage.AgentStore
	var channelStore storage.ChannelConnectionStore
	if server != nil && server.stores.Agents != nil {
		agentStore = server.stores.Agents
	} else {
		agentStore = storage.NewMemoryAgentStore()
	}
	if server != nil && server.stores.Channels != nil {
		channelStore = server.stores.Channels
	} else {
		channelStore = storage.NewMemoryChannelConnectionStore()
	}
	return &grpcService{
		server:       server,
		agentStore:   agentStore,
		channelStore: channelStore,
	}
}

func (g *grpcService) Stream(stream proto.NexusGateway_StreamServer) error {
	ctx := stream.Context()
	for {
		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		switch payload := msg.GetMessage().(type) {
		case *proto.ClientMessage_SendMessage:
			if payload == nil || payload.SendMessage == nil {
				continue
			}
			if err := g.handleSendMessage(ctx, stream, payload.SendMessage); err != nil {
				return err
			}
		case *proto.ClientMessage_Ping:
			if err := stream.Send(&proto.ServerMessage{Message: &proto.ServerMessage_Pong{
				Pong: &proto.PongResponse{Timestamp: timestamppb.Now()},
			}}); err != nil {
				return err
			}
		default:
			continue
		}
	}
}

func (g *grpcService) handleSendMessage(ctx context.Context, stream proto.NexusGateway_StreamServer, req *proto.SendMessageRequest) error {
	if req == nil {
		return nil
	}
	if g.server == nil {
		return status.Error(codes.Internal, "server not configured")
	}

	runtime, err := g.server.ensureRuntime(ctx)
	if err != nil {
		return status.Errorf(codes.FailedPrecondition, "runtime unavailable: %v", err)
	}

	session, err := g.resolveSession(ctx, req)
	if err != nil {
		return err
	}

	msg := &models.Message{
		SessionID:   session.ID,
		Channel:     session.Channel,
		ChannelID:   session.ChannelID,
		Direction:   models.DirectionInbound,
		Role:        models.RoleUser,
		Content:     req.Content,
		Attachments: attachmentsFromProto(req.Attachments),
		Metadata:    metadataFromProto(req.Metadata),
		CreatedAt:   time.Now(),
	}

	if err := g.server.sessions.AppendMessage(ctx, session.ID, msg); err != nil {
		return status.Errorf(codes.Internal, "failed to persist message: %v", err)
	}
	if g.server.memoryLogger != nil {
		if err := g.server.memoryLogger.Append(msg); err != nil && g.server.logger != nil {
			g.server.logger.Warn("failed to append memory log", "error", err)
		}
	}

	promptCtx := ctx
	if systemPrompt := g.server.systemPromptForMessage(ctx, session, msg); systemPrompt != "" {
		promptCtx = agent.WithSystemPrompt(promptCtx, systemPrompt)
	}
	if overrides := g.server.experimentOverrides(session, msg); overrides.Model != "" {
		promptCtx = agent.WithModel(promptCtx, overrides.Model)
	}
	if model := sessionModelOverride(session); model != "" {
		promptCtx = agent.WithModel(promptCtx, model)
	}

	runCtx, cancel := context.WithCancel(promptCtx)
	runToken := g.server.registerActiveRun(session.ID, cancel)
	defer func() {
		cancel()
		g.server.finishActiveRun(session.ID, runToken)
	}()

	chunks, err := runtime.Process(runCtx, session, msg)
	if err != nil {
		return status.Errorf(codes.Internal, "runtime error: %v", err)
	}

	messageID := uuid.NewString()
	sequence := int32(0)
	var response strings.Builder
	var toolResults []models.ToolResult

	for chunk := range chunks {
		if chunk.Error != nil {
			if err := stream.Send(&proto.ServerMessage{Message: &proto.ServerMessage_ErrorNotification{
				ErrorNotification: &proto.ErrorNotification{
					Code:    "runtime_error",
					Message: chunk.Error.Error(),
				},
			}}); err != nil {
				return err
			}
			return chunk.Error
		}
		if chunk.Text != "" {
			response.WriteString(chunk.Text)
			if err := stream.Send(&proto.ServerMessage{Message: &proto.ServerMessage_MessageChunk{
				MessageChunk: &proto.MessageChunk{
					MessageId: messageID,
					SessionId: session.ID,
					Content:   chunk.Text,
					Sequence:  sequence,
					Type:      proto.ChunkType_CHUNK_TYPE_TEXT,
				},
			}}); err != nil {
				return err
			}
			sequence++
		}
		if chunk.ToolResult != nil {
			toolResults = append(toolResults, *chunk.ToolResult)
		}
	}

	outbound := &models.Message{
		ID:          messageID,
		SessionID:   session.ID,
		Channel:     session.Channel,
		ChannelID:   session.ChannelID,
		Direction:   models.DirectionOutbound,
		Role:        models.RoleAssistant,
		Content:     response.String(),
		ToolResults: toolResults,
		CreatedAt:   time.Now(),
	}

	if err := g.server.sessions.AppendMessage(ctx, session.ID, outbound); err != nil {
		return status.Errorf(codes.Internal, "failed to persist response: %v", err)
	}
	if g.server.memoryLogger != nil {
		if err := g.server.memoryLogger.Append(outbound); err != nil && g.server.logger != nil {
			g.server.logger.Warn("failed to append memory log", "error", err)
		}
	}
	if session.Metadata != nil {
		if pending, ok := session.Metadata["memory_flush_pending"].(bool); ok && pending {
			session.Metadata["memory_flush_pending"] = false
			session.Metadata["memory_flush_confirmed_at"] = time.Now().Format(time.RFC3339)
			if err := g.server.sessions.Update(ctx, session); err != nil && g.server.logger != nil {
				g.server.logger.Warn("failed to update session metadata", "error", err)
			}
		}
	}

	return stream.Send(&proto.ServerMessage{Message: &proto.ServerMessage_MessageComplete{
		MessageComplete: &proto.MessageComplete{
			MessageId: messageID,
			SessionId: session.ID,
			Message:   messageToProto(outbound),
		},
	}})
}

func (g *grpcService) resolveSession(ctx context.Context, req *proto.SendMessageRequest) (*models.Session, error) {
	if g.server.sessions == nil {
		return nil, status.Error(codes.FailedPrecondition, "session store not initialized")
	}
	if req.SessionId != "" {
		session, err := g.server.sessions.Get(ctx, req.SessionId)
		if err != nil {
			return nil, status.Error(codes.NotFound, "session not found")
		}
		return session, nil
	}
	channelID := req.Metadata["channel_id"]
	if channelID == "" {
		if user, ok := auth.UserFromContext(ctx); ok {
			channelID = user.ID
		}
	}
	if channelID == "" {
		channelID = "grpc"
	}
	agentID := g.server.config.Session.DefaultAgentID
	if agentID == "" {
		agentID = "main"
	}
	key := sessions.SessionKey(agentID, models.ChannelAPI, channelID)
	session, err := g.server.sessions.GetOrCreate(ctx, key, agentID, models.ChannelAPI, channelID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create session: %v", err)
	}
	return session, nil
}

func (g *grpcService) CreateSession(ctx context.Context, req *proto.CreateSessionRequest) (*proto.CreateSessionResponse, error) {
	if g.server.sessions == nil {
		return nil, status.Error(codes.FailedPrecondition, "session store not initialized")
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	agentID := req.AgentId
	if agentID == "" {
		agentID = g.server.config.Session.DefaultAgentID
	}
	channel := channelFromProto(req.Channel)
	channelID := req.ChannelId
	key := req.Key
	if key == "" {
		key = sessions.SessionKey(agentID, channel, channelID)
	}
	session := &models.Session{
		AgentID:   agentID,
		Channel:   channel,
		ChannelID: channelID,
		Key:       key,
		Title:     req.Title,
		Metadata:  metadataFromProto(req.Metadata),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := g.server.sessions.Create(ctx, session); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to create session: %v", err)
	}
	return &proto.CreateSessionResponse{Session: sessionToProto(session)}, nil
}

func (g *grpcService) GetSession(ctx context.Context, req *proto.GetSessionRequest) (*proto.GetSessionResponse, error) {
	if g.server.sessions == nil {
		return nil, status.Error(codes.FailedPrecondition, "session store not initialized")
	}
	if req == nil || req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "session id required")
	}
	session, err := g.server.sessions.Get(ctx, req.Id)
	if err != nil {
		return nil, status.Error(codes.NotFound, "session not found")
	}
	return &proto.GetSessionResponse{Session: sessionToProto(session)}, nil
}

func (g *grpcService) ListSessions(ctx context.Context, req *proto.ListSessionsRequest) (*proto.ListSessionsResponse, error) {
	if g.server.sessions == nil {
		return nil, status.Error(codes.FailedPrecondition, "session store not initialized")
	}
	agentID := ""
	channel := models.ChannelType("")
	limit := 25
	if req != nil {
		agentID = req.AgentId
		if req.PageSize > 0 {
			limit = int(req.PageSize)
		}
		if req.Channel != proto.ChannelType_CHANNEL_TYPE_UNSPECIFIED {
			channel = channelFromProto(req.Channel)
		}
	}
	offset := parsePageToken(req.GetPageToken())

	sessionsList, err := g.server.sessions.List(ctx, agentID, sessions.ListOptions{Channel: channel, Limit: limit, Offset: offset})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list sessions: %v", err)
	}
	response := &proto.ListSessionsResponse{}
	for _, session := range sessionsList {
		response.Sessions = append(response.Sessions, sessionToProto(session))
	}
	if len(sessionsList) == limit {
		response.NextPageToken = strconv.Itoa(offset + limit)
	}
	response.TotalCount = clampNonNegativeIntToInt32(len(sessionsList))
	return response, nil
}

func (g *grpcService) UpdateSession(ctx context.Context, req *proto.UpdateSessionRequest) (*proto.UpdateSessionResponse, error) {
	if g.server.sessions == nil {
		return nil, status.Error(codes.FailedPrecondition, "session store not initialized")
	}
	if req == nil || req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "session id required")
	}
	session, err := g.server.sessions.Get(ctx, req.Id)
	if err != nil {
		return nil, status.Error(codes.NotFound, "session not found")
	}
	if req.Title != "" {
		session.Title = req.Title
	}
	if req.Metadata != nil {
		session.Metadata = metadataFromProto(req.Metadata)
	}
	if err := g.server.sessions.Update(ctx, session); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update session: %v", err)
	}
	return &proto.UpdateSessionResponse{Session: sessionToProto(session)}, nil
}

func (g *grpcService) DeleteSession(ctx context.Context, req *proto.DeleteSessionRequest) (*proto.DeleteSessionResponse, error) {
	if g.server.sessions == nil {
		return nil, status.Error(codes.FailedPrecondition, "session store not initialized")
	}
	if req == nil || req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "session id required")
	}
	if err := g.server.sessions.Delete(ctx, req.Id); err != nil {
		return nil, status.Error(codes.NotFound, "session not found")
	}
	return &proto.DeleteSessionResponse{Success: true}, nil
}

func (g *grpcService) CreateAgent(ctx context.Context, req *proto.CreateAgentRequest) (*proto.CreateAgentResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	agent := &models.Agent{
		ID:           uuid.NewString(),
		UserID:       resolveUserID(ctx, req.UserId),
		Name:         req.Name,
		SystemPrompt: req.SystemPrompt,
		Model:        req.Model,
		Provider:     req.Provider,
		Tools:        req.Tools,
		Config:       mapStringToAny(req.Config),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := g.agentStore.Create(ctx, agent); err != nil {
		if errors.Is(err, storage.ErrAlreadyExists) {
			return nil, status.Error(codes.AlreadyExists, "agent already exists")
		}
		return nil, status.Errorf(codes.Internal, "failed to create agent: %v", err)
	}
	return &proto.CreateAgentResponse{Agent: agentToProto(agent)}, nil
}

func (g *grpcService) GetAgent(ctx context.Context, req *proto.GetAgentRequest) (*proto.GetAgentResponse, error) {
	if req == nil || req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "agent id required")
	}
	agent, err := g.agentStore.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "agent not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to fetch agent: %v", err)
	}
	return &proto.GetAgentResponse{Agent: agentToProto(agent)}, nil
}

func (g *grpcService) ListAgents(ctx context.Context, req *proto.ListAgentsRequest) (*proto.ListAgentsResponse, error) {
	userID := ""
	limit := 25
	if req != nil {
		userID = resolveUserID(ctx, req.UserId)
		if req.PageSize > 0 {
			limit = int(req.PageSize)
		}
	}
	offset := parsePageToken(req.GetPageToken())
	agents, total, err := g.agentStore.List(ctx, userID, limit, offset)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list agents: %v", err)
	}
	response := &proto.ListAgentsResponse{}
	for _, agent := range agents {
		response.Agents = append(response.Agents, agentToProto(agent))
	}
	nextOffset := offset + len(agents)
	if total > nextOffset {
		response.NextPageToken = strconv.Itoa(nextOffset)
	}
	response.TotalCount = clampNonNegativeIntToInt32(total)
	return response, nil
}

func (g *grpcService) UpdateAgent(ctx context.Context, req *proto.UpdateAgentRequest) (*proto.UpdateAgentResponse, error) {
	if req == nil || req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "agent id required")
	}
	agent, err := g.agentStore.Get(ctx, req.Id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "agent not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to fetch agent: %v", err)
	}
	if req.Name != "" {
		agent.Name = req.Name
	}
	if req.SystemPrompt != "" {
		agent.SystemPrompt = req.SystemPrompt
	}
	if req.Model != "" {
		agent.Model = req.Model
	}
	if req.Provider != "" {
		agent.Provider = req.Provider
	}
	if len(req.Tools) > 0 {
		agent.Tools = req.Tools
	}
	if req.Config != nil {
		agent.Config = mapStringToAny(req.Config)
	}
	agent.UpdatedAt = time.Now()
	if err := g.agentStore.Update(ctx, agent); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "agent not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to update agent: %v", err)
	}
	return &proto.UpdateAgentResponse{Agent: agentToProto(agent)}, nil
}

func (g *grpcService) DeleteAgent(ctx context.Context, req *proto.DeleteAgentRequest) (*proto.DeleteAgentResponse, error) {
	if req == nil || req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "agent id required")
	}
	if err := g.agentStore.Delete(ctx, req.Id); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "agent not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to delete agent: %v", err)
	}
	return &proto.DeleteAgentResponse{Success: true}, nil
}

func (g *grpcService) ConnectChannel(ctx context.Context, req *proto.ConnectChannelRequest) (*proto.ConnectChannelResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	userID := resolveUserID(ctx, "")
	if userID == "" {
		userID = req.Credentials["user_id"]
	}
	connection := &models.ChannelConnection{
		ID:          uuid.NewString(),
		UserID:      userID,
		ChannelType: channelFromProto(req.ChannelType),
		ChannelID:   req.ChannelId,
		Status:      models.ConnectionStatusConnected,
		Config:      mapStringToAny(req.Config),
		ConnectedAt: time.Now(),
	}
	if err := g.channelStore.Create(ctx, connection); err != nil {
		if errors.Is(err, storage.ErrAlreadyExists) {
			return nil, status.Error(codes.AlreadyExists, "channel connection already exists")
		}
		return nil, status.Errorf(codes.Internal, "failed to connect channel: %v", err)
	}
	return &proto.ConnectChannelResponse{Connection: connectionToProto(connection)}, nil
}

func (g *grpcService) DisconnectChannel(ctx context.Context, req *proto.DisconnectChannelRequest) (*proto.DisconnectChannelResponse, error) {
	if req == nil || req.ConnectionId == "" {
		return nil, status.Error(codes.InvalidArgument, "connection id required")
	}
	connection, err := g.channelStore.Get(ctx, req.ConnectionId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return &proto.DisconnectChannelResponse{Success: false}, nil
		}
		return nil, status.Errorf(codes.Internal, "failed to fetch channel connection: %v", err)
	}
	connection.Status = models.ConnectionStatusDisconnected
	connection.LastActivityAt = time.Now()
	if err := g.channelStore.Update(ctx, connection); err != nil {
		return nil, status.Errorf(codes.Internal, "failed to update channel connection: %v", err)
	}
	return &proto.DisconnectChannelResponse{Success: true}, nil
}

func (g *grpcService) GetChannelStatus(ctx context.Context, req *proto.GetChannelStatusRequest) (*proto.GetChannelStatusResponse, error) {
	if req == nil || req.ConnectionId == "" {
		return nil, status.Error(codes.InvalidArgument, "connection id required")
	}
	connection, err := g.channelStore.Get(ctx, req.ConnectionId)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "connection not found")
		}
		return nil, status.Errorf(codes.Internal, "failed to fetch channel connection: %v", err)
	}
	return &proto.GetChannelStatusResponse{Connection: connectionToProto(connection)}, nil
}

func (g *grpcService) ListChannels(ctx context.Context, req *proto.ListChannelsRequest) (*proto.ListChannelsResponse, error) {
	userID := ""
	limit := 25
	if req != nil {
		userID = resolveUserID(ctx, req.UserId)
		if req.PageSize > 0 {
			limit = int(req.PageSize)
		}
	}
	offset := parsePageToken(req.GetPageToken())
	connections, total, err := g.channelStore.List(ctx, userID, limit, offset)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to list channel connections: %v", err)
	}
	response := &proto.ListChannelsResponse{}
	for _, conn := range connections {
		response.Connections = append(response.Connections, connectionToProto(conn))
	}
	nextOffset := offset + len(connections)
	if total > nextOffset {
		response.NextPageToken = strconv.Itoa(nextOffset)
	}
	response.TotalCount = clampNonNegativeIntToInt32(total)
	return response, nil
}

func (g *grpcService) Check(ctx context.Context, req *proto.HealthCheckRequest) (*proto.HealthCheckResponse, error) {
	metadata := map[string]string{}
	if g.server != nil && !g.server.startTime.IsZero() {
		metadata["uptime"] = time.Since(g.server.startTime).String()
	}
	return &proto.HealthCheckResponse{
		Status:    proto.ServingStatus_SERVING_STATUS_SERVING,
		Metadata:  metadata,
		Timestamp: timestamppb.Now(),
	}, nil
}

func (g *grpcService) Watch(req *proto.HealthCheckRequest, stream proto.HealthService_WatchServer) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		resp, err := g.Check(stream.Context(), req)
		if err != nil {
			return err
		}
		if err := stream.Send(resp); err != nil {
			return err
		}
		select {
		case <-stream.Context().Done():
			return nil
		case <-ticker.C:
		}
	}
}

func parsePageToken(token string) int {
	if token == "" {
		return 0
	}
	value, err := strconv.Atoi(token)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func resolveUserID(ctx context.Context, fallback string) string {
	if user, ok := auth.UserFromContext(ctx); ok {
		if user.ID != "" {
			return user.ID
		}
	}
	return fallback
}

func mapStringToAny(input map[string]string) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func agentToProto(agent *models.Agent) *proto.Agent {
	if agent == nil {
		return nil
	}
	config := map[string]string{}
	for k, v := range agent.Config {
		config[k] = fmt.Sprint(v)
	}
	return &proto.Agent{
		Id:           agent.ID,
		UserId:       agent.UserID,
		Name:         agent.Name,
		SystemPrompt: agent.SystemPrompt,
		Model:        agent.Model,
		Provider:     agent.Provider,
		Tools:        agent.Tools,
		Config:       config,
		CreatedAt:    timestampToProto(agent.CreatedAt),
		UpdatedAt:    timestampToProto(agent.UpdatedAt),
	}
}
