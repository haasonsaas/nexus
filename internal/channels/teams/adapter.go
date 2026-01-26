// Package teams provides a Microsoft Teams channel adapter for Nexus.
//
// It uses the Microsoft Graph API to send and receive messages from Teams chats
// and channels. The adapter supports both polling and webhook modes for receiving
// messages.
package teams

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
)

const (
	graphBaseURL = "https://graph.microsoft.com/v1.0"
	graphBetaURL = "https://graph.microsoft.com/beta"
)

// Adapter implements the channels.Adapter interface for Microsoft Teams.
type Adapter struct {
	config      Config
	messages    chan *models.Message
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	rateLimiter *channels.RateLimiter
	logger      *slog.Logger
	httpClient  *http.Client
	health      *channels.BaseHealthAdapter

	// OAuth tokens
	accessToken  string
	refreshToken string
	tokenExpiry  time.Time
	tokenMu      sync.RWMutex

	// User info
	userID      string
	displayName string

	// Tracking last seen messages to avoid duplicates
	lastMessageTime time.Time
	seenMessages    map[string]bool
	seenMu          sync.Mutex
}

// NewAdapter creates a new Teams adapter with the given configuration.
func NewAdapter(config Config) (*Adapter, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	a := &Adapter{
		config:          config,
		messages:        make(chan *models.Message, 100),
		rateLimiter:     channels.NewRateLimiter(config.RateLimit, config.RateBurst),
		logger:          config.Logger.With("adapter", "teams"),
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		accessToken:     config.AccessToken,
		refreshToken:    config.RefreshToken,
		lastMessageTime: time.Now(),
		seenMessages:    make(map[string]bool),
	}
	a.health = channels.NewBaseHealthAdapter(models.ChannelTeams, a.logger)

	return a, nil
}

// Type returns the channel type.
func (a *Adapter) Type() models.ChannelType {
	return models.ChannelTeams
}

// Start begins listening for messages from Teams.
func (a *Adapter) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	// Authenticate and get tokens if needed
	if a.accessToken == "" {
		if err := a.authenticate(ctx); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}
	}

	// Get current user info
	if err := a.fetchUserInfo(ctx); err != nil {
		a.logger.Warn("failed to fetch user info", "error", err)
	}

	a.setStatus(true, "")
	a.logger.Info("teams adapter started",
		"user_id", a.userID,
		"display_name", a.displayName,
		"mode", a.getMode(),
	)

	// Start message polling (webhooks would be added later)
	a.wg.Add(1)
	go a.pollMessages(ctx)

	// Start token refresh routine
	a.wg.Add(1)
	go a.tokenRefreshRoutine(ctx)

	return nil
}

// Stop gracefully shuts down the adapter.
func (a *Adapter) Stop(ctx context.Context) error {
	a.logger.Info("stopping teams adapter")

	if a.cancel != nil {
		a.cancel()
	}

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		a.logger.Info("teams adapter stopped gracefully")
	case <-ctx.Done():
		a.logger.Warn("teams adapter stop timed out")
	}

	a.setStatus(false, "stopped")
	close(a.messages)

	return nil
}

// Send sends a message to Teams.
func (a *Adapter) Send(ctx context.Context, msg *models.Message) error {
	if err := a.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit: %w", err)
	}

	a.health.RecordMessageSent()

	// Parse channel ID to determine chat type
	// Format: teams:chat:{chatId} or teams:channel:{teamId}:{channelId}
	parts := strings.Split(msg.ChannelID, ":")
	if len(parts) < 2 {
		return fmt.Errorf("invalid channel ID format: %s", msg.ChannelID)
	}

	var endpoint string
	switch parts[0] {
	case "chat":
		if len(parts) < 2 {
			return fmt.Errorf("invalid chat ID format: %s", msg.ChannelID)
		}
		chatID := parts[1]
		endpoint = fmt.Sprintf("%s/chats/%s/messages", graphBaseURL, chatID)
	case "channel":
		if len(parts) < 3 {
			return fmt.Errorf("invalid channel ID format: %s", msg.ChannelID)
		}
		teamID := parts[1]
		channelID := parts[2]
		endpoint = fmt.Sprintf("%s/teams/%s/channels/%s/messages", graphBaseURL, teamID, channelID)
	default:
		// Assume it's a chat ID directly
		endpoint = fmt.Sprintf("%s/chats/%s/messages", graphBaseURL, msg.ChannelID)
	}

	// Build message body
	body := map[string]interface{}{
		"body": map[string]interface{}{
			"contentType": "text",
			"content":     msg.Content,
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+a.getAccessToken())
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		a.health.RecordMessageFailed()
		return fmt.Errorf("send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		if err != nil {
			body = []byte("(failed to read response body)")
		}
		a.health.RecordMessageFailed()
		return fmt.Errorf("teams API error %d: %s", resp.StatusCode, string(body))
	}

	a.logger.Debug("message sent",
		"channel_id", msg.ChannelID,
		"status", resp.StatusCode,
	)

	return nil
}

// Messages returns the channel for receiving inbound messages.
func (a *Adapter) Messages() <-chan *models.Message {
	return a.messages
}

// Status returns the current adapter status.
func (a *Adapter) Status() channels.Status {
	if a.health == nil {
		return channels.Status{}
	}
	return a.health.Status()
}

// HealthCheck performs a health check against the Teams API.
func (a *Adapter) HealthCheck(ctx context.Context) channels.HealthStatus {
	start := time.Now()

	// Try to fetch current user as health check
	req, err := http.NewRequestWithContext(ctx, "GET", graphBaseURL+"/me", nil)
	if err != nil {
		return channels.HealthStatus{
			Healthy: false,
			Message: fmt.Sprintf("create request: %v", err),
			Latency: time.Since(start),
		}
	}

	req.Header.Set("Authorization", "Bearer "+a.getAccessToken())

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return channels.HealthStatus{
			Healthy: false,
			Message: fmt.Sprintf("health check failed: %v", err),
			Latency: time.Since(start),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return channels.HealthStatus{
			Healthy: false,
			Message: fmt.Sprintf("unexpected status: %d", resp.StatusCode),
			Latency: time.Since(start),
		}
	}

	return channels.HealthStatus{
		Healthy: true,
		Message: "connected",
		Latency: time.Since(start),
	}
}

// Metrics returns the current metrics snapshot.
func (a *Adapter) Metrics() channels.MetricsSnapshot {
	if a.health == nil {
		return channels.MetricsSnapshot{ChannelType: models.ChannelTeams}
	}
	return a.health.Metrics()
}

// SendTypingIndicator sends a typing indicator (not directly supported by Graph API for bots).
func (a *Adapter) SendTypingIndicator(ctx context.Context, msg *models.Message) error {
	// Teams doesn't have a direct typing indicator API for Graph
	// This is a no-op but maintains interface compatibility
	return nil
}

// authenticate performs OAuth2 client credentials flow.
func (a *Adapter) authenticate(ctx context.Context) error {
	data := url.Values{}
	data.Set("client_id", a.config.ClientID)
	data.Set("client_secret", a.config.ClientSecret)
	data.Set("scope", "https://graph.microsoft.com/.default")
	data.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, "POST", a.config.TokenEndpoint(), strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		if err != nil {
			body = []byte("(failed to read response body)")
		}
		return fmt.Errorf("token request failed %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return fmt.Errorf("decode token response: %w", err)
	}

	a.tokenMu.Lock()
	a.accessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		a.refreshToken = tokenResp.RefreshToken
	}
	a.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	a.tokenMu.Unlock()

	a.logger.Info("authentication successful", "expires_in", tokenResp.ExpiresIn)
	return nil
}

// fetchUserInfo retrieves the current user's information.
func (a *Adapter) fetchUserInfo(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", graphBaseURL+"/me", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.getAccessToken())

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get user info: %d", resp.StatusCode)
	}

	var user struct {
		ID          string `json:"id"`
		DisplayName string `json:"displayName"`
		Mail        string `json:"mail"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return err
	}

	a.userID = user.ID
	a.displayName = user.DisplayName
	return nil
}

// pollMessages polls for new messages from Teams chats.
func (a *Adapter) pollMessages(ctx context.Context) {
	defer a.wg.Done()

	ticker := time.NewTicker(a.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.fetchNewMessages(ctx); err != nil {
				a.logger.Error("failed to fetch messages", "error", err)
				a.health.RecordMessageFailed()
			}
		}
	}
}

// fetchNewMessages fetches new messages from all chats.
func (a *Adapter) fetchNewMessages(ctx context.Context) error {
	if err := a.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	// Get list of chats
	chats, err := a.getChats(ctx)
	if err != nil {
		return fmt.Errorf("get chats: %w", err)
	}

	for _, chat := range chats {
		if err := a.fetchChatMessages(ctx, chat.ID, chat.ChatType); err != nil {
			a.logger.Warn("failed to fetch chat messages",
				"chat_id", chat.ID,
				"error", err,
			)
		}
	}

	return nil
}

// Chat represents a Teams chat.
type Chat struct {
	ID        string `json:"id"`
	Topic     string `json:"topic"`
	ChatType  string `json:"chatType"`
	CreatedAt string `json:"createdDateTime"`
}

// getChats retrieves the list of chats for the current user.
func (a *Adapter) getChats(ctx context.Context) ([]Chat, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", graphBaseURL+"/me/chats", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.getAccessToken())

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		if err != nil {
			body = []byte("(failed to read response body)")
		}
		return nil, fmt.Errorf("get chats failed %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Value []Chat `json:"value"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Value, nil
}

// TeamsMessage represents a message from Teams API.
type TeamsMessage struct {
	ID              string    `json:"id"`
	CreatedDateTime time.Time `json:"createdDateTime"`
	Body            struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
	From struct {
		User struct {
			ID          string `json:"id"`
			DisplayName string `json:"displayName"`
		} `json:"user"`
	} `json:"from"`
	Attachments []struct {
		ID          string `json:"id"`
		ContentType string `json:"contentType"`
		ContentURL  string `json:"contentUrl"`
		Name        string `json:"name"`
	} `json:"attachments"`
}

// fetchChatMessages fetches messages from a specific chat.
func (a *Adapter) fetchChatMessages(ctx context.Context, chatID string, chatType string) error {
	endpoint := fmt.Sprintf("%s/me/chats/%s/messages?$top=10&$orderby=createdDateTime desc", graphBaseURL, chatID)

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.getAccessToken())

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		if err != nil {
			body = []byte("(failed to read response body)")
		}
		return fmt.Errorf("get messages failed %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Value []TeamsMessage `json:"value"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	// Process messages (newest first, so reverse)
	for i := len(result.Value) - 1; i >= 0; i-- {
		msg := result.Value[i]
		a.processMessage(chatID, chatType, &msg)
	}

	return nil
}

// processMessage converts a Teams message to a Nexus message and sends it to the channel.
func (a *Adapter) processMessage(chatID string, chatType string, msg *TeamsMessage) {
	// Skip if we've seen this message
	a.seenMu.Lock()
	if a.seenMessages[msg.ID] {
		a.seenMu.Unlock()
		return
	}
	a.seenMessages[msg.ID] = true
	a.seenMu.Unlock()

	// Skip messages before our start time
	if msg.CreatedDateTime.Before(a.lastMessageTime) {
		return
	}

	// Skip our own messages
	if msg.From.User.ID == a.userID {
		return
	}

	// Convert to Nexus message
	nexusMsg := &models.Message{
		ID:        uuid.NewString(),
		Channel:   models.ChannelTeams,
		ChannelID: "chat:" + chatID,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   a.extractContent(msg),
		CreatedAt: msg.CreatedDateTime,
		Metadata: map[string]any{
			"teams_message_id":  msg.ID,
			"sender_id":         msg.From.User.ID,
			"sender_name":       msg.From.User.DisplayName,
			"chat_id":           chatID,
			"chat_type":         chatType,
			"conversation_type": "group",
		},
	}
	if strings.EqualFold(chatType, "oneOnOne") {
		nexusMsg.Metadata["conversation_type"] = "dm"
	}

	// Handle attachments
	if len(msg.Attachments) > 0 {
		nexusMsg.Attachments = make([]models.Attachment, 0, len(msg.Attachments))
		for _, att := range msg.Attachments {
			nexusMsg.Attachments = append(nexusMsg.Attachments, models.Attachment{
				ID:       att.ID,
				Type:     att.ContentType,
				URL:      att.ContentURL,
				Filename: att.Name,
			})
		}
	}

	a.health.RecordMessageReceived()

	select {
	case a.messages <- nexusMsg:
		a.logger.Debug("message received",
			"chat_id", chatID,
			"from", msg.From.User.DisplayName,
		)
	default:
		a.logger.Warn("message channel full, dropping message",
			"chat_id", chatID,
		)
		a.health.RecordMessageFailed()
	}
}

// extractContent extracts plain text content from a Teams message.
func (a *Adapter) extractContent(msg *TeamsMessage) string {
	content := msg.Body.Content

	// If HTML, do basic stripping (Teams often sends HTML)
	if msg.Body.ContentType == "html" {
		// Basic HTML tag removal - a proper implementation would use a parser
		content = stripHTMLTags(content)
	}

	return strings.TrimSpace(content)
}

// stripHTMLTags removes HTML tags from a string (basic implementation).
func stripHTMLTags(s string) string {
	var result strings.Builder
	inTag := false

	for _, r := range s {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				result.WriteRune(r)
			}
		}
	}

	return result.String()
}

// tokenRefreshRoutine periodically refreshes the access token.
func (a *Adapter) tokenRefreshRoutine(ctx context.Context) {
	defer a.wg.Done()

	for {
		a.tokenMu.RLock()
		expiry := a.tokenExpiry
		a.tokenMu.RUnlock()

		// Refresh 5 minutes before expiry
		sleepDuration := time.Until(expiry) - 5*time.Minute
		if sleepDuration < time.Minute {
			sleepDuration = time.Minute
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(sleepDuration):
			if err := a.authenticate(ctx); err != nil {
				a.logger.Error("token refresh failed", "error", err)
				a.setStatus(false, "token refresh failed")
			}
		}
	}
}

// getAccessToken returns the current access token.
func (a *Adapter) getAccessToken() string {
	a.tokenMu.RLock()
	defer a.tokenMu.RUnlock()
	return a.accessToken
}

// setStatus updates the adapter status.
func (a *Adapter) setStatus(connected bool, errorMsg string) {
	if a.health == nil {
		return
	}
	a.health.SetStatus(connected, errorMsg)
}

// getMode returns the current operation mode.
func (a *Adapter) getMode() string {
	if a.config.WebhookURL != "" {
		return "webhook"
	}
	return "polling"
}
