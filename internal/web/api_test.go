package web

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/cron"
	"github.com/haasonsaas/nexus/internal/edge"
	"github.com/haasonsaas/nexus/pkg/models"
)

func TestIsSensitiveKey(t *testing.T) {
	tests := []struct {
		key      string
		expected bool
	}{
		{"api_key", true},
		{"apikey", true},
		{"API_KEY", true},
		{"token", true},
		{"access_token", true},
		{"secret", true},
		{"client_secret", true},
		{"password", true},
		{"jwt", true},
		{"signing_key", true},
		{"private_key", true},
		{"name", false},
		{"enabled", false},
		{"host", false},
		{"port", false},
		{"username", false},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := isSensitiveKey(tt.key); got != tt.expected {
				t.Errorf("isSensitiveKey(%q) = %v, want %v", tt.key, got, tt.expected)
			}
		})
	}
}

func TestSanitizeAttachmentFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal.txt", "normal.txt"},
		{"file\r\nname.txt", "filename.txt"},
		{"file\"name.txt", "filename.txt"},
		{"file\\name.txt", "filename.txt"},
		{"  spaced.txt  ", "spaced.txt"},
		{"file\rname\nwith\rstuff.txt", "filenamewithstuff.txt"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := sanitizeAttachmentFilename(tt.input); got != tt.expected {
				t.Errorf("sanitizeAttachmentFilename(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSetPathValue(t *testing.T) {
	t.Run("simple path", func(t *testing.T) {
		raw := map[string]any{}
		setPathValue(raw, "key", "value")
		if raw["key"] != "value" {
			t.Errorf("raw[key] = %v, want value", raw["key"])
		}
	})

	t.Run("nested path", func(t *testing.T) {
		raw := map[string]any{}
		setPathValue(raw, "a.b.c", "deep")
		a := raw["a"].(map[string]any)
		b := a["b"].(map[string]any)
		if b["c"] != "deep" {
			t.Errorf("raw[a][b][c] = %v, want deep", b["c"])
		}
	})

	t.Run("overwrite existing", func(t *testing.T) {
		raw := map[string]any{"key": "old"}
		setPathValue(raw, "key", "new")
		if raw["key"] != "new" {
			t.Errorf("raw[key] = %v, want new", raw["key"])
		}
	})

	t.Run("empty path parts ignored", func(t *testing.T) {
		raw := map[string]any{}
		setPathValue(raw, "a..b", "value")
		// Should still work with empty parts
	})
}

func TestMergeMaps(t *testing.T) {
	t.Run("simple merge", func(t *testing.T) {
		dst := map[string]any{"a": 1}
		src := map[string]any{"b": 2}
		mergeMaps(dst, src)
		if dst["a"] != 1 || dst["b"] != 2 {
			t.Errorf("merge failed: %v", dst)
		}
	})

	t.Run("overwrite", func(t *testing.T) {
		dst := map[string]any{"a": 1}
		src := map[string]any{"a": 2}
		mergeMaps(dst, src)
		if dst["a"] != 2 {
			t.Errorf("dst[a] = %v, want 2", dst["a"])
		}
	})

	t.Run("deep merge", func(t *testing.T) {
		dst := map[string]any{
			"nested": map[string]any{"a": 1},
		}
		src := map[string]any{
			"nested": map[string]any{"b": 2},
		}
		mergeMaps(dst, src)
		nested := dst["nested"].(map[string]any)
		if nested["a"] != 1 || nested["b"] != 2 {
			t.Errorf("deep merge failed: %v", nested)
		}
	})
}

func TestFormatSchedule(t *testing.T) {
	tests := []struct {
		name     string
		schedule cron.Schedule
		contains string
	}{
		{
			name:     "cron expression",
			schedule: cron.Schedule{Kind: "cron", CronExpr: "0 * * * *"},
			contains: "cron: 0 * * * *",
		},
		{
			name:     "every interval",
			schedule: cron.Schedule{Kind: "every", Every: 5 * time.Minute},
			contains: "every 5m",
		},
		{
			name:     "every with timezone",
			schedule: cron.Schedule{Kind: "every", Every: time.Hour, Timezone: "UTC"},
			contains: "UTC",
		},
		{
			name:     "at time",
			schedule: cron.Schedule{Kind: "at", At: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)},
			contains: "at",
		},
		{
			name:     "unknown kind",
			schedule: cron.Schedule{Kind: "custom"},
			contains: "custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatSchedule(tt.schedule)
			if !contains(result, tt.contains) {
				t.Errorf("formatSchedule() = %q, want to contain %q", result, tt.contains)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestChannelEnabled(t *testing.T) {
	t.Run("nil config returns true", func(t *testing.T) {
		if !channelEnabled(nil, models.ChannelTelegram) {
			t.Error("expected true for nil config")
		}
	})

	t.Run("unknown channel returns true", func(t *testing.T) {
		if !channelEnabled(nil, models.ChannelType("unknown")) {
			t.Error("expected true for unknown channel")
		}
	})
}

func TestEdgeExecuteOptions_ToExecuteOptions(t *testing.T) {
	t.Run("with timeout", func(t *testing.T) {
		opts := edgeExecuteOptions{
			timeoutSeconds: 30,
			approved:       true,
			sessionID:      "session-1",
			runID:          "run-1",
			metadata:       map[string]string{"key": "value"},
		}
		result := opts.toExecuteOptions()

		if result.Timeout != 30*time.Second {
			t.Errorf("Timeout = %v, want 30s", result.Timeout)
		}
		if !result.Approved {
			t.Error("Approved should be true")
		}
		if result.SessionID != "session-1" {
			t.Errorf("SessionID = %q, want session-1", result.SessionID)
		}
		if result.RunID != "run-1" {
			t.Errorf("RunID = %q, want run-1", result.RunID)
		}
	})

	t.Run("zero timeout", func(t *testing.T) {
		opts := edgeExecuteOptions{timeoutSeconds: 0}
		result := opts.toExecuteOptions()
		if result.Timeout != 0 {
			t.Errorf("Timeout = %v, want 0", result.Timeout)
		}
	})
}

func TestRedactConfigMap(t *testing.T) {
	t.Run("redacts sensitive keys", func(t *testing.T) {
		raw := map[string]any{
			"api_key":  "secret123",
			"password": "mypass",
			"host":     "localhost",
		}
		result := redactConfigMap(raw)
		if result["api_key"] != "***" {
			t.Errorf("api_key should be redacted")
		}
		if result["password"] != "***" {
			t.Errorf("password should be redacted")
		}
		if result["host"] != "localhost" {
			t.Errorf("host should not be redacted")
		}
	})

	t.Run("handles nested maps", func(t *testing.T) {
		raw := map[string]any{
			"config": map[string]any{
				"token": "secret",
				"name":  "test",
			},
		}
		result := redactConfigMap(raw)
		nested := result["config"].(map[string]any)
		if nested["token"] != "***" {
			t.Errorf("nested token should be redacted")
		}
		if nested["name"] != "test" {
			t.Errorf("nested name should not be redacted")
		}
	})

	t.Run("handles slices", func(t *testing.T) {
		raw := map[string]any{
			"items": []any{
				map[string]any{"secret": "value"},
			},
		}
		result := redactConfigMap(raw)
		items := result["items"].([]any)
		item := items[0].(map[string]any)
		if item["secret"] != "***" {
			t.Errorf("slice item secret should be redacted")
		}
	})
}

func TestSystemStatus_JSON(t *testing.T) {
	status := SystemStatus{
		Uptime:         time.Hour,
		UptimeString:   "1h0m0s",
		GoVersion:      "go1.21",
		NumGoroutines:  100,
		MemAllocMB:     50.5,
		MemSysMB:       100.0,
		NumCPU:         8,
		SessionCount:   10,
		DatabaseStatus: "connected",
		Channels:       []ChannelStatus{},
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded SystemStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.GoVersion != "go1.21" {
		t.Errorf("GoVersion = %q, want go1.21", decoded.GoVersion)
	}
	if decoded.NumCPU != 8 {
		t.Errorf("NumCPU = %d, want 8", decoded.NumCPU)
	}
}

func TestChannelStatus_JSON(t *testing.T) {
	status := ChannelStatus{
		Name:      "telegram",
		Type:      "telegram",
		Status:    "connected",
		Enabled:   true,
		Connected: true,
		Healthy:   true,
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded ChannelStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.Name != "telegram" {
		t.Errorf("Name = %q, want telegram", decoded.Name)
	}
	if !decoded.Enabled {
		t.Error("Enabled should be true")
	}
}

func TestProviderStatus_JSON(t *testing.T) {
	status := ProviderStatus{
		Name:          "slack",
		Enabled:       true,
		Connected:     true,
		Healthy:       true,
		HealthMessage: "OK",
		QRAvailable:   false,
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded ProviderStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.Name != "slack" {
		t.Errorf("Name = %q, want slack", decoded.Name)
	}
}

func TestCronJobSummary_JSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	summary := CronJobSummary{
		ID:       "job-1",
		Name:     "Test Job",
		Type:     "heartbeat",
		Enabled:  true,
		Schedule: "every 5m",
		NextRun:  now.Add(time.Hour),
		LastRun:  now,
	}

	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded CronJobSummary
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.ID != "job-1" {
		t.Errorf("ID = %q, want job-1", decoded.ID)
	}
	if decoded.Schedule != "every 5m" {
		t.Errorf("Schedule = %q, want every 5m", decoded.Schedule)
	}
}

func TestSessionSummary_JSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	summary := SessionSummary{
		ID:        "session-123",
		Title:     "Test Session",
		Channel:   "telegram",
		ChannelID: "chat-456",
		AgentID:   "agent-1",
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded SessionSummary
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.ID != "session-123" {
		t.Errorf("ID = %q, want session-123", decoded.ID)
	}
	if decoded.Channel != "telegram" {
		t.Errorf("Channel = %q, want telegram", decoded.Channel)
	}
}

func TestAPIArtifactSummary_JSON(t *testing.T) {
	summary := APIArtifactSummary{
		ID:         "art-1",
		Type:       "file",
		MimeType:   "text/plain",
		Filename:   "test.txt",
		Size:       1024,
		Reference:  "s3://bucket/key",
		TTLSeconds: 3600,
		Redacted:   false,
	}

	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded APIArtifactSummary
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.ID != "art-1" {
		t.Errorf("ID = %q, want art-1", decoded.ID)
	}
	if decoded.Size != 1024 {
		t.Errorf("Size = %d, want 1024", decoded.Size)
	}
}

func TestNodeSummary_JSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	summary := NodeSummary{
		EdgeID:        "edge-1",
		Name:          "Test Node",
		Status:        "connected",
		ConnectedAt:   now,
		LastHeartbeat: now,
		Tools:         []string{"tool1", "tool2"},
		Version:       "1.0.0",
	}

	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}

	var decoded NodeSummary
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}

	if decoded.EdgeID != "edge-1" {
		t.Errorf("EdgeID = %q, want edge-1", decoded.EdgeID)
	}
	if len(decoded.Tools) != 2 {
		t.Errorf("Tools length = %d, want 2", len(decoded.Tools))
	}
}

// Compile-time interface check
var _ edge.ExecuteOptions = edgeExecuteOptions{}.toExecuteOptions()
