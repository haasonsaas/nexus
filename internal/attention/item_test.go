package attention

import (
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestItem_IsActive(t *testing.T) {
	tests := []struct {
		name     string
		item     *Item
		expected bool
	}{
		{
			name:     "new status is active",
			item:     &Item{Status: StatusNew},
			expected: true,
		},
		{
			name:     "viewed status is active",
			item:     &Item{Status: StatusViewed},
			expected: true,
		},
		{
			name:     "in_progress status is active",
			item:     &Item{Status: StatusInProgress},
			expected: true,
		},
		{
			name:     "handled status is not active",
			item:     &Item{Status: StatusHandled},
			expected: false,
		},
		{
			name:     "archived status is not active",
			item:     &Item{Status: StatusArchived},
			expected: false,
		},
		{
			name: "snoozed status with future time is not active",
			item: func() *Item {
				future := time.Now().Add(time.Hour)
				return &Item{Status: StatusSnoozed, SnoozedUntil: &future}
			}(),
			expected: false,
		},
		{
			name: "snoozed status with past time is active",
			item: func() *Item {
				past := time.Now().Add(-time.Hour)
				return &Item{Status: StatusSnoozed, SnoozedUntil: &past}
			}(),
			expected: true,
		},
		{
			name:     "snoozed status with nil time is not active",
			item:     &Item{Status: StatusSnoozed, SnoozedUntil: nil},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.item.IsActive()
			if result != tt.expected {
				t.Errorf("IsActive() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestItem_SetViewed(t *testing.T) {
	t.Run("updates status from new to viewed", func(t *testing.T) {
		item := &Item{Status: StatusNew}
		item.SetViewed()

		if item.Status != StatusViewed {
			t.Errorf("Status = %v, want %v", item.Status, StatusViewed)
		}
		if item.ViewedAt == nil {
			t.Error("ViewedAt should be set")
		}
	})

	t.Run("does not change status if already in_progress", func(t *testing.T) {
		item := &Item{Status: StatusInProgress}
		item.SetViewed()

		if item.Status != StatusInProgress {
			t.Errorf("Status = %v, want %v", item.Status, StatusInProgress)
		}
		if item.ViewedAt == nil {
			t.Error("ViewedAt should still be set")
		}
	})

	t.Run("does not change status if handled", func(t *testing.T) {
		item := &Item{Status: StatusHandled}
		item.SetViewed()

		if item.Status != StatusHandled {
			t.Errorf("Status = %v, want %v", item.Status, StatusHandled)
		}
	})
}

func TestItem_SetHandled(t *testing.T) {
	item := &Item{Status: StatusViewed}
	item.SetHandled()

	if item.Status != StatusHandled {
		t.Errorf("Status = %v, want %v", item.Status, StatusHandled)
	}
	if item.HandledAt == nil {
		t.Error("HandledAt should be set")
	}
}

func TestItem_Snooze(t *testing.T) {
	item := &Item{Status: StatusNew}
	until := time.Now().Add(time.Hour)
	item.Snooze(until)

	if item.Status != StatusSnoozed {
		t.Errorf("Status = %v, want %v", item.Status, StatusSnoozed)
	}
	if item.SnoozedUntil == nil || !item.SnoozedUntil.Equal(until) {
		t.Error("SnoozedUntil should be set to the given time")
	}
}

func TestItem_Unsnooze(t *testing.T) {
	t.Run("returns to new if not previously viewed", func(t *testing.T) {
		until := time.Now().Add(time.Hour)
		item := &Item{
			Status:       StatusSnoozed,
			SnoozedUntil: &until,
		}
		item.Unsnooze()

		if item.Status != StatusNew {
			t.Errorf("Status = %v, want %v", item.Status, StatusNew)
		}
		if item.SnoozedUntil != nil {
			t.Error("SnoozedUntil should be nil")
		}
	})

	t.Run("returns to viewed if previously viewed", func(t *testing.T) {
		now := time.Now()
		until := now.Add(time.Hour)
		item := &Item{
			Status:       StatusSnoozed,
			ViewedAt:     &now,
			SnoozedUntil: &until,
		}
		item.Unsnooze()

		if item.Status != StatusViewed {
			t.Errorf("Status = %v, want %v", item.Status, StatusViewed)
		}
		if item.SnoozedUntil != nil {
			t.Error("SnoozedUntil should be nil")
		}
	})
}

func TestItemFromMessage(t *testing.T) {
	t.Run("converts basic message", func(t *testing.T) {
		now := time.Now()
		msg := &models.Message{
			ID:        "msg-1",
			Content:   "Hello, this is a test message",
			Channel:   models.ChannelSlack,
			ChannelID: "channel-1",
			CreatedAt: now,
		}

		item := ItemFromMessage(msg)

		if item.ID != "msg-1" {
			t.Errorf("ID = %q, want %q", item.ID, "msg-1")
		}
		if item.Type != ItemTypeMessage {
			t.Errorf("Type = %v, want %v", item.Type, ItemTypeMessage)
		}
		if item.Channel != models.ChannelSlack {
			t.Errorf("Channel = %v, want %v", item.Channel, models.ChannelSlack)
		}
		if item.ChannelID != "channel-1" {
			t.Errorf("ChannelID = %q, want %q", item.ChannelID, "channel-1")
		}
		if item.Status != StatusNew {
			t.Errorf("Status = %v, want %v", item.Status, StatusNew)
		}
		if item.Priority != PriorityNormal {
			t.Errorf("Priority = %v, want %v", item.Priority, PriorityNormal)
		}
		if item.OriginalMessage != msg {
			t.Error("OriginalMessage should reference the input message")
		}
	})

	t.Run("converts email message", func(t *testing.T) {
		msg := &models.Message{
			ID:      "email-1",
			Content: "Email content",
			Channel: models.ChannelEmail,
			Metadata: map[string]any{
				"subject":      "Test Subject",
				"sender_name":  "John Doe",
				"sender_email": "john@example.com",
				"sender_id":    "sender-123",
			},
		}

		item := ItemFromMessage(msg)

		if item.Type != ItemTypeEmail {
			t.Errorf("Type = %v, want %v", item.Type, ItemTypeEmail)
		}
		if item.Title != "Test Subject" {
			t.Errorf("Title = %q, want %q", item.Title, "Test Subject")
		}
		if item.Sender.Name != "John Doe" {
			t.Errorf("Sender.Name = %q, want %q", item.Sender.Name, "John Doe")
		}
		if item.Sender.Email != "john@example.com" {
			t.Errorf("Sender.Email = %q, want %q", item.Sender.Email, "john@example.com")
		}
		if item.Sender.ID != "sender-123" {
			t.Errorf("Sender.ID = %q, want %q", item.Sender.ID, "sender-123")
		}
	})

	t.Run("truncates long content", func(t *testing.T) {
		longContent := ""
		for i := 0; i < 300; i++ {
			longContent += "x"
		}

		msg := &models.Message{
			ID:      "msg-long",
			Content: longContent,
			Channel: models.ChannelSlack,
		}

		item := ItemFromMessage(msg)

		if len(item.Title) > 80 {
			t.Errorf("Title length = %d, want <= 80", len(item.Title))
		}
		if len(item.Preview) > 200 {
			t.Errorf("Preview length = %d, want <= 200", len(item.Preview))
		}
	})
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		max      int
		expected string
	}{
		{
			name:     "short string unchanged",
			input:    "hello",
			max:      10,
			expected: "hello",
		},
		{
			name:     "exact length unchanged",
			input:    "hello",
			max:      5,
			expected: "hello",
		},
		{
			name:     "long string truncated with ellipsis",
			input:    "hello world",
			max:      8,
			expected: "hello...",
		},
		{
			name:     "max <= 3 no ellipsis",
			input:    "hello",
			max:      3,
			expected: "hel",
		},
		{
			name:     "empty string",
			input:    "",
			max:      10,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncate(tt.input, tt.max)
			if result != tt.expected {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, result, tt.expected)
			}
		})
	}
}

func TestItemType_Constants(t *testing.T) {
	// Verify constants have expected values
	if ItemTypeMessage != "message" {
		t.Errorf("ItemTypeMessage = %q, want %q", ItemTypeMessage, "message")
	}
	if ItemTypeEmail != "email" {
		t.Errorf("ItemTypeEmail = %q, want %q", ItemTypeEmail, "email")
	}
	if ItemTypeTicket != "ticket" {
		t.Errorf("ItemTypeTicket = %q, want %q", ItemTypeTicket, "ticket")
	}
}

func TestPriority_Constants(t *testing.T) {
	// Verify priority ordering
	if PriorityLow >= PriorityNormal {
		t.Error("PriorityLow should be less than PriorityNormal")
	}
	if PriorityNormal >= PriorityHigh {
		t.Error("PriorityNormal should be less than PriorityHigh")
	}
	if PriorityHigh >= PriorityUrgent {
		t.Error("PriorityHigh should be less than PriorityUrgent")
	}
	if PriorityUrgent >= PriorityCritical {
		t.Error("PriorityUrgent should be less than PriorityCritical")
	}
}

func TestStatus_Constants(t *testing.T) {
	// Verify constants have expected values
	if StatusNew != "new" {
		t.Errorf("StatusNew = %q, want %q", StatusNew, "new")
	}
	if StatusViewed != "viewed" {
		t.Errorf("StatusViewed = %q, want %q", StatusViewed, "viewed")
	}
	if StatusHandled != "handled" {
		t.Errorf("StatusHandled = %q, want %q", StatusHandled, "handled")
	}
}
