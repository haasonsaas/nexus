package voice

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// TwilioProvider implements the Provider interface for Twilio Voice API.
// It supports outbound calls, TwiML webhooks, and media streams.
//
// Thread Safety:
// TwilioProvider is safe for concurrent use.
type TwilioProvider struct {
	accountSID string
	authToken  string
	baseURL    string
	publicURL  string
	streamPath string

	// Call state tracking
	webhookURLs map[string]string // providerCallID -> webhookURL
	mu          sync.RWMutex

	client *http.Client
}

// TwilioConfig holds configuration for the Twilio provider.
type TwilioConfig struct {
	// AccountSID is the Twilio account SID (required)
	AccountSID string

	// AuthToken is the Twilio auth token (required)
	AuthToken string

	// PublicURL is the public URL for webhooks (optional)
	PublicURL string

	// StreamPath is the path for media stream WebSocket (optional)
	StreamPath string
}

// NewTwilioProvider creates a new Twilio voice provider.
func NewTwilioProvider(cfg TwilioConfig) (*TwilioProvider, error) {
	if cfg.AccountSID == "" {
		return nil, errors.New("twilio: account SID is required")
	}
	if cfg.AuthToken == "" {
		return nil, errors.New("twilio: auth token is required")
	}

	return &TwilioProvider{
		accountSID:  cfg.AccountSID,
		authToken:   cfg.AuthToken,
		baseURL:     fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s", cfg.AccountSID),
		publicURL:   cfg.PublicURL,
		streamPath:  cfg.StreamPath,
		webhookURLs: make(map[string]string),
		client:      &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Name returns the provider identifier.
func (p *TwilioProvider) Name() ProviderName {
	return ProviderTwilio
}

// SetPublicURL updates the public webhook URL.
func (p *TwilioProvider) SetPublicURL(url string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.publicURL = url
}

// InitiateCall starts an outbound call via Twilio API.
func (p *TwilioProvider) InitiateCall(ctx context.Context, input *InitiateCallInput) (*InitiateCallResult, error) {
	webhookURL := input.WebhookURL
	if webhookURL == "" {
		return nil, errors.New("twilio: webhook URL is required")
	}

	// Add callId to webhook URL
	u, err := url.Parse(webhookURL)
	if err != nil {
		return nil, fmt.Errorf("twilio: invalid webhook URL: %w", err)
	}
	q := u.Query()
	q.Set("callId", input.CallID)
	u.RawQuery = q.Encode()
	webhookURL = u.String()

	// Create status callback URL
	statusURL := *u
	sq := statusURL.Query()
	sq.Set("type", "status")
	statusURL.RawQuery = sq.Encode()

	params := url.Values{
		"To":                  {input.To},
		"From":                {input.From},
		"Url":                 {webhookURL},
		"StatusCallback":      {statusURL.String()},
		"StatusCallbackEvent": {"initiated", "ringing", "answered", "completed"},
		"Timeout":             {"30"},
	}

	resp, err := p.apiRequest(ctx, "/Calls.json", params)
	if err != nil {
		return nil, fmt.Errorf("twilio: failed to initiate call: %w", err)
	}

	var result struct {
		SID    string `json:"sid"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("twilio: failed to parse response: %w", err)
	}

	// Store webhook URL for this call
	p.mu.Lock()
	p.webhookURLs[result.SID] = webhookURL
	p.mu.Unlock()

	status := "initiated"
	if result.Status == "queued" {
		status = "queued"
	}

	return &InitiateCallResult{
		ProviderCallID: result.SID,
		Status:         status,
	}, nil
}

// HangupCall ends an active call.
func (p *TwilioProvider) HangupCall(ctx context.Context, input *HangupCallInput) error {
	p.mu.Lock()
	delete(p.webhookURLs, input.ProviderCallID)
	p.mu.Unlock()

	params := url.Values{
		"Status": {"completed"},
	}

	_, err := p.apiRequest(ctx, fmt.Sprintf("/Calls/%s.json", input.ProviderCallID), params)
	if err != nil && !strings.Contains(err.Error(), "404") {
		return fmt.Errorf("twilio: failed to hangup call: %w", err)
	}

	return nil
}

// PlayTTS plays text-to-speech audio.
func (p *TwilioProvider) PlayTTS(ctx context.Context, input *PlayTTSInput) error {
	p.mu.RLock()
	webhookURL := p.webhookURLs[input.ProviderCallID]
	p.mu.RUnlock()

	if webhookURL == "" {
		return errors.New("twilio: missing webhook URL for call")
	}

	voice := input.Voice
	if voice == "" {
		voice = "Polly.Joanna"
	}

	locale := input.Locale
	if locale == "" {
		locale = "en-US"
	}

	twiml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Say voice="%s" language="%s">%s</Say>
  <Gather input="speech" speechTimeout="auto" action="%s" method="POST">
    <Say>.</Say>
  </Gather>
</Response>`, escapeXML(voice), escapeXML(locale), escapeXML(input.Text), escapeXML(webhookURL))

	params := url.Values{
		"Twiml": {twiml},
	}

	_, err := p.apiRequest(ctx, fmt.Sprintf("/Calls/%s.json", input.ProviderCallID), params)
	if err != nil {
		return fmt.Errorf("twilio: failed to play TTS: %w", err)
	}

	return nil
}

// StartListening begins speech recognition.
func (p *TwilioProvider) StartListening(ctx context.Context, input *StartListeningInput) error {
	p.mu.RLock()
	webhookURL := p.webhookURLs[input.ProviderCallID]
	p.mu.RUnlock()

	if webhookURL == "" {
		return errors.New("twilio: missing webhook URL for call")
	}

	language := input.Language
	if language == "" {
		language = "en-US"
	}

	twiml := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Gather input="speech" speechTimeout="auto" language="%s" action="%s" method="POST">
  </Gather>
</Response>`, escapeXML(language), escapeXML(webhookURL))

	params := url.Values{
		"Twiml": {twiml},
	}

	_, err := p.apiRequest(ctx, fmt.Sprintf("/Calls/%s.json", input.ProviderCallID), params)
	if err != nil {
		return fmt.Errorf("twilio: failed to start listening: %w", err)
	}

	return nil
}

// StopListening stops speech recognition (no-op for Twilio).
func (p *TwilioProvider) StopListening(ctx context.Context, callID, providerCallID string) error {
	// Twilio's <Gather> automatically stops on speech end
	return nil
}

// VerifyWebhook validates webhook authenticity using HMAC-SHA1.
func (p *TwilioProvider) VerifyWebhook(ctx *WebhookContext) (bool, error) {
	signature := ctx.Headers["x-twilio-signature"]
	if signature == "" {
		signature = ctx.Headers["X-Twilio-Signature"]
	}
	if signature == "" {
		return false, nil
	}

	// Build the full URL for signature verification
	fullURL := ctx.URL

	// Parse body as form data
	params, err := url.ParseQuery(ctx.Body)
	if err != nil {
		return false, fmt.Errorf("twilio: failed to parse body: %w", err)
	}

	// Build signature string: URL + sorted params
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	sigString := fullURL
	for _, k := range keys {
		for _, v := range params[k] {
			sigString += k + v
		}
	}

	// Compute HMAC-SHA1
	mac := hmac.New(sha1.New, []byte(p.authToken))
	mac.Write([]byte(sigString))
	expected := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expected)), nil
}

// ParseWebhook parses a webhook into events.
func (p *TwilioProvider) ParseWebhook(ctx *WebhookContext) (*WebhookParseResult, error) {
	params, err := url.ParseQuery(ctx.Body)
	if err != nil {
		return nil, fmt.Errorf("twilio: failed to parse body: %w", err)
	}

	callSID := params.Get("CallSid")
	callID := ctx.Query["callId"]
	if callID == "" {
		callID = callSID
	}

	event := p.normalizeEvent(params, callID, callSID)

	// Generate TwiML response
	twiml := p.generateTwiML(ctx, params)

	result := &WebhookParseResult{
		ResponseBody:    twiml,
		ResponseHeaders: map[string]string{"Content-Type": "application/xml"},
		StatusCode:      200,
	}

	if event != nil {
		result.Events = []CallEvent{*event}
	}

	return result, nil
}

// normalizeEvent converts Twilio webhook params to a normalized event.
func (p *TwilioProvider) normalizeEvent(params url.Values, callID, callSID string) *CallEvent {
	baseEvent := &CallEvent{
		ID:             uuid.New().String(),
		CallID:         callID,
		ProviderCallID: callSID,
		Timestamp:      time.Now(),
		From:           params.Get("From"),
		To:             params.Get("To"),
	}

	// Parse direction
	direction := params.Get("Direction")
	if direction == "inbound" {
		baseEvent.Direction = DirectionInbound
	} else if direction == "outbound-api" || direction == "outbound-dial" {
		baseEvent.Direction = DirectionOutbound
	}

	// Handle speech result
	if speechResult := params.Get("SpeechResult"); speechResult != "" {
		baseEvent.Type = EventCallSpeech
		baseEvent.Transcript = speechResult
		baseEvent.IsFinal = true
		if conf := params.Get("Confidence"); conf != "" {
			if _, err := fmt.Sscanf(conf, "%f", &baseEvent.Confidence); err != nil {
				baseEvent.Confidence = 0
			}
		}
		return baseEvent
	}

	// Handle DTMF
	if digits := params.Get("Digits"); digits != "" {
		baseEvent.Type = EventCallDTMF
		baseEvent.Digits = digits
		return baseEvent
	}

	// Handle call status
	switch params.Get("CallStatus") {
	case "initiated":
		baseEvent.Type = EventCallInitiated
		return baseEvent
	case "ringing":
		baseEvent.Type = EventCallRinging
		return baseEvent
	case "in-progress":
		baseEvent.Type = EventCallAnswered
		return baseEvent
	case "completed":
		baseEvent.Type = EventCallEnded
		baseEvent.Reason = EndReasonCompleted
		return baseEvent
	case "busy":
		baseEvent.Type = EventCallEnded
		baseEvent.Reason = EndReasonBusy
		return baseEvent
	case "no-answer":
		baseEvent.Type = EventCallEnded
		baseEvent.Reason = EndReasonNoAnswer
		return baseEvent
	case "failed":
		baseEvent.Type = EventCallEnded
		baseEvent.Reason = EndReasonFailed
		return baseEvent
	case "canceled":
		baseEvent.Type = EventCallEnded
		baseEvent.Reason = EndReasonHangupBot
		return baseEvent
	}

	return nil
}

// generateTwiML creates a TwiML response for the webhook.
func (p *TwilioProvider) generateTwiML(ctx *WebhookContext, params url.Values) string {
	callStatus := params.Get("CallStatus")
	direction := params.Get("Direction")
	isStatusCallback := ctx.Query["type"] == "status"

	if isStatusCallback {
		return `<?xml version="1.0" encoding="UTF-8"?><Response></Response>`
	}

	// For inbound calls or answered outbound calls, connect to stream if available
	if direction == "inbound" || callStatus == "in-progress" {
		if streamURL := p.getStreamURL(); streamURL != "" {
			return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Connect>
    <Stream url="%s" />
  </Connect>
</Response>`, escapeXML(streamURL))
		}
		return `<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Pause length="30"/>
</Response>`
	}

	return `<?xml version="1.0" encoding="UTF-8"?><Response></Response>`
}

// getStreamURL returns the WebSocket URL for media streaming.
func (p *TwilioProvider) getStreamURL() string {
	p.mu.RLock()
	publicURL := p.publicURL
	streamPath := p.streamPath
	p.mu.RUnlock()

	if publicURL == "" || streamPath == "" {
		return ""
	}

	u, err := url.Parse(publicURL)
	if err != nil {
		return ""
	}

	// Convert to WebSocket URL
	scheme := "wss"
	if u.Scheme == "http" {
		scheme = "ws"
	}

	return fmt.Sprintf("%s://%s%s", scheme, u.Host, streamPath)
}

// apiRequest makes an authenticated request to the Twilio API.
func (p *TwilioProvider) apiRequest(ctx context.Context, endpoint string, params url.Values) ([]byte, error) {
	reqURL := p.baseURL + endpoint

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewBufferString(params.Encode()))
	if err != nil {
		return nil, err
	}

	req.SetBasicAuth(p.accountSID, p.authToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 1<<20 {
		return nil, fmt.Errorf("API response too large (%d bytes)", len(body))
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// escapeXML escapes special characters for XML content.
func escapeXML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "'", "&apos;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
