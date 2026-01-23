package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/haasonsaas/nexus/internal/auth"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
	proto "github.com/haasonsaas/nexus/pkg/proto"
)

const (
	wsProtocolVersion  = 1
	wsMaxPayloadBytes  = 1 << 20
	wsMaxBufferedBytes = 1 << 20
	wsTickInterval     = 15 * time.Second
	wsPongWait         = 45 * time.Second
	wsWriteWait        = 10 * time.Second
)

type wsControlPlane struct {
	server   *Server
	grpc     *grpcService
	auth     *auth.Service
	logger   *slog.Logger
	upgrader websocket.Upgrader
}

func (s *Server) newWSControlPlane() http.Handler {
	return &wsControlPlane{
		server: s,
		grpc:   newGRPCService(s),
		auth:   s.authService,
		logger: s.logger,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  8192,
			WriteBufferSize: 8192,
			CheckOrigin: func(*http.Request) bool {
				return true
			},
		},
	}
}

type wsFrame struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Event   string          `json:"event,omitempty"`
	OK      *bool           `json:"ok,omitempty"`
	Payload any             `json:"payload,omitempty"`
	Error   *wsError        `json:"error,omitempty"`
	Seq     *int64          `json:"seq,omitempty"`
}

type wsError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type wsConnectParams struct {
	MinProtocol int            `json:"minProtocol"`
	MaxProtocol int            `json:"maxProtocol"`
	Client      wsClientInfo   `json:"client"`
	Auth        *wsAuthPayload `json:"auth,omitempty"`
	Caps        []string       `json:"caps,omitempty"`
	Locale      string         `json:"locale,omitempty"`
	UserAgent   string         `json:"userAgent,omitempty"`
}

type wsClientInfo struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	Platform string `json:"platform"`
	Mode     string `json:"mode,omitempty"`
}

type wsAuthPayload struct {
	Token string `json:"token"`
}

type wsChatSendParams struct {
	SessionID      string            `json:"sessionId,omitempty"`
	Content        string            `json:"content"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	Attachments    []wsAttachment    `json:"attachments,omitempty"`
	IdempotencyKey string            `json:"idempotencyKey,omitempty"`
}

type wsChatHistoryParams struct {
	SessionID string `json:"sessionId"`
	Limit     int    `json:"limit,omitempty"`
}

type wsChatAbortParams struct {
	SessionID string `json:"sessionId"`
}

type wsSessionsListParams struct {
	AgentID string `json:"agentId,omitempty"`
	Channel string `json:"channel,omitempty"`
	Limit   int    `json:"limit,omitempty"`
	Offset  int    `json:"offset,omitempty"`
}

type wsSessionsPatchParams struct {
	SessionID string            `json:"sessionId"`
	Title     string            `json:"title,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

type wsAttachment struct {
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	URL      string `json:"url,omitempty"`
	Filename string `json:"filename,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

type wsSession struct {
	control *wsControlPlane
	conn    *websocket.Conn
	send    chan []byte
	ctx     context.Context
	cancel  context.CancelFunc

	id          string
	connected   atomic.Bool
	seq         int64
	user        *models.User
	headerUser  *models.User
	idempotency map[string]struct{}
	idemMu      sync.Mutex
}

func (h *wsControlPlane) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	session := &wsSession{
		control:     h,
		conn:        conn,
		send:        make(chan []byte, 64),
		ctx:         ctx,
		cancel:      cancel,
		id:          uuid.NewString(),
		headerUser:  h.authenticateRequest(r),
		idempotency: make(map[string]struct{}),
	}
	session.run()
}

func (s *wsSession) run() {
	defer s.close()
	go s.writeLoop()
	s.readLoop()
}

func (s *wsSession) close() {
	s.cancel()
	close(s.send)
	_ = s.conn.Close()
}

func (s *wsSession) readLoop() {
	s.conn.SetReadLimit(wsMaxPayloadBytes)
	_ = s.conn.SetReadDeadline(time.Now().Add(wsPongWait)) //nolint:errcheck
	s.conn.SetPongHandler(func(string) error {
		return s.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})

	for {
		messageType, data, err := s.conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}

		frame, err := s.decodeFrame(data)
		if err != nil {
			s.sendError("", "invalid_frame", err.Error())
			continue
		}

		if !s.connected.Load() {
			if frame.Method != "connect" {
				s.sendError(frame.ID, "handshake_required", "first request must be connect")
				continue
			}
			if err := s.handleConnect(frame); err != nil {
				s.sendError(frame.ID, "connect_failed", err.Error())
				return
			}
			continue
		}

		if err := s.handleRequest(frame); err != nil {
			s.sendError(frame.ID, "request_failed", err.Error())
		}
	}
}

func (s *wsSession) writeLoop() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case msg, ok := <-s.send:
			if !ok {
				return
			}
			_ = s.conn.SetWriteDeadline(time.Now().Add(wsWriteWait)) //nolint:errcheck
			if err := s.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}
}

func (s *wsSession) decodeFrame(raw []byte) (*wsFrame, error) {
	var frame wsFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		return nil, err
	}
	if frame.Type == "" {
		frame.Type = "req"
	}
	if frame.Type != "req" {
		return nil, fmt.Errorf("unsupported frame type %q", frame.Type)
	}
	if err := validateWSRequestFrame(raw, &frame); err != nil {
		return nil, err
	}
	return &frame, nil
}

func (s *wsSession) handleRequest(frame *wsFrame) error {
	switch frame.Method {
	case "health":
		return s.handleHealth(frame)
	case "ping":
		return s.sendResponse(frame.ID, true, map[string]any{"timestamp": time.Now().UnixMilli()}, nil)
	case "chat.send":
		return s.handleChatSend(frame)
	case "chat.history":
		return s.handleChatHistory(frame)
	case "chat.abort":
		return s.handleChatAbort(frame)
	case "sessions.list":
		return s.handleSessionsList(frame)
	case "sessions.patch":
		return s.handleSessionsPatch(frame)
	default:
		return fmt.Errorf("unknown method %q", frame.Method)
	}
}

func (s *wsSession) handleConnect(frame *wsFrame) error {
	var params wsConnectParams
	if err := json.Unmarshal(frame.Params, &params); err != nil {
		return err
	}

	minProtocol := params.MinProtocol
	maxProtocol := params.MaxProtocol
	if minProtocol <= 0 {
		minProtocol = wsProtocolVersion
	}
	if maxProtocol <= 0 {
		maxProtocol = wsProtocolVersion
	}
	if wsProtocolVersion < minProtocol || wsProtocolVersion > maxProtocol {
		return fmt.Errorf("unsupported protocol version")
	}

	if s.control.auth != nil && s.control.auth.Enabled() {
		user := s.headerUser
		if user == nil && params.Auth != nil {
			user = s.authenticateToken(params.Auth.Token)
		}
		if user == nil {
			return fmt.Errorf("unauthorized")
		}
		s.user = user
	}

	payload := s.buildHelloPayload()
	if err := s.sendResponse(frame.ID, true, payload, nil); err != nil {
		return err
	}
	s.connected.Store(true)
	go s.startTicking()
	return nil
}

func (s *wsSession) handleHealth(frame *wsFrame) error {
	payload := s.buildHealthSnapshot()
	return s.sendResponse(frame.ID, true, payload, nil)
}

func (s *wsSession) handleChatSend(frame *wsFrame) error {
	if s.control.grpc == nil {
		return errors.New("server unavailable")
	}
	var params wsChatSendParams
	if err := json.Unmarshal(frame.Params, &params); err != nil {
		return err
	}
	if strings.TrimSpace(params.Content) == "" {
		return errors.New("content is required")
	}

	if params.IdempotencyKey != "" {
		if s.isIdempotencyDuplicate(params.IdempotencyKey) {
			return s.sendResponse(frame.ID, true, map[string]any{"status": "duplicate"}, nil)
		}
	}

	req := &proto.SendMessageRequest{
		SessionId: params.SessionID,
		Content:   params.Content,
		Metadata:  params.Metadata,
	}
	if len(params.Attachments) > 0 {
		req.Attachments = make([]*proto.Attachment, 0, len(params.Attachments))
		for _, att := range params.Attachments {
			req.Attachments = append(req.Attachments, &proto.Attachment{
				Id:       att.ID,
				Type:     att.Type,
				Url:      att.URL,
				Filename: att.Filename,
				MimeType: att.MimeType,
				Size:     att.Size,
			})
		}
	}

	if err := s.sendResponse(frame.ID, true, map[string]any{"status": "accepted"}, nil); err != nil {
		return err
	}

	stream := &wsStream{
		ctx: s.ctx,
		send: func(msg *proto.ServerMessage) error {
			return s.sendProtoMessage(frame.ID, msg)
		},
	}
	if err := s.control.grpc.handleSendMessage(s.ctx, stream, req); err != nil {
		_ = s.sendEvent("error", map[string]any{ //nolint:errcheck
			"requestId": frame.ID,
			"code":      "runtime_error",
			"message":   err.Error(),
		})
	}
	return nil
}

func (s *wsSession) handleChatHistory(frame *wsFrame) error {
	if s.control.server == nil || s.control.server.sessions == nil {
		return errors.New("session store unavailable")
	}
	var params wsChatHistoryParams
	if err := json.Unmarshal(frame.Params, &params); err != nil {
		return err
	}
	limit := params.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}

	msgs, err := s.control.server.sessions.GetHistory(s.ctx, params.SessionID, limit)
	if err != nil {
		return err
	}
	payload, err := marshalProtoListMessages(msgs)
	if err != nil {
		return err
	}
	return s.sendResponse(frame.ID, true, payload, nil)
}

func (s *wsSession) handleChatAbort(frame *wsFrame) error {
	var params wsChatAbortParams
	if err := json.Unmarshal(frame.Params, &params); err != nil {
		return err
	}
	ok := false
	if s.control.server != nil {
		ok = s.control.server.cancelActiveRun(params.SessionID)
	}
	return s.sendResponse(frame.ID, true, map[string]any{"aborted": ok}, nil)
}

func (s *wsSession) handleSessionsList(frame *wsFrame) error {
	if s.control.server == nil || s.control.server.sessions == nil {
		return errors.New("session store unavailable")
	}
	var params wsSessionsListParams
	if err := json.Unmarshal(frame.Params, &params); err != nil {
		return err
	}

	agentID := strings.TrimSpace(params.AgentID)
	if agentID == "" && s.control.server.config != nil {
		agentID = s.control.server.config.Session.DefaultAgentID
	}
	if agentID == "" {
		agentID = "main"
	}

	opts := sessions.ListOptions{
		Limit:  params.Limit,
		Offset: params.Offset,
	}
	if opts.Limit <= 0 || opts.Limit > 500 {
		opts.Limit = 50
	}
	if params.Channel != "" {
		opts.Channel = models.ChannelType(params.Channel)
	}

	list, err := s.control.server.sessions.List(s.ctx, agentID, opts)
	if err != nil {
		return err
	}
	payload, err := marshalProtoListSessions(list)
	if err != nil {
		return err
	}
	return s.sendResponse(frame.ID, true, payload, nil)
}

func (s *wsSession) handleSessionsPatch(frame *wsFrame) error {
	if s.control.server == nil || s.control.server.sessions == nil {
		return errors.New("session store unavailable")
	}
	var params wsSessionsPatchParams
	if err := json.Unmarshal(frame.Params, &params); err != nil {
		return err
	}
	session, err := s.control.server.sessions.Get(s.ctx, params.SessionID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(params.Title) != "" {
		session.Title = params.Title
	}
	if params.Metadata != nil {
		if session.Metadata == nil {
			session.Metadata = map[string]any{}
		}
		for k, v := range params.Metadata {
			session.Metadata[k] = v
		}
	}
	if err := s.control.server.sessions.Update(s.ctx, session); err != nil {
		return err
	}
	payload, err := marshalProtoSession(session)
	if err != nil {
		return err
	}
	return s.sendResponse(frame.ID, true, payload, nil)
}

func (s *wsSession) sendProtoMessage(requestID string, msg *proto.ServerMessage) error {
	if msg == nil {
		return nil
	}
	switch payload := msg.Message.(type) {
	case *proto.ServerMessage_MessageChunk:
		chunk := payload.MessageChunk
		return s.sendEvent("chat.chunk", map[string]any{
			"requestId": requestID,
			"messageId": chunk.MessageId,
			"sessionId": chunk.SessionId,
			"content":   chunk.Content,
			"sequence":  chunk.Sequence,
			"type":      chunk.Type.String(),
		})
	case *proto.ServerMessage_MessageComplete:
		complete := payload.MessageComplete
		encoded, err := marshalProtoMessage(complete.Message)
		if err != nil {
			return err
		}
		return s.sendEvent("chat.complete", map[string]any{
			"requestId": requestID,
			"messageId": complete.MessageId,
			"sessionId": complete.SessionId,
			"message":   encoded,
		})
	case *proto.ServerMessage_ErrorNotification:
		notice := payload.ErrorNotification
		return s.sendEvent("error", map[string]any{
			"requestId": requestID,
			"code":      notice.Code,
			"message":   notice.Message,
		})
	case *proto.ServerMessage_ToolCallRequest:
		encoded, err := marshalProtoToolCallRequest(payload.ToolCallRequest)
		if err != nil {
			return err
		}
		return s.sendEvent("tool.call", encoded)
	case *proto.ServerMessage_SessionEventNotification:
		encoded, err := marshalProtoSessionEvent(payload.SessionEventNotification)
		if err != nil {
			return err
		}
		return s.sendEvent("session.event", encoded)
	case *proto.ServerMessage_Pong:
		return s.sendEvent("pong", map[string]any{"timestamp": time.Now().UnixMilli()})
	default:
		return nil
	}
}

func (s *wsSession) sendResponse(id string, ok bool, payload any, err *wsError) error {
	frame := wsFrame{
		Type:    "res",
		ID:      id,
		OK:      &ok,
		Payload: payload,
		Error:   err,
	}
	return s.enqueue(frame)
}

func (s *wsSession) sendEvent(event string, payload any) error {
	seq := atomic.AddInt64(&s.seq, 1)
	frame := wsFrame{
		Type:    "event",
		Event:   event,
		Payload: payload,
		Seq:     &seq,
	}
	return s.enqueue(frame)
}

func (s *wsSession) sendError(id string, code string, message string) {
	_ = s.sendResponse(id, false, nil, &wsError{Code: code, Message: message}) //nolint:errcheck
}

func (s *wsSession) enqueue(frame wsFrame) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	if len(data) > wsMaxPayloadBytes {
		return fmt.Errorf("payload too large")
	}
	select {
	case s.send <- data:
		return nil
	default:
		return fmt.Errorf("send buffer full")
	}
}

func (s *wsSession) startTicking() {
	ticker := time.NewTicker(wsTickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			_ = s.sendEvent("tick", map[string]any{"timestamp": time.Now().UnixMilli()}) //nolint:errcheck
		}
	}
}

func (s *wsSession) buildHelloPayload() map[string]any {
	return map[string]any{
		"type":     "hello-ok",
		"protocol": wsProtocolVersion,
		"server": map[string]any{
			"id": s.id,
		},
		"features": map[string]any{
			"methods": supportedWSMethods(),
			"events":  supportedWSEvents(),
		},
		"policy": map[string]any{
			"maxPayloadBytes":  wsMaxPayloadBytes,
			"maxBufferedBytes": wsMaxBufferedBytes,
			"tickIntervalMs":   wsTickInterval.Milliseconds(),
		},
		"snapshot": s.buildHealthSnapshot(),
	}
}

func (s *wsSession) buildHealthSnapshot() map[string]any {
	payload := map[string]any{
		"uptimeMs": time.Since(s.control.server.startTime).Milliseconds(),
		"health": map[string]any{
			"status": "ok",
		},
	}
	if s.control.server == nil {
		return payload
	}

	channelStatuses := make([]map[string]any, 0)
	for channel, adapter := range s.control.server.channels.HealthAdapters() {
		status := adapter.Status()
		channelStatuses = append(channelStatuses, map[string]any{
			"channel":   string(channel),
			"connected": status.Connected,
			"error":     status.Error,
			"lastPing":  status.LastPing,
		})
	}
	if len(channelStatuses) > 0 {
		payload["channels"] = channelStatuses
	}
	return payload
}

func (s *wsSession) authenticateToken(token string) *models.User {
	if s.control.auth == nil || !s.control.auth.Enabled() {
		return nil
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	if user, err := s.control.auth.ValidateJWT(token); err == nil {
		return user
	}
	if user, err := s.control.auth.ValidateAPIKey(token); err == nil {
		return user
	}
	return nil
}

func (h *wsControlPlane) authenticateRequest(r *http.Request) *models.User {
	if h.auth == nil || !h.auth.Enabled() {
		return nil
	}
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(authHeader), "bearer ") {
		token := strings.TrimSpace(authHeader[7:])
		if user, err := h.auth.ValidateJWT(token); err == nil {
			return user
		}
	}
	apiKey := r.Header.Get("X-API-Key")
	if apiKey == "" {
		apiKey = r.Header.Get("Api-Key")
	}
	if apiKey != "" {
		if user, err := h.auth.ValidateAPIKey(apiKey); err == nil {
			return user
		}
	}
	return nil
}

func supportedWSMethods() []string {
	return []string{
		"connect",
		"health",
		"ping",
		"chat.send",
		"chat.history",
		"chat.abort",
		"sessions.list",
		"sessions.patch",
	}
}

func supportedWSEvents() []string {
	return []string{
		"tick",
		"chat.chunk",
		"chat.complete",
		"error",
		"tool.call",
		"session.event",
		"pong",
	}
}

func (s *wsSession) isIdempotencyDuplicate(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	s.idemMu.Lock()
	defer s.idemMu.Unlock()
	if _, ok := s.idempotency[key]; ok {
		return true
	}
	s.idempotency[key] = struct{}{}
	return false
}

func marshalProtoMessage(message *proto.Message) (json.RawMessage, error) {
	if message == nil {
		return json.RawMessage("null"), nil
	}
	data, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(message)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func marshalProtoListMessages(messages []*models.Message) (map[string]any, error) {
	out := make([]json.RawMessage, 0, len(messages))
	for _, msg := range messages {
		data, err := marshalProtoMessage(messageToProto(msg))
		if err != nil {
			return nil, err
		}
		out = append(out, data)
	}
	return map[string]any{"messages": out}, nil
}

func marshalProtoSession(session *models.Session) (json.RawMessage, error) {
	data, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(sessionToProto(session))
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func marshalProtoListSessions(sessionsList []*models.Session) (map[string]any, error) {
	out := make([]json.RawMessage, 0, len(sessionsList))
	for _, session := range sessionsList {
		data, err := marshalProtoSession(session)
		if err != nil {
			return nil, err
		}
		out = append(out, data)
	}
	return map[string]any{"sessions": out}, nil
}

func marshalProtoToolCallRequest(req *proto.ToolCallRequest) (json.RawMessage, error) {
	if req == nil {
		return json.RawMessage("null"), nil
	}
	data, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(req)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

func marshalProtoSessionEvent(evt *proto.SessionEventNotification) (json.RawMessage, error) {
	if evt == nil {
		return json.RawMessage("null"), nil
	}
	data, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(evt)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

type wsStream struct {
	ctx  context.Context
	send func(*proto.ServerMessage) error
}

func (w *wsStream) SetHeader(metadata.MD) error  { return nil }
func (w *wsStream) SendHeader(metadata.MD) error { return nil }
func (w *wsStream) SetTrailer(metadata.MD)       {}
func (w *wsStream) Context() context.Context     { return w.ctx }
func (w *wsStream) Send(msg *proto.ServerMessage) error {
	if w.send == nil {
		return nil
	}
	return w.send(msg)
}
func (w *wsStream) Recv() (*proto.ClientMessage, error) { return nil, errors.New("not implemented") }
func (w *wsStream) SendMsg(m any) error                 { return nil }
func (w *wsStream) RecvMsg(m any) error                 { return nil }

var _ proto.NexusGateway_StreamServer = (*wsStream)(nil)
var _ grpc.ServerStream = (*wsStream)(nil)
