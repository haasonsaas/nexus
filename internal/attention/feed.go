package attention

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// FeedOptions configures how items are filtered and sorted.
type FeedOptions struct {
	// Channels filters to specific channels (empty = all)
	Channels []models.ChannelType

	// Types filters to specific item types (empty = all)
	Types []ItemType

	// Priorities filters to minimum priority level
	MinPriority Priority

	// Statuses filters to specific statuses (empty = active items)
	Statuses []Status

	// Tags filters to items with any of these tags
	Tags []string

	// SenderIDs filters to items from specific senders
	SenderIDs []string

	// Since filters to items received after this time
	Since time.Time

	// Until filters to items received before this time
	Until time.Time

	// Limit caps the number of items returned
	Limit int

	// Offset for pagination
	Offset int

	// SortBy determines sort order
	SortBy SortOrder

	// IncludeSnoozed includes snoozed items if true
	IncludeSnoozed bool
}

// SortOrder determines how items are sorted.
type SortOrder string

const (
	SortByReceivedDesc SortOrder = "received_desc" // newest first (default)
	SortByReceivedAsc  SortOrder = "received_asc"  // oldest first
	SortByPriorityDesc SortOrder = "priority_desc" // highest priority first
	SortByPriorityAsc  SortOrder = "priority_asc"  // lowest priority first
)

// FeedStats provides aggregate statistics about the feed.
type FeedStats struct {
	TotalItems    int            `json:"total_items"`
	NewItems      int            `json:"new_items"`
	ViewedItems   int            `json:"viewed_items"`
	SnoozedItems  int            `json:"snoozed_items"`
	ByChannel     map[string]int `json:"by_channel"`
	ByType        map[string]int `json:"by_type"`
	ByPriority    map[int]int    `json:"by_priority"`
	OldestItem    *time.Time     `json:"oldest_item,omitempty"`
	NewestItem    *time.Time     `json:"newest_item,omitempty"`
}

// Feed aggregates attention items from multiple channels.
type Feed struct {
	items    map[string]*Item
	mu       sync.RWMutex
	handlers []ItemHandler
}

// ItemHandler is called when items are added or updated.
type ItemHandler func(item *Item, event string)

// NewFeed creates a new attention feed.
func NewFeed() *Feed {
	return &Feed{
		items: make(map[string]*Item),
	}
}

// Add adds a new item to the feed.
func (f *Feed) Add(item *Item) {
	f.mu.Lock()
	f.items[item.ID] = item
	f.mu.Unlock()

	f.notifyHandlers(item, "added")
}

// AddMessage adds a message as an attention item.
func (f *Feed) AddMessage(msg *models.Message) *Item {
	item := ItemFromMessage(msg)
	f.Add(item)
	return item
}

// Get retrieves an item by ID.
func (f *Feed) Get(id string) (*Item, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	item, ok := f.items[id]
	return item, ok
}

// Update updates an existing item.
func (f *Feed) Update(item *Item) bool {
	f.mu.Lock()
	if _, exists := f.items[item.ID]; !exists {
		f.mu.Unlock()
		return false
	}
	f.items[item.ID] = item
	f.mu.Unlock()

	f.notifyHandlers(item, "updated")
	return true
}

// Remove removes an item from the feed.
func (f *Feed) Remove(id string) bool {
	f.mu.Lock()
	item, exists := f.items[id]
	if exists {
		delete(f.items, id)
	}
	f.mu.Unlock()

	if exists {
		f.notifyHandlers(item, "removed")
	}
	return exists
}

// MarkViewed marks an item as viewed.
func (f *Feed) MarkViewed(id string) bool {
	f.mu.Lock()
	item, exists := f.items[id]
	if exists {
		item.SetViewed()
	}
	f.mu.Unlock()

	if exists {
		f.notifyHandlers(item, "viewed")
	}
	return exists
}

// MarkHandled marks an item as handled.
func (f *Feed) MarkHandled(id string) bool {
	f.mu.Lock()
	item, exists := f.items[id]
	if exists {
		item.SetHandled()
	}
	f.mu.Unlock()

	if exists {
		f.notifyHandlers(item, "handled")
	}
	return exists
}

// Snooze snoozes an item until the given time.
func (f *Feed) Snooze(id string, until time.Time) bool {
	f.mu.Lock()
	item, exists := f.items[id]
	if exists {
		item.Snooze(until)
	}
	f.mu.Unlock()

	if exists {
		f.notifyHandlers(item, "snoozed")
	}
	return exists
}

// Unsnooze brings a snoozed item back to active.
func (f *Feed) Unsnooze(id string) bool {
	f.mu.Lock()
	item, exists := f.items[id]
	if exists {
		item.Unsnooze()
	}
	f.mu.Unlock()

	if exists {
		f.notifyHandlers(item, "unsnoozed")
	}
	return exists
}

// List returns items matching the given options.
func (f *Feed) List(opts FeedOptions) []*Item {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Collect matching items
	var result []*Item
	for _, item := range f.items {
		if f.matchesOptions(item, opts) {
			result = append(result, item)
		}
	}

	// Sort
	f.sortItems(result, opts.SortBy)

	// Apply pagination
	if opts.Offset > 0 && opts.Offset < len(result) {
		result = result[opts.Offset:]
	} else if opts.Offset >= len(result) {
		return nil
	}

	if opts.Limit > 0 && opts.Limit < len(result) {
		result = result[:opts.Limit]
	}

	return result
}

// Active returns all active (requiring attention) items.
func (f *Feed) Active() []*Item {
	return f.List(FeedOptions{
		Statuses: []Status{StatusNew, StatusViewed, StatusInProgress},
	})
}

// New returns all new (unviewed) items.
func (f *Feed) New() []*Item {
	return f.List(FeedOptions{
		Statuses: []Status{StatusNew},
	})
}

// Urgent returns high priority and above items.
func (f *Feed) Urgent() []*Item {
	return f.List(FeedOptions{
		MinPriority: PriorityHigh,
		Statuses:    []Status{StatusNew, StatusViewed, StatusInProgress},
		SortBy:      SortByPriorityDesc,
	})
}

// Stats returns aggregate statistics about the feed.
func (f *Feed) Stats() FeedStats {
	f.mu.RLock()
	defer f.mu.RUnlock()

	stats := FeedStats{
		ByChannel:  make(map[string]int),
		ByType:     make(map[string]int),
		ByPriority: make(map[int]int),
	}

	for _, item := range f.items {
		stats.TotalItems++
		stats.ByChannel[string(item.Channel)]++
		stats.ByType[string(item.Type)]++
		stats.ByPriority[int(item.Priority)]++

		switch item.Status {
		case StatusNew:
			stats.NewItems++
		case StatusViewed, StatusInProgress:
			stats.ViewedItems++
		case StatusSnoozed:
			stats.SnoozedItems++
		}

		if stats.OldestItem == nil || item.ReceivedAt.Before(*stats.OldestItem) {
			stats.OldestItem = &item.ReceivedAt
		}
		if stats.NewestItem == nil || item.ReceivedAt.After(*stats.NewestItem) {
			stats.NewestItem = &item.ReceivedAt
		}
	}

	return stats
}

// OnItemChange registers a handler for item changes.
func (f *Feed) OnItemChange(handler ItemHandler) {
	f.mu.Lock()
	f.handlers = append(f.handlers, handler)
	f.mu.Unlock()
}

// WakeSnoozed checks for snoozed items that should be unsnoozed.
func (f *Feed) WakeSnoozed() []*Item {
	f.mu.Lock()
	defer f.mu.Unlock()

	var woken []*Item
	now := time.Now()

	for _, item := range f.items {
		if item.Status == StatusSnoozed && item.SnoozedUntil != nil {
			if now.After(*item.SnoozedUntil) {
				item.Unsnooze()
				woken = append(woken, item)
			}
		}
	}

	return woken
}

// Prune removes old handled/archived items.
func (f *Feed) Prune(olderThan time.Duration) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	threshold := time.Now().Add(-olderThan)
	var removed int

	for id, item := range f.items {
		if item.Status == StatusHandled || item.Status == StatusArchived {
			if item.HandledAt != nil && item.HandledAt.Before(threshold) {
				delete(f.items, id)
				removed++
			}
		}
	}

	return removed
}

// matchesOptions checks if an item matches filter options.
func (f *Feed) matchesOptions(item *Item, opts FeedOptions) bool {
	// Channel filter
	if len(opts.Channels) > 0 {
		found := false
		for _, ch := range opts.Channels {
			if item.Channel == ch {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Type filter
	if len(opts.Types) > 0 {
		found := false
		for _, t := range opts.Types {
			if item.Type == t {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Priority filter
	if opts.MinPriority > 0 && item.Priority < opts.MinPriority {
		return false
	}

	// Status filter
	if len(opts.Statuses) > 0 {
		found := false
		for _, s := range opts.Statuses {
			if item.Status == s {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	} else if !opts.IncludeSnoozed && item.Status == StatusSnoozed {
		// Default: exclude snoozed unless explicitly included
		return false
	}

	// Tag filter
	if len(opts.Tags) > 0 {
		found := false
		for _, tag := range opts.Tags {
			for _, itemTag := range item.Tags {
				if tag == itemTag {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}

	// Sender filter
	if len(opts.SenderIDs) > 0 {
		found := false
		for _, id := range opts.SenderIDs {
			if item.Sender.ID == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Time filters
	if !opts.Since.IsZero() && item.ReceivedAt.Before(opts.Since) {
		return false
	}
	if !opts.Until.IsZero() && item.ReceivedAt.After(opts.Until) {
		return false
	}

	return true
}

// sortItems sorts the result set based on sort order.
func (f *Feed) sortItems(items []*Item, sortBy SortOrder) {
	switch sortBy {
	case SortByReceivedAsc:
		sort.Slice(items, func(i, j int) bool {
			return items[i].ReceivedAt.Before(items[j].ReceivedAt)
		})
	case SortByPriorityDesc:
		sort.Slice(items, func(i, j int) bool {
			if items[i].Priority != items[j].Priority {
				return items[i].Priority > items[j].Priority
			}
			return items[i].ReceivedAt.After(items[j].ReceivedAt)
		})
	case SortByPriorityAsc:
		sort.Slice(items, func(i, j int) bool {
			if items[i].Priority != items[j].Priority {
				return items[i].Priority < items[j].Priority
			}
			return items[i].ReceivedAt.Before(items[j].ReceivedAt)
		})
	default: // SortByReceivedDesc
		sort.Slice(items, func(i, j int) bool {
			return items[i].ReceivedAt.After(items[j].ReceivedAt)
		})
	}
}

// notifyHandlers calls all registered handlers.
func (f *Feed) notifyHandlers(item *Item, event string) {
	f.mu.RLock()
	handlers := make([]ItemHandler, len(f.handlers))
	copy(handlers, f.handlers)
	f.mu.RUnlock()

	for _, h := range handlers {
		h(item, event)
	}
}

// Aggregator collects items from multiple channel sources.
type Aggregator struct {
	feed    *Feed
	sources []ItemSource
	mu      sync.Mutex
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// ItemSource provides attention items from a channel.
type ItemSource interface {
	// Items returns a channel of items to add to the feed
	Items() <-chan *Item
}

// NewAggregator creates an aggregator with the given feed.
func NewAggregator(feed *Feed) *Aggregator {
	return &Aggregator{
		feed: feed,
	}
}

// AddSource adds a source of attention items.
func (a *Aggregator) AddSource(source ItemSource) {
	a.mu.Lock()
	a.sources = append(a.sources, source)
	a.mu.Unlock()
}

// Start begins aggregating items from all sources.
func (a *Aggregator) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	a.mu.Lock()
	sources := make([]ItemSource, len(a.sources))
	copy(sources, a.sources)
	a.mu.Unlock()

	for _, source := range sources {
		a.wg.Add(1)
		go a.consumeSource(ctx, source)
	}

	// Start snooze watcher
	a.wg.Add(1)
	go a.watchSnoozed(ctx)
}

// Stop stops the aggregator.
func (a *Aggregator) Stop() {
	if a.cancel != nil {
		a.cancel()
	}
	a.wg.Wait()
}

// consumeSource reads items from a source and adds them to the feed.
func (a *Aggregator) consumeSource(ctx context.Context, source ItemSource) {
	defer a.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-source.Items():
			if !ok {
				return
			}
			a.feed.Add(item)
		}
	}
}

// watchSnoozed periodically checks for snoozed items to wake.
func (a *Aggregator) watchSnoozed(ctx context.Context) {
	defer a.wg.Done()

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.feed.WakeSnoozed()
		}
	}
}
