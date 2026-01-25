package agent

import (
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestDefaultMaxToolResultSize(t *testing.T) {
	if DefaultMaxToolResultSize != 64*1024 {
		t.Errorf("DefaultMaxToolResultSize = %d, want %d", DefaultMaxToolResultSize, 64*1024)
	}
}

func TestToolResultGuard_SanitizeSecrets(t *testing.T) {
	guard := ToolResultGuard{
		SanitizeSecrets: true,
	}

	tests := []struct {
		name    string
		content string
		wantRed bool
	}{
		{
			name:    "api key",
			content: "api_key=sk-12345678901234567890",
			wantRed: true,
		},
		{
			name:    "bearer token",
			content: "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9",
			wantRed: true,
		},
		{
			name:    "password",
			content: "password=mysecretpassword",
			wantRed: true,
		},
		{
			name:    "private key",
			content: "-----BEGIN RSA PRIVATE KEY-----",
			wantRed: true,
		},
		{
			name:    "normal content",
			content: "This is normal output",
			wantRed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := models.ToolResult{Content: tt.content}
			guarded := guard.Apply("test_tool", result, nil)
			hasRedacted := strings.Contains(guarded.Content, "[REDACTED]")
			if hasRedacted != tt.wantRed {
				t.Errorf("Apply() redacted = %v, want %v; result = %q",
					hasRedacted, tt.wantRed, guarded.Content)
			}
		})
	}
}

func TestToolResultGuard_SanitizeSecretsDisabled(t *testing.T) {
	guard := ToolResultGuard{
		Enabled:         true,
		SanitizeSecrets: false,
	}

	result := models.ToolResult{Content: "api_key=sk-12345678901234567890"}
	guarded := guard.Apply("test_tool", result, nil)

	// Without SanitizeSecrets, the secret should NOT be redacted
	if strings.Contains(guarded.Content, "[REDACTED]") {
		t.Error("Secret was redacted even though SanitizeSecrets is false")
	}
}

func TestToolResultGuard_CustomRedactionText(t *testing.T) {
	guard := ToolResultGuard{
		SanitizeSecrets: true,
		RedactionText:   "[HIDDEN]",
	}

	result := models.ToolResult{Content: "api_key=sk-12345678901234567890"}
	guarded := guard.Apply("test_tool", result, nil)

	if !strings.Contains(guarded.Content, "[HIDDEN]") {
		t.Errorf("Expected custom redaction text [HIDDEN], got: %s", guarded.Content)
	}
}

func TestToolResultGuard_MaxCharsWithSecrets(t *testing.T) {
	guard := ToolResultGuard{
		MaxChars:        50,
		SanitizeSecrets: true,
	}

	// Create content with secret that exceeds limit even after redaction
	content := "api_key=sk-12345678901234567890 and lots and lots and lots and lots of extra text to ensure it's still over 50 chars after [REDACTED] replaces the secret"
	result := models.ToolResult{Content: content}
	guarded := guard.Apply("test_tool", result, nil)

	// Should have both redaction AND truncation
	if !strings.Contains(guarded.Content, "[REDACTED]") {
		t.Error("Secret was not redacted")
	}
	if !strings.Contains(guarded.Content, "[truncated]") {
		t.Errorf("Content was not truncated, got: %s", guarded.Content)
	}
}

func TestToolResultGuard_Active(t *testing.T) {
	tests := []struct {
		name   string
		guard  ToolResultGuard
		active bool
	}{
		{
			name:   "empty guard",
			guard:  ToolResultGuard{},
			active: false,
		},
		{
			name:   "enabled",
			guard:  ToolResultGuard{Enabled: true},
			active: true,
		},
		{
			name:   "max chars set",
			guard:  ToolResultGuard{MaxChars: 100},
			active: true,
		},
		{
			name:   "sanitize secrets",
			guard:  ToolResultGuard{SanitizeSecrets: true},
			active: true,
		},
		{
			name:   "denylist",
			guard:  ToolResultGuard{Denylist: []string{"tool"}},
			active: true,
		},
		{
			name:   "redact patterns",
			guard:  ToolResultGuard{RedactPatterns: []string{"secret"}},
			active: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.guard.active(); got != tt.active {
				t.Errorf("active() = %v, want %v", got, tt.active)
			}
		})
	}
}

func TestDetectSecrets(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "no secrets",
			content: "normal content",
			want:    nil,
		},
		{
			name:    "api key",
			content: "api_key=sk-12345678901234567890",
			want:    []string{"api_key"},
		},
		{
			name:    "multiple types",
			content: "api_key=test12345678901234567890 password=secret123456",
			want:    []string{"api_key", "generic_secret"},
		},
		{
			name:    "empty",
			content: "",
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectSecrets(tt.content)
			if len(got) != len(tt.want) {
				t.Errorf("DetectSecrets() = %v, want %v", got, tt.want)
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("DetectSecrets()[%d] = %q, want %q", i, v, tt.want[i])
				}
			}
		})
	}
}

func TestSanitizeToolResult(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantTrunc  bool
		wantRedact bool
	}{
		{
			name:       "normal content",
			input:      "hello world",
			wantTrunc:  false,
			wantRedact: false,
		},
		{
			name:       "with secret",
			input:      "password=supersecret123",
			wantTrunc:  false,
			wantRedact: true,
		},
		{
			name:       "large content",
			input:      strings.Repeat("a", DefaultMaxToolResultSize+100),
			wantTrunc:  true,
			wantRedact: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeToolResult(tt.input)
			hasTrunc := strings.Contains(result, "[truncated]")
			hasRedact := strings.Contains(result, "[REDACTED]")

			if hasTrunc != tt.wantTrunc {
				t.Errorf("truncated = %v, want %v", hasTrunc, tt.wantTrunc)
			}
			if hasRedact != tt.wantRedact {
				t.Errorf("redacted = %v, want %v", hasRedact, tt.wantRedact)
			}
		})
	}
}
