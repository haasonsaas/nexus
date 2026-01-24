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

func TestNewMemoryHooks_CustomValues(t *testing.T) {
	captureConfig := AutoCaptureConfig{
		Enabled:                    true,
		MaxCapturesPerConversation: 5,
		MinContentLength:           20,
		MaxContentLength:           1000,
		DuplicateThreshold:         0.85,
		DefaultImportance:          0.9,
	}
	recallConfig := AutoRecallConfig{
		Enabled:        true,
		MaxResults:     10,
		MinScore:       0.5,
		MinQueryLength: 15,
	}

	hooks := NewMemoryHooks(nil, captureConfig, recallConfig, nil)

	// Check custom values were preserved
	if hooks.captureConfig.MaxCapturesPerConversation != 5 {
		t.Errorf("expected MaxCapturesPerConversation=5, got %d", hooks.captureConfig.MaxCapturesPerConversation)
	}
	if hooks.captureConfig.MinContentLength != 20 {
		t.Errorf("expected MinContentLength=20, got %d", hooks.captureConfig.MinContentLength)
	}
	if hooks.captureConfig.MaxContentLength != 1000 {
		t.Errorf("expected MaxContentLength=1000, got %d", hooks.captureConfig.MaxContentLength)
	}
	if hooks.captureConfig.DuplicateThreshold != 0.85 {
		t.Errorf("expected DuplicateThreshold=0.85, got %f", hooks.captureConfig.DuplicateThreshold)
	}
	if hooks.captureConfig.DefaultImportance != 0.9 {
		t.Errorf("expected DefaultImportance=0.9, got %f", hooks.captureConfig.DefaultImportance)
	}

	if hooks.recallConfig.MaxResults != 10 {
		t.Errorf("expected MaxResults=10, got %d", hooks.recallConfig.MaxResults)
	}
	if hooks.recallConfig.MinScore != 0.5 {
		t.Errorf("expected MinScore=0.5, got %f", hooks.recallConfig.MinScore)
	}
	if hooks.recallConfig.MinQueryLength != 15 {
		t.Errorf("expected MinQueryLength=15, got %d", hooks.recallConfig.MinQueryLength)
	}
}

func TestMemoryCategory_Constants(t *testing.T) {
	tests := []struct {
		category MemoryCategory
		expected string
	}{
		{CategoryPreference, "preference"},
		{CategoryFact, "fact"},
		{CategoryDecision, "decision"},
		{CategoryEntity, "entity"},
		{CategoryOther, "other"},
	}

	for _, tt := range tests {
		if string(tt.category) != tt.expected {
			t.Errorf("MemoryCategory %v = %q, want %q", tt.category, tt.category, tt.expected)
		}
	}
}

func TestAutoCaptureConfig_Struct(t *testing.T) {
	cfg := AutoCaptureConfig{
		Enabled:                    true,
		MaxCapturesPerConversation: 5,
		MinContentLength:           20,
		MaxContentLength:           1000,
		DuplicateThreshold:         0.9,
		DefaultImportance:          0.8,
	}

	if !cfg.Enabled {
		t.Error("Enabled should be true")
	}
	if cfg.MaxCapturesPerConversation != 5 {
		t.Errorf("MaxCapturesPerConversation = %d, want 5", cfg.MaxCapturesPerConversation)
	}
	if cfg.MinContentLength != 20 {
		t.Errorf("MinContentLength = %d, want 20", cfg.MinContentLength)
	}
	if cfg.MaxContentLength != 1000 {
		t.Errorf("MaxContentLength = %d, want 1000", cfg.MaxContentLength)
	}
	if cfg.DuplicateThreshold != 0.9 {
		t.Errorf("DuplicateThreshold = %f, want 0.9", cfg.DuplicateThreshold)
	}
	if cfg.DefaultImportance != 0.8 {
		t.Errorf("DefaultImportance = %f, want 0.8", cfg.DefaultImportance)
	}
}

func TestAutoRecallConfig_Struct(t *testing.T) {
	cfg := AutoRecallConfig{
		Enabled:        true,
		MaxResults:     10,
		MinScore:       0.6,
		MinQueryLength: 15,
	}

	if !cfg.Enabled {
		t.Error("Enabled should be true")
	}
	if cfg.MaxResults != 10 {
		t.Errorf("MaxResults = %d, want 10", cfg.MaxResults)
	}
	if cfg.MinScore != 0.6 {
		t.Errorf("MinScore = %f, want 0.6", cfg.MinScore)
	}
	if cfg.MinQueryLength != 15 {
		t.Errorf("MinQueryLength = %d, want 15", cfg.MinQueryLength)
	}
}

func TestShouldCapture_EdgeCases(t *testing.T) {
	cfg := AutoCaptureConfig{
		MinContentLength: 10,
		MaxContentLength: 500,
	}

	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		// More edge cases
		{"emoji_heavy", "😀😀😀😀 Hello there everyone 😀", false},
		{"czech_remember", "Pamatuj si moje jméno je Jan", true},
		{"czech_prefer", "Preferuji tmavý režim", true},
		{"czech_decided", "Rozhodli jsme se použít Go", true},
		{"love_something", "I love programming in Rust", true},
		{"hate_something", "I hate slow compile times", true},
		{"want_something", "I want to learn Kubernetes", true},
		{"need_something", "I need help with Docker", true},
		{"always_do", "I always use VSCode for editing", true},
		{"never_do", "I never work on weekends", true},
		{"crucial_marker", "This is crucial for the project", true},
		{"key_point_marker", "The key point is that we need tests", true},
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

func TestDetectCategory_MoreCases(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected MemoryCategory
	}{
		// Czech language
		{"czech_preference", "Radši používám vim", CategoryPreference},
		{"czech_decision", "Budeme používat Python", CategoryDecision},
		{"czech_entity", "Jmenuje se Pavel", CategoryEntity},
		{"czech_fact", "Server je na portu 3000", CategoryFact},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := detectCategory(tc.content)
			if result != tc.expected {
				t.Errorf("detectCategory(%q) = %v, want %v", tc.content, result, tc.expected)
			}
		})
	}
}

func TestCaptureCandidate_Struct(t *testing.T) {
	candidate := captureCandidate{
		content:  "Test content",
		category: CategoryPreference,
		role:     "user",
	}

	if candidate.content != "Test content" {
		t.Errorf("content = %q, want %q", candidate.content, "Test content")
	}
	if candidate.category != CategoryPreference {
		t.Errorf("category = %v, want %v", candidate.category, CategoryPreference)
	}
	if candidate.role != "user" {
		t.Errorf("role = %q, want %q", candidate.role, "user")
	}
}
