package gateway

import (
	"testing"
)

func TestIsHTTPURL(t *testing.T) {
	tests := []struct {
		value    string
		expected bool
	}{
		{"http://example.com", true},
		{"https://example.com", true},
		{"https://example.com/path/to/file", true},
		{"http://localhost:8080", true},
		{"HTTP://EXAMPLE.COM", false}, // Case sensitive
		{"ftp://example.com", false},
		{"file:///path/to/file", false},
		{"example.com", false},
		{"/path/to/file", false},
		{"", false},
		{"httpx://example.com", false},
		{"http", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			result := isHTTPURL(tt.value)
			if result != tt.expected {
				t.Errorf("isHTTPURL(%q) = %v, want %v", tt.value, result, tt.expected)
			}
		})
	}
}

func TestScopeUsesThread(t *testing.T) {
	tests := []struct {
		scope    string
		expected bool
	}{
		{"channel", false},
		{"Channel", false},
		{"CHANNEL", false},
		{"  channel  ", false},
		{"thread", true},
		{"Thread", true},
		{"", true},        // Default to thread
		{"  ", true},      // Whitespace defaults to thread
		{"message", true}, // Unknown scope defaults to thread
		{"custom", true},
	}

	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			result := scopeUsesThread(tt.scope)
			if result != tt.expected {
				t.Errorf("scopeUsesThread(%q) = %v, want %v", tt.scope, result, tt.expected)
			}
		})
	}
}
