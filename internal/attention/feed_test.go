package attention

import (
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestNewFeed(t *testing.T) {
	feed := NewFeed()
	if feed == nil {
		t.Fatal("NewFeed() returned nil")
	}
	if feed.items == nil {
		t.Error("items map should be initialized")
	}
}

func TestFeed_AddAndGet(t *testing.T) {
	feed := NewFeed()

	item := &Item{
		ID:      "item-1",
		Type:    ItemTypeMessage,
		Status:  StatusNew,
		Content: "Test content",
	}

	feed.Add(item)

	got, ok := feed.Get("item-1")
	if !ok {
		t.Error("expected item to be found")
	}
	if got.ID != "item-1" {
		t.Errorf("ID = %q, want %q", got.ID, "item-1")
	}
}

func TestFeed_GetNotFound(t *testing.T) {
	feed := NewFeed()

	_, ok := feed.Get("nonexistent")
	if ok {
		t.Error("expected ok to be false for nonexistent item")
	}
}

func TestFeed_AddMessage(t *testing.T) {
	feed := NewFeed()

	msg := &models.Message{
		ID:      "msg-1",
		Content: "Hello",
		Channel: models.ChannelSlack,
	}

	item := feed.AddMessage(msg)

	if item.ID != "msg-1" {
		t.Errorf("ID = %q, want %q", item.ID, "msg-1")
	}

	got, ok := feed.Get("msg-1")
	if !ok {
		t.Error("expected item to be found after AddMessage")
	}
	if got.Content != "Hello" {
		t.Errorf("Content = %q, want %q", got.Content, "Hello")
	}
}

func TestFeed_Update(t *testing.T) {
	feed := NewFeed()

	item := &Item{ID: "item-1", Content: "original"}
	feed.Add(item)

	item.Content = "updated"
	ok := feed.Update(item)
	if !ok {
		t.Error("Update should return true for existing item")
	}

	got, _ := feed.Get("item-1")
	if got.Content != "updated" {
		t.Errorf("Content = %q, want %q", got.Content, "updated")
	}
}

func TestFeed_UpdateNonexistent(t *testing.T) {
	feed := NewFeed()

	item := &Item{ID: "nonexistent", Content: "test"}
	ok := feed.Update(item)
	if ok {
		t.Error("Update should return false for nonexistent item")
	}
}

func TestFeed_Remove(t *testing.T) {
	feed := NewFeed()

	item := &Item{ID: "item-1"}
	feed.Add(item)

	ok := feed.Remove("item-1")
	if !ok {
		t.Error("Remove should return true for existing item")
	}

	_, found := feed.Get("item-1")
	if found {
		t.Error("item should not be found after Remove")
	}
}

func TestFeed_RemoveNonexistent(t *testing.T) {
	feed := NewFeed()

	ok := feed.Remove("nonexistent")
	if ok {
		t.Error("Remove should return false for nonexistent item")
	}
}

func TestFeed_MarkViewed(t *testing.T) {
	feed := NewFeed()

	item := &Item{ID: "item-1", Status: StatusNew}
	feed.Add(item)

	ok := feed.MarkViewed("item-1")
	if !ok {
		t.Error("MarkViewed should return true")
	}

	got, _ := feed.Get("item-1")
	if got.Status != StatusViewed {
		t.Errorf("Status = %v, want %v", got.Status, StatusViewed)
	}
	if got.ViewedAt == nil {
		t.Error("ViewedAt should be set")
	}
}

func TestFeed_MarkViewedNonexistent(t *testing.T) {
	feed := NewFeed()

	ok := feed.MarkViewed("nonexistent")
	if ok {
		t.Error("MarkViewed should return false for nonexistent item")
	}
}

func TestFeed_MarkHandled(t *testing.T) {
	feed := NewFeed()

	item := &Item{ID: "item-1", Status: StatusViewed}
	feed.Add(item)

	ok := feed.MarkHandled("item-1")
	if !ok {
		t.Error("MarkHandled should return true")
	}

	got, _ := feed.Get("item-1")
	if got.Status != StatusHandled {
		t.Errorf("Status = %v, want %v", got.Status, StatusHandled)
	}
}

func TestFeed_Snooze(t *testing.T) {
	feed := NewFeed()

	item := &Item{ID: "item-1", Status: StatusNew}
	feed.Add(item)

	until := time.Now().Add(time.Hour)
	ok := feed.Snooze("item-1", until)
	if !ok {
		t.Error("Snooze should return true")
	}

	got, _ := feed.Get("item-1")
	if got.Status != StatusSnoozed {
		t.Errorf("Status = %v, want %v", got.Status, StatusSnoozed)
	}
	if got.SnoozedUntil == nil {
		t.Error("SnoozedUntil should be set")
	}
}

func TestFeed_Unsnooze(t *testing.T) {
	feed := NewFeed()

	until := time.Now().Add(time.Hour)
	item := &Item{ID: "item-1", Status: StatusSnoozed, SnoozedUntil: &until}
	feed.Add(item)

	ok := feed.Unsnooze("item-1")
	if !ok {
		t.Error("Unsnooze should return true")
	}

	got, _ := feed.Get("item-1")
	if got.Status == StatusSnoozed {
		t.Error("Status should not be Snoozed after Unsnooze")
	}
	if got.SnoozedUntil != nil {
		t.Error("SnoozedUntil should be nil")
	}
}

func TestFeed_List_Empty(t *testing.T) {
	feed := NewFeed()

	result := feed.List(FeedOptions{})
	if len(result) != 0 {
		t.Errorf("expected empty list, got %d items", len(result))
	}
}

func TestFeed_List_All(t *testing.T) {
	feed := NewFeed()
	feed.Add(&Item{ID: "1", Status: StatusNew, ReceivedAt: time.Now()})
	feed.Add(&Item{ID: "2", Status: StatusViewed, ReceivedAt: time.Now()})
	feed.Add(&Item{ID: "3", Status: StatusHandled, ReceivedAt: time.Now()})

	result := feed.List(FeedOptions{})
	if len(result) != 3 {
		t.Errorf("expected 3 items, got %d", len(result))
	}
}

func TestFeed_List_FilterByChannel(t *testing.T) {
	feed := NewFeed()
	feed.Add(&Item{ID: "1", Channel: models.ChannelSlack, Status: StatusNew})
	feed.Add(&Item{ID: "2", Channel: models.ChannelEmail, Status: StatusNew})
	feed.Add(&Item{ID: "3", Channel: models.ChannelSlack, Status: StatusNew})

	result := feed.List(FeedOptions{
		Channels: []models.ChannelType{models.ChannelSlack},
	})
	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

func TestFeed_List_FilterByType(t *testing.T) {
	feed := NewFeed()
	feed.Add(&Item{ID: "1", Type: ItemTypeMessage, Status: StatusNew})
	feed.Add(&Item{ID: "2", Type: ItemTypeEmail, Status: StatusNew})
	feed.Add(&Item{ID: "3", Type: ItemTypeTicket, Status: StatusNew})

	result := feed.List(FeedOptions{
		Types: []ItemType{ItemTypeMessage, ItemTypeTicket},
	})
	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

func TestFeed_List_FilterByPriority(t *testing.T) {
	feed := NewFeed()
	feed.Add(&Item{ID: "1", Priority: PriorityLow, Status: StatusNew})
	feed.Add(&Item{ID: "2", Priority: PriorityHigh, Status: StatusNew})
	feed.Add(&Item{ID: "3", Priority: PriorityCritical, Status: StatusNew})

	result := feed.List(FeedOptions{
		MinPriority: PriorityHigh,
	})
	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

func TestFeed_List_FilterByStatus(t *testing.T) {
	feed := NewFeed()
	feed.Add(&Item{ID: "1", Status: StatusNew})
	feed.Add(&Item{ID: "2", Status: StatusViewed})
	feed.Add(&Item{ID: "3", Status: StatusHandled})

	result := feed.List(FeedOptions{
		Statuses: []Status{StatusNew, StatusViewed},
	})
	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

func TestFeed_List_FilterByTags(t *testing.T) {
	feed := NewFeed()
	feed.Add(&Item{ID: "1", Status: StatusNew, Tags: []string{"urgent", "bug"}})
	feed.Add(&Item{ID: "2", Status: StatusNew, Tags: []string{"feature"}})
	feed.Add(&Item{ID: "3", Status: StatusNew, Tags: []string{"urgent"}})

	result := feed.List(FeedOptions{
		Tags: []string{"urgent"},
	})
	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

func TestFeed_List_FilterBySender(t *testing.T) {
	feed := NewFeed()
	feed.Add(&Item{ID: "1", Status: StatusNew, Sender: Sender{ID: "user-1"}})
	feed.Add(&Item{ID: "2", Status: StatusNew, Sender: Sender{ID: "user-2"}})
	feed.Add(&Item{ID: "3", Status: StatusNew, Sender: Sender{ID: "user-1"}})

	result := feed.List(FeedOptions{
		SenderIDs: []string{"user-1"},
	})
	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

func TestFeed_List_FilterByTime(t *testing.T) {
	feed := NewFeed()
	now := time.Now()

	feed.Add(&Item{ID: "1", Status: StatusNew, ReceivedAt: now.Add(-2 * time.Hour)})
	feed.Add(&Item{ID: "2", Status: StatusNew, ReceivedAt: now.Add(-1 * time.Hour)})
	feed.Add(&Item{ID: "3", Status: StatusNew, ReceivedAt: now})

	result := feed.List(FeedOptions{
		Since: now.Add(-90 * time.Minute),
	})
	if len(result) != 2 {
		t.Errorf("expected 2 items, got %d", len(result))
	}
}

func TestFeed_List_Pagination(t *testing.T) {
	feed := NewFeed()
	for i := 0; i < 10; i++ {
		feed.Add(&Item{ID: string(rune('a' + i)), Status: StatusNew, ReceivedAt: time.Now()})
	}

	// Test limit
	result := feed.List(FeedOptions{Limit: 5})
	if len(result) != 5 {
		t.Errorf("expected 5 items with limit, got %d", len(result))
	}

	// Test offset
	result = feed.List(FeedOptions{Offset: 8})
	if len(result) != 2 {
		t.Errorf("expected 2 items with offset 8, got %d", len(result))
	}

	// Test offset >= total
	result = feed.List(FeedOptions{Offset: 100})
	if result != nil {
		t.Errorf("expected nil with offset >= total, got %d items", len(result))
	}
}

func TestFeed_List_Sorting(t *testing.T) {
	feed := NewFeed()
	now := time.Now()

	feed.Add(&Item{ID: "old", Status: StatusNew, ReceivedAt: now.Add(-time.Hour), Priority: PriorityLow})
	feed.Add(&Item{ID: "new", Status: StatusNew, ReceivedAt: now, Priority: PriorityHigh})
	feed.Add(&Item{ID: "mid", Status: StatusNew, ReceivedAt: now.Add(-30 * time.Minute), Priority: PriorityNormal})

	// Sort by received desc (default)
	result := feed.List(FeedOptions{SortBy: SortByReceivedDesc})
	if result[0].ID != "new" {
		t.Errorf("first item should be 'new' when sorted by received desc, got %q", result[0].ID)
	}

	// Sort by received asc
	result = feed.List(FeedOptions{SortBy: SortByReceivedAsc})
	if result[0].ID != "old" {
		t.Errorf("first item should be 'old' when sorted by received asc, got %q", result[0].ID)
	}

	// Sort by priority desc
	result = feed.List(FeedOptions{SortBy: SortByPriorityDesc})
	if result[0].ID != "new" {
		t.Errorf("first item should be 'new' when sorted by priority desc, got %q", result[0].ID)
	}

	// Sort by priority asc
	result = feed.List(FeedOptions{SortBy: SortByPriorityAsc})
	if result[0].ID != "old" {
		t.Errorf("first item should be 'old' when sorted by priority asc, got %q", result[0].ID)
	}
}

func TestFeed_Active(t *testing.T) {
	feed := NewFeed()
	feed.Add(&Item{ID: "1", Status: StatusNew})
	feed.Add(&Item{ID: "2", Status: StatusViewed})
	feed.Add(&Item{ID: "3", Status: StatusHandled})
	feed.Add(&Item{ID: "4", Status: StatusInProgress})

	result := feed.Active()
	if len(result) != 3 {
		t.Errorf("expected 3 active items, got %d", len(result))
	}
}

func TestFeed_New(t *testing.T) {
	feed := NewFeed()
	feed.Add(&Item{ID: "1", Status: StatusNew})
	feed.Add(&Item{ID: "2", Status: StatusViewed})
	feed.Add(&Item{ID: "3", Status: StatusNew})

	result := feed.New()
	if len(result) != 2 {
		t.Errorf("expected 2 new items, got %d", len(result))
	}
}

func TestFeed_Urgent(t *testing.T) {
	feed := NewFeed()
	feed.Add(&Item{ID: "1", Status: StatusNew, Priority: PriorityHigh})
	feed.Add(&Item{ID: "2", Status: StatusNew, Priority: PriorityNormal})
	feed.Add(&Item{ID: "3", Status: StatusNew, Priority: PriorityCritical})
	feed.Add(&Item{ID: "4", Status: StatusHandled, Priority: PriorityUrgent})

	result := feed.Urgent()
	if len(result) != 2 {
		t.Errorf("expected 2 urgent items (high+ and active), got %d", len(result))
	}
}

func TestFeed_Stats(t *testing.T) {
	feed := NewFeed()
	now := time.Now()

	feed.Add(&Item{ID: "1", Status: StatusNew, Channel: models.ChannelSlack, Type: ItemTypeMessage, Priority: PriorityHigh, ReceivedAt: now.Add(-time.Hour)})
	feed.Add(&Item{ID: "2", Status: StatusViewed, Channel: models.ChannelEmail, Type: ItemTypeEmail, Priority: PriorityNormal, ReceivedAt: now})
	feed.Add(&Item{ID: "3", Status: StatusSnoozed, Channel: models.ChannelSlack, Type: ItemTypeMessage, Priority: PriorityLow, ReceivedAt: now.Add(-30 * time.Minute)})

	stats := feed.Stats()

	if stats.TotalItems != 3 {
		t.Errorf("TotalItems = %d, want 3", stats.TotalItems)
	}
	if stats.NewItems != 1 {
		t.Errorf("NewItems = %d, want 1", stats.NewItems)
	}
	if stats.ViewedItems != 1 {
		t.Errorf("ViewedItems = %d, want 1", stats.ViewedItems)
	}
	if stats.SnoozedItems != 1 {
		t.Errorf("SnoozedItems = %d, want 1", stats.SnoozedItems)
	}
	if stats.ByChannel["slack"] != 2 {
		t.Errorf("ByChannel[slack] = %d, want 2", stats.ByChannel["slack"])
	}
	if stats.ByType["message"] != 2 {
		t.Errorf("ByType[message] = %d, want 2", stats.ByType["message"])
	}
}

func TestFeed_Stats_Empty(t *testing.T) {
	feed := NewFeed()
	stats := feed.Stats()

	if stats.TotalItems != 0 {
		t.Errorf("TotalItems = %d, want 0", stats.TotalItems)
	}
	if stats.OldestItem != nil {
		t.Error("OldestItem should be nil for empty feed")
	}
	if stats.NewestItem != nil {
		t.Error("NewestItem should be nil for empty feed")
	}
}

func TestFeed_OnItemChange(t *testing.T) {
	feed := NewFeed()

	var events []string
	feed.OnItemChange(func(item *Item, event string) {
		events = append(events, event)
	})

	feed.Add(&Item{ID: "1", Status: StatusNew})
	feed.MarkViewed("1")
	feed.MarkHandled("1")
	feed.Remove("1")

	expected := []string{"added", "viewed", "handled", "removed"}
	if len(events) != len(expected) {
		t.Errorf("got %d events, want %d", len(events), len(expected))
	}
	for i, e := range expected {
		if i < len(events) && events[i] != e {
			t.Errorf("events[%d] = %q, want %q", i, events[i], e)
		}
	}
}

func TestFeed_WakeSnoozed(t *testing.T) {
	feed := NewFeed()

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	feed.Add(&Item{ID: "past", Status: StatusSnoozed, SnoozedUntil: &past})
	feed.Add(&Item{ID: "future", Status: StatusSnoozed, SnoozedUntil: &future})
	feed.Add(&Item{ID: "normal", Status: StatusNew})

	woken := feed.WakeSnoozed()

	if len(woken) != 1 {
		t.Errorf("expected 1 woken item, got %d", len(woken))
	}
	if len(woken) > 0 && woken[0].ID != "past" {
		t.Errorf("woken item ID = %q, want %q", woken[0].ID, "past")
	}

	// Verify status changed
	item, _ := feed.Get("past")
	if item.Status == StatusSnoozed {
		t.Error("woken item should not be Snoozed anymore")
	}
}

func TestFeed_Prune(t *testing.T) {
	feed := NewFeed()

	oldTime := time.Now().Add(-48 * time.Hour)
	recentTime := time.Now().Add(-1 * time.Hour)

	feed.Add(&Item{ID: "old-handled", Status: StatusHandled, HandledAt: &oldTime})
	feed.Add(&Item{ID: "old-archived", Status: StatusArchived, HandledAt: &oldTime})
	feed.Add(&Item{ID: "recent-handled", Status: StatusHandled, HandledAt: &recentTime})
	feed.Add(&Item{ID: "active", Status: StatusNew})

	removed := feed.Prune(24 * time.Hour)

	if removed != 2 {
		t.Errorf("expected 2 removed items, got %d", removed)
	}

	// Verify old items removed
	if _, ok := feed.Get("old-handled"); ok {
		t.Error("old-handled should be removed")
	}
	if _, ok := feed.Get("old-archived"); ok {
		t.Error("old-archived should be removed")
	}

	// Verify recent and active items remain
	if _, ok := feed.Get("recent-handled"); !ok {
		t.Error("recent-handled should remain")
	}
	if _, ok := feed.Get("active"); !ok {
		t.Error("active should remain")
	}
}

func TestNewAggregator(t *testing.T) {
	feed := NewFeed()
	agg := NewAggregator(feed)

	if agg == nil {
		t.Fatal("NewAggregator returned nil")
	}
	if agg.feed != feed {
		t.Error("feed not set correctly")
	}
}

func TestAggregator_AddSource(t *testing.T) {
	feed := NewFeed()
	agg := NewAggregator(feed)

	source := NewTicketSource()
	agg.AddSource(source)

	if len(agg.sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(agg.sources))
	}
}

func TestAggregator_Stop_WithoutStart(t *testing.T) {
	feed := NewFeed()
	agg := NewAggregator(feed)

	// Stop without starting should not panic
	agg.Stop()
}

func TestFeedOptions_Struct(t *testing.T) {
	opts := FeedOptions{
		Channels:       []models.ChannelType{models.ChannelSlack},
		Types:          []ItemType{ItemTypeMessage},
		MinPriority:    PriorityHigh,
		Statuses:       []Status{StatusNew},
		Tags:           []string{"urgent"},
		SenderIDs:      []string{"user-1"},
		Since:          time.Now().Add(-time.Hour),
		Until:          time.Now(),
		Limit:          10,
		Offset:         5,
		SortBy:         SortByPriorityDesc,
		IncludeSnoozed: true,
	}

	if len(opts.Channels) != 1 {
		t.Error("Channels not set correctly")
	}
	if opts.Limit != 10 {
		t.Errorf("Limit = %d, want 10", opts.Limit)
	}
	if !opts.IncludeSnoozed {
		t.Error("IncludeSnoozed should be true")
	}
}

func TestSortOrder_Constants(t *testing.T) {
	if SortByReceivedDesc != "received_desc" {
		t.Errorf("SortByReceivedDesc = %q, want %q", SortByReceivedDesc, "received_desc")
	}
	if SortByReceivedAsc != "received_asc" {
		t.Errorf("SortByReceivedAsc = %q, want %q", SortByReceivedAsc, "received_asc")
	}
	if SortByPriorityDesc != "priority_desc" {
		t.Errorf("SortByPriorityDesc = %q, want %q", SortByPriorityDesc, "priority_desc")
	}
	if SortByPriorityAsc != "priority_asc" {
		t.Errorf("SortByPriorityAsc = %q, want %q", SortByPriorityAsc, "priority_asc")
	}
}

func TestFeedStats_Struct(t *testing.T) {
	now := time.Now()
	stats := FeedStats{
		TotalItems:   100,
		NewItems:     50,
		ViewedItems:  30,
		SnoozedItems: 20,
		ByChannel:    map[string]int{"slack": 60, "email": 40},
		ByType:       map[string]int{"message": 70, "email": 30},
		ByPriority:   map[int]int{1: 10, 2: 50, 3: 40},
		OldestItem:   &now,
		NewestItem:   &now,
	}

	if stats.TotalItems != 100 {
		t.Errorf("TotalItems = %d, want 100", stats.TotalItems)
	}
	if stats.ByChannel["slack"] != 60 {
		t.Errorf("ByChannel[slack] = %d, want 60", stats.ByChannel["slack"])
	}
}
