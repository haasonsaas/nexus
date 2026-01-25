package subagent

import (
	"testing"
	"time"
)

func TestNewAnnounceQueue(t *testing.T) {
	q := NewAnnounceQueue()
	if q == nil {
		t.Fatal("expected non-nil queue")
	}
	if q.queues == nil {
		t.Error("queues map should be initialized")
	}
	if q.settings == nil {
		t.Error("settings map should be initialized")
	}
}

func TestAnnounceQueue_Enqueue(t *testing.T) {
	t.Run("adds item to empty queue", func(t *testing.T) {
		q := NewAnnounceQueue()
		item := &AnnounceQueueItem{
			Prompt:     "test prompt",
			SessionKey: "sess-1",
			EnqueuedAt: time.Now(),
		}

		q.Enqueue("sess-1", item, nil)

		if q.Size("sess-1") != 1 {
			t.Errorf("Size = %d, want 1", q.Size("sess-1"))
		}
	})

	t.Run("adds multiple items", func(t *testing.T) {
		q := NewAnnounceQueue()

		for i := 0; i < 5; i++ {
			item := &AnnounceQueueItem{
				Prompt:     "prompt",
				SessionKey: "sess-1",
				EnqueuedAt: time.Now(),
			}
			q.Enqueue("sess-1", item, nil)
		}

		if q.Size("sess-1") != 5 {
			t.Errorf("Size = %d, want 5", q.Size("sess-1"))
		}
	})

	t.Run("respects maxItems with oldest drop policy", func(t *testing.T) {
		q := NewAnnounceQueue()
		settings := &QueueSettings{
			MaxItems:   3,
			DropPolicy: "oldest",
		}

		for i := 0; i < 5; i++ {
			item := &AnnounceQueueItem{
				Prompt:     "prompt-" + string(rune('a'+i)),
				SessionKey: "sess-1",
				EnqueuedAt: time.Now(),
			}
			q.Enqueue("sess-1", item, settings)
		}

		if q.Size("sess-1") != 3 {
			t.Errorf("Size = %d, want 3", q.Size("sess-1"))
		}

		// The oldest items should have been dropped
		first := q.Peek("sess-1")
		if first.Prompt != "prompt-c" {
			t.Errorf("first item Prompt = %q, want 'prompt-c'", first.Prompt)
		}
	})

	t.Run("respects maxItems with newest drop policy", func(t *testing.T) {
		q := NewAnnounceQueue()
		settings := &QueueSettings{
			MaxItems:   3,
			DropPolicy: "newest",
		}

		for i := 0; i < 5; i++ {
			item := &AnnounceQueueItem{
				Prompt:     "prompt-" + string(rune('a'+i)),
				SessionKey: "sess-1",
				EnqueuedAt: time.Now(),
			}
			q.Enqueue("sess-1", item, settings)
		}

		// With newest drop policy, new items are not added when full
		if q.Size("sess-1") != 3 {
			t.Errorf("Size = %d, want 3", q.Size("sess-1"))
		}

		// First item should still be 'a'
		first := q.Peek("sess-1")
		if first.Prompt != "prompt-a" {
			t.Errorf("first item Prompt = %q, want 'prompt-a'", first.Prompt)
		}
	})

	t.Run("stores settings", func(t *testing.T) {
		q := NewAnnounceQueue()
		settings := &QueueSettings{
			Mode:       "steer",
			MaxItems:   10,
			DropPolicy: "oldest",
		}

		item := &AnnounceQueueItem{Prompt: "test"}
		q.Enqueue("sess-1", item, settings)

		stored := q.GetSettings("sess-1")
		if stored == nil {
			t.Fatal("settings should be stored")
		}
		if stored.Mode != "steer" {
			t.Errorf("Mode = %q, want 'steer'", stored.Mode)
		}
	})

	t.Run("different sessions are independent", func(t *testing.T) {
		q := NewAnnounceQueue()

		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "a"}, nil)
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "b"}, nil)
		q.Enqueue("sess-2", &AnnounceQueueItem{Prompt: "c"}, nil)

		if q.Size("sess-1") != 2 {
			t.Errorf("sess-1 Size = %d, want 2", q.Size("sess-1"))
		}
		if q.Size("sess-2") != 1 {
			t.Errorf("sess-2 Size = %d, want 1", q.Size("sess-2"))
		}
	})
}

func TestAnnounceQueue_Dequeue(t *testing.T) {
	t.Run("returns nil for empty queue", func(t *testing.T) {
		q := NewAnnounceQueue()
		item := q.Dequeue("nonexistent")
		if item != nil {
			t.Error("expected nil for empty queue")
		}
	})

	t.Run("returns nil for unknown session", func(t *testing.T) {
		q := NewAnnounceQueue()
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "test"}, nil)

		item := q.Dequeue("sess-2")
		if item != nil {
			t.Error("expected nil for unknown session")
		}
	})

	t.Run("returns and removes first item", func(t *testing.T) {
		q := NewAnnounceQueue()
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "first"}, nil)
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "second"}, nil)

		item := q.Dequeue("sess-1")
		if item == nil {
			t.Fatal("expected non-nil item")
		}
		if item.Prompt != "first" {
			t.Errorf("Prompt = %q, want 'first'", item.Prompt)
		}
		if q.Size("sess-1") != 1 {
			t.Errorf("Size after dequeue = %d, want 1", q.Size("sess-1"))
		}
	})

	t.Run("FIFO order", func(t *testing.T) {
		q := NewAnnounceQueue()
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "a"}, nil)
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "b"}, nil)
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "c"}, nil)

		items := []string{
			q.Dequeue("sess-1").Prompt,
			q.Dequeue("sess-1").Prompt,
			q.Dequeue("sess-1").Prompt,
		}

		expected := []string{"a", "b", "c"}
		for i, v := range items {
			if v != expected[i] {
				t.Errorf("item[%d] = %q, want %q", i, v, expected[i])
			}
		}
	})
}

func TestAnnounceQueue_DequeueAll(t *testing.T) {
	t.Run("returns nil for empty queue", func(t *testing.T) {
		q := NewAnnounceQueue()
		items := q.DequeueAll("nonexistent")
		if items != nil {
			t.Error("expected nil for empty queue")
		}
	})

	t.Run("returns all items and clears queue", func(t *testing.T) {
		q := NewAnnounceQueue()
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "a"}, nil)
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "b"}, nil)
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "c"}, nil)

		items := q.DequeueAll("sess-1")
		if len(items) != 3 {
			t.Fatalf("got %d items, want 3", len(items))
		}
		if items[0].Prompt != "a" {
			t.Errorf("items[0].Prompt = %q, want 'a'", items[0].Prompt)
		}
		if items[2].Prompt != "c" {
			t.Errorf("items[2].Prompt = %q, want 'c'", items[2].Prompt)
		}

		if q.Size("sess-1") != 0 {
			t.Errorf("Size after DequeueAll = %d, want 0", q.Size("sess-1"))
		}
	})

	t.Run("returned items are a copy", func(t *testing.T) {
		q := NewAnnounceQueue()
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "test"}, nil)

		items := q.DequeueAll("sess-1")

		// Add new items after dequeue
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "new"}, nil)

		// Original items should not be affected
		if len(items) != 1 {
			t.Errorf("original items affected, len = %d", len(items))
		}
	})
}

func TestAnnounceQueue_Size(t *testing.T) {
	t.Run("returns 0 for unknown session", func(t *testing.T) {
		q := NewAnnounceQueue()
		if q.Size("unknown") != 0 {
			t.Error("expected 0 for unknown session")
		}
	})

	t.Run("returns correct size", func(t *testing.T) {
		q := NewAnnounceQueue()
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "a"}, nil)
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "b"}, nil)

		if q.Size("sess-1") != 2 {
			t.Errorf("Size = %d, want 2", q.Size("sess-1"))
		}
	})
}

func TestAnnounceQueue_Clear(t *testing.T) {
	t.Run("clears items and settings", func(t *testing.T) {
		q := NewAnnounceQueue()
		settings := &QueueSettings{Mode: "steer", MaxItems: 5}
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "a"}, settings)
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "b"}, nil)

		q.Clear("sess-1")

		if q.Size("sess-1") != 0 {
			t.Errorf("Size after Clear = %d, want 0", q.Size("sess-1"))
		}
		if q.GetSettings("sess-1") != nil {
			t.Error("settings should be cleared")
		}
	})

	t.Run("clearing unknown session does not panic", func(t *testing.T) {
		q := NewAnnounceQueue()
		q.Clear("unknown") // Should not panic
	})

	t.Run("clearing one session doesn't affect others", func(t *testing.T) {
		q := NewAnnounceQueue()
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "a"}, nil)
		q.Enqueue("sess-2", &AnnounceQueueItem{Prompt: "b"}, nil)

		q.Clear("sess-1")

		if q.Size("sess-1") != 0 {
			t.Error("sess-1 should be cleared")
		}
		if q.Size("sess-2") != 1 {
			t.Error("sess-2 should not be affected")
		}
	})
}

func TestAnnounceQueue_Peek(t *testing.T) {
	t.Run("returns nil for empty queue", func(t *testing.T) {
		q := NewAnnounceQueue()
		item := q.Peek("unknown")
		if item != nil {
			t.Error("expected nil for empty queue")
		}
	})

	t.Run("returns first item without removing", func(t *testing.T) {
		q := NewAnnounceQueue()
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "first"}, nil)
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "second"}, nil)

		item := q.Peek("sess-1")
		if item == nil {
			t.Fatal("expected non-nil item")
		}
		if item.Prompt != "first" {
			t.Errorf("Prompt = %q, want 'first'", item.Prompt)
		}

		// Size should not change
		if q.Size("sess-1") != 2 {
			t.Errorf("Size after Peek = %d, want 2", q.Size("sess-1"))
		}

		// Peek again should return same item
		item2 := q.Peek("sess-1")
		if item2.Prompt != "first" {
			t.Error("Peek should return same item each time")
		}
	})
}

func TestAnnounceQueue_GetSettings(t *testing.T) {
	t.Run("returns nil for unknown session", func(t *testing.T) {
		q := NewAnnounceQueue()
		if q.GetSettings("unknown") != nil {
			t.Error("expected nil for unknown session")
		}
	})

	t.Run("returns stored settings", func(t *testing.T) {
		q := NewAnnounceQueue()
		settings := &QueueSettings{
			Mode:       "interrupt",
			MaxItems:   20,
			DropPolicy: "newest",
		}
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "test"}, settings)

		stored := q.GetSettings("sess-1")
		if stored.Mode != "interrupt" {
			t.Errorf("Mode = %q, want 'interrupt'", stored.Mode)
		}
		if stored.MaxItems != 20 {
			t.Errorf("MaxItems = %d, want 20", stored.MaxItems)
		}
		if stored.DropPolicy != "newest" {
			t.Errorf("DropPolicy = %q, want 'newest'", stored.DropPolicy)
		}
	})
}

func TestAnnounceQueue_SetSettings(t *testing.T) {
	t.Run("sets settings for session", func(t *testing.T) {
		q := NewAnnounceQueue()
		settings := &QueueSettings{
			Mode:       "collect",
			MaxItems:   15,
			DropPolicy: "oldest",
		}

		q.SetSettings("sess-1", settings)

		stored := q.GetSettings("sess-1")
		if stored == nil {
			t.Fatal("settings should be stored")
		}
		if stored.Mode != "collect" {
			t.Errorf("Mode = %q, want 'collect'", stored.Mode)
		}
	})

	t.Run("overwrites existing settings", func(t *testing.T) {
		q := NewAnnounceQueue()
		q.SetSettings("sess-1", &QueueSettings{Mode: "steer"})
		q.SetSettings("sess-1", &QueueSettings{Mode: "followup"})

		stored := q.GetSettings("sess-1")
		if stored.Mode != "followup" {
			t.Errorf("Mode = %q, want 'followup'", stored.Mode)
		}
	})
}

func TestAnnounceQueue_Sessions(t *testing.T) {
	t.Run("returns empty for no sessions", func(t *testing.T) {
		q := NewAnnounceQueue()
		sessions := q.Sessions()
		if len(sessions) != 0 {
			t.Errorf("got %d sessions, want 0", len(sessions))
		}
	})

	t.Run("returns sessions with items", func(t *testing.T) {
		q := NewAnnounceQueue()
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "a"}, nil)
		q.Enqueue("sess-2", &AnnounceQueueItem{Prompt: "b"}, nil)
		q.Enqueue("sess-3", &AnnounceQueueItem{Prompt: "c"}, nil)

		sessions := q.Sessions()
		if len(sessions) != 3 {
			t.Errorf("got %d sessions, want 3", len(sessions))
		}

		// Check all sessions are present
		found := make(map[string]bool)
		for _, s := range sessions {
			found[s] = true
		}
		for _, expected := range []string{"sess-1", "sess-2", "sess-3"} {
			if !found[expected] {
				t.Errorf("session %q not found", expected)
			}
		}
	})

	t.Run("excludes empty sessions", func(t *testing.T) {
		q := NewAnnounceQueue()
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "a"}, nil)
		q.Enqueue("sess-2", &AnnounceQueueItem{Prompt: "b"}, nil)
		q.DequeueAll("sess-1") // Empty sess-1

		sessions := q.Sessions()
		if len(sessions) != 1 {
			t.Errorf("got %d sessions, want 1", len(sessions))
		}
		if sessions[0] != "sess-2" {
			t.Errorf("session = %q, want 'sess-2'", sessions[0])
		}
	})
}

func TestAnnounceQueue_TotalSize(t *testing.T) {
	t.Run("returns 0 for empty queue", func(t *testing.T) {
		q := NewAnnounceQueue()
		if q.TotalSize() != 0 {
			t.Errorf("TotalSize = %d, want 0", q.TotalSize())
		}
	})

	t.Run("returns total across all sessions", func(t *testing.T) {
		q := NewAnnounceQueue()
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "a"}, nil)
		q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "b"}, nil)
		q.Enqueue("sess-2", &AnnounceQueueItem{Prompt: "c"}, nil)
		q.Enqueue("sess-3", &AnnounceQueueItem{Prompt: "d"}, nil)
		q.Enqueue("sess-3", &AnnounceQueueItem{Prompt: "e"}, nil)
		q.Enqueue("sess-3", &AnnounceQueueItem{Prompt: "f"}, nil)

		if q.TotalSize() != 6 {
			t.Errorf("TotalSize = %d, want 6", q.TotalSize())
		}
	})
}

func TestAnnounceQueueItem(t *testing.T) {
	t.Run("can create with all fields", func(t *testing.T) {
		now := time.Now()
		item := &AnnounceQueueItem{
			Prompt:      "test prompt",
			SummaryLine: "summary",
			EnqueuedAt:  now,
			SessionKey:  "sess-123",
			Origin: &DeliveryContext{
				Channel:   "slack",
				AccountID: "acc-456",
			},
		}

		if item.Prompt != "test prompt" {
			t.Error("Prompt not set correctly")
		}
		if item.SummaryLine != "summary" {
			t.Error("SummaryLine not set correctly")
		}
		if !item.EnqueuedAt.Equal(now) {
			t.Error("EnqueuedAt not set correctly")
		}
		if item.SessionKey != "sess-123" {
			t.Error("SessionKey not set correctly")
		}
		if item.Origin == nil || item.Origin.Channel != "slack" {
			t.Error("Origin not set correctly")
		}
	})
}

func TestQueueSettings(t *testing.T) {
	t.Run("can create with all fields", func(t *testing.T) {
		settings := &QueueSettings{
			Mode:       "steer",
			MaxItems:   50,
			DropPolicy: "oldest",
		}

		if settings.Mode != "steer" {
			t.Error("Mode not set correctly")
		}
		if settings.MaxItems != 50 {
			t.Error("MaxItems not set correctly")
		}
		if settings.DropPolicy != "oldest" {
			t.Error("DropPolicy not set correctly")
		}
	})
}

func TestAnnounceQueue_Concurrency(t *testing.T) {
	t.Run("concurrent enqueue and dequeue", func(t *testing.T) {
		q := NewAnnounceQueue()
		done := make(chan bool)

		// Concurrent enqueue
		go func() {
			for i := 0; i < 100; i++ {
				q.Enqueue("sess-1", &AnnounceQueueItem{Prompt: "item"}, nil)
			}
			done <- true
		}()

		// Concurrent dequeue
		go func() {
			for i := 0; i < 50; i++ {
				q.Dequeue("sess-1")
			}
			done <- true
		}()

		// Wait for both goroutines
		<-done
		<-done

		// Queue should have some items (exact count depends on timing)
		// The important thing is no race condition or panic
	})

	t.Run("concurrent operations on different sessions", func(t *testing.T) {
		q := NewAnnounceQueue()
		done := make(chan bool)

		for i := 0; i < 5; i++ {
			sessionKey := "sess-" + string(rune('a'+i))
			go func(key string) {
				for j := 0; j < 20; j++ {
					q.Enqueue(key, &AnnounceQueueItem{Prompt: "item"}, nil)
					q.Size(key)
					q.Peek(key)
				}
				done <- true
			}(sessionKey)
		}

		// Wait for all goroutines
		for i := 0; i < 5; i++ {
			<-done
		}

		// Verify each session has items
		for i := 0; i < 5; i++ {
			sessionKey := "sess-" + string(rune('a'+i))
			if q.Size(sessionKey) != 20 {
				t.Errorf("%s Size = %d, want 20", sessionKey, q.Size(sessionKey))
			}
		}
	})
}
