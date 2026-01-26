# Nexus Personal Messaging Channels Design

## Overview

This document specifies the design for personal messaging channel adapters (WhatsApp, Signal, iMessage) in Nexus, using a shared base abstraction with protocol-specific implementations.

## Goals

1. **Unified abstraction**: Common interface for personal messaging protocols
2. **Go-native**: Native Go implementations where possible (whatsmeow for WhatsApp)
3. **Subprocess bridges**: Clean integration for non-Go tools (signal-cli)
4. **macOS integration**: Native iMessage support via Messages.app

---

## 1. Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     PersonalChannelAdapter                       │
│   (shared utilities: contact resolution, media handling, etc.)  │
└─────────────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
      ┌───────────┐   ┌───────────┐   ┌───────────┐
      │ WhatsApp  │   │  Signal   │   │ iMessage  │
      │ (whatsmeow)│   │(signal-cli)│   │ (native)  │
      └───────────┘   └───────────┘   └───────────┘
```

---

## 2. Shared Base Adapter

### 2.1 Interface

```go
// internal/channels/personal/adapter.go

// PersonalChannelAdapter extends ChannelAdapter with personal messaging features
type PersonalChannelAdapter interface {
    pluginsdk.ChannelAdapter
    pluginsdk.InboundAdapter
    pluginsdk.OutboundAdapter
    pluginsdk.LifecycleAdapter
    pluginsdk.HealthAdapter

    // Personal messaging features
    Contacts() ContactManager
    Media() MediaHandler
    Presence() PresenceManager  // Online/typing indicators

    // Conversation management
    GetConversation(ctx context.Context, peerID string) (*Conversation, error)
    ListConversations(ctx context.Context, opts ListOptions) ([]*Conversation, error)
}

type ContactManager interface {
    Resolve(ctx context.Context, identifier string) (*Contact, error)
    Search(ctx context.Context, query string) ([]*Contact, error)
    Sync(ctx context.Context) error
}

type MediaHandler interface {
    Download(ctx context.Context, mediaID string) ([]byte, string, error)  // data, mimeType
    Upload(ctx context.Context, data []byte, mimeType string) (string, error)  // mediaID
}

type PresenceManager interface {
    SetTyping(ctx context.Context, peerID string, typing bool) error
    SetOnline(ctx context.Context, online bool) error
    Subscribe(ctx context.Context, peerID string) (<-chan PresenceEvent, error)
}
```

### 2.2 Shared Types

```go
// internal/channels/personal/types.go

type Contact struct {
    ID          string            `json:"id"`
    Name        string            `json:"name"`
    Phone       string            `json:"phone,omitempty"`
    Email       string            `json:"email,omitempty"`
    Avatar      string            `json:"avatar,omitempty"`
    Verified    bool              `json:"verified"`
    Extra       map[string]any    `json:"extra"`
}

type Conversation struct {
    ID           string            `json:"id"`
    Type         ConversationType  `json:"type"`
    Name         string            `json:"name,omitempty"`
    Participants []*Contact        `json:"participants"`
    LastMessage  *models.Message   `json:"last_message,omitempty"`
    UnreadCount  int               `json:"unread_count"`
    Muted        bool              `json:"muted"`
    Pinned       bool              `json:"pinned"`
    CreatedAt    time.Time         `json:"created_at"`
    UpdatedAt    time.Time         `json:"updated_at"`
}

type ConversationType string

const (
    ConversationDM    ConversationType = "dm"
    ConversationGroup ConversationType = "group"
)

type PresenceEvent struct {
    PeerID    string
    Type      PresenceType  // online, offline, typing, stopped_typing
    Timestamp time.Time
}

type PresenceType string

const (
    PresenceOnline        PresenceType = "online"
    PresenceOffline       PresenceType = "offline"
    PresenceTyping        PresenceType = "typing"
    PresenceStoppedTyping PresenceType = "stopped_typing"
)
```

### 2.3 Base Implementation

```go
// internal/channels/personal/base.go

type BasePersonalAdapter struct {
    channelType  models.ChannelType
    messages     chan *models.Message
    config       *PersonalChannelConfig
    logger       *slog.Logger

    contacts     map[string]*Contact
    contactsMu   sync.RWMutex
}

func NewBasePersonalAdapter(channelType models.ChannelType, cfg *PersonalChannelConfig) *BasePersonalAdapter {
    return &BasePersonalAdapter{
        channelType: channelType,
        messages:    make(chan *models.Message, 100),
        config:      cfg,
        logger:      slog.Default().With("channel", channelType),
        contacts:    make(map[string]*Contact),
    }
}

func (b *BasePersonalAdapter) Type() models.ChannelType {
    return b.channelType
}

func (b *BasePersonalAdapter) Messages() <-chan *models.Message {
    return b.messages
}

// Common message normalization
func (b *BasePersonalAdapter) NormalizeInbound(raw RawMessage) *models.Message {
    return &models.Message{
        ID:        raw.ID,
        Channel:   b.channelType,
        Direction: models.DirectionInbound,
        Role:      models.RoleUser,
        Content:   raw.Content,
        Metadata: map[string]any{
            "peer_id":    raw.PeerID,
            "peer_name":  raw.PeerName,
            "group_id":   raw.GroupID,
            "group_name": raw.GroupName,
            "timestamp":  raw.Timestamp,
        },
        CreatedAt: raw.Timestamp,
    }
}

// Common attachment handling
func (b *BasePersonalAdapter) ProcessAttachments(raw RawMessage, msg *models.Message) error {
    for _, att := range raw.Attachments {
        attachment := models.Attachment{
            Type:     att.MIMEType,
            Filename: att.Filename,
            Size:     att.Size,
            URL:      att.URL,
        }
        msg.Attachments = append(msg.Attachments, attachment)
    }
    return nil
}
```

---

## 3. WhatsApp Adapter (whatsmeow)

### 3.1 Implementation

```go
// internal/channels/whatsapp/adapter.go

import (
    "go.mau.fi/whatsmeow"
    "go.mau.fi/whatsmeow/store/sqlstore"
    "go.mau.fi/whatsmeow/types"
    "go.mau.fi/whatsmeow/types/events"
    waProto "go.mau.fi/whatsmeow/binary/proto"
)

type WhatsAppAdapter struct {
    *personal.BasePersonalAdapter

    client   *whatsmeow.Client
    store    *sqlstore.Container
    device   *store.Device
    qrChan   chan string  // For QR code display during pairing
}

func New(cfg *Config) (*WhatsAppAdapter, error) {
    // Initialize SQLite store for session persistence
    store, err := sqlstore.New("sqlite3",
        fmt.Sprintf("file:%s?_foreign_keys=on", cfg.SessionPath),
        nil)
    if err != nil {
        return nil, fmt.Errorf("failed to create store: %w", err)
    }

    adapter := &WhatsAppAdapter{
        BasePersonalAdapter: personal.NewBasePersonalAdapter(models.ChannelWhatsApp, &cfg.Personal),
        store:               store,
        qrChan:              make(chan string, 1),
    }

    return adapter, nil
}

func (a *WhatsAppAdapter) Start(ctx context.Context) error {
    // Get or create device
    device, err := a.store.GetFirstDevice()
    if err != nil {
        return err
    }

    a.client = whatsmeow.NewClient(device, nil)
    a.client.AddEventHandler(a.handleEvent)

    if a.client.Store.ID == nil {
        // Not logged in, need QR code
        qrChan, _ := a.client.GetQRChannel(ctx)
        err = a.client.Connect()
        if err != nil {
            return err
        }

        for evt := range qrChan {
            if evt.Event == "code" {
                a.qrChan <- evt.Code
                a.logger.Info("scan QR code to login", "code", evt.Code)
            }
        }
    } else {
        // Already logged in
        err = a.client.Connect()
        if err != nil {
            return err
        }
    }

    return nil
}

func (a *WhatsAppAdapter) handleEvent(evt interface{}) {
    switch v := evt.(type) {
    case *events.Message:
        a.handleMessage(v)
    case *events.Receipt:
        a.handleReceipt(v)
    case *events.Presence:
        a.handlePresence(v)
    case *events.Connected:
        a.logger.Info("connected to WhatsApp")
    case *events.Disconnected:
        a.logger.Warn("disconnected from WhatsApp")
    }
}

func (a *WhatsAppAdapter) handleMessage(evt *events.Message) {
    // Skip status broadcasts
    if evt.Info.Chat.Server == "broadcast" {
        return
    }

    var content string
    var attachments []personal.RawAttachment

    // Extract message content
    if evt.Message.Conversation != nil {
        content = *evt.Message.Conversation
    } else if evt.Message.ExtendedTextMessage != nil {
        content = evt.Message.ExtendedTextMessage.GetText()
    } else if evt.Message.ImageMessage != nil {
        attachments = append(attachments, a.downloadImage(evt))
        content = evt.Message.ImageMessage.GetCaption()
    } else if evt.Message.DocumentMessage != nil {
        attachments = append(attachments, a.downloadDocument(evt))
        content = evt.Message.DocumentMessage.GetCaption()
    }
    // ... handle other message types

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

    select {
    case a.messages <- msg:
    default:
        a.logger.Warn("message channel full, dropping message")
    }
}

func (a *WhatsAppAdapter) Send(ctx context.Context, msg *models.Message) error {
    peerID := msg.Metadata["peer_id"].(string)
    jid, err := types.ParseJID(peerID)
    if err != nil {
        return fmt.Errorf("invalid peer ID: %w", err)
    }

    // Send text message
    if msg.Content != "" {
        _, err = a.client.SendMessage(ctx, jid, &waProto.Message{
            Conversation: &msg.Content,
        })
        if err != nil {
            return fmt.Errorf("failed to send message: %w", err)
        }
    }

    // Send attachments
    for _, att := range msg.Attachments {
        if err := a.sendAttachment(ctx, jid, att); err != nil {
            a.logger.Error("failed to send attachment", "error", err)
        }
    }

    return nil
}

func (a *WhatsAppAdapter) sendAttachment(ctx context.Context, jid types.JID, att models.Attachment) error {
    // Download attachment data
    data, err := a.downloadURL(att.URL)
    if err != nil {
        return err
    }

    // Upload to WhatsApp
    uploaded, err := a.client.Upload(ctx, data, whatsmeow.MediaImage)
    if err != nil {
        return err
    }

    // Send based on type
    mimeType := att.Type
    if strings.HasPrefix(mimeType, "image/") {
        _, err = a.client.SendMessage(ctx, jid, &waProto.Message{
            ImageMessage: &waProto.ImageMessage{
                URL:           &uploaded.URL,
                DirectPath:    &uploaded.DirectPath,
                MediaKey:      uploaded.MediaKey,
                FileEncSHA256: uploaded.FileEncSHA256,
                FileSHA256:    uploaded.FileSHA256,
                FileLength:    &uploaded.FileLength,
                Mimetype:      &mimeType,
            },
        })
    }
    // ... handle other types

    return err
}

// ContactManager implementation
func (a *WhatsAppAdapter) Contacts() personal.ContactManager {
    return &whatsappContacts{adapter: a}
}

type whatsappContacts struct {
    adapter *WhatsAppAdapter
}

func (c *whatsappContacts) Resolve(ctx context.Context, identifier string) (*personal.Contact, error) {
    jid, err := types.ParseJID(identifier)
    if err != nil {
        // Try as phone number
        jid = types.NewJID(identifier, types.DefaultUserServer)
    }

    contact, err := c.adapter.client.Store.Contacts.GetContact(jid)
    if err != nil {
        return nil, err
    }

    return &personal.Contact{
        ID:    jid.String(),
        Name:  contact.FullName,
        Phone: jid.User,
    }, nil
}

func (c *whatsappContacts) Sync(ctx context.Context) error {
    return c.adapter.client.FetchAppState(appstate.WAPatchCriticalBlock, true, false)
}

// PresenceManager implementation
func (a *WhatsAppAdapter) Presence() personal.PresenceManager {
    return &whatsappPresence{adapter: a}
}

type whatsappPresence struct {
    adapter *WhatsAppAdapter
}

func (p *whatsappPresence) SetTyping(ctx context.Context, peerID string, typing bool) error {
    jid, err := types.ParseJID(peerID)
    if err != nil {
        return err
    }

    if typing {
        return p.adapter.client.SendChatPresence(jid, types.ChatPresenceComposing, types.ChatPresenceMediaText)
    }
    return p.adapter.client.SendChatPresence(jid, types.ChatPresencePaused, types.ChatPresenceMediaText)
}

func (a *WhatsAppAdapter) Stop(ctx context.Context) error {
    a.client.Disconnect()
    return nil
}

func (a *WhatsAppAdapter) HealthCheck(ctx context.Context) pluginsdk.HealthStatus {
    if a.client == nil || !a.client.IsConnected() {
        return pluginsdk.HealthStatus{
            Status:  pluginsdk.StatusUnhealthy,
            Message: "not connected",
        }
    }
    return pluginsdk.HealthStatus{
        Status:  pluginsdk.StatusHealthy,
        Message: "connected",
    }
}
```

### 3.2 Configuration

```yaml
channels:
  whatsapp:
    enabled: true
    session_path: ~/.nexus/whatsapp/session.db
    media_path: ~/.nexus/whatsapp/media
    sync_contacts: true
    presence:
      send_read_receipts: true
      send_typing: true
```

---

## 4. Signal Adapter (signal-cli)

### 4.1 Implementation

```go
// internal/channels/signal/adapter.go

type SignalAdapter struct {
    *personal.BasePersonalAdapter

    process    *exec.Cmd
    stdin      io.WriteCloser
    stdout     *bufio.Scanner
    account    string
    socketPath string
}

func New(cfg *Config) (*SignalAdapter, error) {
    adapter := &SignalAdapter{
        BasePersonalAdapter: personal.NewBasePersonalAdapter(models.ChannelSignal, &cfg.Personal),
        account:             cfg.Account,  // Phone number
        socketPath:          cfg.SocketPath,
    }
    return adapter, nil
}

func (a *SignalAdapter) Start(ctx context.Context) error {
    // Start signal-cli in JSON-RPC mode
    a.process = exec.CommandContext(ctx,
        "signal-cli",
        "--output=json",
        "-a", a.account,
        "jsonRpc",
    )

    var err error
    a.stdin, err = a.process.StdinPipe()
    if err != nil {
        return err
    }

    stdout, err := a.process.StdoutPipe()
    if err != nil {
        return err
    }
    a.stdout = bufio.NewScanner(stdout)

    if err := a.process.Start(); err != nil {
        return fmt.Errorf("failed to start signal-cli: %w", err)
    }

    // Start message receiver
    go a.receiveLoop(ctx)

    return nil
}

func (a *SignalAdapter) receiveLoop(ctx context.Context) {
    for a.stdout.Scan() {
        line := a.stdout.Text()

        var event signalEvent
        if err := json.Unmarshal([]byte(line), &event); err != nil {
            a.logger.Error("failed to parse signal event", "error", err)
            continue
        }

        switch event.Method {
        case "receive":
            a.handleReceive(event.Params)
        }
    }
}

type signalEvent struct {
    JSONRPC string          `json:"jsonrpc"`
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params"`
}

type signalReceiveParams struct {
    Envelope struct {
        Source       string `json:"source"`
        SourceName   string `json:"sourceName"`
        Timestamp    int64  `json:"timestamp"`
        DataMessage  *struct {
            Message     string `json:"message"`
            GroupInfo   *struct {
                GroupID string `json:"groupId"`
                Type    string `json:"type"`
            } `json:"groupInfo"`
            Attachments []struct {
                ContentType string `json:"contentType"`
                Filename    string `json:"filename"`
                ID          string `json:"id"`
                Size        int    `json:"size"`
            } `json:"attachments"`
        } `json:"dataMessage"`
    } `json:"envelope"`
}

func (a *SignalAdapter) handleReceive(params json.RawMessage) {
    var p signalReceiveParams
    if err := json.Unmarshal(params, &p); err != nil {
        return
    }

    if p.Envelope.DataMessage == nil {
        return
    }

    raw := personal.RawMessage{
        ID:        fmt.Sprintf("%d", p.Envelope.Timestamp),
        Content:   p.Envelope.DataMessage.Message,
        PeerID:    p.Envelope.Source,
        PeerName:  p.Envelope.SourceName,
        Timestamp: time.UnixMilli(p.Envelope.Timestamp),
    }

    if p.Envelope.DataMessage.GroupInfo != nil {
        raw.GroupID = p.Envelope.DataMessage.GroupInfo.GroupID
    }

    for _, att := range p.Envelope.DataMessage.Attachments {
        raw.Attachments = append(raw.Attachments, personal.RawAttachment{
            ID:       att.ID,
            MIMEType: att.ContentType,
            Filename: att.Filename,
            Size:     int64(att.Size),
        })
    }

    msg := a.NormalizeInbound(raw)
    a.ProcessAttachments(raw, msg)
    a.messages <- msg
}

func (a *SignalAdapter) Send(ctx context.Context, msg *models.Message) error {
    peerID := msg.Metadata["peer_id"].(string)

    req := map[string]any{
        "jsonrpc": "2.0",
        "method":  "send",
        "id":      uuid.New().String(),
        "params": map[string]any{
            "recipient": []string{peerID},
            "message":   msg.Content,
        },
    }

    // Handle attachments
    if len(msg.Attachments) > 0 {
        var attachments []string
        for _, att := range msg.Attachments {
            // Download and save attachment locally
            path, err := a.saveAttachment(att)
            if err != nil {
                a.logger.Error("failed to save attachment", "error", err)
                continue
            }
            attachments = append(attachments, path)
        }
        req["params"].(map[string]any)["attachments"] = attachments
    }

    data, _ := json.Marshal(req)
    _, err := a.stdin.Write(append(data, '\n'))
    return err
}

func (a *SignalAdapter) Stop(ctx context.Context) error {
    if a.process != nil {
        a.stdin.Close()
        return a.process.Wait()
    }
    return nil
}
```

### 4.2 Configuration

```yaml
channels:
  signal:
    enabled: true
    account: "+1234567890"  # Phone number
    signal_cli_path: signal-cli
    config_dir: ~/.config/signal-cli
    dm:
      policy: pairing
    group:
      policy: allowlist
    presence:
      send_read_receipts: true
      send_typing: true
```

---

## 5. iMessage Adapter (macOS Native)

### 5.1 Implementation

```go
// internal/channels/imessage/adapter.go

// Only builds on darwin
// +build darwin

type IMessageAdapter struct {
    *personal.BasePersonalAdapter

    dbPath      string
    pollTicker  *time.Ticker
    lastRowID   int64
    stopChan    chan struct{}
}

func New(cfg *Config) (*IMessageAdapter, error) {
    // Default Messages database path
    dbPath := cfg.DatabasePath
    if dbPath == "" {
        homeDir, _ := os.UserHomeDir()
        dbPath = filepath.Join(homeDir, "Library/Messages/chat.db")
    }

    // Check Full Disk Access
    if _, err := os.Stat(dbPath); os.IsPermission(err) {
        return nil, fmt.Errorf("need Full Disk Access permission to read Messages database")
    }

    adapter := &IMessageAdapter{
        BasePersonalAdapter: personal.NewBasePersonalAdapter(models.ChannelIMessage, &cfg.Personal),
        dbPath:              dbPath,
        stopChan:            make(chan struct{}),
    }

    return adapter, nil
}

func (a *IMessageAdapter) Start(ctx context.Context) error {
    // Open database in read-only mode
    db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", a.dbPath))
    if err != nil {
        return err
    }
    defer db.Close()

    // Get latest message ID
    row := db.QueryRow("SELECT MAX(ROWID) FROM message")
    row.Scan(&a.lastRowID)

    // Start polling for new messages
    a.pollTicker = time.NewTicker(time.Second)
    go a.pollLoop(ctx)

    return nil
}

func (a *IMessageAdapter) pollLoop(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case <-a.stopChan:
            return
        case <-a.pollTicker.C:
            a.checkNewMessages()
        }
    }
}

func (a *IMessageAdapter) checkNewMessages() {
    db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", a.dbPath))
    if err != nil {
        a.logger.Error("failed to open database", "error", err)
        return
    }
    defer db.Close()

    query := `
        SELECT
            m.ROWID,
            m.guid,
            m.text,
            m.is_from_me,
            m.date,
            m.service,
            h.id as handle_id,
            COALESCE(h.uncanonicalized_id, h.id) as sender,
            c.chat_identifier,
            c.display_name
        FROM message m
        LEFT JOIN handle h ON m.handle_id = h.ROWID
        LEFT JOIN chat_message_join cmj ON m.ROWID = cmj.message_id
        LEFT JOIN chat c ON cmj.chat_id = c.ROWID
        WHERE m.ROWID > ?
          AND m.is_from_me = 0
        ORDER BY m.ROWID ASC
    `

    rows, err := db.Query(query, a.lastRowID)
    if err != nil {
        a.logger.Error("failed to query messages", "error", err)
        return
    }
    defer rows.Close()

    for rows.Next() {
        var (
            rowID          int64
            guid           string
            text           sql.NullString
            isFromMe       bool
            date           int64
            service        string
            handleID       sql.NullString
            sender         sql.NullString
            chatIdentifier sql.NullString
            displayName    sql.NullString
        )

        if err := rows.Scan(&rowID, &guid, &text, &isFromMe, &date,
            &service, &handleID, &sender, &chatIdentifier, &displayName); err != nil {
            continue
        }

        if !text.Valid || text.String == "" {
            continue
        }

        // Convert Apple epoch (2001-01-01) to Unix
        timestamp := time.Unix(date/1e9+978307200, date%1e9)

        raw := personal.RawMessage{
            ID:        guid,
            Content:   text.String,
            PeerID:    sender.String,
            Timestamp: timestamp,
        }

        if chatIdentifier.Valid && strings.HasPrefix(chatIdentifier.String, "chat") {
            raw.GroupID = chatIdentifier.String
            raw.GroupName = displayName.String
        }

        msg := a.NormalizeInbound(raw)
        a.messages <- msg

        a.lastRowID = rowID
    }
}

func (a *IMessageAdapter) Send(ctx context.Context, msg *models.Message) error {
    peerID := msg.Metadata["peer_id"].(string)
    content := msg.Content

    // Use AppleScript to send message
    script := fmt.Sprintf(`
        tell application "Messages"
            set targetService to 1st service whose service type = iMessage
            set targetBuddy to buddy "%s" of targetService
            send "%s" to targetBuddy
        end tell
    `, escapeAppleScript(peerID), escapeAppleScript(content))

    cmd := exec.CommandContext(ctx, "osascript", "-e", script)
    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("AppleScript error: %s: %w", output, err)
    }

    return nil
}

func (a *IMessageAdapter) SendToGroup(ctx context.Context, chatID string, content string) error {
    script := fmt.Sprintf(`
        tell application "Messages"
            set targetChat to chat id "%s"
            send "%s" to targetChat
        end tell
    `, escapeAppleScript(chatID), escapeAppleScript(content))

    cmd := exec.CommandContext(ctx, "osascript", "-e", script)
    return cmd.Run()
}

func escapeAppleScript(s string) string {
    s = strings.ReplaceAll(s, "\\", "\\\\")
    s = strings.ReplaceAll(s, "\"", "\\\"")
    return s
}

// ContactManager using Contacts framework
func (a *IMessageAdapter) Contacts() personal.ContactManager {
    return &imessageContacts{}
}

type imessageContacts struct{}

func (c *imessageContacts) Resolve(ctx context.Context, identifier string) (*personal.Contact, error) {
    // Query Contacts database
    script := fmt.Sprintf(`
        tell application "Contacts"
            set foundPerson to first person whose value of phones contains "%s"
            return {name of foundPerson, value of first phone of foundPerson}
        end tell
    `, identifier)

    cmd := exec.CommandContext(ctx, "osascript", "-e", script)
    output, err := cmd.Output()
    if err != nil {
        return &personal.Contact{
            ID:    identifier,
            Phone: identifier,
        }, nil
    }

    // Parse AppleScript output
    parts := strings.Split(strings.TrimSpace(string(output)), ", ")
    return &personal.Contact{
        ID:    identifier,
        Name:  parts[0],
        Phone: identifier,
    }, nil
}

func (c *imessageContacts) Search(ctx context.Context, query string) ([]*personal.Contact, error) {
    // Search Contacts via AppleScript
    script := fmt.Sprintf(`
        tell application "Contacts"
            set matchingPeople to every person whose name contains "%s"
            set results to {}
            repeat with p in matchingPeople
                set end of results to {name of p, value of first phone of p}
            end repeat
            return results
        end tell
    `, escapeAppleScript(query))

    cmd := exec.CommandContext(ctx, "osascript", "-e", script)
    // ... parse output
    return nil, nil
}

func (a *IMessageAdapter) Stop(ctx context.Context) error {
    close(a.stopChan)
    if a.pollTicker != nil {
        a.pollTicker.Stop()
    }
    return nil
}
```

### 5.2 Configuration

```yaml
channels:
  imessage:
    enabled: true
    database_path: ~/Library/Messages/chat.db  # Optional, uses default
    poll_interval: 1s
```

---

## 6. Configuration Schema

```yaml
# nexus.yaml
channels:
  # WhatsApp (Go native via whatsmeow)
  whatsapp:
    enabled: false
    session_path: ~/.nexus/whatsapp/session.db
    media_path: ~/.nexus/whatsapp/media
    sync_contacts: true
    dm:
      policy: pairing
    group:
      policy: allowlist
    presence:
      send_read_receipts: true
      send_typing: true
      broadcast_online: false

  # Signal (via signal-cli subprocess)
  signal:
    enabled: false
    account: ""  # Phone number with country code
    signal_cli_path: signal-cli
    config_dir: ~/.config/signal-cli
    dm:
      policy: pairing
    group:
      policy: allowlist
    presence:
      send_read_receipts: true
      send_typing: true

  # iMessage (macOS only)
  imessage:
    enabled: false
    database_path: ~/Library/Messages/chat.db
    poll_interval: 1s
    dm:
      policy: pairing
    group:
      policy: allowlist
```

---

## 7. CLI Commands

As of 2026-01-26, the `nexus` CLI does **not** ship these commands; they are a proposed interface.

```bash
# WhatsApp setup
nexus channels whatsapp setup     # Show QR code for pairing
nexus channels whatsapp logout    # Disconnect and clear session

# Signal setup
nexus channels signal register    # Register new account
nexus channels signal verify      # Verify with SMS code
nexus channels signal link        # Link as secondary device

# Status
nexus channels status             # Show all channel health
nexus channels test whatsapp      # Send test message

# Contacts
nexus contacts list --channel whatsapp
nexus contacts sync --channel signal
```

---

## 8. Implementation Phases

### Phase 1: Base Abstraction (Week 1)
- [ ] PersonalChannelAdapter interface
- [ ] BasePersonalAdapter with shared logic
- [ ] Contact and Conversation types
- [ ] Message normalization utilities

### Phase 2: WhatsApp (Week 2)
- [ ] whatsmeow integration
- [ ] Session persistence
- [ ] QR code pairing flow
- [ ] Text/media message handling
- [ ] Contact sync

### Phase 3: Signal (Week 3)
- [ ] signal-cli subprocess management
- [ ] JSON-RPC protocol handling
- [ ] Message send/receive
- [ ] Attachment handling

### Phase 4: iMessage (Week 4)
- [ ] Messages.app database polling
- [ ] AppleScript send integration
- [ ] Contacts framework integration
- [ ] Group chat support

---

## Appendix: Dependencies

| Channel | Library | License | Notes |
|---------|---------|---------|-------|
| WhatsApp | [whatsmeow](https://github.com/tulir/whatsmeow) | MPL-2.0 | Pure Go, well-maintained |
| Signal | [signal-cli](https://github.com/AsamK/signal-cli) | GPL-3.0 | Java, runs as subprocess |
| iMessage | Native macOS | - | Requires Full Disk Access |
