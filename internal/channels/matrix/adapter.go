package matrix

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

// Adapter implements the channels.Adapter interface for Matrix.
type Adapter struct {
	config  *Config
	client  *mautrix.Client
	logger  *slog.Logger
	metrics *channels.Metrics

	messages chan *models.Message
	errors   chan error

	allowedRooms map[string]bool
	allowedUsers map[string]bool

	mu      sync.RWMutex
	running bool
	stopCh  chan struct{}
}

// NewAdapter creates a new Matrix adapter.
func NewAdapter(cfg Config) (*Adapter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Create Matrix client
	client, err := mautrix.NewClient(cfg.Homeserver, id.UserID(cfg.UserID), cfg.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("create matrix client: %w", err)
	}

	if cfg.DeviceID != "" {
		client.DeviceID = id.DeviceID(cfg.DeviceID)
	}

	a := &Adapter{
		config:   &cfg,
		client:   client,
		logger:   cfg.Logger.With("adapter", "matrix"),
		metrics:  channels.NewMetrics("matrix"),
		messages: make(chan *models.Message, 100),
		errors:   make(chan error, 10),
		stopCh:   make(chan struct{}),
	}

	// Build allowed rooms/users maps
	if len(cfg.AllowedRooms) > 0 {
		a.allowedRooms = make(map[string]bool)
		for _, room := range cfg.AllowedRooms {
			a.allowedRooms[room] = true
		}
	}

	if len(cfg.AllowedUsers) > 0 {
		a.allowedUsers = make(map[string]bool)
		for _, user := range cfg.AllowedUsers {
			a.allowedUsers[user] = true
		}
	}

	return a, nil
}

// Type returns the channel type.
func (a *Adapter) Type() models.ChannelType {
	return models.ChannelType("matrix")
}

// Start begins the Matrix adapter.
func (a *Adapter) Start(ctx context.Context) error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return nil
	}
	a.running = true
	a.mu.Unlock()

	// Register event handlers
	syncer := a.client.Syncer.(*mautrix.DefaultSyncer)

	// Handle room messages
	syncer.OnEventType(event.EventMessage, func(ctx context.Context, evt *event.Event) {
		a.handleMessage(ctx, evt)
	})

	// Handle room invites
	if a.config.JoinOnInvite {
		syncer.OnEventType(event.StateMember, func(ctx context.Context, evt *event.Event) {
			a.handleMemberEvent(ctx, evt)
		})
	}

	// Start sync in background
	go a.syncLoop(ctx)

	a.logger.Info("matrix adapter started",
		"homeserver", a.config.Homeserver,
		"user_id", a.config.UserID)

	return nil
}

// Stop stops the Matrix adapter.
func (a *Adapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	if !a.running {
		a.mu.Unlock()
		return nil
	}
	a.running = false
	close(a.stopCh)
	a.mu.Unlock()

	// Stop sync
	a.client.StopSync()

	a.logger.Info("matrix adapter stopped")
	return nil
}

// Messages returns the message channel.
func (a *Adapter) Messages() <-chan *models.Message {
	return a.messages
}

// Errors returns the error channel.
func (a *Adapter) Errors() <-chan error {
	return a.errors
}

// Send sends a message to a Matrix room.
func (a *Adapter) Send(ctx context.Context, msg *models.Message) error {
	if msg == nil {
		return channels.ErrInvalidInput("message is nil", nil)
	}

	roomID := id.RoomID(msg.ChannelID)
	if roomID == "" {
		return channels.ErrInvalidInput("room_id is required", nil)
	}

	start := time.Now()

	// Build message content
	content := &event.MessageEventContent{
		MsgType: event.MsgText,
		Body:    msg.Content,
	}

	// Use formatted body for markdown
	if strings.Contains(msg.Content, "**") || strings.Contains(msg.Content, "```") {
		content.Format = event.FormatHTML
		content.FormattedBody = markdownToHTML(msg.Content)
	}

	// Handle reply - get reply ID from metadata if present
	if msg.Metadata != nil {
		if replyTo, ok := msg.Metadata["reply_to"].(string); ok && replyTo != "" {
			content.RelatesTo = &event.RelatesTo{
				InReplyTo: &event.InReplyTo{
					EventID: id.EventID(replyTo),
				},
			}
		}
	}

	// Send message
	resp, err := a.client.SendMessageEvent(ctx, roomID, event.EventMessage, content)
	if err != nil {
		a.metrics.RecordError(channels.ErrCodeInternal)
		return channels.ErrInternal(fmt.Sprintf("send message to %s", roomID), err)
	}

	a.metrics.RecordMessageSent()
	a.metrics.RecordSendLatency(time.Since(start))

	a.logger.Debug("sent message",
		"room_id", roomID,
		"event_id", resp.EventID)

	return nil
}

// SendReaction sends a reaction to a message.
func (a *Adapter) SendReaction(ctx context.Context, roomID, eventID, reaction string) error {
	content := &event.ReactionEventContent{
		RelatesTo: event.RelatesTo{
			Type:    event.RelAnnotation,
			EventID: id.EventID(eventID),
			Key:     reaction,
		},
	}

	_, err := a.client.SendMessageEvent(ctx, id.RoomID(roomID), event.EventReaction, content)
	if err != nil {
		return channels.ErrInternal("send reaction", err)
	}

	return nil
}

// Healthy returns the health status.
func (a *Adapter) Healthy(ctx context.Context) bool {
	// Try a simple API call
	_, err := a.client.Whoami(ctx)
	return err == nil
}

// Status returns adapter status information.
func (a *Adapter) Status() map[string]any {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return map[string]any{
		"running":     a.running,
		"homeserver":  a.config.Homeserver,
		"user_id":     a.config.UserID,
		"device_id":   a.client.DeviceID,
		"metrics":     a.metrics.Snapshot(),
	}
}

func (a *Adapter) syncLoop(ctx context.Context) {
	for {
		select {
		case <-a.stopCh:
			return
		case <-ctx.Done():
			return
		default:
			err := a.client.SyncWithContext(ctx)
			if err != nil {
				a.logger.Error("sync error", "error", err)
				select {
				case a.errors <- err:
				default:
				}

				// Backoff before retry
				select {
				case <-time.After(5 * time.Second):
				case <-a.stopCh:
					return
				case <-ctx.Done():
					return
				}
			}
		}
	}
}

func (a *Adapter) handleMessage(ctx context.Context, evt *event.Event) {
	// Ignore own messages
	if a.config.IgnoreOwnMessages && string(evt.Sender) == a.config.UserID {
		return
	}

	// Check allowed rooms
	if a.allowedRooms != nil && !a.allowedRooms[string(evt.RoomID)] {
		return
	}

	// Check allowed users
	if a.allowedUsers != nil && !a.allowedUsers[string(evt.Sender)] {
		return
	}

	content, ok := evt.Content.Parsed.(*event.MessageEventContent)
	if !ok {
		return
	}

	// Only handle text messages for now
	if content.MsgType != event.MsgText && content.MsgType != event.MsgNotice {
		return
	}

	metadata := map[string]any{
		"event_type": evt.Type.Type,
		"room_id":    evt.RoomID,
		"sender":     string(evt.Sender),
		"msg_type":   content.MsgType,
	}

	// Handle reply context
	if content.RelatesTo != nil && content.RelatesTo.InReplyTo != nil {
		metadata["reply_to"] = string(content.RelatesTo.InReplyTo.EventID)
	}

	msg := &models.Message{
		ID:        string(evt.ID),
		Channel:   models.ChannelType("matrix"),
		ChannelID: string(evt.RoomID),
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   content.Body,
		CreatedAt: time.UnixMilli(evt.Timestamp),
		Metadata:  metadata,
	}

	a.metrics.RecordMessageReceived()

	select {
	case a.messages <- msg:
	default:
		a.logger.Warn("message channel full, dropping message",
			"event_id", evt.ID)
	}
}

func (a *Adapter) handleMemberEvent(ctx context.Context, evt *event.Event) {
	content, ok := evt.Content.Parsed.(*event.MemberEventContent)
	if !ok {
		return
	}

	// Check if this is an invite to us
	if content.Membership == event.MembershipInvite &&
		evt.GetStateKey() == a.config.UserID {
		a.logger.Info("received room invite", "room_id", evt.RoomID)

		// Auto-join
		_, err := a.client.JoinRoom(ctx, string(evt.RoomID), nil)
		if err != nil {
			a.logger.Error("failed to join room",
				"room_id", evt.RoomID,
				"error", err)
		} else {
			a.logger.Info("joined room", "room_id", evt.RoomID)
		}
	}
}

// markdownToHTML performs basic markdown to HTML conversion.
func markdownToHTML(text string) string {
	// Basic conversion for bold and code blocks
	text = strings.ReplaceAll(text, "**", "<strong>")
	text = strings.ReplaceAll(text, "```", "<pre><code>")
	return text
}
