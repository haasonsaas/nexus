package memory

import (
	"testing"
)

func TestShouldCapture(t *testing.T) {
	cfg := AutoCaptureConfig{
		MinContentLength: 10,
		MaxContentLength: 500,
	}

	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		// Should capture
		{"explicit_remember", "Please remember my email is test@example.com", true},
		{"preference_like", "I like using TypeScript for frontend development", true},
		{"preference_prefer", "I prefer dark mode in all applications", true},
		{"decision_will_use", "We decided to use PostgreSQL for the database", true},
		{"phone_number", "My phone number is +1234567890123", true},
		{"email_address", "Contact me at user@domain.com please", true},
		{"personal_fact", "My name is John and I work at Acme Corp", true},
		{"important_marker", "This is important: always backup before deploy", true},

		// Should not capture
		{"too_short", "Hi there", false},
		{"too_long", string(make([]byte, 600)), false},
		{"memory_context", "<relevant-memories>Some context</relevant-memories>", false},
		{"xml_content", "<system>Do not capture this</system>", false},
		{"markdown_list", "**Summary**\n- Item one\n- Item two", false},
		{"generic_text", "The weather is nice today in the city", false},
		{"code_block", "function hello() { console.log('hi'); }", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := shouldCapture(tc.content, cfg)
			if result != tc.expected {
				t.Errorf("shouldCapture(%q) = %v, want %v", truncate(tc.content, 50), result, tc.expected)
			}
		})
	}
}

func TestDetectCategory(t *testing.T) {
	tests := []struct {
		content  string
		expected MemoryCategory
	}{
		// Preferences
		{"I prefer using vim over emacs", CategoryPreference},
		{"I like coffee in the morning", CategoryPreference},
		{"I hate slow internet connections", CategoryPreference},

		// Decisions
		{"We decided to use React for the frontend", CategoryDecision},
		{"I will use Python for this project", CategoryDecision},

		// Entities
		{"My phone is +12025551234", CategoryEntity},
		{"Email me at john@example.com", CategoryEntity},
		{"The project is called Project Alpha", CategoryEntity},

		// Facts
		{"The server is running on port 8080", CategoryFact},
		{"There are 5 team members", CategoryFact},

		// Other
		{"random content here", CategoryOther},
	}

	for _, tc := range tests {
		name := tc.content
		if len(name) > 20 {
			name = name[:20]
		}
		t.Run(name, func(t *testing.T) {
			result := detectCategory(tc.content)
			if result != tc.expected {
				t.Errorf("detectCategory(%q) = %v, want %v", tc.content, result, tc.expected)
			}
		})
	}
}

func TestCountEmojis(t *testing.T) {
	tests := []struct {
		text     string
		expected int
	}{
		{"Hello world", 0},
		{"Hello 😀", 1},
		{"🎉🎊🎁", 3},
		{"No emojis here!", 0},
		{"Mixed 😀 content 🎉 here", 2},
	}

	for _, tc := range tests {
		t.Run(tc.text, func(t *testing.T) {
			result := countEmojis(tc.text)
			if result != tc.expected {
				t.Errorf("countEmojis(%q) = %d, want %d", tc.text, result, tc.expected)
			}
		})
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"short", 5, "short"},
		{"", 5, ""},
	}

	for _, tc := range tests {
		result := truncate(tc.input, tc.maxLen)
		if result != tc.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.maxLen, result, tc.expected)
		}
	}
}

func TestNewMemoryHooks_Defaults(t *testing.T) {
	captureConfig := AutoCaptureConfig{Enabled: true}
	recallConfig := AutoRecallConfig{Enabled: true}

	hooks := NewMemoryHooks(nil, captureConfig, recallConfig, nil)

	// Check defaults were applied
	if hooks.captureConfig.MaxCapturesPerConversation != 3 {
		t.Errorf("expected MaxCapturesPerConversation=3, got %d", hooks.captureConfig.MaxCapturesPerConversation)
	}
	if hooks.captureConfig.MinContentLength != 10 {
		t.Errorf("expected MinContentLength=10, got %d", hooks.captureConfig.MinContentLength)
	}
	if hooks.captureConfig.MaxContentLength != 500 {
		t.Errorf("expected MaxContentLength=500, got %d", hooks.captureConfig.MaxContentLength)
	}
	if hooks.captureConfig.DuplicateThreshold != 0.95 {
		t.Errorf("expected DuplicateThreshold=0.95, got %f", hooks.captureConfig.DuplicateThreshold)
	}

	if hooks.recallConfig.MaxResults != 3 {
		t.Errorf("expected MaxResults=3, got %d", hooks.recallConfig.MaxResults)
	}
	if hooks.recallConfig.MinScore != 0.3 {
		t.Errorf("expected MinScore=0.3, got %f", hooks.recallConfig.MinScore)
	}
	if hooks.recallConfig.MinQueryLength != 5 {
		t.Errorf("expected MinQueryLength=5, got %d", hooks.recallConfig.MinQueryLength)
	}
}
