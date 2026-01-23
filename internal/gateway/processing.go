// Package gateway provides the main Nexus gateway server.
//
// processing.go contains message processing logic including the agentic loop
// and broadcast message handling.
package gateway

import (
	"context"
	"encoding/base64"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

const defaultAgentID = "main"

// Processing limits to prevent resource exhaustion
const (
	// maxProcessingTime is the maximum time allowed for a single message processing.
	maxProcessingTime = 10 * time.Minute

	// maxResponseSize is the maximum size of accumulated response text (1MB).
	maxResponseSize = 1 << 20 // 1MB

	// maxInputSize is the maximum size of input message content (1MB).
	maxInputSize = 1 << 20 // 1MB

	// maxToolResults is the maximum number of tool results per processing.
	maxToolResults = 100

	// streamingUpdateInterval is the minimum time between streaming message updates
	// to avoid hitting API rate limits.
	streamingUpdateInterval = 1 * time.Second

	// streamingTypingInterval is how often to refresh the typing indicator.
	streamingTypingInterval = 4 * time.Second
)

// startProcessing starts the background message processing goroutine.
func (s *Server) startProcessing(ctx context.Context) {
	processCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.wg.Add(1)
	go s.processMessages(processCtx)
}

// processMessages handles incoming messages from all channels.
func (s *Server) processMessages(ctx context.Context) {
	defer s.wg.Done()
	messages := s.channels.AggregateMessages(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-messages:
			if !ok {
				return
			}
			// Acquire semaphore slot to limit concurrent handlers
			select {
			case s.messageSem <- struct{}{}:
				s.wg.Add(1)
				go func(message *models.Message) {
					defer func() {
						<-s.messageSem // Release semaphore slot
						s.wg.Done()
					}()
					s.handleMessage(ctx, message)
				}(msg)
			case <-ctx.Done():
				return
			}
		}
	}
}

// handleMessage processes a single incoming message.
func (s *Server) handleMessage(ctx context.Context, msg *models.Message) {
	s.logger.Debug("received message",
		"channel", msg.Channel,
		"content_length", len(msg.Content),
	)

	if s.handleMessageHook != nil {
		s.handleMessageHook(ctx, msg)
		return
	}

	// Normalize message metadata to canonical format
	if s.normalizer != nil {
		s.normalizer.Normalize(msg)
	} else if msg.Metadata == nil {
		msg.Metadata = map[string]any{}
	}

	// Validate input message size to prevent memory exhaustion
	if len(msg.Content) > maxInputSize {
		s.logger.Warn("input message too large, truncating",
			"channel", msg.Channel,
			"original_size", len(msg.Content),
			"max_size", maxInputSize)
		msg.Content = msg.Content[:maxInputSize]
	}

	if s.enforceAccessPolicy(ctx, msg) {
		return
	}

	runtime, err := s.ensureRuntime(ctx)
	if err != nil {
		s.logger.Error("runtime initialization failed", "error", err)
		return
	}

	// Check for broadcast routing
	peerID := s.extractPeerID(msg)
	if peerID != "" && s.broadcastManager != nil && s.broadcastManager.IsBroadcastPeer(peerID) {
		s.handleBroadcastMessage(ctx, peerID, msg, runtime)
		return
	}

	channelID, err := s.resolveConversationID(msg)
	if err != nil {
		s.logger.Error("failed to resolve conversation id", "error", err)
		return
	}

	agentID := defaultAgentID
	if s.config != nil && s.config.Session.DefaultAgentID != "" {
		agentID = s.config.Session.DefaultAgentID
	}
	key := sessions.SessionKey(agentID, msg.Channel, channelID)
	session, err := s.sessions.GetOrCreate(ctx, key, agentID, msg.Channel, channelID)
	if err != nil {
		s.logger.Error("failed to get or create session", "error", err)
		return
	}

	msg.SessionID = session.ID
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}

	if s.maybeHandleCommand(ctx, session, msg) {
		return
	}
	if s.maybeHandleInlineCommands(ctx, session, msg) {
		if strings.TrimSpace(msg.Content) == "" {
			return
		}
	}

	s.enrichMessageWithMedia(ctx, msg)

	// Note: inbound message persistence is handled by runtime.Process()
	// to avoid double-persisting the same message.
	if s.memoryLogger != nil {
		if err := s.memoryLogger.Append(msg); err != nil {
			s.logger.Error("failed to write memory log", "error", err)
		}
	}

	var agentModel *models.Agent
	if s.stores.Agents != nil {
		var err error
		agentModel, err = s.stores.Agents.Get(ctx, agentID)
		if err != nil {
			s.logger.Warn("failed to load agent config", "error", err)
		}
	}
	overrides := parseAgentToolOverrides(agentModel)

	var agentElevatedCfg *config.ElevatedConfig
	if overrides.HasElevated {
		agentElevatedCfg = &overrides.Elevated
	}
	elevatedAllowed, elevatedReason := resolveElevatedPermission(s.config.Tools.Elevated, agentElevatedCfg, msg)
	inlineElevatedSet := false
	inlineElevatedMode := agent.ElevatedOff

	if directive, ok := parseElevatedDirective(msg.Content); ok {
		switch directive.Scope {
		case elevatedScopeStatus:
			s.sendImmediateReply(ctx, session, msg, formatElevatedStatus(elevatedModeFromSession(session), elevatedAllowed, elevatedReason))
			return
		case elevatedScopeSession:
			if !elevatedAllowed {
				s.sendImmediateReply(ctx, session, msg, formatElevatedUnavailable(elevatedReason))
				return
			}
			setSessionElevatedMode(session, directive.Mode)
			if err := s.sessions.Update(ctx, session); err != nil {
				s.logger.Error("failed to persist elevated mode", "error", err)
			}
			s.sendImmediateReply(ctx, session, msg, formatElevatedSet(directive.Mode))
			return
		case elevatedScopeInline:
			if directive.Cleaned != "" {
				msg.Content = directive.Cleaned
			}
			if elevatedAllowed {
				inlineElevatedMode = directive.Mode
				inlineElevatedSet = true
			}
		}
	}

	effectiveElevated := elevatedModeFromSession(session)
	if inlineElevatedSet {
		effectiveElevated = inlineElevatedMode
	}
	if !elevatedAllowed {
		effectiveElevated = agent.ElevatedOff
	}

	promptCtx := ctx
	if systemPrompt := s.systemPromptForMessage(ctx, session, msg); systemPrompt != "" {
		promptCtx = agent.WithSystemPrompt(promptCtx, systemPrompt)
	}
	if model := sessionModelOverride(session); model != "" {
		promptCtx = agent.WithModel(promptCtx, model)
	}
	if effectiveElevated != agent.ElevatedOff {
		promptCtx = agent.WithElevated(promptCtx, effectiveElevated)
	}
	if overrides.HasExecution || overrides.HasElevated {
		override := runtimeOptionsOverrideFromExecution(overrides.Execution)
		if overrides.HasElevated {
			override.ElevatedTools = effectiveElevatedTools(s.config.Tools.Elevated, agentElevatedCfg)
		}
		promptCtx = agent.WithRuntimeOptions(promptCtx, override)
	}
	if s.toolPolicyResolver != nil {
		if toolPolicy := toolPolicyFromAgent(agentModel); toolPolicy != nil {
			promptCtx = agent.WithToolPolicy(promptCtx, s.toolPolicyResolver, toolPolicy)
		}
	}
	if s.approvalChecker != nil {
		basePolicy := s.approvalChecker.PolicyFor("")
		policy := approvalPolicyForAgent(basePolicy, overrides, s.toolPolicyResolver)
		if policy != nil && policy != basePolicy {
			s.approvalChecker.SetAgentPolicy(agentID, policy)
		}
	}

	runCtx, cancel := context.WithTimeout(promptCtx, maxProcessingTime)
	runToken := s.registerActiveRun(session.ID, cancel)
	defer func() {
		cancel()
		s.finishActiveRun(session.ID, runToken)
	}()

	chunks, err := runtime.Process(runCtx, session, msg)
	if err != nil {
		s.logger.Error("runtime processing failed", "error", err)
		return
	}

	// Check for streaming support
	streamingAdapter, hasStreaming := s.channels.GetStreaming(msg.Channel)
	outboundAdapter, hasOutbound := s.channels.GetOutbound(msg.Channel)
	if !hasOutbound {
		s.logger.Error("no adapter registered for channel", "channel", msg.Channel)
		return
	}

	// Build outbound message template for streaming operations
	outboundMsg := &models.Message{
		SessionID: session.ID,
		Channel:   msg.Channel,
		Direction: models.DirectionOutbound,
		Role:      models.RoleAssistant,
		Metadata:  s.buildReplyMetadata(msg),
		CreatedAt: time.Now(),
	}

	// Streaming state - use atomic for hasStreaming to avoid race conditions
	var streamingEnabled atomic.Bool
	streamingEnabled.Store(hasStreaming)
	var streamingMsgID string
	var lastUpdate time.Time
	var lastTyping time.Time
	var mu sync.Mutex

	// Send initial typing indicator if streaming is supported
	if streamingEnabled.Load() {
		if err := streamingAdapter.SendTypingIndicator(runCtx, outboundMsg); err != nil {
			s.logger.Debug("failed to send typing indicator", "error", err)
		}
		lastTyping = time.Now()
	}

	var response strings.Builder
	var toolResults []models.ToolResult
	var attachments []models.Attachment
	var truncated bool
	for chunk := range chunks {
		if chunk.Error != nil {
			s.logger.Error("runtime stream error", "error", chunk.Error)
			return
		}
		if chunk.Text != "" {
			// Check size limit to prevent memory exhaustion
			if response.Len()+len(chunk.Text) > maxResponseSize {
				if !truncated {
					s.logger.Warn("response truncated due to size limit",
						"session_id", session.ID,
						"limit", maxResponseSize)
					truncated = true
				}
				continue
			}
			response.WriteString(chunk.Text)

			// Handle streaming updates
			if streamingEnabled.Load() {
				mu.Lock()
				now := time.Now()

				// Start streaming response on first text
				if streamingMsgID == "" {
					var err error
					streamingMsgID, err = streamingAdapter.StartStreamingResponse(runCtx, outboundMsg)
					if err != nil {
						s.logger.Warn("streaming fallback triggered, switching to non-streaming mode",
							"error", err,
							"session_id", session.ID)
						// Fall back to non-streaming mode
						streamingEnabled.Store(false)
					} else {
						lastUpdate = now
					}
				} else if now.Sub(lastUpdate) >= streamingUpdateInterval {
					// Throttle updates to avoid rate limits
					if err := streamingAdapter.UpdateStreamingResponse(runCtx, outboundMsg, streamingMsgID, response.String()); err != nil {
						s.logger.Debug("failed to update streaming response", "error", err)
					}
					lastUpdate = now
				}
				mu.Unlock()
			}
		}
		if chunk.ToolResult != nil {
			// Check tool results limit
			if len(toolResults) >= maxToolResults {
				s.logger.Warn("tool results truncated due to limit",
					"session_id", session.ID,
					"limit", maxToolResults)
				continue
			}
			toolResults = append(toolResults, *chunk.ToolResult)

			// Refresh typing indicator during tool execution
			mu.Lock()
			shouldRefreshTyping := streamingEnabled.Load() && streamingMsgID == ""
			if shouldRefreshTyping && time.Since(lastTyping) >= streamingTypingInterval {
				if err := streamingAdapter.SendTypingIndicator(runCtx, outboundMsg); err != nil {
					s.logger.Debug("failed to refresh typing indicator", "error", err)
				}
				lastTyping = time.Now()
			}
			mu.Unlock()
		}
		// Collect artifacts from tool executions for sending as attachments
		if len(chunk.Artifacts) > 0 {
			for _, art := range chunk.Artifacts {
				attachments = append(attachments, s.artifactToAttachment(art))
			}
		}
	}

	if response.Len() == 0 && len(toolResults) == 0 {
		return
	}

	outboundMsg.Content = response.String()
	outboundMsg.ToolResults = toolResults
	outboundMsg.Attachments = attachments

	// Final update or send
	mu.Lock()
	finalStreamingMsgID := streamingMsgID
	finalStreamingEnabled := streamingEnabled.Load()
	mu.Unlock()

	if finalStreamingEnabled && finalStreamingMsgID != "" {
		// Do final update with complete content
		if err := streamingAdapter.UpdateStreamingResponse(runCtx, outboundMsg, finalStreamingMsgID, response.String()); err != nil {
			s.logger.Debug("failed to send final streaming update", "error", err)
			// Fall back to sending a new message
			if err := outboundAdapter.Send(ctx, outboundMsg); err != nil {
				s.logger.Error("failed to send outbound message", "error", err)
				return
			}
		}
	} else {
		// Non-streaming: send complete message
		if err := outboundAdapter.Send(ctx, outboundMsg); err != nil {
			s.logger.Error("failed to send outbound message", "error", err)
			return
		}
	}

	// Note: assistant message persistence is handled by runtime.Process()
	// The runtime persists the full message with tool calls during the agentic loop.
	if s.memoryLogger != nil {
		if err := s.memoryLogger.Append(outboundMsg); err != nil {
			s.logger.Error("failed to write memory log", "error", err)
		}
	}

	if session.Metadata != nil {
		if pending, ok := session.Metadata["memory_flush_pending"].(bool); ok && pending {
			session.Metadata["memory_flush_pending"] = false
			session.Metadata["memory_flush_confirmed_at"] = time.Now().Format(time.RFC3339)
			if err := s.sessions.Update(ctx, session); err != nil {
				s.logger.Error("failed to update memory flush confirmation", "error", err)
			}
		}
	}
}

func (s *Server) sendImmediateReply(ctx context.Context, session *models.Session, inbound *models.Message, content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	adapter, ok := s.channels.GetOutbound(inbound.Channel)
	if !ok {
		s.logger.Error("no adapter registered for channel", "channel", inbound.Channel)
		return
	}
	outbound := &models.Message{
		SessionID: session.ID,
		Channel:   inbound.Channel,
		Direction: models.DirectionOutbound,
		Role:      models.RoleAssistant,
		Content:   content,
		Metadata:  s.buildReplyMetadata(inbound),
		CreatedAt: time.Now(),
	}
	if err := adapter.Send(ctx, outbound); err != nil {
		s.logger.Error("failed to send outbound message", "error", err)
		return
	}
	if s.memoryLogger != nil {
		if err := s.memoryLogger.Append(outbound); err != nil {
			s.logger.Error("failed to write memory log", "error", err)
		}
	}
}

// artifactToAttachment converts an agent.Artifact to a models.Attachment.
// Artifacts from edge tools (like screenshots) are converted to attachments
// so they can be sent via messaging channels like WhatsApp.
func (s *Server) artifactToAttachment(art agent.Artifact) models.Attachment {
	// Determine attachment type from artifact type or mime type
	attType := "file"
	switch art.Type {
	case "screenshot", "image":
		attType = "image"
	case "recording", "video":
		attType = "video"
	case "audio":
		attType = "audio"
	default:
		if strings.HasPrefix(art.MimeType, "image/") {
			attType = "image"
		} else if strings.HasPrefix(art.MimeType, "video/") {
			attType = "video"
		} else if strings.HasPrefix(art.MimeType, "audio/") {
			attType = "audio"
		}
	}

	att := models.Attachment{
		ID:       art.ID,
		Type:     attType,
		Filename: art.Filename,
		MimeType: art.MimeType,
		Size:     int64(len(art.Data)),
	}

	// If artifact has a URL, use it; otherwise create a data URL
	if art.URL != "" {
		att.URL = art.URL
	} else if len(art.Data) > 0 {
		// Create data URL for inline artifacts
		att.URL = "data:" + art.MimeType + ";base64," + base64.StdEncoding.EncodeToString(art.Data)
	}

	return att
}

// extractPeerID extracts a peer identifier from the message metadata.
// The peer_id is used to determine if the message should be broadcast to multiple agents.
func (s *Server) extractPeerID(msg *models.Message) string {
	if msg.Metadata == nil {
		return ""
	}
	// Check for explicit peer_id in metadata
	if peerID, ok := msg.Metadata["peer_id"].(string); ok && peerID != "" {
		return peerID
	}
	// Fall back to channel-specific identifiers that can serve as peer IDs
	switch msg.Channel {
	case models.ChannelTelegram:
		if chatID, ok := msg.Metadata["chat_id"]; ok {
			switch v := chatID.(type) {
			case int64:
				return strconv.FormatInt(v, 10)
			case int:
				return strconv.Itoa(v)
			case string:
				return v
			}
		}
	case models.ChannelSlack:
		// Use user_id as peer identifier for Slack
		if userID, ok := msg.Metadata["slack_user"].(string); ok && userID != "" {
			return userID
		}
	case models.ChannelDiscord:
		// Use user_id as peer identifier for Discord
		if userID, ok := msg.Metadata["discord_user_id"].(string); ok && userID != "" {
			return userID
		}
	case models.ChannelWhatsApp:
		if sender, ok := msg.Metadata["sender"].(string); ok && sender != "" {
			return sender
		}
	case models.ChannelSignal:
		if sender, ok := msg.Metadata["sender"].(string); ok && sender != "" {
			return sender
		}
	case models.ChannelIMessage:
		if sender, ok := msg.Metadata["sender"].(string); ok && sender != "" {
			return sender
		}
	case models.ChannelMatrix:
		if sender, ok := msg.Metadata["sender"].(string); ok && sender != "" {
			return sender
		}
	}
	return ""
}

// handleBroadcastMessage processes a message that should be broadcast to multiple agents.
func (s *Server) handleBroadcastMessage(ctx context.Context, peerID string, msg *models.Message, runtime *agent.Runtime) {
	s.logger.Debug("processing broadcast message",
		"peer_id", peerID,
		"channel", msg.Channel,
	)

	// Ensure broadcast manager has current runtime and sessions
	s.broadcastManager.runtime = runtime
	s.broadcastManager.sessions = s.sessions

	results, err := s.broadcastManager.ProcessBroadcast(
		ctx,
		peerID,
		msg,
		s.resolveConversationID,
		s.systemPromptForMessage,
	)
	if err != nil {
		s.logger.Error("broadcast processing failed", "error", err)
		return
	}

	adapter, ok := s.channels.GetOutbound(msg.Channel)
	if !ok {
		s.logger.Error("no adapter registered for channel", "channel", msg.Channel)
		return
	}

	// Send responses from all agents
	for _, result := range results {
		if result.Error != nil {
			s.logger.Error("agent broadcast error",
				"agent_id", result.AgentID,
				"error", result.Error,
			)
			continue
		}

		if result.Response == "" {
			continue
		}

		outbound := &models.Message{
			SessionID: result.SessionID,
			Channel:   msg.Channel,
			Direction: models.DirectionOutbound,
			Role:      models.RoleAssistant,
			Content:   result.Response,
			Metadata:  s.buildReplyMetadata(msg),
			CreatedAt: time.Now(),
		}

		// Add agent identifier to metadata so the recipient knows which agent responded
		if outbound.Metadata == nil {
			outbound.Metadata = make(map[string]any)
		}
		outbound.Metadata["broadcast_agent_id"] = result.AgentID

		if err := adapter.Send(ctx, outbound); err != nil {
			s.logger.Error("failed to send broadcast response",
				"agent_id", result.AgentID,
				"error", err,
			)
		}

		if s.memoryLogger != nil {
			if err := s.memoryLogger.Append(outbound); err != nil {
				s.logger.Error("failed to write memory log", "error", err)
			}
		}
	}
}

// waitForProcessing waits for all in-flight message processing to complete.
func (s *Server) waitForProcessing(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
