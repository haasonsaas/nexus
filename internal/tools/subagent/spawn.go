// Package subagent provides tools for spawning and managing sub-agents.
package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/tools/policy"
	"github.com/haasonsaas/nexus/pkg/models"
)

// SubAgent represents a spawned sub-agent.
type SubAgent struct {
	ID           string    `json:"id"`
	ParentID     string    `json:"parent_id"`
	SessionID    string    `json:"session_id"`
	Name         string    `json:"name"`
	Task         string    `json:"task"`
	Status       string    `json:"status"` // running, completed, failed, cancelled
	CreatedAt    time.Time `json:"created_at"`
	CompletedAt  time.Time `json:"completed_at,omitempty"`
	Result       string    `json:"result,omitempty"`
	Error        string    `json:"error,omitempty"`
	AllowedTools []string  `json:"allowed_tools,omitempty"`
	DeniedTools  []string  `json:"denied_tools,omitempty"`
}

// Manager manages sub-agent lifecycle.
type Manager struct {
	mu          sync.RWMutex
	agents      map[string]*SubAgent
	runtime     *agent.Runtime
	maxActive   int
	activeCount int64
	announcer   func(ctx context.Context, parentSession string, msg string) error
}

// NewManager creates a new sub-agent manager.
func NewManager(runtime *agent.Runtime, maxActive int) *Manager {
	if maxActive <= 0 {
		maxActive = 5
	}
	return &Manager{
		agents:    make(map[string]*SubAgent),
		runtime:   runtime,
		maxActive: maxActive,
	}
}

// SetAnnouncer sets the function to announce sub-agent spawns.
func (m *Manager) SetAnnouncer(fn func(ctx context.Context, parentSession string, msg string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.announcer = fn
}

// Spawn creates and starts a new sub-agent.
func (m *Manager) Spawn(ctx context.Context, parentID, parentSession, name, task string, allowedTools, deniedTools []string) (*SubAgent, error) {
	// Check concurrency limit
	if atomic.LoadInt64(&m.activeCount) >= int64(m.maxActive) {
		return nil, fmt.Errorf("max active sub-agents reached (%d)", m.maxActive)
	}

	subagent := &SubAgent{
		ID:           uuid.NewString(),
		ParentID:     parentID,
		SessionID:    parentSession + "-" + uuid.NewString()[:8],
		Name:         name,
		Task:         task,
		Status:       "running",
		CreatedAt:    time.Now(),
		AllowedTools: allowedTools,
		DeniedTools:  deniedTools,
	}

	m.mu.Lock()
	m.agents[subagent.ID] = subagent
	announcer := m.announcer
	m.mu.Unlock()

	atomic.AddInt64(&m.activeCount, 1)

	// Announce spawn
	if announcer != nil {
		announcement := fmt.Sprintf("🤖 Spawning sub-agent '%s' to: %s", name, task)
		if err := announcer(ctx, parentSession, announcement); err != nil {
			// Best-effort announcement; ignore errors.
			_ = err
		}
	}

	// Run sub-agent in background
	go m.runSubAgent(context.Background(), subagent)

	return subagent, nil
}

// runSubAgent executes the sub-agent's task.
func (m *Manager) runSubAgent(ctx context.Context, sa *SubAgent) {
	defer atomic.AddInt64(&m.activeCount, -1)

	// Create a mock session for the sub-agent
	session := &models.Session{
		ID:        sa.SessionID,
		AgentID:   sa.ID,
		CreatedAt: sa.CreatedAt,
		UpdatedAt: sa.CreatedAt,
	}

	// Create the task message
	msg := &models.Message{
		ID:        uuid.NewString(),
		SessionID: sa.SessionID,
		Role:      models.RoleUser,
		Content:   sa.Task,
		CreatedAt: time.Now(),
	}

	// Apply per-agent tool policy based on AllowedTools/DeniedTools
	if len(sa.AllowedTools) > 0 || len(sa.DeniedTools) > 0 {
		resolver := policy.NewResolver()
		toolPolicy := &policy.Policy{
			Allow: sa.AllowedTools,
			Deny:  sa.DeniedTools,
		}
		ctx = agent.WithToolPolicy(ctx, resolver, toolPolicy)
	}

	chunks, err := m.runtime.Process(ctx, session, msg)
	if err != nil {
		m.completeSubAgent(sa.ID, "", err.Error())
		return
	}

	var result string
	for chunk := range chunks {
		if chunk.Error != nil {
			m.completeSubAgent(sa.ID, "", chunk.Error.Error())
			return
		}
		if chunk.Text != "" {
			result += chunk.Text
		}
	}

	m.completeSubAgent(sa.ID, result, "")
}

// completeSubAgent marks a sub-agent as completed.
func (m *Manager) completeSubAgent(id, result, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sa, ok := m.agents[id]
	if !ok {
		return
	}

	sa.CompletedAt = time.Now()
	if errMsg != "" {
		sa.Status = "failed"
		sa.Error = errMsg
	} else {
		sa.Status = "completed"
		sa.Result = result
	}
}

// Get returns a sub-agent by ID.
func (m *Manager) Get(id string) (*SubAgent, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	sa, ok := m.agents[id]
	return sa, ok
}

// List returns all sub-agents for a parent.
func (m *Manager) List(parentID string) []*SubAgent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*SubAgent
	for _, sa := range m.agents {
		if sa.ParentID == parentID {
			result = append(result, sa)
		}
	}
	return result
}

// Cancel cancels a running sub-agent.
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	sa, ok := m.agents[id]
	if !ok {
		return fmt.Errorf("sub-agent not found: %s", id)
	}
	if sa.Status != "running" {
		return fmt.Errorf("sub-agent not running: %s", sa.Status)
	}

	sa.Status = "cancelled"
	sa.CompletedAt = time.Now()
	sa.Error = "cancelled by user"
	return nil
}

// ActiveCount returns the number of active sub-agents.
func (m *Manager) ActiveCount() int {
	return int(atomic.LoadInt64(&m.activeCount))
}

// SpawnTool is a tool for spawning sub-agents.
type SpawnTool struct {
	manager *Manager
}

// NewSpawnTool creates a new spawn tool.
func NewSpawnTool(manager *Manager) *SpawnTool {
	return &SpawnTool{manager: manager}
}

// Name returns the tool name.
func (t *SpawnTool) Name() string {
	return "spawn_subagent"
}

// Description returns the tool description.
func (t *SpawnTool) Description() string {
	return "Spawn a sub-agent to work on a specific task. Returns the sub-agent ID for tracking."
}

// Schema returns the tool's input schema.
func (t *SpawnTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{
				"type":        "string",
				"description": "A short name for the sub-agent (e.g., 'researcher', 'coder')",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "The task for the sub-agent to complete",
			},
			"allowed_tools": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Tools the sub-agent is allowed to use (optional, defaults to all)",
			},
			"denied_tools": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Tools the sub-agent is NOT allowed to use (optional)",
			},
		},
		"required": []string{"name", "task"},
	}
}

// Execute spawns a sub-agent.
func (t *SpawnTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		Name         string   `json:"name"`
		Task         string   `json:"task"`
		AllowedTools []string `json:"allowed_tools"`
		DeniedTools  []string `json:"denied_tools"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.Name == "" {
		return "", fmt.Errorf("name is required")
	}
	if params.Task == "" {
		return "", fmt.Errorf("task is required")
	}

	// Get parent context from ctx (session ID, agent ID)
	parentID := ""
	parentSession := ""
	if session := agent.SessionFromContext(ctx); session != nil {
		parentID = session.AgentID
		parentSession = session.ID
	}

	sa, err := t.manager.Spawn(ctx, parentID, parentSession, params.Name, params.Task, params.AllowedTools, params.DeniedTools)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("Sub-agent '%s' spawned with ID: %s\nTask: %s\nUse subagent_status to check progress.", params.Name, sa.ID, params.Task), nil
}

// StatusTool is a tool for checking sub-agent status.
type StatusTool struct {
	manager *Manager
}

// NewStatusTool creates a new status tool.
func NewStatusTool(manager *Manager) *StatusTool {
	return &StatusTool{manager: manager}
}

// Name returns the tool name.
func (t *StatusTool) Name() string {
	return "subagent_status"
}

// Description returns the tool description.
func (t *StatusTool) Description() string {
	return "Check the status of a sub-agent or list all sub-agents."
}

// Schema returns the tool's input schema.
func (t *StatusTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "string",
				"description": "Sub-agent ID to check (optional, omit to list all)",
			},
		},
	}
}

// Execute checks sub-agent status.
func (t *StatusTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.ID != "" {
		sa, ok := t.manager.Get(params.ID)
		if !ok {
			return "", fmt.Errorf("sub-agent not found: %s", params.ID)
		}

		result := fmt.Sprintf("Sub-agent: %s (%s)\nStatus: %s\nTask: %s\n", sa.Name, sa.ID, sa.Status, sa.Task)
		if sa.Status == "completed" {
			result += fmt.Sprintf("Result: %s\n", sa.Result)
		}
		if sa.Status == "failed" {
			result += fmt.Sprintf("Error: %s\n", sa.Error)
		}
		return result, nil
	}

	// List all sub-agents
	parentID := ""
	if session := agent.SessionFromContext(ctx); session != nil {
		parentID = session.AgentID
	}

	agents := t.manager.List(parentID)
	if len(agents) == 0 {
		return "No sub-agents found.", nil
	}

	result := fmt.Sprintf("Active sub-agents: %d/%d\n\n", t.manager.ActiveCount(), t.manager.maxActive)
	for _, sa := range agents {
		result += fmt.Sprintf("- %s (%s): %s - %s\n", sa.Name, sa.ID, sa.Status, truncate(sa.Task, 50))
	}
	return result, nil
}

// CancelTool is a tool for cancelling sub-agents.
type CancelTool struct {
	manager *Manager
}

// NewCancelTool creates a new cancel tool.
func NewCancelTool(manager *Manager) *CancelTool {
	return &CancelTool{manager: manager}
}

// Name returns the tool name.
func (t *CancelTool) Name() string {
	return "subagent_cancel"
}

// Description returns the tool description.
func (t *CancelTool) Description() string {
	return "Cancel a running sub-agent."
}

// Schema returns the tool's input schema.
func (t *CancelTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{
				"type":        "string",
				"description": "Sub-agent ID to cancel",
			},
		},
		"required": []string{"id"},
	}
}

// Execute cancels a sub-agent.
func (t *CancelTool) Execute(ctx context.Context, input json.RawMessage) (string, error) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(input, &params); err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	if params.ID == "" {
		return "", fmt.Errorf("id is required")
	}

	if err := t.manager.Cancel(params.ID); err != nil {
		return "", err
	}

	return fmt.Sprintf("Sub-agent %s cancelled.", params.ID), nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
