package signal

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/channels/personal"
	"github.com/haasonsaas/nexus/pkg/models"
)

// Adapter implements the Signal channel adapter using signal-cli.
type Adapter struct {
	*personal.BaseAdapter

	config *Config

	process *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser

	requestID atomic.Int64
	pending   map[int64]chan json.RawMessage
	pendingMu sync.Mutex

	cancelFunc context.CancelFunc
	wg         sync.WaitGroup
}

// New creates a new Signal adapter.
func New(cfg *Config, logger *slog.Logger) (*Adapter, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	if cfg.Account == "" {
		return nil, channels.ErrConfig("signal account (phone number) is required", nil)
	}

	// Verify signal-cli is available
	if _, err := exec.LookPath(cfg.SignalCLIPath); err != nil {
		return nil, channels.ErrNotFound(fmt.Sprintf("signal-cli not found at %q", cfg.SignalCLIPath), err)
	}

	adapter := &Adapter{
		BaseAdapter: personal.NewBaseAdapter(models.ChannelSignal, &cfg.Personal, logger),
		config:      cfg,
		pending:     make(map[int64]chan json.RawMessage),
	}

	return adapter, nil
}

// Start connects to Signal and begins listening for messages.
func (a *Adapter) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	a.cancelFunc = cancel

	// Build signal-cli command
	args := []string{
		"--output=json",
		"-a", a.config.Account,
	}

	if a.config.ConfigDir != "" {
		configDir := expandPath(a.config.ConfigDir)
		args = append(args, "--config", configDir)
	}

	args = append(args, "jsonRpc")

	a.process = exec.CommandContext(ctx, a.config.SignalCLIPath, args...)

	// Set up pipes
	var err error
	a.stdin, err = a.process.StdinPipe()
	if err != nil {
		return channels.ErrConnection("failed to create stdin pipe", err)
	}

	a.stdout, err = a.process.StdoutPipe()
	if err != nil {
		return channels.ErrConnection("failed to create stdout pipe", err)
	}

	a.stderr, err = a.process.StderrPipe()
	if err != nil {
		return channels.ErrConnection("failed to create stderr pipe", err)
	}

	// Start the process
	if err := a.process.Start(); err != nil {
		return channels.ErrConnection("failed to start signal-cli", err)
	}

	a.SetStatus(true, "")
	a.Logger().Info("started signal-cli",
		"account", a.config.Account)

	// Start message receiver
	a.wg.Add(2)
	go a.receiveLoop(ctx)
	go a.stderrLoop(ctx)

	return nil
}

// Stop disconnects from Signal.
func (a *Adapter) Stop(ctx context.Context) error {
	if a.cancelFunc != nil {
		a.cancelFunc()
	}

	if a.stdin != nil {
		a.stdin.Close()
	}

	if a.process != nil {
		if err := a.process.Wait(); err != nil {
			a.Logger().Debug("signal process wait returned error", "error", err)
		}
	}

	a.wg.Wait()
	a.SetStatus(false, "")
	a.BaseAdapter.Close()
	return nil
}

// Send sends a message through Signal.
func (a *Adapter) Send(ctx context.Context, msg *models.Message) error {
	peerID, ok := msg.Metadata["peer_id"].(string)
	if !ok || peerID == "" {
		msgID := ""
		if msg != nil {
			msgID = msg.ID
		}
		return channels.ErrInvalidInput(channels.MissingMetadata("peer_id", msgID), nil)
	}

	// Build send request
	params := map[string]any{
		"recipient": []string{peerID},
		"message":   msg.Content,
	}
	req := map[string]any{
		"method": "send",
		"params": params,
	}

	// Handle group messages
	if groupID, ok := msg.Metadata["group_id"].(string); ok && groupID != "" {
		params["groupId"] = groupID
		delete(params, "recipient")
	}

	// Send attachments
	var attachmentPaths []string
	for _, att := range msg.Attachments {
		if att.URL != "" {
			// Download to scratch file
			path, err := downloadToTempFile(ctx, att.URL)
			if err != nil {
				a.Logger().Error("failed to download attachment",
					"error", err,
					"url", att.URL)
				continue
			}
			attachmentPaths = append(attachmentPaths, path)
		}
	}

	// Clean up scratch files after send completes (or on error)
	defer func() {
		for _, path := range attachmentPaths {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				a.Logger().Debug("failed to remove scratch file", "path", path, "error", err)
			}
		}
	}()

	if len(attachmentPaths) > 0 {
		params["attachments"] = attachmentPaths
	}

	_, err := a.call(ctx, req)
	if err != nil {
		a.IncrementErrors()
		return channels.ErrConnection("failed to send message", err)
	}

	a.IncrementSent()
	return nil
}

// HealthCheck returns the adapter's health status.
func (a *Adapter) HealthCheck(ctx context.Context) channels.HealthStatus {
	start := time.Now()

	if a.process == nil {
		return channels.HealthStatus{
			Healthy:   false,
			Message:   "process not started",
			Latency:   time.Since(start),
			LastCheck: time.Now(),
		}
	}

	// Check if process is still running
	if a.process.ProcessState != nil && a.process.ProcessState.Exited() {
		return channels.HealthStatus{
			Healthy:   false,
			Message:   "process exited",
			Latency:   time.Since(start),
			LastCheck: time.Now(),
		}
	}

	return channels.HealthStatus{
		Healthy:   true,
		Message:   "running",
		Latency:   time.Since(start),
		LastCheck: time.Now(),
	}
}

// Contacts returns the contact manager.
func (a *Adapter) Contacts() personal.ContactManager {
	return &contactManager{adapter: a}
}

// Media returns the media handler.
func (a *Adapter) Media() personal.MediaHandler {
	return &personal.BaseMediaHandler{}
}

// Presence returns the presence manager.
func (a *Adapter) Presence() personal.PresenceManager {
	return &presenceManager{adapter: a}
}

// GetConversation returns a conversation by peer ID.
func (a *Adapter) GetConversation(ctx context.Context, peerID string) (*personal.Conversation, error) {
	return &personal.Conversation{
		ID:   peerID,
		Type: personal.ConversationDM,
	}, nil
}

// ListConversations lists conversations.
func (a *Adapter) ListConversations(ctx context.Context, opts personal.ListOptions) ([]*personal.Conversation, error) {
	return nil, nil
}

// receiveLoop reads and processes messages from signal-cli stdout.
func (a *Adapter) receiveLoop(ctx context.Context) {
	defer a.wg.Done()

	scanner := bufio.NewScanner(a.stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		a.processLine(line)
	}

	if err := scanner.Err(); err != nil {
		a.Logger().Error("scanner error", "error", err)
	}
}

// stderrLoop reads and logs stderr from signal-cli.
func (a *Adapter) stderrLoop(ctx context.Context) {
	defer a.wg.Done()

	scanner := bufio.NewScanner(a.stderr)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if line != "" {
			a.Logger().Warn("signal-cli stderr", "message", line)
		}
	}
}

// processLine handles a single JSON-RPC line from signal-cli.
func (a *Adapter) processLine(line string) {
	var msg jsonRPCMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		a.Logger().Error("failed to parse JSON-RPC message",
			"error", err,
			"line", line)
		return
	}

	// Handle responses to our requests
	if msg.ID != nil {
		a.pendingMu.Lock()
		if ch, ok := a.pending[*msg.ID]; ok {
			delete(a.pending, *msg.ID)
			a.pendingMu.Unlock()

			select {
			case ch <- msg.Result:
			default:
			}
			return
		}
		a.pendingMu.Unlock()
	}

	// Handle incoming messages (notifications)
	if msg.Method == "receive" {
		a.handleReceive(msg.Params)
	}
}

// handleReceive processes incoming Signal messages.
func (a *Adapter) handleReceive(params json.RawMessage) {
	var envelope signalEnvelope
	if err := json.Unmarshal(params, &envelope); err != nil {
		a.Logger().Error("failed to parse envelope", "error", err)
		return
	}

	// Skip non-data messages
	if envelope.DataMessage == nil {
		return
	}

	dm := envelope.DataMessage

	raw := personal.RawMessage{
		ID:        fmt.Sprintf("%d", envelope.Timestamp),
		Content:   dm.Message,
		PeerID:    envelope.Source,
		PeerName:  envelope.SourceName,
		Timestamp: time.UnixMilli(envelope.Timestamp),
	}

	// Handle group messages
	if dm.GroupInfo != nil {
		raw.GroupID = dm.GroupInfo.GroupID
		raw.GroupName = dm.GroupInfo.GroupName
	}

	// Handle attachments
	for _, att := range dm.Attachments {
		raw.Attachments = append(raw.Attachments, personal.RawAttachment{
			ID:       att.ID,
			MIMEType: att.ContentType,
			Filename: att.Filename,
			Size:     att.Size,
		})
	}

	// Handle quotes (replies)
	if dm.Quote != nil {
		raw.ReplyTo = fmt.Sprintf("%d", dm.Quote.ID)
	}

	msg := a.NormalizeInbound(raw)
	a.ProcessAttachments(raw, msg)
	a.Emit(msg)
}

// call sends a JSON-RPC request and waits for a response.
func (a *Adapter) call(ctx context.Context, req map[string]any) (json.RawMessage, error) {
	id := a.requestID.Add(1)
	req["jsonrpc"] = "2.0"
	req["id"] = id

	// Register response channel
	ch := make(chan json.RawMessage, 1)
	a.pendingMu.Lock()
	a.pending[id] = ch
	a.pendingMu.Unlock()

	defer func() {
		a.pendingMu.Lock()
		delete(a.pending, id)
		a.pendingMu.Unlock()
	}()

	// Send request
	data, err := json.Marshal(req)
	if err != nil {
		return nil, channels.ErrInternal("failed to marshal request", err)
	}

	if _, err := fmt.Fprintf(a.stdin, "%s\n", data); err != nil {
		return nil, channels.ErrConnection("failed to write request", err)
	}

	// Wait for response
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-ch:
		return result, nil
	case <-time.After(30 * time.Second):
		return nil, channels.ErrTimeout("request timeout", nil)
	}
}

// JSON-RPC types

type jsonRPCMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type signalEnvelope struct {
	Source      string             `json:"source"`
	SourceName  string             `json:"sourceName"`
	Timestamp   int64              `json:"timestamp"`
	DataMessage *signalDataMessage `json:"dataMessage"`
}

type signalDataMessage struct {
	Timestamp   int64              `json:"timestamp"`
	Message     string             `json:"message"`
	GroupInfo   *signalGroupInfo   `json:"groupInfo"`
	Attachments []signalAttachment `json:"attachments"`
	Quote       *signalQuote       `json:"quote"`
}

type signalGroupInfo struct {
	GroupID   string `json:"groupId"`
	GroupName string `json:"groupName"`
}

type signalAttachment struct {
	ID          string `json:"id"`
	ContentType string `json:"contentType"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
}

type signalQuote struct {
	ID     int64  `json:"id"`
	Author string `json:"author"`
	Text   string `json:"text"`
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

// downloadToTempFile downloads a URL to a scratch file.
func downloadToTempFile(ctx context.Context, url string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Create scratch file
	f, err := os.CreateTemp("", "signal-attachment-*")
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Download content
	data, err := downloadURL(ctx, url)
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}

	if _, err := f.Write(data); err != nil {
		os.Remove(f.Name())
		return "", err
	}

	return f.Name(), nil
}

// SendTypingIndicator sends a typing indicator to the recipient.
// This is part of the StreamingAdapter interface.
func (a *Adapter) SendTypingIndicator(ctx context.Context, msg *models.Message) error {
	peerID, ok := msg.Metadata["peer_id"].(string)
	if !ok || peerID == "" {
		// Can't send typing without peer ID, but don't error
		return nil
	}

	// Build sendTyping request
	params := map[string]any{
		"recipient": []string{peerID},
	}

	// Handle group messages
	if groupID, ok := msg.Metadata["group_id"].(string); ok && groupID != "" {
		params["groupId"] = groupID
		delete(params, "recipient")
	}

	req := map[string]any{
		"method": "sendTyping",
		"params": params,
	}

	// Send typing indicator (don't fail if it doesn't work)
	if _, err := a.call(ctx, req); err != nil {
		a.Logger().Debug("failed to send typing indicator", "error", err)
	}

	return nil
}

// StartStreamingResponse is a stub for Signal as it doesn't support message editing.
// This is part of the StreamingAdapter interface.
func (a *Adapter) StartStreamingResponse(ctx context.Context, msg *models.Message) (string, error) {
	// Signal doesn't support message editing, so we can't do true streaming.
	// Return empty string to indicate streaming is not available.
	return "", nil
}

// UpdateStreamingResponse is a no-op for Signal as sent messages cannot be edited.
// This is part of the StreamingAdapter interface.
func (a *Adapter) UpdateStreamingResponse(ctx context.Context, msg *models.Message, messageID string, content string) error {
	// Signal doesn't support editing sent messages
	return nil
}
