package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/haasonsaas/nexus/internal/audit"
	"github.com/haasonsaas/nexus/internal/canvas"
	"github.com/haasonsaas/nexus/pkg/models"
)

func (s *Server) handleCanvasAction(ctx context.Context, action canvas.Action) error {
	if s == nil || s.canvasManager == nil || s.canvasManager.Store() == nil {
		return fmt.Errorf("canvas manager unavailable")
	}
	canvasSession, err := s.canvasManager.Store().GetSession(ctx, action.SessionID)
	if err != nil {
		return err
	}
	msg, details, err := s.buildCanvasActionMessage(action, canvasSession)
	if err != nil {
		return err
	}

	if s.auditLogger != nil {
		s.auditLogger.Log(ctx, &audit.Event{
			Type:      audit.EventCanvasAction,
			Level:     audit.LevelInfo,
			Timestamp: time.Now(),
			SessionID: action.SessionID,
			Action:    "canvas.action",
			Details:   details,
			UserID:    action.UserID,
			Channel:   string(models.ChannelSlack),
		})
	}

	go s.handleMessage(context.Background(), msg)
	return nil
}

func (s *Server) buildCanvasActionMessage(action canvas.Action, session *canvas.Session) (*models.Message, map[string]any, error) {
	if session == nil {
		return nil, nil, fmt.Errorf("canvas session missing")
	}
	if session.ChannelID == "" {
		return nil, nil, fmt.Errorf("canvas session missing channel id")
	}
	contextValue := any(nil)
	if len(action.Context) > 0 {
		var decoded any
		if err := json.Unmarshal(action.Context, &decoded); err == nil {
			contextValue = decoded
		} else {
			contextValue = string(action.Context)
		}
	}
	details := map[string]any{
		"canvas_action_id":           action.ID,
		"canvas_action_name":         action.Name,
		"canvas_source_component_id": action.SourceComponentID,
		"canvas_context":             contextValue,
		"canvas_user_id":             action.UserID,
		"canvas_session_id":          action.SessionID,
	}

	payload := map[string]any{
		"type":                "canvas.action",
		"name":                action.Name,
		"id":                  action.ID,
		"source_component_id": action.SourceComponentID,
		"context":             contextValue,
		"user_id":             action.UserID,
		"session_id":          action.SessionID,
		"received_at":         action.ReceivedAt.Format(time.RFC3339),
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		payloadJSON = []byte("{}")
	}

	metadata := map[string]any{
		"slack_channel":      session.ChannelID,
		"slack_workspace_id": session.WorkspaceID,
		"canvas_action":      payload,
	}
	if session.ThreadTS != "" {
		metadata["slack_thread_ts"] = session.ThreadTS
	}
	if action.ID != "" {
		metadata["canvas_action_id"] = action.ID
	}
	if action.Name != "" {
		metadata["canvas_action_name"] = action.Name
	}
	if action.SourceComponentID != "" {
		metadata["canvas_source_component_id"] = action.SourceComponentID
	}
	if action.UserID != "" {
		metadata["canvas_user_id"] = action.UserID
	}

	msg := &models.Message{
		ID:        uuid.NewString(),
		Channel:   models.ChannelSlack,
		ChannelID: session.ChannelID,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   fmt.Sprintf("Canvas action: %s\n%s", action.Name, string(payloadJSON)),
		Metadata:  metadata,
		CreatedAt: time.Now(),
	}
	if msg.ChannelID == "" {
		msg.ChannelID = msg.ID
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}

	return msg, details, nil
}
