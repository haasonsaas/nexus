package subagent

import (
	"sync"
	"time"
)

// AnnounceQueueItem represents a queued announcement
type AnnounceQueueItem struct {
	Prompt      string
	SummaryLine string
	EnqueuedAt  time.Time
	SessionKey  string
	Origin      *DeliveryContext
}

// AnnounceQueue manages queued announcements per session
type AnnounceQueue struct {
	mu       sync.Mutex
	queues   map[string][]*AnnounceQueueItem
	settings map[string]*QueueSettings
}

// QueueSettings configures queue behavior
type QueueSettings struct {
	Mode       string // "steer", "followup", "collect", "interrupt"
	MaxItems   int
	DropPolicy string // "oldest", "newest"
}

// NewAnnounceQueue creates a new announce queue
func NewAnnounceQueue() *AnnounceQueue {
	return &AnnounceQueue{
		queues:   make(map[string][]*AnnounceQueueItem),
		settings: make(map[string]*QueueSettings),
	}
}

// Enqueue adds an item to the queue
func (q *AnnounceQueue) Enqueue(sessionKey string, item *AnnounceQueueItem, settings *QueueSettings) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Initialize queue if needed
	if _, exists := q.queues[sessionKey]; !exists {
		q.queues[sessionKey] = make([]*AnnounceQueueItem, 0)
	}

	// Store settings if provided
	if settings != nil {
		q.settings[sessionKey] = settings
	}

	// Get effective settings
	effectiveSettings := q.settings[sessionKey]
	maxItems := 100 // default max
	dropPolicy := "oldest"

	if effectiveSettings != nil {
		if effectiveSettings.MaxItems > 0 {
			maxItems = effectiveSettings.MaxItems
		}
		if effectiveSettings.DropPolicy != "" {
			dropPolicy = effectiveSettings.DropPolicy
		}
	}

	queue := q.queues[sessionKey]

	// Check if we need to drop items
	if len(queue) >= maxItems {
		switch dropPolicy {
		case "oldest":
			// Remove the oldest item (front of queue)
			queue = queue[1:]
		case "newest":
			// Don't add the new item, just return
			return
		}
	}

	// Add the new item
	q.queues[sessionKey] = append(queue, item)
}

// Dequeue removes and returns the next item
func (q *AnnounceQueue) Dequeue(sessionKey string) *AnnounceQueueItem {
	q.mu.Lock()
	defer q.mu.Unlock()

	queue, exists := q.queues[sessionKey]
	if !exists || len(queue) == 0 {
		return nil
	}

	// Get the first item
	item := queue[0]

	// Remove it from the queue
	q.queues[sessionKey] = queue[1:]

	return item
}

// DequeueAll removes and returns all items for a session
func (q *AnnounceQueue) DequeueAll(sessionKey string) []*AnnounceQueueItem {
	q.mu.Lock()
	defer q.mu.Unlock()

	queue, exists := q.queues[sessionKey]
	if !exists || len(queue) == 0 {
		return nil
	}

	// Make a copy of the items
	items := make([]*AnnounceQueueItem, len(queue))
	copy(items, queue)

	// Clear the queue
	q.queues[sessionKey] = make([]*AnnounceQueueItem, 0)

	return items
}

// Size returns queue size for a session
func (q *AnnounceQueue) Size(sessionKey string) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	queue, exists := q.queues[sessionKey]
	if !exists {
		return 0
	}
	return len(queue)
}

// Clear removes all items for a session
func (q *AnnounceQueue) Clear(sessionKey string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	delete(q.queues, sessionKey)
	delete(q.settings, sessionKey)
}

// Peek returns the next item without removing it
func (q *AnnounceQueue) Peek(sessionKey string) *AnnounceQueueItem {
	q.mu.Lock()
	defer q.mu.Unlock()

	queue, exists := q.queues[sessionKey]
	if !exists || len(queue) == 0 {
		return nil
	}

	return queue[0]
}

// GetSettings returns the settings for a session
func (q *AnnounceQueue) GetSettings(sessionKey string) *QueueSettings {
	q.mu.Lock()
	defer q.mu.Unlock()

	return q.settings[sessionKey]
}

// SetSettings updates the settings for a session
func (q *AnnounceQueue) SetSettings(sessionKey string, settings *QueueSettings) {
	q.mu.Lock()
	defer q.mu.Unlock()

	q.settings[sessionKey] = settings
}

// Sessions returns all session keys that have items in the queue
func (q *AnnounceQueue) Sessions() []string {
	q.mu.Lock()
	defer q.mu.Unlock()

	sessions := make([]string, 0, len(q.queues))
	for sessionKey, queue := range q.queues {
		if len(queue) > 0 {
			sessions = append(sessions, sessionKey)
		}
	}
	return sessions
}

// TotalSize returns the total number of items across all queues
func (q *AnnounceQueue) TotalSize() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	total := 0
	for _, queue := range q.queues {
		total += len(queue)
	}
	return total
}
