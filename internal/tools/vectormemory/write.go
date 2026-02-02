package vectormemory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/memory"
	"github.com/haasonsaas/nexus/pkg/models"
)

// Indexer defines the subset of memory manager behavior used by the write tool.
type Indexer interface {
	Index(ctx context.Context, entries []*models.MemoryEntry) error
}

// WriteTool writes entries into vector memory.
type WriteTool struct {
	manager Indexer
	config  *memory.Config
}

// NewWriteTool creates a new vector memory write tool.
func NewWriteTool(manager Indexer, cfg *memory.Config) *WriteTool {
	return &WriteTool{
		manager: manager,
		config:  cfg,
	}
}

// Name returns the tool name.
func (t *WriteTool) Name() string {
	return "vector_memory_write"
}

// Description describes the tool.
func (t *WriteTool) Description() string {
	return "Stores a memory entry in vector memory with a specified scope and tags."
}

// Schema defines the tool parameters.
func (t *WriteTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "content": {"type": "string", "description": "Memory content to store"},
    "scope": {
      "type": "string",
      "enum": ["session", "channel", "agent", "global"],
      "description": "Scope to store the memory in (default: session)"
    },
    "scope_id": {"type": "string", "description": "Scope identifier if required"},
    "tags": {"type": "array", "items": {"type": "string"}, "description": "Optional tags for categorization"},
    "source": {"type": "string", "description": "Source label for the memory"},
    "metadata": {"type": "object", "description": "Additional metadata to store with the memory"}
  },
  "required": ["content"]
}`)
}

type writeInput struct {
	Content  string         `json:"content"`
	Scope    string         `json:"scope"`
	ScopeID  string         `json:"scope_id"`
	Tags     []string       `json:"tags"`
	Source   string         `json:"source"`
	Metadata map[string]any `json:"metadata"`
}

// Execute runs the vector memory write tool.
func (t *WriteTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	if t.manager == nil {
		return &agent.ToolResult{Content: "vector memory is unavailable", IsError: true}, nil
	}

	var input writeInput
	if err := json.Unmarshal(params, &input); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}, nil
	}

	content := strings.TrimSpace(input.Content)
	if content == "" {
		return &agent.ToolResult{Content: "content is required", IsError: true}, nil
	}

	scope := strings.ToLower(strings.TrimSpace(input.Scope))
	if scope == "" {
		scope = defaultScopeFromConfig(t.config)
	}
	if scope == "" {
		scope = "session"
	}

	session := agent.SessionFromContext(ctx)
	scopeID := strings.TrimSpace(input.ScopeID)
	switch scope {
	case "session":
		if scopeID == "" && session != nil {
			scopeID = session.ID
		}
		if scopeID == "" {
			return &agent.ToolResult{Content: "scope_id is required for session scope", IsError: true}, nil
		}
	case "channel":
		if scopeID == "" && session != nil {
			scopeID = session.ChannelID
		}
		if scopeID == "" {
			return &agent.ToolResult{Content: "scope_id is required for channel scope", IsError: true}, nil
		}
	case "agent":
		if scopeID == "" && session != nil {
			scopeID = session.AgentID
		}
		if scopeID == "" {
			return &agent.ToolResult{Content: "scope_id is required for agent scope", IsError: true}, nil
		}
	case "global":
		scopeID = ""
	default:
		return &agent.ToolResult{Content: fmt.Sprintf("unsupported scope %q", scope), IsError: true}, nil
	}

	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = "manual"
	}

	metadata := models.MemoryMetadata{
		Source: source,
		Role:   string(models.RoleAssistant),
		Tags:   normalizeTags(input.Tags),
		Extra:  map[string]any{},
	}
	if session != nil {
		metadata.Extra["source_session_id"] = session.ID
		metadata.Extra["source_agent_id"] = session.AgentID
		metadata.Extra["source_channel_id"] = session.ChannelID
	}
	if len(input.Metadata) > 0 {
		for k, v := range input.Metadata {
			metadata.Extra[k] = v
		}
	}

	entry := &models.MemoryEntry{
		ID:        uuid.New().String(),
		Content:   content,
		Metadata:  metadata,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	switch scope {
	case "session":
		entry.SessionID = scopeID
	case "channel":
		entry.ChannelID = scopeID
	case "agent":
		entry.AgentID = scopeID
	}

	if err := t.manager.Index(ctx, []*models.MemoryEntry{entry}); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("failed to write memory: %v", err), IsError: true}, nil
	}

	payload, err := json.MarshalIndent(struct {
		ID        string    `json:"id"`
		Scope     string    `json:"scope"`
		ScopeID   string    `json:"scope_id,omitempty"`
		CreatedAt time.Time `json:"created_at"`
	}{
		ID:        entry.ID,
		Scope:     scope,
		ScopeID:   scopeID,
		CreatedAt: entry.CreatedAt,
	}, "", "  ")
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("failed to encode response: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{Content: string(payload)}, nil
}

func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	return out
}
