// Package memory provides vector-based semantic memory search.
package memory

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/hooks"
	"github.com/haasonsaas/nexus/pkg/models"
)

// MemoryCategory categorizes captured memories.
type MemoryCategory string

const (
	CategoryPreference MemoryCategory = "preference"
	CategoryFact       MemoryCategory = "fact"
	CategoryDecision   MemoryCategory = "decision"
	CategoryEntity     MemoryCategory = "entity"
	CategoryOther      MemoryCategory = "other"
)

// AutoCaptureConfig configures automatic memory capture.
type AutoCaptureConfig struct {
	// Enabled enables auto-capture of conversation content.
	Enabled bool `yaml:"enabled"`

	// MaxCapturesPerConversation limits captures per agent run (default: 3).
	MaxCapturesPerConversation int `yaml:"max_captures_per_conversation"`

	// MinContentLength is the minimum text length to consider (default: 10).
	MinContentLength int `yaml:"min_content_length"`

	// MaxContentLength is the maximum text length to consider (default: 500).
	MaxContentLength int `yaml:"max_content_length"`

	// DuplicateThreshold is the similarity score above which content is considered duplicate (default: 0.95).
	DuplicateThreshold float32 `yaml:"duplicate_threshold"`

	// DefaultImportance is the importance score for auto-captured memories (default: 0.7).
	DefaultImportance float32 `yaml:"default_importance"`
}

// AutoRecallConfig configures automatic memory recall.
type AutoRecallConfig struct {
	// Enabled enables auto-recall of relevant memories.
	Enabled bool `yaml:"enabled"`

	// MaxResults is the maximum number of memories to inject (default: 3).
	MaxResults int `yaml:"max_results"`

	// MinScore is the minimum similarity score for recall (default: 0.3).
	MinScore float32 `yaml:"min_score"`

	// MinQueryLength is the minimum prompt length to trigger recall (default: 5).
	MinQueryLength int `yaml:"min_query_length"`
}

// MemoryHooks provides auto-capture and auto-recall functionality.
type MemoryHooks struct {
	manager       *Manager
	captureConfig AutoCaptureConfig
	recallConfig  AutoRecallConfig
	logger        *slog.Logger
}

// NewMemoryHooks creates a new MemoryHooks instance.
func NewMemoryHooks(manager *Manager, captureConfig AutoCaptureConfig, recallConfig AutoRecallConfig, logger *slog.Logger) *MemoryHooks {
	if logger == nil {
		logger = slog.Default()
	}

	// Apply defaults
	if captureConfig.MaxCapturesPerConversation == 0 {
		captureConfig.MaxCapturesPerConversation = 3
	}
	if captureConfig.MinContentLength == 0 {
		captureConfig.MinContentLength = 10
	}
	if captureConfig.MaxContentLength == 0 {
		captureConfig.MaxContentLength = 500
	}
	if captureConfig.DuplicateThreshold == 0 {
		captureConfig.DuplicateThreshold = 0.95
	}
	if captureConfig.DefaultImportance == 0 {
		captureConfig.DefaultImportance = 0.7
	}

	if recallConfig.MaxResults == 0 {
		recallConfig.MaxResults = 3
	}
	if recallConfig.MinScore == 0 {
		recallConfig.MinScore = 0.3
	}
	if recallConfig.MinQueryLength == 0 {
		recallConfig.MinQueryLength = 5
	}

	return &MemoryHooks{
		manager:       manager,
		captureConfig: captureConfig,
		recallConfig:  recallConfig,
		logger:        logger.With("component", "memory-hooks"),
	}
}

// Register registers the memory hooks with a hook registry.
func (h *MemoryHooks) Register(registry *hooks.Registry) {
	if h.captureConfig.Enabled {
		registry.Register(
			string(hooks.EventAgentCompleted),
			h.handleAgentCompleted,
			hooks.WithName("memory-auto-capture"),
			hooks.WithSource("memory"),
			hooks.WithPriority(hooks.PriorityLow), // Run after other handlers
		)
		h.logger.Info("registered memory auto-capture hook")
	}

	if h.recallConfig.Enabled {
		registry.Register(
			string(hooks.EventMessageReceived),
			h.handleMessageReceived,
			hooks.WithName("memory-auto-recall"),
			hooks.WithSource("memory"),
			hooks.WithPriority(hooks.PriorityHigh), // Run early to inject context
		)
		h.logger.Info("registered memory auto-recall hook")
	}
}

// handleAgentCompleted processes completed agent runs for auto-capture.
func (h *MemoryHooks) handleAgentCompleted(ctx context.Context, event *hooks.Event) error {
	if h.manager == nil {
		return nil
	}

	// Extract messages from event context
	messages, ok := event.Context["messages"].([]*models.Message)
	if !ok || len(messages) == 0 {
		return nil
	}

	// Check if agent run was successful
	if success, ok := event.Context["success"].(bool); ok && !success {
		return nil
	}

	// Extract capturable content from messages
	var capturable []captureCandidate
	for _, msg := range messages {
		if msg == nil || msg.Content == "" {
			continue
		}

		// Only process user and assistant messages
		if msg.Role != models.RoleUser && msg.Role != models.RoleAssistant {
			continue
		}

		if shouldCapture(msg.Content, h.captureConfig) {
			category := detectCategory(msg.Content)
			capturable = append(capturable, captureCandidate{
				content:  msg.Content,
				category: category,
				role:     string(msg.Role),
			})
		}
	}

	if len(capturable) == 0 {
		return nil
	}

	// Limit captures per conversation
	if len(capturable) > h.captureConfig.MaxCapturesPerConversation {
		capturable = capturable[:h.captureConfig.MaxCapturesPerConversation]
	}

	// Store each candidate (with duplicate detection)
	stored := 0
	for _, candidate := range capturable {
		isDuplicate, err := h.checkDuplicate(ctx, candidate.content, event)
		if err != nil {
			h.logger.Warn("duplicate check failed", "error", err)
			continue
		}
		if isDuplicate {
			h.logger.Debug("skipping duplicate memory", "content", truncate(candidate.content, 50))
			continue
		}

		entry := &models.MemoryEntry{
			ID:        uuid.New().String(),
			SessionID: event.SessionKey,
			ChannelID: event.ChannelID,
			Content:   candidate.content,
			Metadata: models.MemoryMetadata{
				Source: "auto-capture",
				Role:   candidate.role,
				Tags:   []string{string(candidate.category)},
				Extra: map[string]any{
					"category":   string(candidate.category),
					"importance": h.captureConfig.DefaultImportance,
				},
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		if err := h.manager.Index(ctx, []*models.MemoryEntry{entry}); err != nil {
			h.logger.Warn("failed to store memory", "error", err)
			continue
		}
		stored++
	}

	if stored > 0 {
		h.logger.Info("auto-captured memories", "count", stored, "session", event.SessionKey)
	}

	return nil
}

// handleMessageReceived injects relevant memories before agent processing.
func (h *MemoryHooks) handleMessageReceived(ctx context.Context, event *hooks.Event) error {
	if h.manager == nil || event.Message == nil {
		return nil
	}

	content := event.Message.Content
	if len(content) < h.recallConfig.MinQueryLength {
		return nil
	}

	// Search for relevant memories
	var (
		results *models.SearchResponse
		err     error
	)
	if h.manager.config != nil && h.manager.config.Search.Hierarchy.Enabled {
		agentID := ""
		if event.Context != nil {
			agentID, _ = event.Context["agent_id"].(string)
		}
		results, err = h.manager.SearchHierarchical(ctx, &HierarchyRequest{
			Query:     content,
			Limit:     h.recallConfig.MaxResults,
			Threshold: h.recallConfig.MinScore,
			SessionID: event.SessionKey,
			ChannelID: event.ChannelID,
			AgentID:   agentID,
		})
	} else {
		results, err = h.manager.Search(ctx, &models.SearchRequest{
			Query:     content,
			Limit:     h.recallConfig.MaxResults,
			Threshold: h.recallConfig.MinScore,
			Scope:     models.ScopeSession,
			ScopeID:   event.SessionKey,
		})
	}
	if err != nil {
		h.logger.Warn("memory recall failed", "error", err)
		return nil
	}

	if results == nil || len(results.Results) == 0 {
		return nil
	}

	// Build context injection
	var memoryLines []string
	for _, result := range results.Results {
		category := "memory"
		if tags := result.Entry.Metadata.Tags; len(tags) > 0 {
			category = tags[0]
		}
		memoryLines = append(memoryLines, "- ["+category+"] "+result.Entry.Content)
	}

	memoryContext := "<relevant-memories>\nThe following memories may be relevant to this conversation:\n" +
		strings.Join(memoryLines, "\n") + "\n</relevant-memories>"

	// Store injected context in event for downstream handlers
	event.WithContext("memory_context", memoryContext)
	event.WithContext("memory_count", len(results.Results))

	h.logger.Debug("injected memories into context",
		"count", len(results.Results),
		"session", event.SessionKey)

	return nil
}

// checkDuplicate checks if similar content already exists in memory.
func (h *MemoryHooks) checkDuplicate(ctx context.Context, content string, event *hooks.Event) (bool, error) {
	results, err := h.manager.Search(ctx, &models.SearchRequest{
		Query:     content,
		Limit:     1,
		Threshold: h.captureConfig.DuplicateThreshold,
		Scope:     models.ScopeSession,
		ScopeID:   event.SessionKey,
	})
	if err != nil {
		return false, err
	}

	return results != nil && len(results.Results) > 0, nil
}

// captureCandidate represents content that may be captured.
type captureCandidate struct {
	content  string
	category MemoryCategory
	role     string
}

// Memory trigger patterns (inspired by clawdbot)
var memoryTriggers = []*regexp.Regexp{
	// Explicit memory requests
	regexp.MustCompile(`(?i)remember|zapamatuj|pamatuj`),
	// Preferences
	regexp.MustCompile(`(?i)i (like|prefer|hate|love|want|need|always|never)`),
	regexp.MustCompile(`(?i)preferuji|radši|nechci`),
	// Decisions
	regexp.MustCompile(`(?i)(we|i) (decided|will use|are going to)`),
	regexp.MustCompile(`(?i)rozhodli jsme|budeme používat`),
	// Contact info (phone, email)
	regexp.MustCompile(`\+\d{10,}`),
	regexp.MustCompile(`[\w.-]+@[\w.-]+\.\w{2,}`),
	// Personal facts
	regexp.MustCompile(`(?i)my\s+\w+\s+is|is\s+my`),
	regexp.MustCompile(`(?i)můj\s+\w+\s+je|je\s+můj`),
	// Important markers
	regexp.MustCompile(`(?i)important|crucial|key point`),
}

// shouldCapture determines if content should be captured as a memory.
func shouldCapture(text string, cfg AutoCaptureConfig) bool {
	// Length checks
	if len(text) < cfg.MinContentLength || len(text) > cfg.MaxContentLength {
		return false
	}

	// Skip injected context from memory recall (avoid recursion)
	if strings.Contains(text, "<relevant-memories>") {
		return false
	}

	// Skip system-generated content (XML-like tags)
	if strings.HasPrefix(text, "<") && strings.Contains(text, "</") {
		return false
	}

	// Skip agent summary responses (markdown formatted lists)
	if strings.Contains(text, "**") && strings.Contains(text, "\n-") {
		return false
	}

	// Skip emoji-heavy responses (likely agent output)
	emojiCount := countEmojis(text)
	if emojiCount > 3 {
		return false
	}

	// Check for trigger patterns
	for _, pattern := range memoryTriggers {
		if pattern.MatchString(text) {
			return true
		}
	}

	return false
}

// detectCategory determines the category of content.
func detectCategory(text string) MemoryCategory {
	lower := strings.ToLower(text)

	// Preferences
	if regexp.MustCompile(`(?i)prefer|like|love|hate|want|radši`).MatchString(lower) {
		return CategoryPreference
	}

	// Decisions
	if regexp.MustCompile(`(?i)decided|will use|rozhodli|budeme`).MatchString(lower) {
		return CategoryDecision
	}

	// Entities (contacts, names)
	if regexp.MustCompile(`(?i)\+\d{10,}|@[\w.-]+\.\w+|is called|jmenuje se`).MatchString(lower) {
		return CategoryEntity
	}

	// Facts
	if regexp.MustCompile(`(?i)\b(is|are|has|have|je|má|jsou)\b`).MatchString(lower) {
		return CategoryFact
	}

	return CategoryOther
}

// countEmojis counts emoji characters in text.
func countEmojis(text string) int {
	count := 0
	for _, r := range text {
		// Check for common emoji ranges
		if (r >= 0x1F300 && r <= 0x1F9FF) || // Misc Symbols, Emoticons, etc.
			(r >= 0x2600 && r <= 0x26FF) || // Misc Symbols
			(r >= 0x2700 && r <= 0x27BF) { // Dingbats
			count++
		}
	}
	return count
}

// truncate truncates a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
