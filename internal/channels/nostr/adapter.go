package nostr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip04"
	"github.com/nbd-wtf/go-nostr/nip19"
)

// DefaultRelays are commonly used Nostr relays.
var DefaultRelays = []string{
	"wss://relay.damus.io",
	"wss://nos.lol",
	"wss://relay.nostr.band",
}

// Config holds configuration for the Nostr adapter.
type Config struct {
	// PrivateKey is the bot's private key in hex or nsec format (required)
	PrivateKey string

	// Relays is the list of relay URLs to connect to
	Relays []string

	// RateLimit configures rate limiting for API calls (operations per second)
	RateLimit float64

	// RateBurst configures the burst capacity for rate limiting
	RateBurst int

	// Logger is an optional slog.Logger instance
	Logger *slog.Logger
}

// Validate checks if the configuration is valid and applies defaults.
func (c *Config) Validate() error {
	if c.PrivateKey == "" {
		return channels.ErrConfig("private_key is required", nil)
	}

	// Validate private key format
	if _, err := parsePrivateKey(c.PrivateKey); err != nil {
		return channels.ErrConfig("invalid private key format", err)
	}

	if len(c.Relays) == 0 {
		c.Relays = DefaultRelays
	}

	if c.RateLimit == 0 {
		c.RateLimit = 10
	}

	if c.RateBurst == 0 {
		c.RateBurst = 5
	}

	if c.Logger == nil {
		c.Logger = slog.Default()
	}

	return nil
}

// Adapter implements the channels.Adapter interface for Nostr.
type Adapter struct {
	cfg         Config
	privateKey  string
	publicKey   string
	relays      []*nostr.Relay
	messages    chan *models.Message
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	seen        sync.Map // Event ID deduplication
	rateLimiter *channels.RateLimiter
	logger      *slog.Logger
	health      *channels.BaseHealthAdapter
}

// NewAdapter creates a new Nostr adapter with the given configuration.
func NewAdapter(cfg Config) (*Adapter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Parse private key
	privateKey, err := parsePrivateKey(cfg.PrivateKey)
	if err != nil {
		return nil, channels.ErrConfig("failed to parse private key", err)
	}

	// Derive public key
	publicKey, err := nostr.GetPublicKey(privateKey)
	if err != nil {
		return nil, channels.ErrConfig("failed to derive public key", err)
	}

	adapter := &Adapter{
		cfg:         cfg,
		privateKey:  privateKey,
		publicKey:   publicKey,
		messages:    make(chan *models.Message, 100),
		rateLimiter: channels.NewRateLimiter(cfg.RateLimit, cfg.RateBurst),
		logger:      cfg.Logger.With("adapter", "nostr", "pubkey", publicKey[:16]+"..."),
	}
	adapter.health = channels.NewBaseHealthAdapter(models.ChannelNostr, adapter.logger)
	return adapter, nil
}

// Start begins listening for messages from Nostr relays.
func (a *Adapter) Start(ctx context.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx)

	a.logger.Info("starting nostr adapter",
		"relays", a.cfg.Relays,
		"pubkey", a.publicKey)

	// Connect to relays
	for _, url := range a.cfg.Relays {
		relay, err := nostr.RelayConnect(a.ctx, url)
		if err != nil {
			a.logger.Warn("failed to connect to relay",
				"relay", url,
				"error", err)
			continue
		}
		a.relays = append(a.relays, relay)
		a.logger.Debug("connected to relay", "relay", url)
	}

	if len(a.relays) == 0 {
		return channels.ErrConnection("failed to connect to any relay", nil)
	}

	// Start subscription handler for each relay
	for _, relay := range a.relays {
		a.wg.Add(1)
		go a.subscribeToRelay(relay)
	}

	a.updateStatus(true, "")
	a.health.RecordConnectionOpened()

	a.setDegraded(false)

	npub, err := nip19.EncodePublicKey(a.publicKey)
	if err != nil {
		npub = a.publicKey
		a.logger.Debug("failed to encode npub", "error", err)
	}
	a.logger.Info("nostr adapter started successfully",
		"connected_relays", len(a.relays),
		"npub", npub)

	return nil
}

// subscribeToRelay subscribes to DMs on a single relay.
func (a *Adapter) subscribeToRelay(relay *nostr.Relay) {
	defer a.wg.Done()

	// Subscribe to kind 4 (encrypted DMs) addressed to us
	since := nostr.Timestamp(time.Now().Add(-2 * time.Minute).Unix())
	filters := nostr.Filters{{
		Kinds: []int{4}, // Encrypted DM (NIP-04)
		Tags:  nostr.TagMap{"p": []string{a.publicKey}},
		Since: &since,
	}}

	sub, err := relay.Subscribe(a.ctx, filters)
	if err != nil {
		a.logger.Error("failed to subscribe to relay",
			"relay", relay.URL,
			"error", err)
		return
	}

	a.logger.Debug("subscribed to relay", "relay", relay.URL)

	for {
		select {
		case <-a.ctx.Done():
			sub.Unsub()
			return
		case event := <-sub.Events:
			if event == nil {
				continue
			}
			a.handleEvent(event, relay)
		}
	}
}

// handleEvent processes an incoming Nostr event.
func (a *Adapter) handleEvent(event *nostr.Event, relay *nostr.Relay) {
	startTime := time.Now()

	// Deduplicate events
	if _, loaded := a.seen.LoadOrStore(event.ID, true); loaded {
		return
	}

	// Skip our own messages
	if event.PubKey == a.publicKey {
		return
	}

	// Verify signature
	ok, err := event.CheckSignature()
	if err != nil || !ok {
		a.logger.Warn("invalid event signature",
			"event_id", event.ID,
			"error", err)
		return
	}

	// Compute shared secret for NIP-04 decryption
	sharedSecret, err := nip04.ComputeSharedSecret(event.PubKey, a.privateKey)
	if err != nil {
		a.logger.Warn("failed to compute shared secret",
			"event_id", event.ID,
			"sender", event.PubKey[:16]+"...",
			"error", err)
		return
	}

	// Decrypt the message
	plaintext, err := nip04.Decrypt(event.Content, sharedSecret)
	if err != nil {
		a.logger.Warn("failed to decrypt message",
			"event_id", event.ID,
			"sender", event.PubKey[:16]+"...",
			"error", err)
		return
	}

	a.logger.Debug("received DM",
		"event_id", event.ID,
		"sender", event.PubKey[:16]+"...",
		"relay", relay.URL)

	// Convert to unified message format
	msg := a.convertEvent(event, plaintext)

	// Record metrics
	a.health.RecordMessageReceived()
	a.health.RecordReceiveLatency(time.Since(startTime))

	// Send to messages channel
	select {
	case a.messages <- msg:
		a.updateLastPing()
		a.setDegraded(false)
	case <-a.ctx.Done():
		return
	default:
		a.logger.Warn("messages channel full, dropping message",
			"event_id", event.ID)
		a.health.RecordMessageFailed()
		a.setDegraded(true)
	}
}

// convertEvent converts a Nostr event to unified message format.
func (a *Adapter) convertEvent(event *nostr.Event, plaintext string) *models.Message {
	// Generate session ID from sender pubkey (DM conversation)
	sessionID := generateSessionID(event.PubKey)

	// Format sender as npub for readability
	npub, err := nip19.EncodePublicKey(event.PubKey)
	if err != nil {
		npub = event.PubKey
		a.logger.Debug("failed to encode npub", "error", err)
	}

	msg := &models.Message{
		ID:        event.ID,
		SessionID: sessionID,
		Channel:   models.ChannelNostr,
		ChannelID: event.ID,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   plaintext,
		Metadata: map[string]any{
			"nostr_pubkey":      event.PubKey,
			"nostr_npub":        npub,
			"sender_id":         event.PubKey,
			"conversation_type": "dm",
		},
		CreatedAt: time.Unix(int64(event.CreatedAt), 0),
	}

	return msg
}

// Stop gracefully shuts down the adapter.
func (a *Adapter) Stop(ctx context.Context) error {
	a.logger.Info("stopping nostr adapter")

	if a.cancel != nil {
		a.cancel()
	}

	// Close relay connections
	for _, relay := range a.relays {
		if err := relay.Close(); err != nil {
			a.logger.Warn("error closing relay", "relay", relay.URL, "error", err)
		}
	}

	// Wait for goroutines to finish
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		close(a.messages)
		a.updateStatus(false, "")
		a.health.RecordConnectionClosed()
		a.logger.Info("nostr adapter stopped gracefully")
		return nil
	case <-ctx.Done():
		close(a.messages)
		a.updateStatus(false, "shutdown timeout")
		a.logger.Warn("nostr adapter stop timeout")
		a.health.RecordError(channels.ErrCodeTimeout)
		return channels.ErrTimeout("shutdown timeout", ctx.Err())
	}
}

// Send delivers a message via Nostr DM.
func (a *Adapter) Send(ctx context.Context, msg *models.Message) error {
	startTime := time.Now()

	// Apply rate limiting
	if err := a.rateLimiter.Wait(ctx); err != nil {
		a.logger.Warn("rate limit wait cancelled", "error", err)
		a.health.RecordError(channels.ErrCodeTimeout)
		return channels.ErrTimeout("rate limit wait cancelled", err)
	}

	// Extract recipient pubkey from message metadata
	toPubkey, ok := msg.Metadata["nostr_pubkey"].(string)
	if !ok || toPubkey == "" {
		a.health.RecordMessageFailed()
		a.health.RecordError(channels.ErrCodeInvalidInput)
		return channels.ErrInvalidInput("missing nostr_pubkey in message metadata", nil)
	}

	// Normalize pubkey
	normalizedPubkey, err := normalizePubkey(toPubkey)
	if err != nil {
		a.health.RecordMessageFailed()
		return channels.ErrInvalidInput("invalid recipient pubkey", err)
	}

	a.logger.Debug("sending DM",
		"to", normalizedPubkey[:16]+"...",
		"content_length", len(msg.Content))

	// Compute shared secret for NIP-04 encryption
	sharedSecret, err := nip04.ComputeSharedSecret(normalizedPubkey, a.privateKey)
	if err != nil {
		a.health.RecordMessageFailed()
		return channels.ErrInternal("failed to compute shared secret", err)
	}

	// Encrypt the message
	ciphertext, err := nip04.Encrypt(msg.Content, sharedSecret)
	if err != nil {
		a.health.RecordMessageFailed()
		return channels.ErrInternal("failed to encrypt message", err)
	}

	// Create event
	event := nostr.Event{
		PubKey:    a.publicKey,
		CreatedAt: nostr.Timestamp(time.Now().Unix()),
		Kind:      4, // Encrypted DM
		Tags:      nostr.Tags{{"p", normalizedPubkey}},
		Content:   ciphertext,
	}

	// Sign event
	if err := event.Sign(a.privateKey); err != nil {
		a.health.RecordMessageFailed()
		return channels.ErrInternal("failed to sign event", err)
	}

	// Publish to relays
	var lastErr error
	published := false
	for _, relay := range a.relays {
		err := relay.Publish(ctx, event)
		if err != nil {
			lastErr = err
			a.logger.Warn("failed to publish to relay",
				"relay", relay.URL,
				"error", err)
			continue
		}
		published = true
		a.logger.Debug("published to relay",
			"relay", relay.URL,
			"event_id", event.ID)
		break // Success - exit early
	}

	if !published {
		a.health.RecordMessageFailed()
		return channels.ErrConnection("failed to publish to any relay", lastErr)
	}

	// Record success metrics
	a.health.RecordMessageSent()
	a.health.RecordSendLatency(time.Since(startTime))

	a.logger.Debug("DM sent successfully",
		"event_id", event.ID,
		"latency_ms", time.Since(startTime).Milliseconds())

	return nil
}

// Messages returns a channel of inbound messages.
func (a *Adapter) Messages() <-chan *models.Message {
	return a.messages
}

// Type returns the channel type.
func (a *Adapter) Type() models.ChannelType {
	return models.ChannelNostr
}

// Status returns the current connection status.
func (a *Adapter) Status() channels.Status {
	if a.health == nil {
		return channels.Status{}
	}
	return a.health.Status()
}

// HealthCheck performs a connectivity check.
func (a *Adapter) HealthCheck(ctx context.Context) channels.HealthStatus {
	startTime := time.Now()

	health := channels.HealthStatus{
		LastCheck: startTime,
		Healthy:   false,
	}

	// Check if we have any connected relays
	connectedCount := 0
	for _, relay := range a.relays {
		if relay.IsConnected() {
			connectedCount++
		}
	}

	health.Latency = time.Since(startTime)
	health.Healthy = connectedCount > 0
	health.Degraded = a.isDegraded() || connectedCount < len(a.relays)

	if !health.Healthy {
		health.Message = "no connected relays"
	} else if health.Degraded {
		health.Message = fmt.Sprintf("degraded: %d/%d relays connected", connectedCount, len(a.relays))
	} else {
		health.Message = fmt.Sprintf("healthy: %d relays connected", connectedCount)
	}

	return health
}

// Metrics returns the current metrics snapshot.
func (a *Adapter) Metrics() channels.MetricsSnapshot {
	if a.health == nil {
		return channels.MetricsSnapshot{ChannelType: models.ChannelNostr}
	}
	return a.health.Metrics()
}

// PublicKey returns the bot's public key in hex format.
func (a *Adapter) PublicKey() string {
	return a.publicKey
}

// Npub returns the bot's public key in npub format.
func (a *Adapter) Npub() string {
	npub, err := nip19.EncodePublicKey(a.publicKey)
	if err != nil {
		a.logger.Debug("failed to encode npub", "error", err)
		return a.publicKey
	}
	return npub
}

// SendTypingIndicator is a no-op for Nostr.
func (a *Adapter) SendTypingIndicator(ctx context.Context, msg *models.Message) error {
	return nil
}

// StartStreamingResponse sends an initial placeholder message.
func (a *Adapter) StartStreamingResponse(ctx context.Context, msg *models.Message) (string, error) {
	// Nostr doesn't support message editing, so streaming isn't really possible
	return "", nil
}

// UpdateStreamingResponse updates a previously sent message.
func (a *Adapter) UpdateStreamingResponse(ctx context.Context, msg *models.Message, messageID string, content string) error {
	// Nostr doesn't support message editing
	return nil
}

// Helper functions

func (a *Adapter) updateStatus(connected bool, errMsg string) {
	if a.health == nil {
		return
	}
	a.health.SetStatus(connected, errMsg)
}

func (a *Adapter) updateLastPing() {
	if a.health == nil {
		return
	}
	a.health.UpdateLastPing()
}

func (a *Adapter) setDegraded(degraded bool) {
	if a.health == nil {
		return
	}
	a.health.SetDegraded(degraded)
}

func (a *Adapter) isDegraded() bool {
	if a.health == nil {
		return false
	}
	return a.health.IsDegraded()
}

func generateSessionID(pubkey string) string {
	data := fmt.Sprintf("nostr:%s", pubkey)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// parsePrivateKey parses a private key in hex or nsec format.
func parsePrivateKey(key string) (string, error) {
	trimmed := strings.TrimSpace(key)

	// Handle nsec (bech32) format
	if strings.HasPrefix(trimmed, "nsec1") {
		prefix, data, err := nip19.Decode(trimmed)
		if err != nil {
			return "", fmt.Errorf("invalid nsec key: %w", err)
		}
		if prefix != "nsec" {
			return "", fmt.Errorf("invalid key type: expected nsec, got %s", prefix)
		}
		hexKey, ok := data.(string)
		if !ok {
			return "", fmt.Errorf("invalid nsec key type: %T", data)
		}
		return hexKey, nil
	}

	// Handle hex format
	if len(trimmed) != 64 {
		return "", fmt.Errorf("private key must be 64 hex characters or nsec format")
	}
	// Validate hex
	if _, err := hex.DecodeString(trimmed); err != nil {
		return "", fmt.Errorf("invalid hex key: %w", err)
	}
	return trimmed, nil
}

// normalizePubkey normalizes a pubkey to hex format.
func normalizePubkey(input string) (string, error) {
	trimmed := strings.TrimSpace(input)

	// Handle npub (bech32) format
	if strings.HasPrefix(trimmed, "npub1") {
		prefix, data, err := nip19.Decode(trimmed)
		if err != nil {
			return "", fmt.Errorf("invalid npub key: %w", err)
		}
		if prefix != "npub" {
			return "", fmt.Errorf("invalid key type: expected npub, got %s", prefix)
		}
		pubkey, ok := data.(string)
		if !ok {
			return "", fmt.Errorf("invalid npub key type: %T", data)
		}
		return pubkey, nil
	}

	// Handle hex format
	if len(trimmed) != 64 {
		return "", fmt.Errorf("pubkey must be 64 hex characters or npub format")
	}
	// Validate hex
	if _, err := hex.DecodeString(trimmed); err != nil {
		return "", fmt.Errorf("invalid hex pubkey: %w", err)
	}
	return strings.ToLower(trimmed), nil
}
