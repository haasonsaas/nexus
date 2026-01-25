package status

import (
	"strings"
	"testing"
	"time"
)

func TestFormatTokenCount(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{-1, "0"},
		{100, "100"},
		{999, "999"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{9999, "10.0k"},
		{10000, "10k"},
		{15000, "15k"},
		{100000, "100k"},
		{999999, "999k"},
		{1000000, "1.0m"},
		{1500000, "1.5m"},
		{10000000, "10.0m"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := FormatTokenCount(tt.input)
			if result != tt.expected {
				t.Errorf("FormatTokenCount(%d) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatContextUsageShort(t *testing.T) {
	tests := []struct {
		total    int
		context  int
		contains []string
	}{
		{0, 0, []string{"Context", "?"}},
		{0, 200000, []string{"Context", "?/200k"}},
		{15000, 200000, []string{"Context", "15k/200k", "(7%)"}},
		{100000, 200000, []string{"Context", "100k/200k", "(50%)"}},
		{1500000, 2000000, []string{"Context", "1.5m/2.0m", "(75%)"}},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := FormatContextUsageShort(tt.total, tt.context)
			for _, s := range tt.contains {
				if !strings.Contains(result, s) {
					t.Errorf("FormatContextUsageShort(%d, %d) = %q, expected to contain %q",
						tt.total, tt.context, result, s)
				}
			}
		})
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{-1 * time.Second, "unknown"},
		{0, "just now"},
		{30 * time.Second, "just now"},
		{59 * time.Second, "just now"},
		{1 * time.Minute, "1m ago"},
		{5 * time.Minute, "5m ago"},
		{59 * time.Minute, "59m ago"},
		{60 * time.Minute, "1h ago"},
		{90 * time.Minute, "1h ago"},
		{24 * time.Hour, "24h ago"},
		{47 * time.Hour, "47h ago"},
		{48 * time.Hour, "2d ago"},
		{72 * time.Hour, "3d ago"},
		{7 * 24 * time.Hour, "7d ago"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := FormatAge(tt.duration)
			if result != tt.expected {
				t.Errorf("FormatAge(%v) = %q, want %q", tt.duration, result, tt.expected)
			}
		})
	}
}

func TestFormatQueueDetails(t *testing.T) {
	tests := []struct {
		name        string
		queue       *QueueStatus
		contains    []string
		notContains []string
	}{
		{
			name:     "nil queue",
			queue:    nil,
			contains: nil,
		},
		{
			name:     "depth only",
			queue:    &QueueStatus{Depth: 5},
			contains: []string{"(depth 5)"},
		},
		{
			name:     "depth zero without details",
			queue:    &QueueStatus{Depth: 0},
			contains: []string{"(depth 0)"},
		},
		{
			name: "full details",
			queue: &QueueStatus{
				Depth:       3,
				DebounceMs:  500,
				Cap:         10,
				DropPolicy:  "oldest",
				ShowDetails: true,
			},
			contains: []string{"depth 3", "debounce 500ms", "cap 10", "drop oldest"},
		},
		{
			name: "debounce in seconds",
			queue: &QueueStatus{
				DebounceMs:  2000,
				ShowDetails: true,
			},
			contains: []string{"debounce 2s"},
		},
		{
			name: "debounce fractional seconds",
			queue: &QueueStatus{
				DebounceMs:  1500,
				ShowDetails: true,
			},
			contains: []string{"debounce 1.5s"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatQueueDetails(tt.queue)
			for _, s := range tt.contains {
				if !strings.Contains(result, s) {
					t.Errorf("FormatQueueDetails() = %q, expected to contain %q", result, s)
				}
			}
			for _, s := range tt.notContains {
				if strings.Contains(result, s) {
					t.Errorf("FormatQueueDetails() = %q, expected NOT to contain %q", result, s)
				}
			}
		})
	}
}

func TestFormatUsagePair(t *testing.T) {
	tests := []struct {
		input    int
		output   int
		contains []string
		empty    bool
	}{
		{0, 0, nil, true},
		{1000, 500, []string{"Tokens:", "1.0k in", "500 out"}, false},
		{15000, 3000, []string{"15k in", "3.0k out"}, false},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			result := FormatUsagePair(tt.input, tt.output)
			if tt.empty && result != "" {
				t.Errorf("FormatUsagePair(%d, %d) = %q, expected empty", tt.input, tt.output, result)
			}
			if !tt.empty && result == "" {
				t.Errorf("FormatUsagePair(%d, %d) = empty, expected content", tt.input, tt.output)
			}
			for _, s := range tt.contains {
				if !strings.Contains(result, s) {
					t.Errorf("FormatUsagePair(%d, %d) = %q, expected to contain %q",
						tt.input, tt.output, result, s)
				}
			}
		})
	}
}

func TestFormatMediaUnderstandingLine(t *testing.T) {
	tests := []struct {
		name      string
		decisions []MediaDecision
		contains  []string
		empty     bool
	}{
		{
			name:      "empty",
			decisions: nil,
			empty:     true,
		},
		{
			name: "all none",
			decisions: []MediaDecision{
				{Capability: "vision", Outcome: "no-attachment"},
				{Capability: "audio", Outcome: "no-attachment"},
			},
			empty: true,
		},
		{
			name: "vision success",
			decisions: []MediaDecision{
				{
					Capability: "vision",
					Outcome:    "success",
					Attachments: []MediaAttachment{
						{Chosen: &MediaChoice{Provider: "anthropic", Model: "claude-sonnet-4-20250514"}},
					},
				},
			},
			contains: []string{"Media:", "vision ok", "anthropic/claude-sonnet-4-20250514"},
		},
		{
			name: "audio off",
			decisions: []MediaDecision{
				{Capability: "audio", Outcome: "disabled"},
			},
			contains: []string{"Media:", "audio off"},
		},
		{
			name: "vision denied",
			decisions: []MediaDecision{
				{Capability: "vision", Outcome: "scope-deny"},
			},
			contains: []string{"vision denied"},
		},
		{
			name: "mixed status",
			decisions: []MediaDecision{
				{
					Capability: "vision",
					Outcome:    "success",
					Attachments: []MediaAttachment{
						{Chosen: &MediaChoice{Provider: "anthropic", Model: "claude-sonnet-4-20250514"}},
					},
				},
				{Capability: "audio", Outcome: "disabled"},
			},
			contains: []string{"vision ok", "audio off"},
		},
		{
			name: "multiple attachments",
			decisions: []MediaDecision{
				{
					Capability: "vision",
					Outcome:    "success",
					Attachments: []MediaAttachment{
						{Chosen: &MediaChoice{Provider: "anthropic"}},
						{Chosen: &MediaChoice{Provider: "anthropic"}},
						{Chosen: &MediaChoice{Provider: "anthropic"}},
					},
				},
			},
			contains: []string{"vision x3 ok"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatMediaUnderstandingLine(tt.decisions)
			if tt.empty && result != "" {
				t.Errorf("FormatMediaUnderstandingLine() = %q, expected empty", result)
			}
			if !tt.empty && result == "" {
				t.Errorf("FormatMediaUnderstandingLine() = empty, expected content")
			}
			for _, s := range tt.contains {
				if !strings.Contains(result, s) {
					t.Errorf("FormatMediaUnderstandingLine() = %q, expected to contain %q", result, s)
				}
			}
		})
	}
}

func TestFormatResponseTime(t *testing.T) {
	tests := []struct {
		ms       int64
		expected string
	}{
		{0, ""},
		{-100, ""},
		{100, "100ms"},
		{999, "999ms"},
		{1000, "1.0s"},
		{1500, "1.5s"},
		{2000, "2.0s"},
		{12345, "12.3s"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := FormatResponseTime(tt.ms)
			if result != tt.expected {
				t.Errorf("FormatResponseTime(%d) = %q, want %q", tt.ms, result, tt.expected)
			}
		})
	}
}

func TestBuildStatusMessage(t *testing.T) {
	now := time.Now()
	updatedAt := now.Add(-5 * time.Minute)

	args := StatusArgs{
		SessionKey:       "main:telegram:dm:user123",
		Provider:         "anthropic",
		Model:            "claude-sonnet-4-20250514",
		ContextTokens:    200000,
		InputTokens:      1200,
		OutputTokens:     500,
		TotalTokens:      15000,
		CompactionCount:  0,
		ResponseTimeMs:   1234,
		ModelAuth:        "api-key",
		ResolvedThink:    "medium",
		ResolvedVerbose:  "on",
		ResolvedElevated: "on",
		RuntimeMode:      "docker",
		SandboxMode:      "isolated",
		GroupActivation:  "mention",
		UpdatedAt:        &updatedAt,
		Now:              now,
		Queue: &QueueStatus{
			Mode:  "sequential",
			Depth: 0,
		},
	}

	result := BuildStatusMessage(args)

	// Check for expected lines
	expectedSubstrings := []string{
		"Nexus",
		"Response time: 1.2s",
		"Model: anthropic/claude-sonnet-4-20250514",
		"api-key",
		"Tokens: 1.2k in / 500 out",
		"Context",
		"15k/200k",
		"Compactions: 0",
		"Session: main:telegram:dm:user123",
		"updated 5m ago",
		"Queue: sequential",
		"Runtime: docker/isolated",
		"Think: medium",
		"verbose",
		"elevated",
	}

	for _, substr := range expectedSubstrings {
		if !strings.Contains(result, substr) {
			t.Errorf("BuildStatusMessage() missing expected substring: %q\n\nFull result:\n%s", substr, result)
		}
	}
}

func TestBuildStatusMessage_GroupSession(t *testing.T) {
	args := StatusArgs{
		SessionKey:      "main:telegram:group:chat123",
		Provider:        "anthropic",
		Model:           "claude-sonnet-4-20250514",
		GroupActivation: "always",
		Queue: &QueueStatus{
			Mode:  "parallel",
			Depth: 2,
		},
	}

	result := BuildStatusMessage(args)

	if !strings.Contains(result, "Activation: always") {
		t.Errorf("BuildStatusMessage() should show activation for group session\n\nResult:\n%s", result)
	}
}

func TestBuildStatusMessage_DMSession(t *testing.T) {
	args := StatusArgs{
		SessionKey: "main:telegram:dm:user123",
		Provider:   "anthropic",
		Model:      "claude-sonnet-4-20250514",
		Queue: &QueueStatus{
			Mode:  "sequential",
			Depth: 0,
		},
	}

	result := BuildStatusMessage(args)

	// DM sessions should NOT have activation line
	if strings.Contains(result, "Activation:") {
		t.Errorf("BuildStatusMessage() should NOT show activation for DM session\n\nResult:\n%s", result)
	}
}

func TestBuildHelpMessage(t *testing.T) {
	result := BuildHelpMessage(nil)

	expectedSubstrings := []string{
		"Help",
		"/think",
		"/verbose",
		"/model",
		"/commands",
	}

	for _, substr := range expectedSubstrings {
		if !strings.Contains(result, substr) {
			t.Errorf("BuildHelpMessage() missing expected substring: %q", substr)
		}
	}
}

func TestBuildCommandsMessage(t *testing.T) {
	skillCommands := []SkillCommand{
		{Name: "deploy", Description: "Deploy the application"},
		{Name: "test", Description: "Run tests", Aliases: []string{"/t"}},
	}

	result := BuildCommandsMessage(nil, skillCommands)

	expectedSubstrings := []string{
		"Slash commands",
		"/status",
		"/help",
		"/new",
		"/deploy",
		"/test",
	}

	for _, substr := range expectedSubstrings {
		if !strings.Contains(result, substr) {
			t.Errorf("BuildCommandsMessage() missing expected substring: %q\n\nResult:\n%s", substr, result)
		}
	}
}

func TestBuildStatusMessage_VoiceEnabled(t *testing.T) {
	args := StatusArgs{
		SessionKey:        "main:telegram:dm:user123",
		Provider:          "anthropic",
		Model:             "claude-sonnet-4-20250514",
		VoiceEnabled:      true,
		VoiceProvider:     "openai",
		VoiceSummaryLimit: 4096,
		VoiceSummaryOn:    true,
		Queue:             &QueueStatus{Mode: "sequential"},
	}

	result := BuildStatusMessage(args)

	expectedSubstrings := []string{
		"Voice: on",
		"provider=openai",
		"limit=4096",
		"summary=on",
	}

	for _, substr := range expectedSubstrings {
		if !strings.Contains(result, substr) {
			t.Errorf("BuildStatusMessage() missing voice info: %q\n\nResult:\n%s", substr, result)
		}
	}
}

func TestBuildStatusMessage_MediaDecisions(t *testing.T) {
	args := StatusArgs{
		SessionKey: "main:telegram:dm:user123",
		Provider:   "anthropic",
		Model:      "claude-sonnet-4-20250514",
		MediaDecisions: []MediaDecision{
			{
				Capability: "vision",
				Outcome:    "success",
				Attachments: []MediaAttachment{
					{Chosen: &MediaChoice{Provider: "anthropic", Model: "claude-sonnet-4-20250514"}},
				},
			},
			{Capability: "audio", Outcome: "disabled"},
		},
		Queue: &QueueStatus{Mode: "sequential"},
	}

	result := BuildStatusMessage(args)

	if !strings.Contains(result, "Media:") {
		t.Errorf("BuildStatusMessage() should contain Media line\n\nResult:\n%s", result)
	}
	if !strings.Contains(result, "vision ok") {
		t.Errorf("BuildStatusMessage() should contain 'vision ok'\n\nResult:\n%s", result)
	}
	if !strings.Contains(result, "audio off") {
		t.Errorf("BuildStatusMessage() should contain 'audio off'\n\nResult:\n%s", result)
	}
}
