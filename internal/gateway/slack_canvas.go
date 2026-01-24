package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/canvas"
	"github.com/haasonsaas/nexus/internal/channels/slack"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/pkg/models"
)

func (s *Server) configureSlackCanvas() {
	if s == nil || s.channels == nil || s.config == nil {
		return
	}
	if !s.config.Channels.Slack.Canvas.Enabled {
		return
	}
	adapter, ok := s.channels.Get(models.ChannelSlack)
	if !ok {
		return
	}
	slackAdapter, ok := adapter.(*slack.Adapter)
	if !ok {
		return
	}
	if s.canvasHost == nil || s.canvasManager == nil || s.canvasManager.Store() == nil {
		s.logger.Warn("slack canvas enabled but canvas host or store missing")
		return
	}

	slackAdapter.SetCanvasLinkProvider(func(ctx context.Context, req slack.CanvasLinkRequest) (string, error) {
		return s.buildSlackCanvasLink(ctx, req)
	})
}

func (s *Server) buildSlackCanvasLink(ctx context.Context, req slack.CanvasLinkRequest) (string, error) {
	if s == nil || s.canvasHost == nil || s.canvasManager == nil || s.canvasManager.Store() == nil {
		return "", fmt.Errorf("canvas unavailable")
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	channelID := strings.TrimSpace(req.ChannelID)
	threadTS := strings.TrimSpace(req.ThreadTS)
	if workspaceID == "" || channelID == "" {
		return "", fmt.Errorf("canvas requires workspace and channel")
	}
	sessionKey := buildSlackCanvasSessionKey(workspaceID, channelID, threadTS)
	session, err := ensureCanvasSession(ctx, s.canvasManager.Store(), sessionKey, workspaceID, channelID, threadTS)
	if err != nil {
		return "", err
	}
	role := resolveSlackCanvasRole(s.config.Channels.Slack.Canvas, req)
	signed, err := s.canvasHost.SignedSessionURL(canvas.CanvasURLParams{}, session.ID, role, req.UserID)
	if err != nil {
		if errors.Is(err, canvas.ErrTokenInvalid) {
			return s.canvasHost.CanvasSessionURL(canvas.CanvasURLParams{}, session.ID), nil
		}
		return "", err
	}
	return signed, nil
}

func buildSlackCanvasSessionKey(workspaceID, channelID, threadTS string) string {
	key := fmt.Sprintf("slack:%s:%s", workspaceID, channelID)
	if strings.TrimSpace(threadTS) != "" {
		key += ":" + strings.TrimSpace(threadTS)
	}
	return key
}

func resolveSlackCanvasRole(cfg config.SlackCanvasConfig, req slack.CanvasLinkRequest) string {
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	userID := strings.TrimSpace(req.UserID)
	if workspaceID != "" && userID != "" {
		if users := cfg.UserRoles[workspaceID]; len(users) > 0 {
			if role, ok := users[userID]; ok {
				return strings.TrimSpace(role)
			}
		}
	}
	if workspaceID != "" {
		if role, ok := cfg.WorkspaceRoles[workspaceID]; ok {
			return strings.TrimSpace(role)
		}
	}
	if strings.TrimSpace(cfg.DefaultRole) != "" {
		return strings.TrimSpace(cfg.DefaultRole)
	}
	if strings.TrimSpace(cfg.Role) != "" {
		return strings.TrimSpace(cfg.Role)
	}
	return "editor"
}

func ensureCanvasSession(ctx context.Context, store canvas.Store, key string, workspaceID string, channelID string, threadTS string) (*canvas.Session, error) {
	if store == nil {
		return nil, fmt.Errorf("canvas store unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if existing, err := store.GetSessionByKey(ctx, key); err == nil {
		return existing, nil
	} else if !errors.Is(err, canvas.ErrNotFound) {
		return nil, err
	}

	now := time.Now()
	session := &canvas.Session{
		Key:         key,
		WorkspaceID: workspaceID,
		ChannelID:   channelID,
		ThreadTS:    threadTS,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := store.CreateSession(ctx, session); err != nil {
		if errors.Is(err, canvas.ErrAlreadyExists) {
			return store.GetSessionByKey(ctx, key)
		}
		return nil, err
	}
	return session, nil
}
