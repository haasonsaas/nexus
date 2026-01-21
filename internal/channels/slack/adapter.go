package slack

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

// Config holds the configuration for the Slack adapter.
type Config struct {
	BotToken string // xoxb- token for API calls
	AppToken string // xapp- token for Socket Mode
}

// Adapter implements the channels.Adapter interface for Slack.
type Adapter struct {
	cfg            Config
	client         *slack.Client
	socketClient   *socketmode.Client
	messages       chan *models.Message
	status         channels.Status
	statusMu       sync.RWMutex
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	botUserID      string
	botUserIDMu    sync.RWMutex
}

// NewAdapter creates a new Slack adapter.
func NewAdapter(cfg Config) *Adapter {
	client := slack.New(
		cfg.BotToken,
		slack.OptionAppLevelToken(cfg.AppToken),
	)

	socketClient := socketmode.New(
		client,
		socketmode.OptionDebug(false),
	)

	return &Adapter{
		cfg:          cfg,
		client:       client,
		socketClient: socketClient,
		messages:     make(chan *models.Message, 100),
		status: channels.Status{
			Connected: false,
		},
	}
}

// Start begins listening for messages from Slack via Socket Mode.
func (a *Adapter) Start(ctx context.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx)

	// Get bot user ID for mention detection
	authResp, err := a.client.AuthTest()
	if err != nil {
		return fmt.Errorf("failed to authenticate with Slack: %w", err)
	}
	a.botUserIDMu.Lock()
	a.botUserID = authResp.UserID
	a.botUserIDMu.Unlock()

	log.Printf("Slack adapter started for bot user ID: %s", authResp.UserID)

	// Start event handler goroutine
	a.wg.Add(1)
	go a.handleEvents()

	// Start Socket Mode connection in a goroutine
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if err := a.socketClient.Run(); err != nil {
			a.updateStatus(false, fmt.Sprintf("Socket mode error: %v", err))
			log.Printf("Socket mode error: %v", err)
		}
	}()

	a.updateStatus(true, "")
	return nil
}

// Stop gracefully shuts down the adapter.
func (a *Adapter) Stop(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}

	// Close the messages channel
	close(a.messages)

	// Wait for goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		a.updateStatus(false, "")
		return nil
	case <-ctx.Done():
		a.updateStatus(false, "shutdown timeout")
		return ctx.Err()
	}
}

// Send delivers a message to Slack.
func (a *Adapter) Send(ctx context.Context, msg *models.Message) error {
	// Extract channel and thread timestamp from message metadata
	channelID, ok := msg.Metadata["slack_channel"].(string)
	if !ok {
		return fmt.Errorf("missing slack_channel in message metadata")
	}

	options := buildBlockKitMessage(msg)

	// Add thread timestamp if this is a reply
	if threadTS, ok := msg.Metadata["slack_thread_ts"].(string); ok && threadTS != "" {
		options = append(options, slack.MsgOptionTS(threadTS))
	}

	// Send the message
	channel, timestamp, err := a.client.PostMessageContext(ctx, channelID, options...)
	if err != nil {
		return fmt.Errorf("failed to send Slack message: %w", err)
	}

	log.Printf("Sent message to Slack channel %s: %s", channel, timestamp)

	// Handle attachments (file uploads)
	if len(msg.Attachments) > 0 {
		for _, att := range msg.Attachments {
			// For now, we just log that we would upload files
			// In a real implementation, you'd need the file data, not just URLs
			log.Printf("Would upload file: %s (%s)", att.Filename, att.URL)
		}
	}

	// Handle reactions if specified in metadata
	if reaction, ok := msg.Metadata["slack_reaction"].(string); ok && reaction != "" {
		msgRef := slack.ItemRef{
			Channel:   channelID,
			Timestamp: timestamp,
		}
		if err := a.client.AddReactionContext(ctx, reaction, msgRef); err != nil {
			log.Printf("Failed to add reaction: %v", err)
		}
	}

	return nil
}

// Messages returns a channel of inbound messages.
func (a *Adapter) Messages() <-chan *models.Message {
	return a.messages
}

// Type returns the channel type.
func (a *Adapter) Type() models.ChannelType {
	return models.ChannelSlack
}

// Status returns the current connection status.
func (a *Adapter) Status() channels.Status {
	a.statusMu.RLock()
	defer a.statusMu.RUnlock()
	return a.status
}

// handleEvents processes incoming Socket Mode events.
func (a *Adapter) handleEvents() {
	defer a.wg.Done()

	for {
		select {
		case <-a.ctx.Done():
			return
		case event, ok := <-a.socketClient.Events:
			if !ok {
				return
			}

			// Update last ping timestamp
			a.statusMu.Lock()
			a.status.LastPing = time.Now().Unix()
			a.statusMu.Unlock()

			switch event.Type {
			case socketmode.EventTypeConnecting:
				log.Println("Slack: Connecting to Socket Mode...")

			case socketmode.EventTypeConnectionError:
				log.Printf("Slack: Connection error: %v", event.Data)
				a.updateStatus(false, "connection error")

			case socketmode.EventTypeConnected:
				log.Println("Slack: Connected to Socket Mode")
				a.updateStatus(true, "")

			case socketmode.EventTypeEventsAPI:
				a.handleEventsAPI(event)

			case socketmode.EventTypeSlashCommand:
				// Acknowledge slash commands (implement if needed)
				a.socketClient.Ack(*event.Request)

			case socketmode.EventTypeInteractive:
				// Acknowledge interactive events (implement if needed)
				a.socketClient.Ack(*event.Request)
			}
		}
	}
}

// handleEventsAPI processes Events API callbacks.
func (a *Adapter) handleEventsAPI(event socketmode.Event) {
	eventsAPIEvent, ok := event.Data.(slackevents.EventsAPIEvent)
	if !ok {
		log.Printf("Slack: Could not type cast event to EventsAPIEvent: %v", event.Data)
		a.socketClient.Ack(*event.Request)
		return
	}

	// Acknowledge the event
	a.socketClient.Ack(*event.Request)

	switch eventsAPIEvent.Type {
	case slackevents.CallbackEvent:
		innerEvent := eventsAPIEvent.InnerEvent
		switch ev := innerEvent.Data.(type) {
		case *slackevents.AppMentionEvent:
			a.handleAppMention(ev)

		case *slackevents.MessageEvent:
			// Filter out bot messages and message subtypes we don't care about
			if ev.BotID != "" {
				return
			}
			if ev.SubType != "" && ev.SubType != "file_share" {
				return
			}

			a.handleMessage(ev)
		}
	}
}

// handleAppMention processes app mention events (@bot mentions).
func (a *Adapter) handleAppMention(event *slackevents.AppMentionEvent) {
	// Convert to MessageEvent for unified handling
	msgEvent := &slackevents.MessageEvent{
		Type:            "message",
		User:            event.User,
		Text:            event.Text,
		Channel:         event.Channel,
		TimeStamp:       event.TimeStamp,
		ThreadTimeStamp: event.ThreadTimeStamp,
	}

	a.handleMessage(msgEvent)
}

// handleMessage processes incoming messages.
func (a *Adapter) handleMessage(event *slackevents.MessageEvent) {
	// Check if this is a DM or if bot is mentioned
	a.botUserIDMu.RLock()
	botUserID := a.botUserID
	a.botUserIDMu.RUnlock()

	isDM := strings.HasPrefix(event.Channel, "D")
	isMention := strings.Contains(event.Text, fmt.Sprintf("<@%s>", botUserID))

	// Only process DMs, mentions, or thread replies
	if !isDM && !isMention && event.ThreadTimeStamp == "" {
		return
	}

	// Convert Slack message to unified format
	msg := convertSlackMessage(event, a.cfg.BotToken)

	// Send to messages channel
	select {
	case a.messages <- msg:
	case <-a.ctx.Done():
	default:
		log.Printf("Slack: Messages channel full, dropping message")
	}
}

// updateStatus updates the connection status.
func (a *Adapter) updateStatus(connected bool, errMsg string) {
	a.statusMu.Lock()
	defer a.statusMu.Unlock()
	a.status.Connected = connected
	a.status.Error = errMsg
	if connected {
		a.status.LastPing = time.Now().Unix()
	}
}

// convertSlackMessage converts a Slack message event to the unified message format.
func convertSlackMessage(event *slackevents.MessageEvent, botToken string) *models.Message {
	// Remove bot mention from text
	text := event.Text
	// Remove <@USERID> mentions
	for strings.Contains(text, "<@") {
		start := strings.Index(text, "<@")
		end := strings.Index(text[start:], ">")
		if end == -1 {
			break
		}
		text = text[:start] + text[start+end+1:]
	}
	text = strings.TrimSpace(text)

	// Create message ID from channel and timestamp
	msgID := fmt.Sprintf("%s:%s", event.Channel, event.TimeStamp)

	// Generate session ID for threaded conversations
	sessionID := ""
	threadTS := event.ThreadTimeStamp
	if threadTS == "" {
		threadTS = event.TimeStamp // Use message timestamp if not in thread
	}
	sessionID = generateSessionID(event.Channel, threadTS)

	// Parse timestamp
	createdAt := time.Now()
	if ts, err := parseSlackTimestamp(event.TimeStamp); err == nil {
		createdAt = ts
	}

	msg := &models.Message{
		ID:        msgID,
		SessionID: sessionID,
		Channel:   models.ChannelSlack,
		ChannelID: msgID,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   text,
		Metadata: map[string]any{
			"slack_user_id":   event.User,
			"slack_channel":   event.Channel,
			"slack_ts":        event.TimeStamp,
			"slack_thread_ts": event.ThreadTimeStamp,
		},
		CreatedAt: createdAt,
	}

	// Process file attachments from the Message field if present
	if event.Message != nil && len(event.Message.Files) > 0 {
		attachments := make([]models.Attachment, 0, len(event.Message.Files))
		for _, file := range event.Message.Files {
			att := models.Attachment{
				ID:       file.ID,
				Type:     getAttachmentType(file.Mimetype),
				URL:      file.URLPrivateDownload,
				Filename: file.Name,
				MimeType: file.Mimetype,
				Size:     int64(file.Size),
			}
			attachments = append(attachments, att)
		}
		msg.Attachments = attachments
	}

	return msg
}

// buildBlockKitMessage creates Block Kit formatted message options.
func buildBlockKitMessage(msg *models.Message) []slack.MsgOption {
	options := []slack.MsgOption{}

	// Add text content as a section block
	if msg.Content != "" {
		textBlock := slack.NewTextBlockObject("mrkdwn", msg.Content, false, false)
		sectionBlock := slack.NewSectionBlock(textBlock, nil, nil)
		options = append(options, slack.MsgOptionBlocks(sectionBlock))
	}

	// Add attachments as context blocks
	if len(msg.Attachments) > 0 {
		for _, att := range msg.Attachments {
			// For images, use image blocks
			if att.Type == "image" {
				imageBlock := slack.NewImageBlock(att.URL, att.Filename, "", nil)
				options = append(options, slack.MsgOptionBlocks(imageBlock))
			} else {
				// For other files, add as context
				contextText := fmt.Sprintf("📎 %s (%s)", att.Filename, att.MimeType)
				contextBlock := slack.NewContextBlock("",
					slack.NewTextBlockObject("mrkdwn", contextText, false, false),
				)
				options = append(options, slack.MsgOptionBlocks(contextBlock))
			}
		}
	}

	// If no blocks were added, fall back to simple text
	if len(options) == 0 && msg.Content != "" {
		options = append(options, slack.MsgOptionText(msg.Content, false))
	}

	return options
}

// getAttachmentType determines the attachment type from MIME type.
func getAttachmentType(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	default:
		return "document"
	}
}

// generateSessionID creates a deterministic session ID for a Slack thread.
func generateSessionID(channel, threadTS string) string {
	data := fmt.Sprintf("slack:%s:%s", channel, threadTS)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// parseSlackTimestamp converts a Slack timestamp string to time.Time.
func parseSlackTimestamp(ts string) (time.Time, error) {
	// Slack timestamps are in the format "1234567890.123456"
	parts := strings.Split(ts, ".")
	if len(parts) != 2 {
		return time.Time{}, fmt.Errorf("invalid timestamp format: %s", ts)
	}

	var sec, nsec int64
	_, err := fmt.Sscanf(ts, "%d.%d", &sec, &nsec)
	if err != nil {
		return time.Time{}, err
	}

	// Convert microseconds to nanoseconds
	nsec = nsec * 1000

	return time.Unix(sec, nsec), nil
}
