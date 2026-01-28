// Package email provides a Microsoft Graph Email channel adapter for Nexus.
//
// It uses the Microsoft Graph API to send and receive emails through Outlook/Exchange.
// The adapter supports polling mode for receiving new messages.
package email

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
)

// Adapter implements the channels.Adapter interface for Microsoft Graph Email.
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
	userEmail   string
	displayName string

	// Tracking last seen messages to avoid duplicates
	lastMessageTime time.Time
	seenMessages    map[string]bool
	seenMu          sync.Mutex
}

// NewAdapter creates a new Email adapter with the given configuration.
func NewAdapter(config Config) (*Adapter, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	a := &Adapter{
		config:          config,
		messages:        make(chan *models.Message, 100),
		rateLimiter:     channels.NewRateLimiter(config.RateLimit, config.RateBurst),
		logger:          config.Logger.With("adapter", "email"),
		httpClient:      &http.Client{Timeout: 30 * time.Second},
		accessToken:     config.AccessToken,
		refreshToken:    config.RefreshToken,
		lastMessageTime: time.Now(),
		seenMessages:    make(map[string]bool),
	}
	a.health = channels.NewBaseHealthAdapter(models.ChannelEmail, a.logger)

	return a, nil
}

// Type returns the channel type.
func (a *Adapter) Type() models.ChannelType {
	return models.ChannelEmail
}

// Start begins listening for messages from Email.
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
	a.logger.Info("email adapter started",
		"user_id", a.userID,
		"user_email", a.userEmail,
		"display_name", a.displayName,
		"folder", a.config.FolderID,
	)

	// Start message polling
	a.wg.Add(1)
	go a.pollMessages(ctx)

	// Start token refresh routine
	a.wg.Add(1)
	go a.tokenRefreshRoutine(ctx)

	return nil
}

// Stop gracefully shuts down the adapter.
func (a *Adapter) Stop(ctx context.Context) error {
	a.logger.Info("stopping email adapter")

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
		a.logger.Info("email adapter stopped gracefully")
	case <-ctx.Done():
		a.logger.Warn("email adapter stop timed out")
	}

	a.setStatus(false, "stopped")
	close(a.messages)

	return nil
}

// Send sends an email.
func (a *Adapter) Send(ctx context.Context, msg *models.Message) error {
	if err := a.rateLimiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limit: %w", err)
	}

	a.health.RecordMessageSent()

	// Parse channel ID to extract recipient
	// Format: email:{recipient_email} or email:{message_id} (for replies)
	recipient := strings.TrimPrefix(msg.ChannelID, "email:")
	if recipient == "" {
		recipient = msg.ChannelID
	}

	// Check if this is a reply (metadata contains original message ID)
	var replyToID string
	if msg.Metadata != nil {
		if id, ok := msg.Metadata["reply_to_message_id"].(string); ok {
			replyToID = id
		}
	}

	if replyToID != "" {
		return a.sendReply(ctx, replyToID, msg.Content)
	}

	return a.sendNewEmail(ctx, recipient, msg)
}

// sendNewEmail sends a new email.
func (a *Adapter) sendNewEmail(ctx context.Context, recipient string, msg *models.Message) error {
	subject := "Message from Nexus"
	if msg.Metadata != nil {
		if s, ok := msg.Metadata["subject"].(string); ok {
			subject = s
		}
	}

	// Build message body
	emailMsg := map[string]interface{}{
		"message": map[string]interface{}{
			"subject": subject,
			"body": map[string]interface{}{
				"contentType": "Text",
				"content":     msg.Content,
			},
			"toRecipients": []map[string]interface{}{
				{
					"emailAddress": map[string]interface{}{
						"address": recipient,
					},
				},
			},
		},
		"saveToSentItems": true,
	}

	jsonBody, err := json.Marshal(emailMsg)
	if err != nil {
		return fmt.Errorf("marshal email: %w", err)
	}

	endpoint := graphBaseURL + "/me/sendMail"
	if a.config.UserEmail != "" {
		endpoint = fmt.Sprintf("%s/users/%s/sendMail", graphBaseURL, a.config.UserEmail)
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
		return fmt.Errorf("send email: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		if err != nil {
			body = []byte("(failed to read response body)")
		}
		a.health.RecordMessageFailed()
		return fmt.Errorf("graph API error %d: %s", resp.StatusCode, string(body))
	}

	a.logger.Debug("email sent",
		"to", recipient,
		"subject", subject,
	)

	return nil
}

// sendReply sends a reply to an existing email thread.
func (a *Adapter) sendReply(ctx context.Context, messageID, content string) error {
	// Build reply body
	replyBody := map[string]interface{}{
		"message": map[string]interface{}{
			"body": map[string]interface{}{
				"contentType": "Text",
				"content":     content,
			},
		},
	}

	jsonBody, err := json.Marshal(replyBody)
	if err != nil {
		return fmt.Errorf("marshal reply: %w", err)
	}

	endpoint := fmt.Sprintf("%s/me/messages/%s/reply", graphBaseURL, messageID)
	if a.config.UserEmail != "" {
		endpoint = fmt.Sprintf("%s/users/%s/messages/%s/reply", graphBaseURL, a.config.UserEmail, messageID)
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
		return fmt.Errorf("send reply: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		if err != nil {
			body = []byte("(failed to read response body)")
		}
		a.health.RecordMessageFailed()
		return fmt.Errorf("graph API error %d: %s", resp.StatusCode, string(body))
	}

	a.logger.Debug("reply sent", "message_id", messageID)

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

// HealthCheck performs a health check against the Graph API.
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
		return channels.MetricsSnapshot{ChannelType: models.ChannelEmail}
	}
	return a.health.Metrics()
}

// SendTypingIndicator reports typing indicators as unsupported for email.
func (a *Adapter) SendTypingIndicator(ctx context.Context, msg *models.Message) error {
	return channels.ErrNotSupported
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
	endpoint := graphBaseURL + "/me"
	if a.config.UserEmail != "" {
		endpoint = fmt.Sprintf("%s/users/%s", graphBaseURL, a.config.UserEmail)
	}

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
	a.userEmail = user.Mail
	return nil
}

// pollMessages polls for new emails.
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
				a.logger.Error("failed to fetch emails", "error", err)
				a.health.RecordMessageFailed()
			}
		}
	}
}

// EmailMessage represents an email from Graph API.
type EmailMessage struct {
	ID               string    `json:"id"`
	ReceivedDateTime time.Time `json:"receivedDateTime"`
	Subject          string    `json:"subject"`
	IsRead           bool      `json:"isRead"`
	From             struct {
		EmailAddress struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"from"`
	ToRecipients []struct {
		EmailAddress struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"toRecipients"`
	Body struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
	ConversationID string `json:"conversationId"`
	HasAttachments bool   `json:"hasAttachments"`
}

// fetchNewMessages fetches new emails from the mailbox.
func (a *Adapter) fetchNewMessages(ctx context.Context) error {
	if err := a.rateLimiter.Wait(ctx); err != nil {
		return err
	}

	// Build endpoint
	endpoint := fmt.Sprintf("%s/me/mailFolders/%s/messages", graphBaseURL, a.config.FolderID)
	if a.config.UserEmail != "" {
		endpoint = fmt.Sprintf("%s/users/%s/mailFolders/%s/messages",
			graphBaseURL, a.config.UserEmail, a.config.FolderID)
	}

	// Add query parameters
	params := url.Values{}
	params.Set("$top", "20")
	params.Set("$orderby", "receivedDateTime desc")
	params.Set("$select", "id,receivedDateTime,subject,isRead,from,toRecipients,body,conversationId,hasAttachments")

	// Only unread messages unless configured otherwise
	if !a.config.IncludeRead {
		params.Set("$filter", "isRead eq false")
	}

	fullURL := endpoint + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.getAccessToken())
	req.Header.Set("Prefer", "outlook.body-content-type=\"text\"")

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
		return fmt.Errorf("get emails failed %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Value []EmailMessage `json:"value"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	// Process messages (newest first, so reverse)
	for i := len(result.Value) - 1; i >= 0; i-- {
		msg := result.Value[i]
		a.processMessage(ctx, &msg)
	}

	return nil
}

// processMessage converts an email to a Nexus message and sends it to the channel.
func (a *Adapter) processMessage(ctx context.Context, msg *EmailMessage) {
	// Skip if we've seen this message
	a.seenMu.Lock()
	if a.seenMessages[msg.ID] {
		a.seenMu.Unlock()
		return
	}
	a.seenMessages[msg.ID] = true
	a.seenMu.Unlock()

	// Skip messages before our start time
	if msg.ReceivedDateTime.Before(a.lastMessageTime) {
		return
	}

	// Skip emails from ourselves
	if a.userEmail != "" && msg.From.EmailAddress.Address == a.userEmail {
		return
	}

	// Extract plain text content
	content := msg.Body.Content
	if msg.Body.ContentType == "html" {
		content = stripHTMLTags(content)
	}
	content = strings.TrimSpace(content)

	// Convert to Nexus message
	nexusMsg := &models.Message{
		ID:        uuid.NewString(),
		Channel:   models.ChannelEmail,
		ChannelID: "email:" + msg.From.EmailAddress.Address,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   content,
		CreatedAt: msg.ReceivedDateTime,
		Metadata: map[string]any{
			"email_message_id":    msg.ID,
			"conversation_id":     msg.ConversationID,
			"subject":             msg.Subject,
			"sender_email":        msg.From.EmailAddress.Address,
			"sender_name":         msg.From.EmailAddress.Name,
			"has_attachments":     msg.HasAttachments,
			"reply_to_message_id": msg.ID, // For easy reply
		},
	}

	// Fetch attachments if present
	if msg.HasAttachments {
		attachments, err := a.fetchAttachments(ctx, msg.ID)
		if err != nil {
			a.logger.Warn("failed to fetch attachments",
				"message_id", msg.ID,
				"error", err,
			)
		} else {
			nexusMsg.Attachments = attachments
		}
	}

	a.health.RecordMessageReceived()

	select {
	case a.messages <- nexusMsg:
		a.logger.Debug("email received",
			"from", msg.From.EmailAddress.Address,
			"subject", msg.Subject,
		)

		// Mark as read if configured
		if a.config.AutoMarkRead {
			go func() {
				if err := a.markAsRead(context.Background(), msg.ID); err != nil {
					a.logger.Warn("failed to mark email as read",
						"message_id", msg.ID,
						"error", err,
					)
				}
			}()
		}
	default:
		a.logger.Warn("message channel full, dropping email",
			"from", msg.From.EmailAddress.Address,
		)
		a.health.RecordMessageFailed()
	}
}

// Attachment represents an email attachment from Graph API.
type Attachment struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ContentType  string `json:"contentType"`
	Size         int    `json:"size"`
	IsInline     bool   `json:"isInline"`
	ContentBytes string `json:"contentBytes"`
}

// fetchAttachments retrieves attachments for a message.
func (a *Adapter) fetchAttachments(ctx context.Context, messageID string) ([]models.Attachment, error) {
	endpoint := fmt.Sprintf("%s/me/messages/%s/attachments", graphBaseURL, messageID)
	if a.config.UserEmail != "" {
		endpoint = fmt.Sprintf("%s/users/%s/messages/%s/attachments",
			graphBaseURL, a.config.UserEmail, messageID)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
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
		return nil, fmt.Errorf("get attachments failed: %d", resp.StatusCode)
	}

	var result struct {
		Value []Attachment `json:"value"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	attachments := make([]models.Attachment, 0, len(result.Value))
	for _, att := range result.Value {
		attachments = append(attachments, models.Attachment{
			ID:       att.ID,
			Type:     att.ContentType,
			MimeType: att.ContentType,
			Filename: att.Name,
			Size:     int64(att.Size),
		})
	}

	return attachments, nil
}

// markAsRead marks an email as read.
func (a *Adapter) markAsRead(ctx context.Context, messageID string) error {
	endpoint := fmt.Sprintf("%s/me/messages/%s", graphBaseURL, messageID)
	if a.config.UserEmail != "" {
		endpoint = fmt.Sprintf("%s/users/%s/messages/%s",
			graphBaseURL, a.config.UserEmail, messageID)
	}

	body := []byte(`{"isRead": true}`)

	req, err := http.NewRequestWithContext(ctx, "PATCH", endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.getAccessToken())
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("mark as read failed: %d", resp.StatusCode)
	}

	return nil
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
