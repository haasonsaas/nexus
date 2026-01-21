package discord

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
)

// discordSession interface allows for mocking the Discord session
type discordSession interface {
	Open() error
	Close() error
	ChannelMessageSend(channelID string, content string, options ...discordgo.RequestOption) (*discordgo.Message, error)
	ChannelMessageSendComplex(channelID string, data *discordgo.MessageSend, options ...discordgo.RequestOption) (*discordgo.Message, error)
	MessageReactionAdd(channelID, messageID, emoji string, options ...discordgo.RequestOption) error
	ThreadStart(channelID, name string, typ discordgo.ChannelType, archiveDuration int, options ...discordgo.RequestOption) (*discordgo.Channel, error)
	AddHandler(handler interface{}) func()
	ApplicationCommandBulkOverwrite(appID, guildID string, commands []*discordgo.ApplicationCommand, options ...discordgo.RequestOption) ([]*discordgo.ApplicationCommand, error)
}

// Adapter implements the channels.Adapter interface for Discord.
type Adapter struct {
	token          string
	session        discordSession
	status         channels.Status
	messages       chan *models.Message
	mu             sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
	reconnectCount int
	maxBackoff     time.Duration
}

// NewAdapter creates a new Discord adapter.
func NewAdapter(token string) *Adapter {
	return &Adapter{
		token:      token,
		status:     channels.Status{Connected: false},
		messages:   make(chan *models.Message, 100),
		maxBackoff: 60 * time.Second,
	}
}

// Start begins listening for messages from Discord.
func (a *Adapter) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.status.Connected {
		return fmt.Errorf("adapter already started")
	}

	// Create a new session if not already set (for non-test cases)
	if a.session == nil {
		dg, err := discordgo.New("Bot " + a.token)
		if err != nil {
			return fmt.Errorf("failed to create Discord session: %w", err)
		}
		a.session = dg
	}

	// Set up event handlers
	a.session.AddHandler(a.handleMessageCreate)
	a.session.AddHandler(a.handleInteractionCreate)
	a.session.AddHandler(a.handleReady)
	a.session.AddHandler(a.handleDisconnect)

	// Open the connection with retry logic
	err := a.connectWithRetry(ctx)
	if err != nil {
		return fmt.Errorf("failed to connect to Discord: %w", err)
	}

	a.ctx, a.cancel = context.WithCancel(ctx)
	a.status.Connected = true
	a.status.Error = ""
	a.status.LastPing = time.Now().Unix()

	return nil
}

// Stop gracefully shuts down the adapter.
func (a *Adapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.status.Connected {
		return nil
	}

	if a.cancel != nil {
		a.cancel()
	}

	err := a.session.Close()
	if err != nil {
		a.status.Error = err.Error()
		return fmt.Errorf("failed to close Discord session: %w", err)
	}

	a.status.Connected = false
	close(a.messages)

	return nil
}

// Send delivers a message to Discord.
func (a *Adapter) Send(ctx context.Context, msg *models.Message) error {
	a.mu.RLock()
	connected := a.status.Connected
	a.mu.RUnlock()

	if !connected {
		return fmt.Errorf("adapter not connected")
	}

	// Extract Discord-specific metadata
	channelID, ok := msg.Metadata["discord_channel_id"].(string)
	if !ok || channelID == "" {
		return fmt.Errorf("missing discord_channel_id in metadata")
	}

	// Handle reactions
	if reactionEmoji, ok := msg.Metadata["discord_reaction_emoji"].(string); ok {
		if reactionMsgID, ok := msg.Metadata["discord_reaction_msg_id"].(string); ok {
			return a.session.MessageReactionAdd(channelID, reactionMsgID, reactionEmoji)
		}
	}

	// Handle thread creation
	if createThread, ok := msg.Metadata["discord_create_thread"].(bool); ok && createThread {
		threadName, _ := msg.Metadata["discord_thread_name"].(string)
		if threadName == "" {
			threadName = "Discussion"
		}
		thread, err := a.session.ThreadStart(channelID, threadName, discordgo.ChannelTypeGuildPublicThread, 1440) // 24 hours
		if err != nil {
			return fmt.Errorf("failed to create thread: %w", err)
		}
		// Send message to the new thread
		channelID = thread.ID
	}

	// Build message with embeds if specified
	embedTitle, hasEmbedTitle := msg.Metadata["discord_embed_title"].(string)
	embedColor, hasEmbedColor := msg.Metadata["discord_embed_color"].(int)
	embedDescription, hasEmbedDescription := msg.Metadata["discord_embed_description"].(string)

	if hasEmbedTitle || hasEmbedColor || hasEmbedDescription {
		// Send as embed
		embed := &discordgo.MessageEmbed{
			Title:       embedTitle,
			Description: embedDescription,
			Color:       embedColor,
		}
		if embed.Description == "" && msg.Content != "" {
			embed.Description = msg.Content
		}

		messageSend := &discordgo.MessageSend{
			Embeds: []*discordgo.MessageEmbed{embed},
		}

		_, err := a.session.ChannelMessageSendComplex(channelID, messageSend)
		return err
	}

	// Send as regular message
	if msg.Content != "" {
		_, err := a.session.ChannelMessageSend(channelID, msg.Content)
		return err
	}

	return nil
}

// Messages returns a channel of inbound messages.
func (a *Adapter) Messages() <-chan *models.Message {
	return a.messages
}

// Type returns the channel type.
func (a *Adapter) Type() models.ChannelType {
	return models.ChannelDiscord
}

// Status returns the current connection status.
func (a *Adapter) Status() channels.Status {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.status
}

// RegisterSlashCommands registers slash commands with Discord.
func (a *Adapter) RegisterSlashCommands(commands []*discordgo.ApplicationCommand, guildID string) error {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Get application ID from session
	dg, ok := a.session.(*discordgo.Session)
	if !ok {
		// In test mode, skip actual registration
		return nil
	}

	if dg.State == nil || dg.State.User == nil {
		return fmt.Errorf("session not ready, cannot register commands")
	}

	appID := dg.State.User.ID

	_, err := a.session.ApplicationCommandBulkOverwrite(appID, guildID, commands)
	if err != nil {
		return fmt.Errorf("failed to register slash commands: %w", err)
	}

	return nil
}

// Event handlers

func (a *Adapter) handleMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore messages from bots
	if m.Author.Bot {
		return
	}

	msg := convertDiscordMessage(m.Message)
	if msg == nil {
		return
	}

	select {
	case a.messages <- msg:
	case <-a.ctx.Done():
		return
	default:
		// Channel full, log or handle appropriately
	}
}

func (a *Adapter) handleInteractionCreate(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}

	// Convert slash command to message
	msg := &models.Message{
		Channel:   models.ChannelDiscord,
		ChannelID: i.ID,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Metadata: map[string]any{
			"discord_interaction_id":   i.ID,
			"discord_channel_id":       i.ChannelID,
			"discord_user_id":          i.Member.User.ID,
			"discord_username":         i.Member.User.Username,
			"discord_command_name":     i.ApplicationCommandData().Name,
			"discord_command_options":  i.ApplicationCommandData().Options,
		},
		CreatedAt: time.Now(),
	}

	// Build content from command
	cmdData := i.ApplicationCommandData()
	msg.Content = fmt.Sprintf("/%s", cmdData.Name)

	for _, opt := range cmdData.Options {
		msg.Content += fmt.Sprintf(" %s:%v", opt.Name, opt.Value)
	}

	select {
	case a.messages <- msg:
	case <-a.ctx.Done():
		return
	default:
		// Channel full
	}
}

func (a *Adapter) handleReady(s *discordgo.Session, r *discordgo.Ready) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.status.Connected = true
	a.status.Error = ""
	a.status.LastPing = time.Now().Unix()
	a.reconnectCount = 0 // Reset on successful connection
}

func (a *Adapter) handleDisconnect(s *discordgo.Session, d *discordgo.Disconnect) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.status.Connected = false
	a.status.Error = "disconnected from Discord"

	// Attempt reconnection in background
	go a.reconnect()
}

// Reconnection logic

func (a *Adapter) connectWithRetry(ctx context.Context) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = a.session.Open()
		if err == nil {
			return nil
		}

		backoff := calculateBackoff(attempt, a.maxBackoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			continue
		}
	}
	return fmt.Errorf("failed to connect after retries: %w", err)
}

func (a *Adapter) reconnect() {
	if a.ctx.Err() != nil {
		return // Context cancelled, don't reconnect
	}

	a.mu.Lock()
	a.reconnectCount++
	attempt := a.reconnectCount
	a.mu.Unlock()

	backoff := calculateBackoff(attempt, a.maxBackoff)
	time.Sleep(backoff)

	err := a.session.Open()
	a.mu.Lock()
	defer a.mu.Unlock()

	if err != nil {
		a.status.Error = fmt.Sprintf("reconnection attempt %d failed: %v", attempt, err)
		// Will retry on next disconnect event
	} else {
		a.status.Connected = true
		a.status.Error = ""
		a.status.LastPing = time.Now().Unix()
		a.reconnectCount = 0
	}
}

func calculateBackoff(attempt int, maxWait time.Duration) time.Duration {
	// Exponential backoff: 1s, 2s, 4s, 8s, 16s, ...
	backoff := time.Duration(1<<uint(attempt)) * time.Second
	if backoff > maxWait {
		backoff = maxWait
	}
	return backoff
}

// Message conversion

func convertDiscordMessage(m *discordgo.Message) *models.Message {
	if m == nil || m.Author == nil {
		return nil
	}

	msg := &models.Message{
		Channel:     models.ChannelDiscord,
		ChannelID:   m.ID,
		Direction:   models.DirectionInbound,
		Role:        models.RoleUser,
		Content:     m.Content,
		Attachments: make([]models.Attachment, 0, len(m.Attachments)),
		Metadata: map[string]any{
			"discord_channel_id": m.ChannelID,
			"discord_user_id":    m.Author.ID,
			"discord_username":   m.Author.Username,
		},
		CreatedAt: time.Now(),
	}

	// Use timestamp from Discord message
	if !m.Timestamp.IsZero() {
		msg.CreatedAt = m.Timestamp
	}

	// Convert attachments
	for _, att := range m.Attachments {
		msg.Attachments = append(msg.Attachments, models.Attachment{
			ID:       att.ID,
			Type:     detectAttachmentType(att.ContentType),
			URL:      att.URL,
			Filename: att.Filename,
			MimeType: att.ContentType,
			Size:     int64(att.Size),
		})
	}

	// Handle thread metadata
	if m.Thread != nil {
		msg.Metadata["discord_thread_id"] = m.Thread.ID
		msg.Metadata["discord_thread_name"] = m.Thread.Name
		msg.Metadata["discord_parent_id"] = m.Thread.ParentID
	}

	// Handle mentions
	if len(m.Mentions) > 0 {
		mentions := make([]string, len(m.Mentions))
		for i, user := range m.Mentions {
			mentions[i] = user.ID
		}
		msg.Metadata["discord_mentions"] = mentions
	}

	return msg
}

func detectAttachmentType(contentType string) string {
	if strings.HasPrefix(contentType, "image/") {
		return "image"
	}
	if strings.HasPrefix(contentType, "audio/") {
		return "audio"
	}
	if strings.HasPrefix(contentType, "video/") {
		return "video"
	}
	return "document"
}
