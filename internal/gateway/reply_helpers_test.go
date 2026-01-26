package gateway

import "testing"

func TestNormalizeReplyContent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		suppress bool
		reason   string
	}{
		{
			name:     "silent token",
			input:    "NO_REPLY",
			want:     "",
			suppress: true,
			reason:   "silent_reply",
		},
		{
			name:     "heartbeat token",
			input:    "HEARTBEAT_OK",
			want:     "",
			suppress: true,
			reason:   "heartbeat",
		},
		{
			name:     "silent token with whitespace",
			input:    "  NO_REPLY  ",
			want:     "",
			suppress: true,
			reason:   "silent_reply",
		},
		{
			name:     "silent token with content",
			input:    "NO_REPLY saved",
			want:     "saved",
			suppress: false,
		},
		{
			name:     "heartbeat token with content",
			input:    "HEARTBEAT_OK status",
			want:     "status",
			suppress: false,
		},
		{
			name:     "normal text",
			input:    "hello",
			want:     "hello",
			suppress: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, suppressed, reason := normalizeReplyContent(tt.input)
			if got != tt.want {
				t.Fatalf("content = %q, want %q", got, tt.want)
			}
			if suppressed != tt.suppress {
				t.Fatalf("suppressed = %v, want %v", suppressed, tt.suppress)
			}
			if reason != tt.reason {
				t.Fatalf("reason = %q, want %q", reason, tt.reason)
			}
		})
	}
}
