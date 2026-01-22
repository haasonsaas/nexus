package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/auth"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
	proto "github.com/haasonsaas/nexus/pkg/proto"
)

type grpcService struct {
	proto.UnimplementedNexusGatewayServer
	proto.UnimplementedSessionServiceServer
	proto.UnimplementedAgentServiceServer
	proto.UnimplementedChannelServiceServer
	proto.UnimplementedHealthServiceServer

	server      *Server
	agentStore  *agentStore
	channelConn *channelConnStore
}

func newGRPCService(server *Server) *grpcService {
	return &grpcService{
		server:      server,
		agentStore:  newAgentStore(),
		channelConn: newChannelConnStore(),
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
			_ = stream.Send(&proto.ServerMessage{Message: &proto.ServerMessage_Pong{
				Pong: &proto.PongResponse{Timestamp: timestamppb.Now()},
			}})
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
		_ = g.server.memoryLogger.Append(msg)
	}

	promptCtx := ctx
	if systemPrompt := g.server.systemPromptForMessage(ctx, session, msg); systemPrompt != "" {
		promptCtx = agent.WithSystemPrompt(promptCtx, systemPrompt)
	}

	chunks, err := runtime.Process(promptCtx, session, msg)
	if err != nil {
		return status.Errorf(codes.Internal, "runtime error: %v", err)
	}

	messageID := uuid.NewString()
	sequence := int32(0)
	var response strings.Builder
	var toolResults []models.ToolResult

	for chunk := range chunks {
		if chunk.Error != nil {
			_ = stream.Send(&proto.ServerMessage{Message: &proto.ServerMessage_ErrorNotification{
				ErrorNotification: &proto.ErrorNotification{
					Code:    "runtime_error",
					Message: chunk.Error.Error(),
				},
			}})
			return chunk.Error
		}
		if chunk.Text != "" {
			response.WriteString(chunk.Text)
			_ = stream.Send(&proto.ServerMessage{Message: &proto.ServerMessage_MessageChunk{
				MessageChunk: &proto.MessageChunk{
					MessageId: messageID,
					SessionId: session.ID,
					Content:   chunk.Text,
					Sequence:  sequence,
					Type:      proto.ChunkType_CHUNK_TYPE_TEXT,
				},
			}})
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
		_ = g.server.memoryLogger.Append(outbound)
	}
	if session.Metadata != nil {
		if pending, ok := session.Metadata["memory_flush_pending"].(bool); ok && pending {
			session.Metadata["memory_flush_pending"] = false
			session.Metadata["memory_flush_confirmed_at"] = time.Now().Format(time.RFC3339)
			_ = g.server.sessions.Update(ctx, session)
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
	response.TotalCount = int32(len(sessionsList))
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
	g.agentStore.Save(agent)
	return &proto.CreateAgentResponse{Agent: agentToProto(agent)}, nil
}

func (g *grpcService) GetAgent(ctx context.Context, req *proto.GetAgentRequest) (*proto.GetAgentResponse, error) {
	if req == nil || req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "agent id required")
	}
	agent, ok := g.agentStore.Get(req.Id)
	if !ok {
		return nil, status.Error(codes.NotFound, "agent not found")
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
	agents := g.agentStore.List(userID)
	if offset > len(agents) {
		offset = len(agents)
	}
	end := len(agents)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	response := &proto.ListAgentsResponse{}
	for _, agent := range agents[offset:end] {
		response.Agents = append(response.Agents, agentToProto(agent))
	}
	if end < len(agents) {
		response.NextPageToken = strconv.Itoa(end)
	}
	response.TotalCount = int32(len(agents))
	return response, nil
}

func (g *grpcService) UpdateAgent(ctx context.Context, req *proto.UpdateAgentRequest) (*proto.UpdateAgentResponse, error) {
	if req == nil || req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "agent id required")
	}
	agent, ok := g.agentStore.Get(req.Id)
	if !ok {
		return nil, status.Error(codes.NotFound, "agent not found")
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
	g.agentStore.Save(agent)
	return &proto.UpdateAgentResponse{Agent: agentToProto(agent)}, nil
}

func (g *grpcService) DeleteAgent(ctx context.Context, req *proto.DeleteAgentRequest) (*proto.DeleteAgentResponse, error) {
	if req == nil || req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "agent id required")
	}
	g.agentStore.Delete(req.Id)
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
	connection := &proto.ChannelConnection{
		Id:          uuid.NewString(),
		UserId:      userID,
		ChannelType: req.ChannelType,
		ChannelId:   req.ChannelId,
		Status:      proto.ConnectionStatus_CONNECTION_STATUS_CONNECTED,
		Config:      req.Config,
		ConnectedAt: timestamppb.Now(),
	}
	g.channelConn.Save(connection)
	return &proto.ConnectChannelResponse{Connection: connection}, nil
}

func (g *grpcService) DisconnectChannel(ctx context.Context, req *proto.DisconnectChannelRequest) (*proto.DisconnectChannelResponse, error) {
	if req == nil || req.ConnectionId == "" {
		return nil, status.Error(codes.InvalidArgument, "connection id required")
	}
	connection, ok := g.channelConn.Get(req.ConnectionId)
	if !ok {
		return &proto.DisconnectChannelResponse{Success: false}, nil
	}
	connection.Status = proto.ConnectionStatus_CONNECTION_STATUS_DISCONNECTED
	connection.LastActivityAt = timestamppb.Now()
	g.channelConn.Save(connection)
	return &proto.DisconnectChannelResponse{Success: true}, nil
}

func (g *grpcService) GetChannelStatus(ctx context.Context, req *proto.GetChannelStatusRequest) (*proto.GetChannelStatusResponse, error) {
	if req == nil || req.ConnectionId == "" {
		return nil, status.Error(codes.InvalidArgument, "connection id required")
	}
	connection, ok := g.channelConn.Get(req.ConnectionId)
	if !ok {
		return nil, status.Error(codes.NotFound, "connection not found")
	}
	return &proto.GetChannelStatusResponse{Connection: connection}, nil
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
	connections := g.channelConn.List(userID)
	if offset > len(connections) {
		offset = len(connections)
	}
	end := len(connections)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	response := &proto.ListChannelsResponse{}
	response.Connections = append(response.Connections, connections[offset:end]...)
	if end < len(connections) {
		response.NextPageToken = strconv.Itoa(end)
	}
	response.TotalCount = int32(len(connections))
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
	for {
		resp, _ := g.Check(stream.Context(), req)
		if err := stream.Send(resp); err != nil {
			return err
		}
		select {
		case <-stream.Context().Done():
			return nil
		case <-time.After(5 * time.Second):
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

// agentStore keeps agents in memory until persistence is implemented.
type agentStore struct {
	mu     sync.RWMutex
	agents map[string]*models.Agent
}

func newAgentStore() *agentStore {
	return &agentStore{agents: map[string]*models.Agent{}}
}

func (s *agentStore) Save(agent *models.Agent) {
	if agent == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agents[agent.ID] = agent
}

func (s *agentStore) Get(id string) (*models.Agent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	agent, ok := s.agents[id]
	return agent, ok
}

func (s *agentStore) List(userID string) []*models.Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*models.Agent, 0, len(s.agents))
	for _, agent := range s.agents {
		if userID != "" && agent.UserID != userID {
			continue
		}
		out = append(out, agent)
	}
	return out
}

func (s *agentStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.agents, id)
}

// channelConnStore tracks channel connections in memory.
type channelConnStore struct {
	mu          sync.RWMutex
	connections map[string]*proto.ChannelConnection
}

func newChannelConnStore() *channelConnStore {
	return &channelConnStore{connections: map[string]*proto.ChannelConnection{}}
}

func (s *channelConnStore) Save(conn *proto.ChannelConnection) {
	if conn == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connections[conn.Id] = conn
}

func (s *channelConnStore) Get(id string) (*proto.ChannelConnection, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	conn, ok := s.connections[id]
	return conn, ok
}

func (s *channelConnStore) List(userID string) []*proto.ChannelConnection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*proto.ChannelConnection, 0, len(s.connections))
	for _, conn := range s.connections {
		if userID != "" && conn.UserId != userID {
			continue
		}
		out = append(out, conn)
	}
	return out
}
