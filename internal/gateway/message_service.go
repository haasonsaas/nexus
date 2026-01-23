// Package gateway provides the main Nexus gateway server.
//
// message_service.go implements the MessageService gRPC handlers for proactive messaging.
package gateway

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
	proto "github.com/haasonsaas/nexus/pkg/proto"
)

// messageService implements the proto.MessageServiceServer interface.
type messageService struct {
	proto.UnimplementedMessageServiceServer
	server *Server
}

// newMessageService creates a new message service handler.
func newMessageService(s *Server) *messageService {
	return &messageService{server: s}
}

// SendMessage sends a message to a specific channel/peer without requiring an inbound message.
func (s *messageService) SendMessage(ctx context.Context, req *proto.ProactiveSendRequest) (*proto.ProactiveSendResponse, error) {
	if req.Channel == "" {
		return nil, fmt.Errorf("channel is required")
	}
	if req.PeerId == "" {
		return nil, fmt.Errorf("peer_id is required")
	}
	if req.Content == "" {
		return nil, fmt.Errorf("content is required")
	}

	// Get the outbound adapter for this channel
	channelType := models.ChannelType(req.Channel)
	adapter, ok := s.server.channels.GetOutbound(channelType)
	if !ok {
		return nil, fmt.Errorf("channel %s not found or doesn't support outbound messages", req.Channel)
	}

	// Create the message
	msg := &models.Message{
		ID:        uuid.NewString(),
		Channel:   channelType,
		ChannelID: req.PeerId,
		Direction: models.DirectionOutbound,
		Role:      models.RoleAssistant,
		Content:   req.Content,
		CreatedAt: time.Now(),
	}

	// Handle session
	sessionID := req.SessionId
	if sessionID == "" && s.server.sessions != nil {
		// Create or get session for this peer
		agentID := req.AgentId
		if agentID == "" {
			agentID = "default"
		}
		key := sessions.SessionKey(agentID, channelType, req.PeerId)
		session, err := s.server.sessions.GetOrCreate(ctx, key, agentID, channelType, req.PeerId)
		if err != nil {
			s.server.logger.Warn("failed to create session for proactive message",
				"channel", req.Channel,
				"peer_id", req.PeerId,
				"error", err,
			)
		} else {
			sessionID = session.ID
			msg.SessionID = sessionID
		}
	} else {
		msg.SessionID = sessionID
	}

	// Apply metadata
	if len(req.Metadata) > 0 {
		msg.Metadata = make(map[string]any)
		for k, v := range req.Metadata {
			msg.Metadata[k] = v
		}
	}

	// Convert attachments
	if len(req.Attachments) > 0 {
		msg.Attachments = make([]models.Attachment, 0, len(req.Attachments))
		for _, a := range req.Attachments {
			msg.Attachments = append(msg.Attachments, models.Attachment{
				ID:       a.Id,
				Type:     a.Type,
				URL:      a.Url,
				Filename: a.Filename,
				MimeType: a.MimeType,
				Size:     a.Size,
			})
		}
	}

	// Send the message
	if err := adapter.Send(ctx, msg); err != nil {
		return &proto.ProactiveSendResponse{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	s.server.logger.Info("proactive message sent",
		"channel", req.Channel,
		"peer_id", req.PeerId,
		"message_id", msg.ID,
		"session_id", sessionID,
	)

	// Store the message if we have a session
	if sessionID != "" && s.server.sessions != nil {
		if err := s.server.sessions.AppendMessage(ctx, sessionID, msg); err != nil {
			s.server.logger.Warn("failed to store proactive message",
				"session_id", sessionID,
				"error", err,
			)
		}
	}

	return &proto.ProactiveSendResponse{
		Success:   true,
		MessageId: msg.ID,
		SessionId: sessionID,
	}, nil
}

// BroadcastMessage sends a message to multiple recipients.
func (s *messageService) BroadcastMessage(ctx context.Context, req *proto.BroadcastMessageRequest) (*proto.BroadcastMessageResponse, error) {
	if req.Channel == "" {
		return nil, fmt.Errorf("channel is required")
	}
	if len(req.PeerIds) == 0 {
		return nil, fmt.Errorf("peer_ids is required")
	}
	if req.Content == "" {
		return nil, fmt.Errorf("content is required")
	}

	// Get the outbound adapter for this channel
	channelType := models.ChannelType(req.Channel)
	adapter, ok := s.server.channels.GetOutbound(channelType)
	if !ok {
		return nil, fmt.Errorf("channel %s not found or doesn't support outbound messages", req.Channel)
	}

	// Send to all peers concurrently
	var wg sync.WaitGroup
	results := make([]*proto.BroadcastResult, len(req.PeerIds))
	var mu sync.Mutex

	// Limit concurrency
	sem := make(chan struct{}, 10)

	for i, peerID := range req.PeerIds {
		wg.Add(1)
		go func(idx int, pid string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			result := &proto.BroadcastResult{
				PeerId: pid,
			}

			// Create the message
			msg := &models.Message{
				ID:        uuid.NewString(),
				Channel:   channelType,
				ChannelID: pid,
				Direction: models.DirectionOutbound,
				Role:      models.RoleAssistant,
				Content:   req.Content,
				CreatedAt: time.Now(),
			}

			// Apply metadata
			if len(req.Metadata) > 0 {
				msg.Metadata = make(map[string]any)
				for k, v := range req.Metadata {
					msg.Metadata[k] = v
				}
			}

			// Convert attachments
			if len(req.Attachments) > 0 {
				msg.Attachments = make([]models.Attachment, 0, len(req.Attachments))
				for _, a := range req.Attachments {
					msg.Attachments = append(msg.Attachments, models.Attachment{
						ID:       a.Id,
						Type:     a.Type,
						URL:      a.Url,
						Filename: a.Filename,
						MimeType: a.MimeType,
						Size:     a.Size,
					})
				}
			}

			// Send the message
			if err := adapter.Send(ctx, msg); err != nil {
				result.Success = false
				result.Error = err.Error()
			} else {
				result.Success = true
				result.MessageId = msg.ID
			}

			mu.Lock()
			results[idx] = result
			mu.Unlock()
		}(i, peerID)
	}

	wg.Wait()

	// Count successes and failures
	var successCount, failureCount int32
	for _, r := range results {
		if r.Success {
			successCount++
		} else {
			failureCount++
		}
	}

	s.server.logger.Info("broadcast message completed",
		"channel", req.Channel,
		"total", len(req.PeerIds),
		"success", successCount,
		"failure", failureCount,
	)

	return &proto.BroadcastMessageResponse{
		SuccessCount: successCount,
		FailureCount: failureCount,
		Results:      results,
	}, nil
}

// SendProactiveMessage is a helper function for internal use to send a proactive message.
// This is useful for task executors and other internal components.
func (s *Server) SendProactiveMessage(ctx context.Context, channel models.ChannelType, peerID, content string) error {
	adapter, ok := s.channels.GetOutbound(channel)
	if !ok {
		return fmt.Errorf("channel %s not found or doesn't support outbound messages", channel)
	}

	msg := &models.Message{
		ID:        uuid.NewString(),
		Channel:   channel,
		ChannelID: peerID,
		Direction: models.DirectionOutbound,
		Role:      models.RoleAssistant,
		Content:   content,
		CreatedAt: time.Now(),
	}

	return adapter.Send(ctx, msg)
}

// MessageExecutor is a task executor that sends messages directly via channels.
// Unlike AgentExecutor which processes through the LLM, this sends messages directly.
type MessageExecutor struct {
	registry *channels.Registry
}

// NewMessageExecutor creates a new executor that sends messages directly.
func NewMessageExecutor(registry *channels.Registry) *MessageExecutor {
	return &MessageExecutor{registry: registry}
}
