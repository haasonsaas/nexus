package links

import (
	"context"
	"testing"
	"time"
)

func TestRunLinkUnderstanding_Disabled(t *testing.T) {
	params := RunnerParams{
		Config:  &LinkToolsConfig{Enabled: false},
		Context: &MsgContext{Channel: "telegram"},
		Message: "Check https://example.com",
	}

	result, err := RunLinkUnderstanding(context.Background(), params)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(result.URLs) != 0 {
		t.Errorf("expected no URLs when disabled, got %v", result.URLs)
	}
}

func TestRunLinkUnderstanding_NilConfig(t *testing.T) {
	params := RunnerParams{
		Config:  nil,
		Context: &MsgContext{Channel: "telegram"},
		Message: "Check https://example.com",
	}

	result, err := RunLinkUnderstanding(context.Background(), params)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(result.URLs) != 0 {
		t.Errorf("expected no URLs with nil config, got %v", result.URLs)
	}
}

func TestRunLinkUnderstanding_NoLinks(t *testing.T) {
	params := RunnerParams{
		Config:  &LinkToolsConfig{Enabled: true},
		Context: &MsgContext{Channel: "telegram"},
		Message: "This message has no links",
	}

	result, err := RunLinkUnderstanding(context.Background(), params)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(result.URLs) != 0 {
		t.Errorf("expected no URLs, got %v", result.URLs)
	}
}

func TestRunLinkUnderstanding_ExtractsLinks(t *testing.T) {
	params := RunnerParams{
		Config: &LinkToolsConfig{
			Enabled:  true,
			MaxLinks: 5,
		},
		Context: &MsgContext{Channel: "telegram"},
		Message: "Check https://example.com and https://test.com",
	}

	result, err := RunLinkUnderstanding(context.Background(), params)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(result.URLs) != 2 {
		t.Errorf("expected 2 URLs, got %v", result.URLs)
	}
	if result.URLs[0] != "https://example.com" {
		t.Errorf("expected first URL to be https://example.com, got %s", result.URLs[0])
	}
	if result.URLs[1] != "https://test.com" {
		t.Errorf("expected second URL to be https://test.com, got %s", result.URLs[1])
	}
}

func TestRunLinkUnderstanding_ScopeDenied(t *testing.T) {
	params := RunnerParams{
		Config: &LinkToolsConfig{
			Enabled: true,
			Scope: &ScopeConfig{
				Mode:      "allowlist",
				Allowlist: []string{"discord"},
			},
		},
		Context: &MsgContext{Channel: "telegram"},
		Message: "Check https://example.com",
	}

	result, err := RunLinkUnderstanding(context.Background(), params)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(result.URLs) != 0 {
		t.Errorf("expected no URLs when scope denied, got %v", result.URLs)
	}
}

func TestRunLinkUnderstanding_ScopeAllowed(t *testing.T) {
	params := RunnerParams{
		Config: &LinkToolsConfig{
			Enabled: true,
			Scope: &ScopeConfig{
				Mode:      "allowlist",
				Allowlist: []string{"telegram", "discord"},
			},
		},
		Context: &MsgContext{Channel: "telegram"},
		Message: "Check https://example.com",
	}

	result, err := RunLinkUnderstanding(context.Background(), params)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if len(result.URLs) != 1 {
		t.Errorf("expected 1 URL, got %v", result.URLs)
	}
}

func TestApplyTemplateToArgs(t *testing.T) {
	msgCtx := &MsgContext{
		Channel:   "telegram",
		SessionID: "sess123",
		PeerID:    "peer456",
		AgentID:   "agent789",
	}

	tests := []struct {
		name string
		args []string
		url  string
		want []string
	}{
		{
			name: "LinkUrl template",
			args: []string{"fetch", "{{LinkUrl}}"},
			url:  "https://example.com",
			want: []string{"fetch", "https://example.com"},
		},
		{
			name: "URL template",
			args: []string{"--url={{URL}}"},
			url:  "https://example.com",
			want: []string{"--url=https://example.com"},
		},
		{
			name: "lowercase url template",
			args: []string{"{{url}}"},
			url:  "https://example.com",
			want: []string{"https://example.com"},
		},
		{
			name: "context templates",
			args: []string{"--channel={{Channel}}", "--session={{SessionID}}"},
			url:  "https://example.com",
			want: []string{"--channel=telegram", "--session=sess123"},
		},
		{
			name: "multiple templates in one arg",
			args: []string{"{{Channel}}:{{PeerID}}"},
			url:  "https://example.com",
			want: []string{"telegram:peer456"},
		},
		{
			name: "no templates",
			args: []string{"echo", "hello"},
			url:  "https://example.com",
			want: []string{"echo", "hello"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyTemplateToArgs(tt.args, msgCtx, tt.url)
			if len(got) != len(tt.want) {
				t.Errorf("applyTemplateToArgs() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("applyTemplateToArgs()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestApplyTemplateToArgs_NilContext(t *testing.T) {
	args := []string{"{{LinkUrl}}", "{{Channel}}"}
	got := applyTemplateToArgs(args, nil, "https://example.com")

	if got[0] != "https://example.com" {
		t.Errorf("expected URL to be replaced, got %q", got[0])
	}
	// Channel should not be replaced without context
	if got[1] != "{{Channel}}" {
		t.Errorf("expected Channel to remain unchanged without context, got %q", got[1])
	}
}

func TestResolveTimeout(t *testing.T) {
	tests := []struct {
		name          string
		entryTimeout  int
		configTimeout int
		want          time.Duration
	}{
		{
			name:          "entry timeout takes precedence",
			entryTimeout:  60,
			configTimeout: 30,
			want:          60 * time.Second,
		},
		{
			name:          "config timeout when entry is zero",
			entryTimeout:  0,
			configTimeout: 45,
			want:          45 * time.Second,
		},
		{
			name:          "default timeout when both are zero",
			entryTimeout:  0,
			configTimeout: 0,
			want:          30 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTimeout(tt.entryTimeout, tt.configTimeout)
			if got != tt.want {
				t.Errorf("resolveTimeout(%d, %d) = %v, want %v", tt.entryTimeout, tt.configTimeout, got, tt.want)
			}
		})
	}
}

func TestResolveScopeDecision(t *testing.T) {
	tests := []struct {
		name   string
		scope  *ScopeConfig
		msgCtx *MsgContext
		want   string
	}{
		{
			name:   "nil scope allows",
			scope:  nil,
			msgCtx: &MsgContext{Channel: "telegram"},
			want:   "allow",
		},
		{
			name:   "nil context allows",
			scope:  &ScopeConfig{Mode: "allowlist", Allowlist: []string{"telegram"}},
			msgCtx: nil,
			want:   "allow",
		},
		{
			name:   "empty mode allows all",
			scope:  &ScopeConfig{Mode: ""},
			msgCtx: &MsgContext{Channel: "telegram"},
			want:   "allow",
		},
		{
			name:   "all mode allows all",
			scope:  &ScopeConfig{Mode: "all"},
			msgCtx: &MsgContext{Channel: "telegram"},
			want:   "allow",
		},
		{
			name:   "allowlist mode - allowed",
			scope:  &ScopeConfig{Mode: "allowlist", Allowlist: []string{"telegram", "discord"}},
			msgCtx: &MsgContext{Channel: "telegram"},
			want:   "allow",
		},
		{
			name:   "allowlist mode - denied",
			scope:  &ScopeConfig{Mode: "allowlist", Allowlist: []string{"discord"}},
			msgCtx: &MsgContext{Channel: "telegram"},
			want:   "deny",
		},
		{
			name:   "denylist mode - denied",
			scope:  &ScopeConfig{Mode: "denylist", Denylist: []string{"telegram"}},
			msgCtx: &MsgContext{Channel: "telegram"},
			want:   "deny",
		},
		{
			name:   "denylist mode - allowed",
			scope:  &ScopeConfig{Mode: "denylist", Denylist: []string{"discord"}},
			msgCtx: &MsgContext{Channel: "telegram"},
			want:   "allow",
		},
		{
			name:   "allowlist with wildcard",
			scope:  &ScopeConfig{Mode: "allowlist", Allowlist: []string{"*"}},
			msgCtx: &MsgContext{Channel: "telegram"},
			want:   "allow",
		},
		{
			name:   "allowlist with channel:peer",
			scope:  &ScopeConfig{Mode: "allowlist", Allowlist: []string{"telegram:123"}},
			msgCtx: &MsgContext{Channel: "telegram", PeerID: "123"},
			want:   "allow",
		},
		{
			name:   "allowlist with channel:peer mismatch",
			scope:  &ScopeConfig{Mode: "allowlist", Allowlist: []string{"telegram:123"}},
			msgCtx: &MsgContext{Channel: "telegram", PeerID: "456"},
			want:   "deny",
		},
		{
			name:   "allowlist with channel:* wildcard",
			scope:  &ScopeConfig{Mode: "allowlist", Allowlist: []string{"telegram:*"}},
			msgCtx: &MsgContext{Channel: "telegram", PeerID: "any-peer"},
			want:   "allow",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveScopeDecision(tt.scope, tt.msgCtx)
			if got != tt.want {
				t.Errorf("resolveScopeDecision() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsScopeAllowed(t *testing.T) {
	scope := NewAllowlistScope("telegram", "discord")

	if !IsScopeAllowed(scope, "telegram", "123") {
		t.Error("expected telegram to be allowed")
	}
	if IsScopeAllowed(scope, "slack", "123") {
		t.Error("expected slack to be denied")
	}
}

func TestNewAllowlistScope(t *testing.T) {
	scope := NewAllowlistScope("telegram", "discord")

	if scope.Mode != "allowlist" {
		t.Errorf("expected mode to be allowlist, got %s", scope.Mode)
	}
	if len(scope.Allowlist) != 2 {
		t.Errorf("expected 2 allowlist entries, got %d", len(scope.Allowlist))
	}
}

func TestNewDenylistScope(t *testing.T) {
	scope := NewDenylistScope("spam", "test")

	if scope.Mode != "denylist" {
		t.Errorf("expected mode to be denylist, got %s", scope.Mode)
	}
	if len(scope.Denylist) != 2 {
		t.Errorf("expected 2 denylist entries, got %d", len(scope.Denylist))
	}
}

func TestRunCliEntry_EmptyCommand(t *testing.T) {
	entry := LinkModelConfig{
		Type:    "cli",
		Command: "",
		Args:    []string{"arg1"},
	}
	config := &LinkToolsConfig{TimeoutSeconds: 5}

	output, err := runCliEntry(context.Background(), entry, "https://example.com", nil, config)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if output != "" {
		t.Errorf("expected empty output for empty command, got %q", output)
	}
}

func TestRunCliEntry_NonCliType(t *testing.T) {
	entry := LinkModelConfig{
		Type:    "other",
		Command: "echo",
		Args:    []string{"hello"},
	}
	config := &LinkToolsConfig{TimeoutSeconds: 5}

	output, err := runCliEntry(context.Background(), entry, "https://example.com", nil, config)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if output != "" {
		t.Errorf("expected empty output for non-cli type, got %q", output)
	}
}

func TestRunCliEntry_Echo(t *testing.T) {
	entry := LinkModelConfig{
		Type:           "cli",
		Command:        "echo",
		Args:           []string{"{{LinkUrl}}"},
		TimeoutSeconds: 5,
	}
	config := &LinkToolsConfig{TimeoutSeconds: 30}

	output, err := runCliEntry(context.Background(), entry, "https://example.com", nil, config)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if output != "https://example.com" {
		t.Errorf("expected 'https://example.com', got %q", output)
	}
}

func TestRunLinkEntries_TriesMultiple(t *testing.T) {
	entries := []LinkModelConfig{
		{
			Type:           "cli",
			Command:        "false", // Command that fails
			Args:           []string{},
			TimeoutSeconds: 1,
		},
		{
			Type:           "cli",
			Command:        "echo",
			Args:           []string{"success"},
			TimeoutSeconds: 5,
		},
	}
	config := &LinkToolsConfig{TimeoutSeconds: 30}

	output, err := runLinkEntries(context.Background(), entries, "https://example.com", nil, config)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if output != "success" {
		t.Errorf("expected 'success', got %q", output)
	}
}
