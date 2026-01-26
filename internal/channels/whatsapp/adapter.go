package whatsapp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/channels/personal"
	"github.com/haasonsaas/nexus/pkg/models"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"

	_ "github.com/mattn/go-sqlite3" // SQLite driver for whatsmeow
)

// Adapter implements the WhatsApp channel adapter using whatsmeow.
type Adapter struct {
	*personal.BaseAdapter

	config *Config
	client *whatsmeow.Client
	store  *sqlstore.Container
	device *store.Device

	qrChan    chan string
	connected bool
	connMu    sync.RWMutex

	cancelFunc context.CancelFunc
	wg         sync.WaitGroup

	// Conversation tracking for ListConversations
	conversations   map[string]*trackedConversation
	conversationsMu sync.RWMutex

	mediaCache map[string]mediaEntry
	mediaMu    sync.RWMutex
}

// trackedConversation tracks metadata about a conversation.
type trackedConversation struct {
	ID          string
	Type        personal.ConversationType
	LastMessage time.Time
	Name        string
}

type mediaEntry struct {
	data     []byte
	mimeType string
	filename string
	path     string
}

// New creates a new WhatsApp adapter.
func New(cfg *Config, logger *slog.Logger) (*Adapter, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Expand session path
	sessionPath := expandPath(cfg.SessionPath)
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0755); err != nil {
		return nil, channels.ErrConfig("failed to create session directory", err)
	}

	// Initialize SQLite store with timeout to prevent indefinite blocking
	dbLog := waLog.Noop
	initCtx, initCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer initCancel()
	container, err := sqlstore.New(initCtx, "sqlite3",
		fmt.Sprintf("file:%s?_foreign_keys=on", sessionPath),
		dbLog)
	if err != nil {
		return nil, channels.ErrConnection("failed to create store", err)
	}

	adapter := &Adapter{
		BaseAdapter:   personal.NewBaseAdapter(models.ChannelWhatsApp, &cfg.Personal, logger),
		config:        cfg,
		store:         container,
		qrChan:        make(chan string, 1),
		conversations: make(map[string]*trackedConversation),
		mediaCache:    make(map[string]mediaEntry),
	}

	return adapter, nil
}

// Start connects to WhatsApp and begins listening for messages.
func (a *Adapter) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	a.cancelFunc = cancel

	// Get or create device
	device, err := a.store.GetFirstDevice(ctx)
	if err != nil {
		return channels.ErrConnection("failed to get device", err)
	}
	a.device = device

	// Create client
	clientLog := waLog.Noop
	a.client = whatsmeow.NewClient(device, clientLog)
	a.client.AddEventHandler(a.handleEvent)

	// Connect
	if a.client.Store.ID == nil {
		// Not logged in - need QR code
		qrChan, err := a.client.GetQRChannel(ctx)
		if err != nil {
			return channels.ErrAuthentication("failed to get QR channel", err)
		}
		if err := a.client.Connect(); err != nil {
			return channels.ErrConnection("failed to connect", err)
		}

		// Handle QR code events with context cancellation
		a.wg.Add(1)
		go func() {
			defer a.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case evt, ok := <-qrChan:
					if !ok {
						return
					}
					if evt.Event == "code" {
						a.Logger().Info("scan QR code to login",
							"code", evt.Code)
						select {
						case a.qrChan <- evt.Code:
						default:
						}
					}
				}
			}
		}()
	} else {
		// Already logged in
		if err := a.client.Connect(); err != nil {
			return channels.ErrConnection("failed to connect", err)
		}
	}

	return nil
}

// Stop disconnects from WhatsApp.
func (a *Adapter) Stop(ctx context.Context) error {
	if a.cancelFunc != nil {
		a.cancelFunc()
	}

	// Wait for goroutines to exit before closing resources
	a.wg.Wait()

	// Close qrChan to unblock any receivers
	if a.qrChan != nil {
		close(a.qrChan)
	}

	if a.client != nil {
		a.client.Disconnect()
	}
	// Close the SQLite store to release database connection
	if a.store != nil {
		if err := a.store.Close(); err != nil {
			a.Logger().Warn("failed to close store", "error", err)
		}
	}
	a.SetStatus(false, "")
	a.BaseAdapter.Close()
	return nil
}

// Send sends a message through WhatsApp.
func (a *Adapter) Send(ctx context.Context, msg *models.Message) error {
	if !a.isConnected() {
		return channels.ErrUnavailable("not connected to WhatsApp", nil)
	}

	peerID, ok := msg.Metadata["peer_id"].(string)
	if !ok || peerID == "" {
		return channels.ErrInvalidInput("missing peer_id in message metadata", nil)
	}

	jid, err := types.ParseJID(peerID)
	if err != nil {
		return channels.ErrInvalidInput(fmt.Sprintf("invalid peer ID %q", peerID), err)
	}

	// Send text message
	if msg.Content != "" {
		waMsg := &waE2E.Message{
			Conversation: proto.String(msg.Content),
		}
		_, err = a.client.SendMessage(ctx, jid, waMsg)
		if err != nil {
			a.IncrementErrors()
			return channels.ErrConnection("failed to send message", err)
		}
		a.IncrementSent()
	}

	// Send attachments
	for _, att := range msg.Attachments {
		if err := a.sendAttachment(ctx, jid, att); err != nil {
			a.Logger().Error("failed to send attachment",
				"error", err,
				"attachment_id", att.ID)
		}
	}

	return nil
}

// HealthCheck returns the adapter's health status.
func (a *Adapter) HealthCheck(ctx context.Context) channels.HealthStatus {
	start := time.Now()

	if a.client == nil {
		return channels.HealthStatus{
			Healthy:   false,
			Message:   "client not initialized",
			Latency:   time.Since(start),
			LastCheck: time.Now(),
		}
	}

	if !a.client.IsConnected() {
		return channels.HealthStatus{
			Healthy:   false,
			Message:   "not connected",
			Latency:   time.Since(start),
			LastCheck: time.Now(),
		}
	}

	return channels.HealthStatus{
		Healthy:   true,
		Message:   "connected",
		Latency:   time.Since(start),
		LastCheck: time.Now(),
	}
}

// QRChannel returns a channel that receives QR codes for pairing.
func (a *Adapter) QRChannel() <-chan string {
	return a.qrChan
}

// Contacts returns the contact manager.
func (a *Adapter) Contacts() personal.ContactManager {
	return &contactManager{adapter: a}
}

// Media returns the media handler.
func (a *Adapter) Media() personal.MediaHandler {
	return &mediaHandler{adapter: a}
}

// Presence returns the presence manager.
func (a *Adapter) Presence() personal.PresenceManager {
	return &presenceManager{adapter: a}
}

// GetConversation returns a conversation by peer ID.
func (a *Adapter) GetConversation(ctx context.Context, peerID string) (*personal.Conversation, error) {
	jid, err := types.ParseJID(peerID)
	if err != nil {
		return nil, channels.ErrInvalidInput("invalid peer ID", err)
	}

	convType := personal.ConversationDM
	if jid.Server == types.GroupServer {
		convType = personal.ConversationGroup
	}

	return &personal.Conversation{
		ID:   peerID,
		Type: convType,
	}, nil
}

// ErrNotImplemented indicates the operation is not implemented for this adapter.
var ErrNotImplemented = errors.New("operation not implemented")

// ListConversations lists conversations tracked from message history.
func (a *Adapter) ListConversations(ctx context.Context, opts personal.ListOptions) ([]*personal.Conversation, error) {
	a.conversationsMu.RLock()
	defer a.conversationsMu.RUnlock()

	// Collect all tracked conversations
	conversations := make([]*personal.Conversation, 0, len(a.conversations))
	for _, tracked := range a.conversations {
		// Filter by group if specified
		if opts.GroupID != "" {
			if tracked.Type != personal.ConversationGroup {
				continue
			}
			if tracked.ID != opts.GroupID {
				continue
			}
		}

		// Filter by time if specified
		if !opts.After.IsZero() && tracked.LastMessage.Before(opts.After) {
			continue
		}
		if !opts.Before.IsZero() && tracked.LastMessage.After(opts.Before) {
			continue
		}

		conversations = append(conversations, &personal.Conversation{
			ID:        tracked.ID,
			Type:      tracked.Type,
			UpdatedAt: tracked.LastMessage,
		})
	}

	// Sort by last message time (most recent first)
	sortConversationsByTime(conversations, a.conversations)

	// Apply offset and limit
	if opts.Offset > 0 && opts.Offset < len(conversations) {
		conversations = conversations[opts.Offset:]
	} else if opts.Offset >= len(conversations) {
		return []*personal.Conversation{}, nil
	}

	if opts.Limit > 0 && len(conversations) > opts.Limit {
		conversations = conversations[:opts.Limit]
	}

	return conversations, nil
}

// sortConversationsByTime sorts conversations by last message time descending.
func sortConversationsByTime(convs []*personal.Conversation, tracked map[string]*trackedConversation) {
	// Simple bubble sort for typically small lists
	for i := 0; i < len(convs)-1; i++ {
		for j := i + 1; j < len(convs); j++ {
			ti := tracked[convs[i].ID]
			tj := tracked[convs[j].ID]
			if ti != nil && tj != nil && tj.LastMessage.After(ti.LastMessage) {
				convs[i], convs[j] = convs[j], convs[i]
			}
		}
	}
}

// trackConversation records a conversation from an incoming/outgoing message.
func (a *Adapter) trackConversation(jid types.JID, isGroup bool) {
	a.conversationsMu.Lock()
	defer a.conversationsMu.Unlock()

	convID := jid.String()
	convType := personal.ConversationDM
	if isGroup {
		convType = personal.ConversationGroup
	}

	if existing, ok := a.conversations[convID]; ok {
		existing.LastMessage = time.Now()
	} else {
		a.conversations[convID] = &trackedConversation{
			ID:          convID,
			Type:        convType,
			LastMessage: time.Now(),
		}
	}
}

// handleEvent processes WhatsApp events.
func (a *Adapter) handleEvent(evt interface{}) {
	switch v := evt.(type) {
	case *events.Connected:
		a.connMu.Lock()
		a.connected = true
		a.connMu.Unlock()
		a.SetStatus(true, "")
		a.Logger().Info("connected to WhatsApp")

	case *events.Disconnected:
		a.connMu.Lock()
		a.connected = false
		a.connMu.Unlock()
		a.SetStatus(false, "disconnected")
		a.Logger().Warn("disconnected from WhatsApp")

	case *events.Message:
		a.handleMessage(v)

	case *events.Receipt:
		a.handleReceipt(v)

	case *events.Presence:
		a.handlePresence(v)

	case *events.LoggedOut:
		a.connMu.Lock()
		a.connected = false
		a.connMu.Unlock()
		a.SetStatus(false, "logged out")
		a.Logger().Warn("logged out from WhatsApp",
			"reason", v.Reason)
	}
}

// handleMessage processes incoming messages.
func (a *Adapter) handleMessage(evt *events.Message) {
	// Skip status broadcasts
	if evt.Info.Chat.Server == "broadcast" {
		return
	}

	// Track the conversation for ListConversations
	isGroup := evt.Info.Chat.Server == "g.us"
	a.trackConversation(evt.Info.Chat, isGroup)

	var content string
	var attachments []personal.RawAttachment

	// Extract message content based on type
	if evt.Message.Conversation != nil {
		content = *evt.Message.Conversation
	} else if evt.Message.ExtendedTextMessage != nil {
		content = evt.Message.ExtendedTextMessage.GetText()
	} else if evt.Message.ImageMessage != nil {
		content = evt.Message.ImageMessage.GetCaption()
		if att := a.downloadImage(evt); att != nil {
			attachments = append(attachments, *att)
		}
	} else if evt.Message.DocumentMessage != nil {
		content = evt.Message.DocumentMessage.GetCaption()
		if att := a.downloadDocument(evt); att != nil {
			attachments = append(attachments, *att)
		}
	} else if evt.Message.AudioMessage != nil {
		if att := a.downloadAudio(evt); att != nil {
			attachments = append(attachments, *att)
		}
	} else if evt.Message.VideoMessage != nil {
		content = evt.Message.VideoMessage.GetCaption()
		if att := a.downloadVideo(evt); att != nil {
			attachments = append(attachments, *att)
		}
	}

	// Skip empty messages
	if content == "" && len(attachments) == 0 {
		return
	}

	raw := personal.RawMessage{
		ID:          evt.Info.ID,
		Content:     content,
		PeerID:      evt.Info.Sender.String(),
		PeerName:    a.getContactName(evt.Info.Sender),
		Timestamp:   evt.Info.Timestamp,
		Attachments: attachments,
	}

	if evt.Info.IsGroup {
		raw.GroupID = evt.Info.Chat.String()
		raw.GroupName = a.getGroupName(evt.Info.Chat)
	}

	msg := a.NormalizeInbound(raw)
	a.ProcessAttachments(raw, msg)
	a.Emit(msg)
}

// handleReceipt processes message receipts (read/delivered).
func (a *Adapter) handleReceipt(evt *events.Receipt) {
	a.Logger().Debug("received receipt",
		"type", evt.Type,
		"from", evt.Chat.String(),
		"message_ids", evt.MessageIDs)
}

// handlePresence processes presence updates.
func (a *Adapter) handlePresence(evt *events.Presence) {
	a.Logger().Debug("presence update",
		"from", evt.From.String(),
		"available", evt.Unavailable,
		"last_seen", evt.LastSeen)
}

// getContactName retrieves a contact's display name.
func (a *Adapter) getContactName(jid types.JID) string {
	// Use a timeout context to prevent indefinite blocking during shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	contact, err := a.client.Store.Contacts.GetContact(ctx, jid)
	if err == nil && contact.FullName != "" {
		return contact.FullName
	}
	if contact.PushName != "" {
		return contact.PushName
	}
	return jid.User
}

// getGroupName retrieves a group's name.
func (a *Adapter) getGroupName(jid types.JID) string {
	// Use a timeout context to prevent indefinite blocking during shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	group, err := a.client.GetGroupInfo(ctx, jid)
	if err == nil && group.Name != "" {
		return group.Name
	}
	return jid.User
}

// isConnected returns whether the adapter is connected.
func (a *Adapter) isConnected() bool {
	a.connMu.RLock()
	defer a.connMu.RUnlock()
	return a.connected
}

// sendAttachment uploads and sends an attachment.
func (a *Adapter) sendAttachment(ctx context.Context, jid types.JID, att models.Attachment) error {
	// Download attachment data
	data, err := downloadURL(att.URL)
	if err != nil {
		return channels.ErrConnection("failed to download attachment", err)
	}

	mimeType := att.MimeType
	if mimeType == "" {
		mimeType = att.Type
	}

	// Determine upload type based on MIME type
	var uploadType whatsmeow.MediaType
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		uploadType = whatsmeow.MediaImage
	case strings.HasPrefix(mimeType, "video/"):
		uploadType = whatsmeow.MediaVideo
	case strings.HasPrefix(mimeType, "audio/"):
		uploadType = whatsmeow.MediaAudio
	default:
		uploadType = whatsmeow.MediaDocument
	}

	// Upload to WhatsApp
	uploaded, err := a.client.Upload(ctx, data, uploadType)
	if err != nil {
		return channels.ErrConnection("failed to upload", err)
	}

	// Create and send message based on type
	var waMsg *waE2E.Message

	switch uploadType {
	case whatsmeow.MediaImage:
		waMsg = &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{
				URL:           &uploaded.URL,
				DirectPath:    &uploaded.DirectPath,
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    &uploaded.FileLength,
				Mimetype:      &mimeType,
			},
		}
	case whatsmeow.MediaVideo:
		waMsg = &waE2E.Message{
			VideoMessage: &waE2E.VideoMessage{
				URL:           &uploaded.URL,
				DirectPath:    &uploaded.DirectPath,
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    &uploaded.FileLength,
				Mimetype:      &mimeType,
			},
		}
	case whatsmeow.MediaAudio:
		waMsg = &waE2E.Message{
			AudioMessage: &waE2E.AudioMessage{
				URL:           &uploaded.URL,
				DirectPath:    &uploaded.DirectPath,
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    &uploaded.FileLength,
				Mimetype:      &mimeType,
			},
		}
	default:
		filename := att.Filename
		if filename == "" {
			filename = "document"
		}
		waMsg = &waE2E.Message{
			DocumentMessage: &waE2E.DocumentMessage{
				URL:           &uploaded.URL,
				DirectPath:    &uploaded.DirectPath,
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    &uploaded.FileLength,
				Mimetype:      &mimeType,
				FileName:      &filename,
			},
		}
	}

	_, err = a.client.SendMessage(ctx, jid, waMsg)
	if err != nil {
		return channels.ErrConnection("failed to send attachment message", err)
	}

	a.IncrementSent()
	return nil
}

func (a *Adapter) cacheMedia(att *personal.RawAttachment) {
	if att == nil || att.ID == "" || len(att.Data) == 0 {
		return
	}
	if att.Size == 0 {
		att.Size = int64(len(att.Data))
	}
	_, err := a.storeMedia(att.ID, att.Data, att.MIMEType, att.Filename)
	if err != nil {
		a.Logger().Warn("failed to store media", "error", err, "media_id", att.ID)
	}
}

func (a *Adapter) storeMedia(mediaID string, data []byte, mimeType string, filename string) (string, error) {
	if a == nil || mediaID == "" || len(data) == 0 {
		return "", fmt.Errorf("invalid media data")
	}
	entry := mediaEntry{
		data:     data,
		mimeType: strings.TrimSpace(mimeType),
		filename: filename,
	}
	path, err := a.persistMedia(mediaID, data, filename)
	if err == nil {
		entry.path = path
	}
	a.mediaMu.Lock()
	if a.mediaCache == nil {
		a.mediaCache = make(map[string]mediaEntry)
	}
	a.mediaCache[mediaID] = entry
	a.mediaMu.Unlock()
	return path, err
}

func (a *Adapter) persistMedia(mediaID string, data []byte, filename string) (string, error) {
	if a == nil || a.config == nil {
		return "", nil
	}
	root := strings.TrimSpace(a.config.MediaPath)
	if root == "" {
		return "", nil
	}
	root = expandPath(root)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	name := mediaFilename(mediaID, filename)
	path := filepath.Join(root, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func mediaFilename(mediaID string, filename string) string {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(mediaID))
	ext := filepath.Ext(filename)
	if ext != "" {
		return encoded + ext
	}
	return encoded
}

func (a *Adapter) getMedia(mediaID string) (mediaEntry, bool) {
	if a == nil {
		return mediaEntry{}, false
	}
	a.mediaMu.RLock()
	entry, ok := a.mediaCache[mediaID]
	a.mediaMu.RUnlock()
	return entry, ok
}

// downloadImage downloads an image attachment.
func (a *Adapter) downloadImage(evt *events.Message) *personal.RawAttachment {
	img := evt.Message.ImageMessage
	if img == nil {
		return nil
	}

	// Use a timeout context for media downloads (30 seconds max)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	data, err := a.client.Download(ctx, img)
	if err != nil {
		a.Logger().Error("failed to download image", "error", err)
		return nil
	}

	att := &personal.RawAttachment{
		ID:       evt.Info.ID,
		MIMEType: img.GetMimetype(),
		Data:     data,
	}
	a.cacheMedia(att)
	return att
}

// downloadDocument downloads a document attachment.
func (a *Adapter) downloadDocument(evt *events.Message) *personal.RawAttachment {
	doc := evt.Message.DocumentMessage
	if doc == nil {
		return nil
	}

	// Use a timeout context for media downloads (30 seconds max)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	data, err := a.client.Download(ctx, doc)
	if err != nil {
		a.Logger().Error("failed to download document", "error", err)
		return nil
	}

	att := &personal.RawAttachment{
		ID:       evt.Info.ID,
		MIMEType: doc.GetMimetype(),
		Filename: doc.GetFileName(),
		Data:     data,
	}
	a.cacheMedia(att)
	return att
}

// downloadAudio downloads an audio attachment.
func (a *Adapter) downloadAudio(evt *events.Message) *personal.RawAttachment {
	audio := evt.Message.AudioMessage
	if audio == nil {
		return nil
	}

	// Use a timeout context for media downloads (30 seconds max)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	data, err := a.client.Download(ctx, audio)
	if err != nil {
		a.Logger().Error("failed to download audio", "error", err)
		return nil
	}

	att := &personal.RawAttachment{
		ID:       evt.Info.ID,
		MIMEType: audio.GetMimetype(),
		Data:     data,
	}
	a.cacheMedia(att)
	return att
}

// downloadVideo downloads a video attachment.
func (a *Adapter) downloadVideo(evt *events.Message) *personal.RawAttachment {
	video := evt.Message.VideoMessage
	if video == nil {
		return nil
	}

	// Use a timeout context for media downloads (30 seconds max)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	data, err := a.client.Download(ctx, video)
	if err != nil {
		a.Logger().Error("failed to download video", "error", err)
		return nil
	}

	att := &personal.RawAttachment{
		ID:       evt.Info.ID,
		MIMEType: video.GetMimetype(),
		Data:     data,
	}
	a.cacheMedia(att)
	return att
}

// expandPath expands ~ to home directory.
func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// SendTypingIndicator sends a typing indicator to the recipient.
// This is part of the StreamingAdapter interface.
func (a *Adapter) SendTypingIndicator(ctx context.Context, msg *models.Message) error {
	if !a.isConnected() {
		return nil
	}

	peerID, ok := msg.Metadata["peer_id"].(string)
	if !ok || peerID == "" {
		return nil
	}

	jid, err := types.ParseJID(peerID)
	if err != nil {
		return nil
	}

	// Send composing presence (typing indicator)
	if err := a.client.SendChatPresence(ctx, jid, types.ChatPresenceComposing, types.ChatPresenceMediaText); err != nil {
		a.Logger().Debug("failed to send typing indicator", "error", err)
	}

	return nil
}

// StartStreamingResponse is a stub for WhatsApp as it doesn't support message editing.
// This is part of the StreamingAdapter interface.
func (a *Adapter) StartStreamingResponse(ctx context.Context, msg *models.Message) (string, error) {
	// WhatsApp doesn't support message editing, so we can't do true streaming.
	// Return empty string to indicate streaming is not available.
	return "", nil
}

// UpdateStreamingResponse is a no-op for WhatsApp as sent messages cannot be edited.
// This is part of the StreamingAdapter interface.
func (a *Adapter) UpdateStreamingResponse(ctx context.Context, msg *models.Message, messageID string, content string) error {
	// WhatsApp doesn't support editing sent messages
	return nil
}
