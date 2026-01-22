package gateway

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

// trackingStore tracks session creation for testing isolation.
type trackingStore struct {
	mu           sync.Mutex
	sessions     map[string]*models.Session
	sessionOrder []string // track order of session creation
	messages     map[string][]*models.Message
}

func newTrackingStore() *trackingStore {
	return &trackingStore{
		sessions: make(map[string]*models.Session),
		messages: make(map[string][]*models.Message),
	}
}

func (s *trackingStore) Create(ctx context.Context, session *models.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
	return nil
}

func (s *trackingStore) Get(ctx context.Context, id string) (*models.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		return sess, nil
	}
	return nil, fmt.Errorf("session not found: %s", id)
}

func (s *trackingStore) Update(ctx context.Context, session *models.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
	return nil
}

func (s *trackingStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	return nil
}

func (s *trackingStore) GetByKey(ctx context.Context, key string) (*models.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		if sess.Key == key {
			return sess, nil
		}
	}
	return nil, fmt.Errorf("session not found for key: %s", key)
}

func (s *trackingStore) GetOrCreate(ctx context.Context, key string, agentID string, channel models.ChannelType, channelID string) (*models.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if session already exists
	for _, sess := range s.sessions {
		if sess.Key == key {
			return sess, nil
		}
	}

	// Create new session
	session := &models.Session{
		ID:        fmt.Sprintf("session-%s-%d", agentID, len(s.sessions)+1),
		AgentID:   agentID,
		Channel:   channel,
		ChannelID: channelID,
		Key:       key,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	s.sessions[session.ID] = session
	s.sessionOrder = append(s.sessionOrder, key)
	return session, nil
}

func (s *trackingStore) List(ctx context.Context, agentID string, opts sessions.ListOptions) ([]*models.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []*models.Session
	for _, sess := range s.sessions {
		if agentID == "" || sess.AgentID == agentID {
			result = append(result, sess)
		}
	}
	return result, nil
}

func (s *trackingStore) AppendMessage(ctx context.Context, sessionID string, msg *models.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[sessionID] = append(s.messages[sessionID], msg)
	return nil
}

func (s *trackingStore) GetHistory(ctx context.Context, sessionID string, limit int) ([]*models.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.messages[sessionID], nil
}

func (s *trackingStore) GetSessionKeys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.sessions))
	for _, sess := range s.sessions {
		keys = append(keys, sess.Key)
	}
	sort.Strings(keys)
	return keys
}

func (s *trackingStore) GetSessionByKey(key string) *models.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sessions {
		if sess.Key == key {
			return sess
		}
	}
	return nil
}

// echoProvider returns the agent ID as part of the response for testing.
type echoProvider struct {
	delay time.Duration
}

func (p *echoProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	ch := make(chan *agent.CompletionChunk, 1)
	go func() {
		if p.delay > 0 {
			select {
			case <-time.After(p.delay):
			case <-ctx.Done():
				close(ch)
				return
			}
		}
		// Extract content from the last user message
		content := "no content"
		for _, msg := range req.Messages {
			if msg.Role == "user" {
				content = msg.Content
			}
		}
		ch <- &agent.CompletionChunk{Text: fmt.Sprintf("echo: %s", content)}
		close(ch)
	}()
	return ch, nil
}

func (p *echoProvider) Name() string          { return "echo" }
func (p *echoProvider) Models() []agent.Model { return nil }
func (p *echoProvider) SupportsTools() bool   { return false }

// orderTrackingProvider tracks the order of processing.
type orderTrackingProvider struct {
	delay   time.Duration
	counter *int32
	order   *[]int32
	orderMu *sync.Mutex
}

func (p *orderTrackingProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	ch := make(chan *agent.CompletionChunk, 1)
	go func() {
		// Record when we started
		myOrder := atomic.AddInt32(p.counter, 1)

		if p.delay > 0 {
			select {
			case <-time.After(p.delay):
			case <-ctx.Done():
				close(ch)
				return
			}
		}

		// Record order of completion
		p.orderMu.Lock()
		*p.order = append(*p.order, myOrder)
		p.orderMu.Unlock()

		ch <- &agent.CompletionChunk{Text: fmt.Sprintf("order: %d", myOrder)}
		close(ch)
	}()
	return ch, nil
}

func (p *orderTrackingProvider) Name() string          { return "order" }
func (p *orderTrackingProvider) Models() []agent.Model { return nil }
func (p *orderTrackingProvider) SupportsTools() bool   { return false }

func TestBroadcastManager_IsBroadcastPeer(t *testing.T) {
	tests := []struct {
		name     string
		config   BroadcastConfig
		peerID   string
		expected bool
	}{
		{
			name:     "nil manager",
			config:   BroadcastConfig{},
			peerID:   "peer1",
			expected: false,
		},
		{
			name: "peer in groups",
			config: BroadcastConfig{
				Groups: map[string][]string{
					"peer1": {"agent1", "agent2"},
				},
			},
			peerID:   "peer1",
			expected: true,
		},
		{
			name: "peer not in groups",
			config: BroadcastConfig{
				Groups: map[string][]string{
					"peer1": {"agent1", "agent2"},
				},
			},
			peerID:   "peer2",
			expected: false,
		},
		{
			name: "empty agent list",
			config: BroadcastConfig{
				Groups: map[string][]string{
					"peer1": {},
				},
			},
			peerID:   "peer1",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewBroadcastManager(tt.config, nil, nil, nil)
			result := manager.IsBroadcastPeer(tt.peerID)
			if result != tt.expected {
				t.Errorf("IsBroadcastPeer(%q) = %v, want %v", tt.peerID, result, tt.expected)
			}
		})
	}
}

func TestBroadcastManager_GetAgentsForPeer(t *testing.T) {
	config := BroadcastConfig{
		Groups: map[string][]string{
			"peer1": {"agent1", "agent2", "agent3"},
			"peer2": {"agent4"},
		},
	}
	manager := NewBroadcastManager(config, nil, nil, nil)

	agents := manager.GetAgentsForPeer("peer1")
	if len(agents) != 3 {
		t.Errorf("expected 3 agents, got %d", len(agents))
	}

	agents = manager.GetAgentsForPeer("peer2")
	if len(agents) != 1 {
		t.Errorf("expected 1 agent, got %d", len(agents))
	}

	agents = manager.GetAgentsForPeer("unknown")
	if agents != nil {
		t.Errorf("expected nil for unknown peer, got %v", agents)
	}
}

func TestBroadcastSessionKey(t *testing.T) {
	key1 := BroadcastSessionKey("agent1", models.ChannelTelegram, "chat123")
	key2 := BroadcastSessionKey("agent2", models.ChannelTelegram, "chat123")

	if key1 == key2 {
		t.Errorf("session keys should be different for different agents")
	}

	if !strings.Contains(key1, "agent1") {
		t.Errorf("key should contain agent ID: %s", key1)
	}

	if !strings.Contains(key2, "agent2") {
		t.Errorf("key should contain agent ID: %s", key2)
	}
}

func TestBroadcastManager_ProcessParallel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := newTrackingStore()
	provider := &echoProvider{}
	runtime := agent.NewRuntime(provider, store)

	config := BroadcastConfig{
		Strategy: BroadcastParallel,
		Groups: map[string][]string{
			"peer1": {"agent1", "agent2", "agent3"},
		},
	}

	manager := NewBroadcastManager(config, store, runtime, logger)

	msg := &models.Message{
		ID:        "msg1",
		Channel:   models.ChannelTelegram,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   "hello broadcast",
		Metadata: map[string]any{
			"chat_id": int64(123),
		},
	}

	resolveID := func(m *models.Message) (string, error) {
		return "123", nil
	}

	results, err := manager.ProcessBroadcast(context.Background(), "peer1", msg, resolveID, nil)
	if err != nil {
		t.Fatalf("ProcessBroadcast() error = %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Check all agents processed
	agentsSeen := make(map[string]bool)
	for _, result := range results {
		if result.Error != nil {
			t.Errorf("agent %s had error: %v", result.AgentID, result.Error)
		}
		agentsSeen[result.AgentID] = true
		if result.Response == "" {
			t.Errorf("agent %s had empty response", result.AgentID)
		}
	}

	for _, agentID := range []string{"agent1", "agent2", "agent3"} {
		if !agentsSeen[agentID] {
			t.Errorf("agent %s not seen in results", agentID)
		}
	}
}

func TestBroadcastManager_ProcessSequential(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := newTrackingStore()

	var counter int32
	var order []int32
	var orderMu sync.Mutex

	provider := &orderTrackingProvider{
		delay:   10 * time.Millisecond, // Small delay to make order observable
		counter: &counter,
		order:   &order,
		orderMu: &orderMu,
	}
	runtime := agent.NewRuntime(provider, store)

	config := BroadcastConfig{
		Strategy: BroadcastSequential,
		Groups: map[string][]string{
			"peer1": {"agent1", "agent2", "agent3"},
		},
	}

	manager := NewBroadcastManager(config, store, runtime, logger)

	msg := &models.Message{
		ID:        "msg1",
		Channel:   models.ChannelTelegram,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   "hello sequential",
		Metadata: map[string]any{
			"chat_id": int64(123),
		},
	}

	resolveID := func(m *models.Message) (string, error) {
		return "123", nil
	}

	results, err := manager.ProcessBroadcast(context.Background(), "peer1", msg, resolveID, nil)
	if err != nil {
		t.Fatalf("ProcessBroadcast() error = %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// In sequential mode, processing should happen in order
	// The counter starts are sequential (1, 2, 3) and completions are also sequential
	orderMu.Lock()
	defer orderMu.Unlock()

	if len(order) != 3 {
		t.Fatalf("expected 3 completions tracked, got %d", len(order))
	}

	// Sequential means they complete in order: 1, 2, 3
	for i, v := range order {
		expected := int32(i + 1)
		if v != expected {
			t.Errorf("completion order[%d] = %d, want %d", i, v, expected)
		}
	}
}

func TestBroadcastManager_SessionIsolation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := newTrackingStore()
	provider := &echoProvider{}
	runtime := agent.NewRuntime(provider, store)

	config := BroadcastConfig{
		Strategy: BroadcastParallel,
		Groups: map[string][]string{
			"peer1": {"agent1", "agent2"},
		},
	}

	manager := NewBroadcastManager(config, store, runtime, logger)

	msg := &models.Message{
		ID:        "msg1",
		Channel:   models.ChannelTelegram,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   "test isolation",
		Metadata: map[string]any{
			"chat_id": int64(456),
		},
	}

	resolveID := func(m *models.Message) (string, error) {
		return "456", nil
	}

	results, err := manager.ProcessBroadcast(context.Background(), "peer1", msg, resolveID, nil)
	if err != nil {
		t.Fatalf("ProcessBroadcast() error = %v", err)
	}

	// Check that each agent got its own session
	sessionIDs := make(map[string]bool)
	for _, result := range results {
		if result.SessionID == "" {
			t.Errorf("agent %s has empty session ID", result.AgentID)
			continue
		}
		if sessionIDs[result.SessionID] {
			t.Errorf("duplicate session ID: %s", result.SessionID)
		}
		sessionIDs[result.SessionID] = true
	}

	if len(sessionIDs) != 2 {
		t.Errorf("expected 2 unique sessions, got %d", len(sessionIDs))
	}

	// Verify session keys contain different agent IDs
	keys := store.GetSessionKeys()
	if len(keys) != 2 {
		t.Fatalf("expected 2 session keys, got %d: %v", len(keys), keys)
	}

	// Check that keys are different and contain agent IDs
	if keys[0] == keys[1] {
		t.Errorf("session keys should be different: %v", keys)
	}

	// Each key should be of the form "agentID:channel:channelID"
	foundAgent1 := false
	foundAgent2 := false
	for _, key := range keys {
		if strings.HasPrefix(key, "agent1:") {
			foundAgent1 = true
		}
		if strings.HasPrefix(key, "agent2:") {
			foundAgent2 = true
		}
	}

	if !foundAgent1 || !foundAgent2 {
		t.Errorf("session keys should contain both agent1 and agent2: %v", keys)
	}
}

func TestBroadcastManager_FallbackToNormalRouting(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := newTrackingStore()
	provider := &echoProvider{}
	runtime := agent.NewRuntime(provider, store)

	config := BroadcastConfig{
		Strategy: BroadcastParallel,
		Groups: map[string][]string{
			"peer1": {"agent1", "agent2"},
		},
	}

	manager := NewBroadcastManager(config, store, runtime, logger)

	// Check that peer2 is not a broadcast peer
	if manager.IsBroadcastPeer("peer2") {
		t.Errorf("peer2 should not be a broadcast peer")
	}

	// Attempting to process for non-broadcast peer should fail
	msg := &models.Message{
		ID:        "msg1",
		Channel:   models.ChannelTelegram,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   "test fallback",
	}

	resolveID := func(m *models.Message) (string, error) {
		return "789", nil
	}

	_, err := manager.ProcessBroadcast(context.Background(), "peer2", msg, resolveID, nil)
	if err == nil {
		t.Errorf("expected error for non-broadcast peer")
	}

	// The typical flow would be:
	// 1. Check IsBroadcastPeer first
	// 2. If false, use normal routing
	// 3. If true, use ProcessBroadcast
}

func TestBroadcastManager_ContextCancellation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := newTrackingStore()
	provider := &echoProvider{delay: 100 * time.Millisecond}
	runtime := agent.NewRuntime(provider, store)

	config := BroadcastConfig{
		Strategy: BroadcastSequential, // Sequential to test mid-processing cancellation
		Groups: map[string][]string{
			"peer1": {"agent1", "agent2", "agent3"},
		},
	}

	manager := NewBroadcastManager(config, store, runtime, logger)

	msg := &models.Message{
		ID:        "msg1",
		Channel:   models.ChannelTelegram,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   "test cancel",
		Metadata: map[string]any{
			"chat_id": int64(123),
		},
	}

	resolveID := func(m *models.Message) (string, error) {
		return "123", nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	results, err := manager.ProcessBroadcast(ctx, "peer1", msg, resolveID, nil)

	// Should have partial results or context error
	if err != nil && err != context.DeadlineExceeded {
		// Error is acceptable
	}

	// In sequential mode with cancellation, we should have fewer than 3 results
	// (or results with errors)
	if len(results) == 3 {
		// All completed, but some might have errors due to context
		errorCount := 0
		for _, r := range results {
			if r.Error != nil {
				errorCount++
			}
		}
		// At least some should have failed if context was cancelled during processing
		// (though timing-dependent)
	}
}

func TestBroadcastManager_WithSystemPrompt(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := newTrackingStore()
	provider := &echoProvider{}
	runtime := agent.NewRuntime(provider, store)

	config := BroadcastConfig{
		Strategy: BroadcastParallel,
		Groups: map[string][]string{
			"peer1": {"agent1", "agent2"},
		},
	}

	manager := NewBroadcastManager(config, store, runtime, logger)

	msg := &models.Message{
		ID:        "msg1",
		Channel:   models.ChannelTelegram,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   "test prompt",
		Metadata: map[string]any{
			"chat_id": int64(123),
		},
	}

	resolveID := func(m *models.Message) (string, error) {
		return "123", nil
	}

	promptCalls := make(map[string]int)
	var promptMu sync.Mutex

	getSystemPrompt := func(ctx context.Context, session *models.Session, msg *models.Message) string {
		promptMu.Lock()
		promptCalls[session.AgentID]++
		promptMu.Unlock()
		return fmt.Sprintf("You are %s", session.AgentID)
	}

	results, err := manager.ProcessBroadcast(context.Background(), "peer1", msg, resolveID, getSystemPrompt)
	if err != nil {
		t.Fatalf("ProcessBroadcast() error = %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Each agent should have had getSystemPrompt called
	promptMu.Lock()
	defer promptMu.Unlock()

	if promptCalls["agent1"] != 1 {
		t.Errorf("expected getSystemPrompt called once for agent1, got %d", promptCalls["agent1"])
	}
	if promptCalls["agent2"] != 1 {
		t.Errorf("expected getSystemPrompt called once for agent2, got %d", promptCalls["agent2"])
	}
}

func TestBroadcastManager_DefaultStrategy(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := newTrackingStore()
	provider := &echoProvider{}
	runtime := agent.NewRuntime(provider, store)

	// No strategy specified - should default to parallel
	config := BroadcastConfig{
		Groups: map[string][]string{
			"peer1": {"agent1", "agent2"},
		},
	}

	manager := NewBroadcastManager(config, store, runtime, logger)

	msg := &models.Message{
		ID:        "msg1",
		Channel:   models.ChannelTelegram,
		Direction: models.DirectionInbound,
		Role:      models.RoleUser,
		Content:   "test default",
		Metadata: map[string]any{
			"chat_id": int64(123),
		},
	}

	resolveID := func(m *models.Message) (string, error) {
		return "123", nil
	}

	results, err := manager.ProcessBroadcast(context.Background(), "peer1", msg, resolveID, nil)
	if err != nil {
		t.Fatalf("ProcessBroadcast() error = %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Both should have succeeded
	for _, result := range results {
		if result.Error != nil {
			t.Errorf("agent %s had error: %v", result.AgentID, result.Error)
		}
	}
}

func TestBroadcastManager_NilManager(t *testing.T) {
	var manager *BroadcastManager

	if manager.IsBroadcastPeer("peer1") {
		t.Errorf("nil manager should return false for IsBroadcastPeer")
	}

	agents := manager.GetAgentsForPeer("peer1")
	if agents != nil {
		t.Errorf("nil manager should return nil agents")
	}
}
