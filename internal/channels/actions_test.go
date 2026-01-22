package channels

import (
	"testing"
)

func TestAllMessageActions(t *testing.T) {
	actions := AllMessageActions()

	if len(actions) == 0 {
		t.Error("AllMessageActions() returned empty slice")
	}

	// Verify we have all the expected actions
	expected := map[MessageAction]bool{
		ActionSend:    false,
		ActionEdit:    false,
		ActionDelete:  false,
		ActionReact:   false,
		ActionUnreact: false,
		ActionReply:   false,
		ActionPin:     false,
		ActionUnpin:   false,
		ActionTyping:  false,
	}

	for _, action := range actions {
		if _, ok := expected[action]; ok {
			expected[action] = true
		}
	}

	for action, found := range expected {
		if !found {
			t.Errorf("Expected action %q not found in AllMessageActions()", action)
		}
	}
}

func TestCapabilities_SupportsAction(t *testing.T) {
	tests := []struct {
		name   string
		caps   Capabilities
		action MessageAction
		want   bool
	}{
		{
			name:   "send supported",
			caps:   Capabilities{Send: true},
			action: ActionSend,
			want:   true,
		},
		{
			name:   "send not supported",
			caps:   Capabilities{Send: false},
			action: ActionSend,
			want:   false,
		},
		{
			name:   "edit supported",
			caps:   Capabilities{Edit: true},
			action: ActionEdit,
			want:   true,
		},
		{
			name:   "delete supported",
			caps:   Capabilities{Delete: true},
			action: ActionDelete,
			want:   true,
		},
		{
			name:   "react supported",
			caps:   Capabilities{React: true},
			action: ActionReact,
			want:   true,
		},
		{
			name:   "unreact supported",
			caps:   Capabilities{React: true},
			action: ActionUnreact,
			want:   true,
		},
		{
			name:   "reply supported",
			caps:   Capabilities{Reply: true},
			action: ActionReply,
			want:   true,
		},
		{
			name:   "pin supported",
			caps:   Capabilities{Pin: true},
			action: ActionPin,
			want:   true,
		},
		{
			name:   "unpin supported",
			caps:   Capabilities{Pin: true},
			action: ActionUnpin,
			want:   true,
		},
		{
			name:   "typing supported",
			caps:   Capabilities{Typing: true},
			action: ActionTyping,
			want:   true,
		},
		{
			name:   "unknown action",
			caps:   Capabilities{Send: true, Edit: true, Delete: true},
			action: MessageAction("unknown"),
			want:   false,
		},
		{
			name: "full capabilities",
			caps: Capabilities{
				Send:   true,
				Edit:   true,
				Delete: true,
				React:  true,
				Reply:  true,
				Pin:    true,
				Typing: true,
			},
			action: ActionEdit,
			want:   true,
		},
		{
			name:   "empty capabilities",
			caps:   Capabilities{},
			action: ActionSend,
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.caps.SupportsAction(tt.action)
			if got != tt.want {
				t.Errorf("SupportsAction(%q) = %v, want %v", tt.action, got, tt.want)
			}
		})
	}
}

func TestMessageActionRequest_Fields(t *testing.T) {
	req := MessageActionRequest{
		Action:    ActionEdit,
		ChannelID: "channel-123",
		MessageID: "msg-456",
		Content:   "Updated content",
		Reaction:  "👍",
		ReplyToID: "original-msg-789",
		Metadata: map[string]any{
			"custom_key": "custom_value",
		},
	}

	if req.Action != ActionEdit {
		t.Errorf("Action = %q, want %q", req.Action, ActionEdit)
	}
	if req.ChannelID != "channel-123" {
		t.Errorf("ChannelID = %q, want %q", req.ChannelID, "channel-123")
	}
	if req.MessageID != "msg-456" {
		t.Errorf("MessageID = %q, want %q", req.MessageID, "msg-456")
	}
	if req.Content != "Updated content" {
		t.Errorf("Content = %q, want %q", req.Content, "Updated content")
	}
	if req.Reaction != "👍" {
		t.Errorf("Reaction = %q, want %q", req.Reaction, "👍")
	}
	if req.ReplyToID != "original-msg-789" {
		t.Errorf("ReplyToID = %q, want %q", req.ReplyToID, "original-msg-789")
	}
	if req.Metadata["custom_key"] != "custom_value" {
		t.Errorf("Metadata[custom_key] = %v, want %q", req.Metadata["custom_key"], "custom_value")
	}
}

func TestMessageActionResult_Fields(t *testing.T) {
	result := MessageActionResult{
		Success:   true,
		MessageID: "msg-123",
		Error:     "",
		Metadata: map[string]any{
			"response_data": "value",
		},
	}

	if !result.Success {
		t.Error("Success = false, want true")
	}
	if result.MessageID != "msg-123" {
		t.Errorf("MessageID = %q, want %q", result.MessageID, "msg-123")
	}
	if result.Error != "" {
		t.Errorf("Error = %q, want empty", result.Error)
	}
	if result.Metadata["response_data"] != "value" {
		t.Errorf("Metadata[response_data] = %v, want %q", result.Metadata["response_data"], "value")
	}
}

func TestMessageActionResult_ErrorCase(t *testing.T) {
	result := MessageActionResult{
		Success:   false,
		MessageID: "msg-123",
		Error:     "operation failed: permission denied",
	}

	if result.Success {
		t.Error("Success = true, want false")
	}
	if result.Error == "" {
		t.Error("Error should not be empty for failed result")
	}
}

func TestCapabilities_AllFeatures(t *testing.T) {
	caps := Capabilities{
		Send:              true,
		Edit:              true,
		Delete:            true,
		React:             true,
		Reply:             true,
		Pin:               true,
		Typing:            true,
		Attachments:       true,
		RichText:          true,
		Threads:           true,
		MaxMessageLength:  4000,
		MaxAttachmentSize: 10 << 20, // 10MB
	}

	// Test all actions are supported
	allActions := AllMessageActions()
	for _, action := range allActions {
		if !caps.SupportsAction(action) {
			t.Errorf("Expected action %q to be supported with full capabilities", action)
		}
	}

	// Test limits
	if caps.MaxMessageLength != 4000 {
		t.Errorf("MaxMessageLength = %d, want 4000", caps.MaxMessageLength)
	}
	if caps.MaxAttachmentSize != 10<<20 {
		t.Errorf("MaxAttachmentSize = %d, want %d", caps.MaxAttachmentSize, 10<<20)
	}
}

func TestMessageActionConstants(t *testing.T) {
	// Verify action constants have expected values
	if ActionSend != "send" {
		t.Errorf("ActionSend = %q, want %q", ActionSend, "send")
	}
	if ActionEdit != "edit" {
		t.Errorf("ActionEdit = %q, want %q", ActionEdit, "edit")
	}
	if ActionDelete != "delete" {
		t.Errorf("ActionDelete = %q, want %q", ActionDelete, "delete")
	}
	if ActionReact != "react" {
		t.Errorf("ActionReact = %q, want %q", ActionReact, "react")
	}
	if ActionUnreact != "unreact" {
		t.Errorf("ActionUnreact = %q, want %q", ActionUnreact, "unreact")
	}
	if ActionReply != "reply" {
		t.Errorf("ActionReply = %q, want %q", ActionReply, "reply")
	}
	if ActionPin != "pin" {
		t.Errorf("ActionPin = %q, want %q", ActionPin, "pin")
	}
	if ActionUnpin != "unpin" {
		t.Errorf("ActionUnpin = %q, want %q", ActionUnpin, "unpin")
	}
	if ActionTyping != "typing" {
		t.Errorf("ActionTyping = %q, want %q", ActionTyping, "typing")
	}
}
