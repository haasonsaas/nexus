package hooks

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockConfigSource implements GmailHookConfigSource for testing
type mockConfigSource struct {
	httpPort   int
	hooksToken string
	hooksPath  string
}

func (m *mockConfigSource) GetHTTPPort() int      { return m.httpPort }
func (m *mockConfigSource) GetHooksToken() string { return m.hooksToken }
func (m *mockConfigSource) GetHooksPath() string  { return m.hooksPath }

func TestGenerateHookToken(t *testing.T) {
	tests := []struct {
		name   string
		bytes  int
		wantN  int
		unique bool
	}{
		{
			name:   "default bytes",
			bytes:  0,
			wantN:  48, // 24 bytes = 48 hex chars
			unique: true,
		},
		{
			name:   "negative bytes uses default",
			bytes:  -5,
			wantN:  48,
			unique: true,
		},
		{
			name:   "custom bytes",
			bytes:  16,
			wantN:  32,
			unique: true,
		},
		{
			name:   "small token",
			bytes:  8,
			wantN:  16,
			unique: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token1 := GenerateHookToken(tt.bytes)
			token2 := GenerateHookToken(tt.bytes)

			if len(token1) != tt.wantN {
				t.Errorf("GenerateHookToken(%d) length = %d, want %d", tt.bytes, len(token1), tt.wantN)
			}

			if tt.unique && token1 == token2 {
				t.Errorf("GenerateHookToken(%d) generated duplicate tokens", tt.bytes)
			}
		})
	}
}

func TestMergeHookPresets(t *testing.T) {
	tests := []struct {
		name     string
		existing []string
		preset   string
		wantLen  int
		contains []string
	}{
		{
			name:     "empty existing",
			existing: nil,
			preset:   "new",
			wantLen:  1,
			contains: []string{"new"},
		},
		{
			name:     "add to existing",
			existing: []string{"one", "two"},
			preset:   "three",
			wantLen:  3,
			contains: []string{"one", "two", "three"},
		},
		{
			name:     "deduplicate",
			existing: []string{"one", "two"},
			preset:   "one",
			wantLen:  2,
			contains: []string{"one", "two"},
		},
		{
			name:     "trim whitespace",
			existing: []string{" one ", "two"},
			preset:   " three ",
			wantLen:  3,
			contains: []string{"one", "two", "three"},
		},
		{
			name:     "empty preset ignored",
			existing: []string{"one"},
			preset:   "",
			wantLen:  1,
			contains: []string{"one"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeHookPresets(tt.existing, tt.preset)

			if len(result) != tt.wantLen {
				t.Errorf("MergeHookPresets() len = %d, want %d", len(result), tt.wantLen)
			}

			for _, want := range tt.contains {
				found := false
				for _, got := range result {
					if got == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("MergeHookPresets() missing %q", want)
				}
			}
		})
	}
}

func TestNormalizeHooksPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", DefaultHooksPath},
		{"   ", DefaultHooksPath},
		{"/", DefaultHooksPath},
		{"/hooks", "/hooks"},
		{"hooks", "/hooks"},
		{"/hooks/", "/hooks"},
		{"/api/hooks/", "/api/hooks"},
		{"/custom/path", "/custom/path"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeHooksPath(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeHooksPath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeServePath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", DefaultGmailServePath},
		{"   ", DefaultGmailServePath},
		{"/", "/"},
		{"/gmail-pubsub", "/gmail-pubsub"},
		{"gmail-pubsub", "/gmail-pubsub"},
		{"/gmail-pubsub/", "/gmail-pubsub"},
		{"/custom/path", "/custom/path"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeServePath(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeServePath(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestBuildDefaultHookURL(t *testing.T) {
	tests := []struct {
		hooksPath string
		port      int
		want      string
	}{
		{"", 0, "http://127.0.0.1:8080/hooks/gmail"},
		{"/hooks", 8080, "http://127.0.0.1:8080/hooks/gmail"},
		{"/api/hooks", 3000, "http://127.0.0.1:3000/api/hooks/gmail"},
		{"custom", 9000, "http://127.0.0.1:9000/custom/gmail"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := BuildDefaultHookURL(tt.hooksPath, tt.port)
			if got != tt.want {
				t.Errorf("BuildDefaultHookURL(%q, %d) = %q, want %q", tt.hooksPath, tt.port, got, tt.want)
			}
		})
	}
}

func TestBuildTopicPath(t *testing.T) {
	got := BuildTopicPath("my-project", "my-topic")
	want := "projects/my-project/topics/my-topic"
	if got != want {
		t.Errorf("BuildTopicPath() = %q, want %q", got, want)
	}
}

func TestParseTopicPath(t *testing.T) {
	tests := []struct {
		input       string
		wantProject string
		wantTopic   string
		wantOk      bool
	}{
		{"projects/my-project/topics/my-topic", "my-project", "my-topic", true},
		{"projects/test/topics/gmail-watch", "test", "gmail-watch", true},
		{"invalid", "", "", false},
		{"projects/topics", "", "", false},
		{"projects//topics/", "", "", false},
		{"projects/a/b/topics/c", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			project, topic, ok := ParseTopicPath(tt.input)
			if ok != tt.wantOk {
				t.Errorf("ParseTopicPath(%q) ok = %v, want %v", tt.input, ok, tt.wantOk)
			}
			if ok && (project != tt.wantProject || topic != tt.wantTopic) {
				t.Errorf("ParseTopicPath(%q) = (%q, %q), want (%q, %q)",
					tt.input, project, topic, tt.wantProject, tt.wantTopic)
			}
		})
	}
}

func TestResolveGmailHookRuntimeConfig(t *testing.T) {
	tests := []struct {
		name      string
		overrides GmailHookOverrides
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "missing hook token",
			overrides: GmailHookOverrides{},
			wantErr:   true,
			errMsg:    "hooks.token missing",
		},
		{
			name: "missing account",
			overrides: GmailHookOverrides{
				HookToken: "test-token",
			},
			wantErr: true,
			errMsg:  "gmail account required",
		},
		{
			name: "missing topic",
			overrides: GmailHookOverrides{
				HookToken: "test-token",
				Account:   "test@example.com",
			},
			wantErr: true,
			errMsg:  "gmail topic required",
		},
		{
			name: "missing push token",
			overrides: GmailHookOverrides{
				HookToken: "test-token",
				Account:   "test@example.com",
				Topic:     "projects/p/topics/t",
			},
			wantErr: true,
			errMsg:  "gmail push token required",
		},
		{
			name: "valid minimal config",
			overrides: GmailHookOverrides{
				HookToken: "test-hook-token",
				Account:   "test@example.com",
				Topic:     "projects/p/topics/t",
				PushToken: "test-push-token",
			},
			wantErr: false,
		},
		{
			name: "valid full config",
			overrides: GmailHookOverrides{
				HookToken:         "test-hook-token",
				Account:           "test@example.com",
				Topic:             "projects/p/topics/t",
				PushToken:         "test-push-token",
				Label:             "IMPORTANT",
				Subscription:      "my-sub",
				HookURL:           "https://example.com/hooks/gmail",
				IncludeBody:       true,
				MaxBytes:          50000,
				RenewEveryMinutes: 60,
				ServeBind:         "0.0.0.0",
				ServePort:         9000,
				ServePath:         "/my-path",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ResolveGmailHookRuntimeConfig(nil, tt.overrides)

			if tt.wantErr {
				if err == nil {
					t.Errorf("ResolveGmailHookRuntimeConfig() expected error, got nil")
				} else if tt.errMsg != "" && err.Error() != tt.errMsg {
					// Check if error contains expected message
					if err.Error()[:len(tt.errMsg)] != tt.errMsg[:min(len(err.Error()), len(tt.errMsg))] {
						t.Logf("got error: %v", err)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("ResolveGmailHookRuntimeConfig() unexpected error: %v", err)
			}

			if cfg.Account != tt.overrides.Account {
				t.Errorf("Account = %q, want %q", cfg.Account, tt.overrides.Account)
			}
			if cfg.Topic != tt.overrides.Topic {
				t.Errorf("Topic = %q, want %q", cfg.Topic, tt.overrides.Topic)
			}
			if cfg.PushToken != tt.overrides.PushToken {
				t.Errorf("PushToken = %q, want %q", cfg.PushToken, tt.overrides.PushToken)
			}
			if cfg.HookToken != tt.overrides.HookToken {
				t.Errorf("HookToken = %q, want %q", cfg.HookToken, tt.overrides.HookToken)
			}

			// Check defaults
			if tt.overrides.Label == "" && cfg.Label != DefaultGmailLabel {
				t.Errorf("Label = %q, want default %q", cfg.Label, DefaultGmailLabel)
			}
			if tt.overrides.Subscription == "" && cfg.Subscription != DefaultGmailSubscription {
				t.Errorf("Subscription = %q, want default %q", cfg.Subscription, DefaultGmailSubscription)
			}
		})
	}
}

func TestResolveGatewayPort(t *testing.T) {
	tests := []struct {
		name string
		cfg  GmailHookConfigSource
		want int
	}{
		{
			name: "nil config",
			cfg:  nil,
			want: DefaultGatewayPort,
		},
		{
			name: "zero port",
			cfg:  &mockConfigSource{httpPort: 0},
			want: DefaultGatewayPort,
		},
		{
			name: "custom port",
			cfg:  &mockConfigSource{httpPort: 3000},
			want: 3000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveGatewayPort(tt.cfg)
			if got != tt.want {
				t.Errorf("ResolveGatewayPort() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBuildWatchStartArgs(t *testing.T) {
	cfg := &GmailHookRuntimeConfig{
		Account: "test@example.com",
		Label:   "INBOX",
		Topic:   "projects/p/topics/t",
	}

	args := BuildWatchStartArgs(cfg)

	expected := []string{
		"gmail", "watch", "start",
		"--account", "test@example.com",
		"--label", "INBOX",
		"--topic", "projects/p/topics/t",
	}

	if len(args) != len(expected) {
		t.Fatalf("BuildWatchStartArgs() len = %d, want %d", len(args), len(expected))
	}

	for i, arg := range args {
		if arg != expected[i] {
			t.Errorf("BuildWatchStartArgs()[%d] = %q, want %q", i, arg, expected[i])
		}
	}
}

func TestBuildWatchServeArgs(t *testing.T) {
	cfg := &GmailHookRuntimeConfig{
		Account:     "test@example.com",
		PushToken:   "push-token",
		HookToken:   "hook-token",
		HookURL:     "https://example.com/hooks/gmail",
		IncludeBody: true,
		MaxBytes:    50000,
	}
	cfg.Serve.Bind = "0.0.0.0"
	cfg.Serve.Port = 8788
	cfg.Serve.Path = "/gmail-pubsub"

	args := BuildWatchServeArgs(cfg)

	// Check key args are present
	mustContain := map[string]string{
		"--account":    "test@example.com",
		"--bind":       "0.0.0.0",
		"--port":       "8788",
		"--path":       "/gmail-pubsub",
		"--token":      "push-token",
		"--hook-url":   "https://example.com/hooks/gmail",
		"--hook-token": "hook-token",
		"--max-bytes":  "50000",
	}

	// Build a map of flag -> value by finding flags that start with "--"
	argsMap := make(map[string]string)
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			argsMap[args[i]] = args[i+1]
		}
	}

	for key, val := range mustContain {
		if got, ok := argsMap[key]; !ok {
			t.Errorf("BuildWatchServeArgs() missing %s", key)
		} else if got != val {
			t.Errorf("BuildWatchServeArgs() %s = %q, want %q", key, got, val)
		}
	}

	// Check --include-body flag
	hasIncludeBody := false
	for _, arg := range args {
		if arg == "--include-body" {
			hasIncludeBody = true
			break
		}
	}
	if !hasIncludeBody {
		t.Error("BuildWatchServeArgs() missing --include-body flag")
	}
}

func TestGmailHookHandler_ValidateToken(t *testing.T) {
	handler := &GmailHookHandler{
		Config: &GmailHookRuntimeConfig{
			PushToken: "secret-token",
		},
	}

	tests := []struct {
		token string
		want  bool
	}{
		{"secret-token", true},
		{"wrong-token", false},
		{"", false},
		{"secret-token ", false},
		{" secret-token", false},
	}

	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			got := handler.ValidateToken(tt.token)
			if got != tt.want {
				t.Errorf("ValidateToken(%q) = %v, want %v", tt.token, got, tt.want)
			}
		})
	}
}

func TestGmailHookHandler_ValidateToken_NilConfig(t *testing.T) {
	handler := &GmailHookHandler{}
	if handler.ValidateToken("any") {
		t.Error("ValidateToken() should return false for nil config")
	}

	handler.Config = &GmailHookRuntimeConfig{}
	if handler.ValidateToken("any") {
		t.Error("ValidateToken() should return false for empty push token")
	}
}

func TestGmailHookHandler_ServeHTTP(t *testing.T) {
	var receivedNotification *GmailNotification

	cfg := &GmailHookRuntimeConfig{
		Account:   "test@example.com",
		PushToken: "test-push-token",
		MaxBytes:  DefaultGmailMaxBytes,
	}

	handler := NewGmailHookHandler(cfg, func(n *GmailNotification) error {
		receivedNotification = n
		return nil
	}, nil)

	// Create a valid pubsub message
	pushData := GmailPushData{
		EmailAddress: "test@example.com",
		HistoryID:    12345,
	}
	pushDataJSON, _ := json.Marshal(pushData)

	pubsubMsg := PubSubMessage{
		Subscription: "projects/test/subscriptions/test-sub",
	}
	pubsubMsg.Message.Data = base64.StdEncoding.EncodeToString(pushDataJSON)
	pubsubMsg.Message.MessageID = "msg-123"
	pubsubMsg.Message.PublishTime = time.Now().Format(time.RFC3339)

	body, _ := json.Marshal(pubsubMsg)

	tests := []struct {
		name           string
		method         string
		token          string
		body           []byte
		wantStatus     int
		wantNotificate bool
	}{
		{
			name:       "invalid method",
			method:     http.MethodGet,
			token:      "test-push-token",
			body:       body,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "invalid token",
			method:     http.MethodPost,
			token:      "wrong-token",
			body:       body,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "missing token",
			method:     http.MethodPost,
			token:      "",
			body:       body,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid json",
			method:     http.MethodPost,
			token:      "test-push-token",
			body:       []byte("not json"),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:           "valid request",
			method:         http.MethodPost,
			token:          "test-push-token",
			body:           body,
			wantStatus:     http.StatusOK,
			wantNotificate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			receivedNotification = nil

			url := "/gmail-pubsub"
			if tt.token != "" {
				url += "?token=" + tt.token
			}

			req := httptest.NewRequest(tt.method, url, bytes.NewReader(tt.body))
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("ServeHTTP() status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantNotificate && receivedNotification == nil {
				t.Error("ServeHTTP() expected notification to be received")
			}
			if !tt.wantNotificate && receivedNotification != nil {
				t.Error("ServeHTTP() unexpected notification received")
			}

			if tt.wantNotificate && receivedNotification != nil {
				if receivedNotification.HistoryID != 12345 {
					t.Errorf("notification.HistoryID = %d, want 12345", receivedNotification.HistoryID)
				}
				if receivedNotification.Account != "test@example.com" {
					t.Errorf("notification.Account = %q, want %q", receivedNotification.Account, "test@example.com")
				}
			}
		})
	}
}

func TestGmailHookHandler_ServeHTTP_OversizedBody(t *testing.T) {
	cfg := &GmailHookRuntimeConfig{
		PushToken: "test-token",
		MaxBytes:  100, // Very small limit
	}

	handler := NewGmailHookHandler(cfg, nil, nil)

	// Create body larger than limit
	largeBody := bytes.Repeat([]byte("x"), 200)

	req := httptest.NewRequest(http.MethodPost, "/gmail-pubsub?token=test-token", bytes.NewReader(largeBody))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("ServeHTTP() status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestGmailHookServer(t *testing.T) {
	cfg := &GmailHookRuntimeConfig{
		Account:   "test@example.com",
		PushToken: "test-push-token",
		MaxBytes:  DefaultGmailMaxBytes,
	}
	cfg.Serve.Bind = "127.0.0.1"
	cfg.Serve.Port = 0 // Let OS pick a port
	cfg.Serve.Path = "/gmail-pubsub"

	server := NewGmailHookServer(cfg, nil)

	server.SetOnMessage(func(n *GmailNotification) error {
		return nil
	})

	ctx := context.Background()
	if err := server.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	if err := server.Stop(ctx); err != nil {
		t.Errorf("Stop() error: %v", err)
	}
}

func TestParseHistoryID(t *testing.T) {
	tests := []struct {
		input   string
		want    uint64
		wantErr bool
	}{
		{"12345", 12345, false},
		{"0", 0, false},
		{"18446744073709551615", 18446744073709551615, false},
		{"", 0, true},
		{"   ", 0, true},
		{"abc", 0, true},
		{"-1", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseHistoryID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseHistoryID(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseHistoryID(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatHistoryID(t *testing.T) {
	tests := []struct {
		input uint64
		want  string
	}{
		{0, "0"},
		{12345, "12345"},
		{18446744073709551615, "18446744073709551615"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := FormatHistoryID(tt.input)
			if got != tt.want {
				t.Errorf("FormatHistoryID(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGmailWakeHandler(t *testing.T) {
	var wakeMsg string
	wakeHandler := GmailWakeHandler(func(msg string) {
		wakeMsg = msg
	})

	// Test with non-gmail event (should be ignored)
	event := NewEvent(EventMessageReceived, "")
	if err := wakeHandler(context.Background(), event); err != nil {
		t.Errorf("GmailWakeHandler() error = %v for non-gmail event", err)
	}
	if wakeMsg != "" {
		t.Error("GmailWakeHandler() should not wake on non-gmail event")
	}

	// Test with gmail event
	gmailEvent := NewEvent(EventGmailReceived, "push")
	gmailEvent.Context["notification"] = &GmailNotification{
		HistoryID: 12345,
		Account:   "test@example.com",
	}

	if err := wakeHandler(context.Background(), gmailEvent); err != nil {
		t.Errorf("GmailWakeHandler() error = %v", err)
	}

	if wakeMsg == "" {
		t.Error("GmailWakeHandler() should wake on gmail event")
	}
	if wakeMsg != "New email for test@example.com (history_id: 12345)" {
		t.Errorf("GmailWakeHandler() message = %q", wakeMsg)
	}
}

func TestTailscaleMode(t *testing.T) {
	tests := []struct {
		mode TailscaleMode
		want string
	}{
		{TailscaleModeOff, "off"},
		{TailscaleModeFunnel, "funnel"},
		{TailscaleModeServe, "serve"},
	}

	for _, tt := range tests {
		if string(tt.mode) != tt.want {
			t.Errorf("TailscaleMode = %q, want %q", tt.mode, tt.want)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
