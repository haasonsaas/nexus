package whatsapp

import (
	"context"
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
}

// New creates a new WhatsApp adapter.
func New(cfg *Config, logger *slog.Logger) (*Adapter, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Expand session path
	sessionPath := expandPath(cfg.SessionPath)
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create session directory: %w", err)
	}

	// Initialize SQLite store
	dbLog := waLog.Noop
	container, err := sqlstore.New(context.Background(), "sqlite3",
		fmt.Sprintf("file:%s?_foreign_keys=on", sessionPath),
		dbLog)
	if err != nil {
		return nil, fmt.Errorf("failed to create store: %w", err)
	}

	adapter := &Adapter{
		BaseAdapter: personal.NewBaseAdapter(models.ChannelWhatsApp, &cfg.Personal, logger),
		config:      cfg,
		store:       container,
		qrChan:      make(chan string, 1),
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
		return fmt.Errorf("failed to get device: %w", err)
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
			return fmt.Errorf("failed to get QR channel: %w", err)
		}
		if err := a.client.Connect(); err != nil {
			return fmt.Errorf("failed to connect: %w", err)
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
			return fmt.Errorf("failed to connect: %w", err)
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
		return fmt.Errorf("not connected to WhatsApp")
	}

	peerID, ok := msg.Metadata["peer_id"].(string)
	if !ok || peerID == "" {
		return fmt.Errorf("missing peer_id in message metadata")
	}

	jid, err := types.ParseJID(peerID)
	if err != nil {
		return fmt.Errorf("invalid peer ID %q: %w", peerID, err)
	}

	// Send text message
	if msg.Content != "" {
		waMsg := &waE2E.Message{
			Conversation: proto.String(msg.Content),
		}
		_, err = a.client.SendMessage(ctx, jid, waMsg)
		if err != nil {
			a.IncrementErrors()
			return fmt.Errorf("failed to send message: %w", err)
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
		return nil, fmt.Errorf("invalid peer ID: %w", err)
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

// ListConversations lists conversations.
func (a *Adapter) ListConversations(ctx context.Context, opts personal.ListOptions) ([]*personal.Conversation, error) {
	// whatsmeow doesn't have a direct conversation list API
	// This would need to be implemented with local state tracking
	return nil, fmt.Errorf("ListConversations: %w", ErrNotImplemented)
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
	ctx := context.Background()
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
	ctx := context.Background()
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
		return fmt.Errorf("failed to download attachment: %w", err)
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
		return fmt.Errorf("failed to upload: %w", err)
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
		return fmt.Errorf("failed to send attachment message: %w", err)
	}

	a.IncrementSent()
	return nil
}

// downloadImage downloads an image attachment.
func (a *Adapter) downloadImage(evt *events.Message) *personal.RawAttachment {
	img := evt.Message.ImageMessage
	if img == nil {
		return nil
	}

	ctx := context.Background()
	data, err := a.client.Download(ctx, img)
	if err != nil {
		a.Logger().Error("failed to download image", "error", err)
		return nil
	}

	return &personal.RawAttachment{
		ID:       evt.Info.ID,
		MIMEType: img.GetMimetype(),
		Data:     data,
	}
}

// downloadDocument downloads a document attachment.
func (a *Adapter) downloadDocument(evt *events.Message) *personal.RawAttachment {
	doc := evt.Message.DocumentMessage
	if doc == nil {
		return nil
	}

	ctx := context.Background()
	data, err := a.client.Download(ctx, doc)
	if err != nil {
		a.Logger().Error("failed to download document", "error", err)
		return nil
	}

	return &personal.RawAttachment{
		ID:       evt.Info.ID,
		MIMEType: doc.GetMimetype(),
		Filename: doc.GetFileName(),
		Data:     data,
	}
}

// downloadAudio downloads an audio attachment.
func (a *Adapter) downloadAudio(evt *events.Message) *personal.RawAttachment {
	audio := evt.Message.AudioMessage
	if audio == nil {
		return nil
	}

	ctx := context.Background()
	data, err := a.client.Download(ctx, audio)
	if err != nil {
		a.Logger().Error("failed to download audio", "error", err)
		return nil
	}

	return &personal.RawAttachment{
		ID:       evt.Info.ID,
		MIMEType: audio.GetMimetype(),
		Data:     data,
	}
}

// downloadVideo downloads a video attachment.
func (a *Adapter) downloadVideo(evt *events.Message) *personal.RawAttachment {
	video := evt.Message.VideoMessage
	if video == nil {
		return nil
	}

	ctx := context.Background()
	data, err := a.client.Download(ctx, video)
	if err != nil {
		a.Logger().Error("failed to download video", "error", err)
		return nil
	}

	return &personal.RawAttachment{
		ID:       evt.Info.ID,
		MIMEType: video.GetMimetype(),
		Data:     data,
	}
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
