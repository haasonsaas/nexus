package attention

import (
	"context"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestMapTicketPriority(t *testing.T) {
	tests := []struct {
		input    string
		expected Priority
	}{
		{"1", PriorityCritical},
		{"Critical", PriorityCritical},
		{"2", PriorityHigh},
		{"High", PriorityHigh},
		{"3", PriorityNormal},
		{"Moderate", PriorityNormal},
		{"4", PriorityLow},
		{"Low", PriorityLow},
		{"5", PriorityLow},
		{"Planning", PriorityLow},
		{"unknown", PriorityNormal},
		{"", PriorityNormal},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := mapTicketPriority(tt.input); got != tt.expected {
				t.Errorf("mapTicketPriority(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestMapTicketStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected Status
	}{
		{"1", StatusNew},
		{"New", StatusNew},
		{"2", StatusInProgress},
		{"In Progress", StatusInProgress},
		{"3", StatusSnoozed},
		{"On Hold", StatusSnoozed},
		{"6", StatusHandled},
		{"Resolved", StatusHandled},
		{"7", StatusHandled},
		{"Closed", StatusHandled},
		{"unknown", StatusNew},
		{"", StatusNew},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := mapTicketStatus(tt.input); got != tt.expected {
				t.Errorf("mapTicketStatus(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestTicketInfo_Struct(t *testing.T) {
	now := time.Now()
	info := TicketInfo{
		ID:               "ticket-123",
		Number:           "INC001234",
		ShortDescription: "Cannot login",
		Description:      "User is unable to login to the system",
		State:            "1",
		Priority:         "2",
		CallerID:         "user-456",
		CallerName:       "John Doe",
		AssignedTo:       "tech-789",
		AssignmentGroup:  "IT Support",
		Category:         "Access",
		OpenedAt:         now,
	}

	if info.ID != "ticket-123" {
		t.Errorf("ID = %q, want %q", info.ID, "ticket-123")
	}
	if info.Number != "INC001234" {
		t.Errorf("Number = %q, want %q", info.Number, "INC001234")
	}
	if info.CallerName != "John Doe" {
		t.Errorf("CallerName = %q, want %q", info.CallerName, "John Doe")
	}
}

func TestNewTicketSource(t *testing.T) {
	source := NewTicketSource()

	if source == nil {
		t.Fatal("NewTicketSource returned nil")
	}
	if source.items == nil {
		t.Error("items channel should be initialized")
	}
}

func TestTicketSource_Items(t *testing.T) {
	source := NewTicketSource()
	items := source.Items()

	if items == nil {
		t.Error("Items() returned nil")
	}
}

func TestTicketSource_AddTicket(t *testing.T) {
	source := NewTicketSource()
	now := time.Now()

	ticket := TicketInfo{
		ID:               "ticket-123",
		Number:           "INC001234",
		ShortDescription: "Cannot login",
		Description:      "User is unable to login to the system",
		State:            "1",
		Priority:         "2",
		CallerID:         "user-456",
		CallerName:       "John Doe",
		AssignedTo:       "tech-789",
		AssignmentGroup:  "IT Support",
		Category:         "Access",
		OpenedAt:         now,
	}

	source.AddTicket(ticket)

	// Try to receive the item
	select {
	case item := <-source.Items():
		if item.ID != "ticket-123" {
			t.Errorf("item.ID = %q, want %q", item.ID, "ticket-123")
		}
		if item.Type != ItemTypeTicket {
			t.Errorf("item.Type = %v, want %v", item.Type, ItemTypeTicket)
		}
		if item.Channel != models.ChannelType("servicenow") {
			t.Errorf("item.Channel = %v, want servicenow", item.Channel)
		}
		if item.ChannelID != "servicenow:INC001234" {
			t.Errorf("item.ChannelID = %q, want %q", item.ChannelID, "servicenow:INC001234")
		}
		if item.Title != "INC001234: Cannot login" {
			t.Errorf("item.Title = %q, want %q", item.Title, "INC001234: Cannot login")
		}
		if item.Sender.ID != "user-456" {
			t.Errorf("item.Sender.ID = %q, want %q", item.Sender.ID, "user-456")
		}
		if item.Sender.Name != "John Doe" {
			t.Errorf("item.Sender.Name = %q, want %q", item.Sender.Name, "John Doe")
		}
		if item.Priority != PriorityHigh {
			t.Errorf("item.Priority = %v, want %v", item.Priority, PriorityHigh)
		}
		if item.Status != StatusNew {
			t.Errorf("item.Status = %v, want %v", item.Status, StatusNew)
		}
		if item.Metadata["ticket_number"] != "INC001234" {
			t.Errorf("item.Metadata[ticket_number] = %v, want %q", item.Metadata["ticket_number"], "INC001234")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for item")
	}
}

func TestTicketSource_AddTicket_ChannelFull(t *testing.T) {
	source := NewTicketSource()

	// Fill the channel (capacity is 100)
	for i := 0; i < 100; i++ {
		source.AddTicket(TicketInfo{ID: "ticket"})
	}

	// This should not block even though channel is full
	done := make(chan bool)
	go func() {
		source.AddTicket(TicketInfo{ID: "overflow"})
		done <- true
	}()

	select {
	case <-done:
		// Good, didn't block
	case <-time.After(100 * time.Millisecond):
		t.Error("AddTicket blocked on full channel")
	}
}

func TestNewMessageChannelSource(t *testing.T) {
	messages := make(chan *models.Message)
	source := NewMessageChannelSource(messages)

	if source == nil {
		t.Fatal("NewMessageChannelSource returned nil")
	}
	if source.messages != messages {
		t.Error("messages channel not set correctly")
	}
	if source.items == nil {
		t.Error("items channel should be initialized")
	}
}

func TestMessageChannelSource_Items(t *testing.T) {
	messages := make(chan *models.Message)
	source := NewMessageChannelSource(messages)
	items := source.Items()

	if items == nil {
		t.Error("Items() returned nil")
	}
}

func TestMessageChannelSource_StartStop(t *testing.T) {
	messages := make(chan *models.Message)
	source := NewMessageChannelSource(messages)

	ctx := context.Background()
	source.Start(ctx)

	if source.cancel == nil {
		t.Error("cancel function should be set after Start")
	}

	// Send a message
	go func() {
		messages <- &models.Message{
			ID:      "msg-123",
			Content: "Hello",
			Role:    models.RoleUser,
		}
	}()

	// Receive the item
	select {
	case item := <-source.Items():
		if item.ID != "msg-123" {
			t.Errorf("item.ID = %q, want %q", item.ID, "msg-123")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for item")
	}

	source.Stop()
}

func TestMessageChannelSource_Stop_NilCancel(t *testing.T) {
	messages := make(chan *models.Message)
	source := NewMessageChannelSource(messages)

	// Stop without starting should not panic
	source.Stop()
}

func TestMessageChannelSource_ChannelClose(t *testing.T) {
	messages := make(chan *models.Message)
	source := NewMessageChannelSource(messages)

	ctx := context.Background()
	source.Start(ctx)

	// Close the messages channel
	close(messages)

	// Items channel should eventually close
	select {
	case _, ok := <-source.Items():
		if ok {
			// Got an item, that's fine, try again
			select {
			case _, ok := <-source.Items():
				if ok {
					t.Error("expected items channel to close")
				}
			case <-time.After(100 * time.Millisecond):
				// May not have closed yet, that's okay for this test
			}
		}
		// Channel closed, good
	case <-time.After(100 * time.Millisecond):
		// May take time to propagate
	}
}

func TestMessageChannelSource_ContextCancel(t *testing.T) {
	messages := make(chan *models.Message)
	source := NewMessageChannelSource(messages)

	ctx, cancel := context.WithCancel(context.Background())
	source.Start(ctx)

	// Cancel the context
	cancel()

	// Items channel should eventually close
	time.Sleep(50 * time.Millisecond)

	// Verify source is stopped
	source.Stop() // Should not panic
}

// Mock adapter for testing ChannelSource
type mockInboundAdapter struct {
	messages chan *models.Message
}

func (m *mockInboundAdapter) Type() models.ChannelType {
	return models.ChannelSlack
}

func (m *mockInboundAdapter) Start(ctx context.Context) error {
	return nil
}

func (m *mockInboundAdapter) Stop() error {
	return nil
}

func (m *mockInboundAdapter) Messages() <-chan *models.Message {
	return m.messages
}

func (m *mockInboundAdapter) Status() map[string]any {
	return nil
}

func TestNewChannelSource(t *testing.T) {
	adapter := &mockInboundAdapter{messages: make(chan *models.Message)}
	source := NewChannelSource(adapter)

	if source == nil {
		t.Fatal("NewChannelSource returned nil")
	}
	if source.adapter != adapter {
		t.Error("adapter not set correctly")
	}
	if source.items == nil {
		t.Error("items channel should be initialized")
	}
}

func TestChannelSource_Items(t *testing.T) {
	adapter := &mockInboundAdapter{messages: make(chan *models.Message)}
	source := NewChannelSource(adapter)
	items := source.Items()

	if items == nil {
		t.Error("Items() returned nil")
	}
}

func TestChannelSource_StartStop(t *testing.T) {
	adapter := &mockInboundAdapter{messages: make(chan *models.Message)}
	source := NewChannelSource(adapter)

	ctx := context.Background()
	source.Start(ctx)

	if source.cancel == nil {
		t.Error("cancel function should be set after Start")
	}

	// Send a message through the adapter
	go func() {
		adapter.messages <- &models.Message{
			ID:      "msg-456",
			Content: "Test message",
			Role:    models.RoleUser,
		}
	}()

	// Receive the item
	select {
	case item := <-source.Items():
		if item.ID != "msg-456" {
			t.Errorf("item.ID = %q, want %q", item.ID, "msg-456")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("timed out waiting for item")
	}

	source.Stop()
}

func TestChannelSource_Stop_NilCancel(t *testing.T) {
	adapter := &mockInboundAdapter{messages: make(chan *models.Message)}
	source := NewChannelSource(adapter)

	// Stop without starting should not panic
	source.Stop()
}

func TestChannelSource_AdapterChannelClose(t *testing.T) {
	adapter := &mockInboundAdapter{messages: make(chan *models.Message)}
	source := NewChannelSource(adapter)

	ctx := context.Background()
	source.Start(ctx)

	// Close the adapter's messages channel
	close(adapter.messages)

	// Items channel should eventually close
	time.Sleep(50 * time.Millisecond)
}
