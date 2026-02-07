package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/jobs"
	"github.com/haasonsaas/nexus/internal/tools/policy"
	"github.com/haasonsaas/nexus/pkg/models"
)

// ToolRegistry manages available tools with thread-safe registration and lookup.
// Tools are registered by name and can be retrieved for execution during agent conversations.
type ToolRegistry struct {
	mu    sync.RWMutex
	tools map[string]Tool
}

// NewToolRegistry creates a new empty tool registry ready for tool registration.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		tools: make(map[string]Tool),
	}
}

// Register adds a tool to the registry by its name.
// If a tool with the same name already exists, it is replaced.
func (r *ToolRegistry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
}

// Unregister removes a tool from the registry by name.
func (r *ToolRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
}

// Get returns a tool by name and a boolean indicating if it was found.
func (r *ToolRegistry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[name]
	return tool, ok
}

// Tool parameter limits to prevent resource exhaustion
const (
	// MaxToolNameLength is the maximum length of a tool name.
	MaxToolNameLength = 256

	// MaxToolParamsSize is the maximum size of tool parameters JSON (10MB).
	MaxToolParamsSize = 10 << 20
)

// Execute runs a tool by name with the given JSON parameters.
// Returns an error result if the tool is not found or parameters are invalid.
func (r *ToolRegistry) Execute(ctx context.Context, name string, params json.RawMessage) (*ToolResult, error) {
	// Validate tool name
	if len(name) > MaxToolNameLength {
		return &ToolResult{
			Content: fmt.Sprintf("tool name exceeds maximum length of %d characters", MaxToolNameLength),
			IsError: true,
		}, nil
	}

	// Validate params size
	if len(params) > MaxToolParamsSize {
		return &ToolResult{
			Content: fmt.Sprintf("tool parameters exceed maximum size of %d bytes", MaxToolParamsSize),
			IsError: true,
		}, nil
	}

	r.mu.RLock()
	tool, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return &ToolResult{
			Content: "tool not found: " + name,
			IsError: true,
		}, nil
	}
	return tool.Execute(ctx, params)
}

// AsLLMTools returns all registered tools as a slice for passing to LLM providers.
func (r *ToolRegistry) AsLLMTools() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}

func filterToolsByPolicy(resolver *policy.Resolver, toolPolicy *policy.Policy, tools []Tool) []Tool {
	if resolver == nil || toolPolicy == nil {
		return tools
	}
	filtered := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		if resolver.IsAllowed(toolPolicy, tool.Name()) {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func (r *Runtime) emitToolEvent(chunks chan<- *ResponseChunk, event *models.ToolEvent, disable bool) {
	if disable || event == nil {
		return
	}
	chunks <- &ResponseChunk{ToolEvent: event}
}

func (r *Runtime) requiresApproval(opts RuntimeOptions, toolName string, resolver *policy.Resolver) bool {
	return matchesToolPatterns(opts.RequireApproval, toolName, resolver)
}

func (r *Runtime) isAsyncTool(opts RuntimeOptions, toolName string, resolver *policy.Resolver) bool {
	return matchesToolPatterns(opts.AsyncTools, toolName, resolver)
}

func (r *Runtime) runToolJob(tc models.ToolCall, job *jobs.Job, toolExec *ToolExecutor, jobStore jobs.Store) {
	if job == nil || jobStore == nil {
		return
	}
	ctx := context.Background()
	job.Status = jobs.StatusRunning
	job.StartedAt = time.Now()
	if err := jobStore.Update(ctx, job); err != nil {
		r.opts.Logger.Warn(
			"failed to update job status to running",
			"error", err,
			"job_id", job.ID,
			"tool_call_id", tc.ID,
		)
	}

	var result models.ToolResult
	var execErr error
	if toolExec != nil {
		execResults := toolExec.ExecuteConcurrentlyWithOverrides(ctx, []models.ToolCall{tc}, nil, func(call models.ToolCall) ToolExecConfig {
			return r.toolExecOverrides(call.Name)
		})
		if len(execResults) > 0 {
			result = execResults[0].Result
		} else {
			execErr = fmt.Errorf("tool execution failed")
		}
	} else {
		res, err := r.tools.Execute(ctx, tc.Name, tc.Input)
		if err != nil {
			execErr = err
		} else if res != nil {
			result = models.ToolResult{
				ToolCallID: tc.ID,
				Content:    res.Content,
				IsError:    res.IsError,
			}
		}
	}

	if execErr != nil {
		job.Status = jobs.StatusFailed
		job.Error = execErr.Error()
	} else if result.IsError {
		job.Status = jobs.StatusFailed
		job.Error = result.Content
		job.Result = &result
	} else {
		job.Status = jobs.StatusSucceeded
		job.Result = &result
	}
	job.FinishedAt = time.Now()
	if err := jobStore.Update(ctx, job); err != nil {
		r.opts.Logger.Warn(
			"failed to update job status on completion",
			"error", err,
			"job_id", job.ID,
			"status", job.Status,
			"tool_call_id", tc.ID,
		)
	}
}

func normalizeToolName(name string, resolver *policy.Resolver) string {
	if resolver == nil {
		return policy.NormalizeTool(name)
	}
	return resolver.CanonicalName(name)
}

func matchesToolPatterns(patterns []string, toolName string, resolver *policy.Resolver) bool {
	if len(patterns) == 0 {
		return false
	}
	name := normalizeToolName(toolName, resolver)
	for _, pattern := range patterns {
		if matchToolPattern(normalizeToolName(pattern, resolver), name) {
			return true
		}
	}
	return false
}

func matchToolPattern(pattern, toolName string) bool {
	if pattern == "" || toolName == "" {
		return false
	}
	if pattern == "mcp:*" {
		return strings.HasPrefix(toolName, "mcp:")
	}
	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(toolName, prefix)
	}
	return pattern == toolName
}

func guardToolResult(guard ToolResultGuard, toolName string, result models.ToolResult, resolver *policy.Resolver) models.ToolResult {
	return guard.Apply(toolName, result, resolver)
}

func guardToolResults(guard ToolResultGuard, toolCalls []models.ToolCall, results []models.ToolResult, resolver *policy.Resolver) []models.ToolResult {
	if !guard.active() {
		return results
	}
	if len(results) == 0 {
		return results
	}

	namesByID := make(map[string]string, len(toolCalls))
	for _, tc := range toolCalls {
		if tc.ID != "" {
			namesByID[tc.ID] = tc.Name
		}
	}

	guarded := make([]models.ToolResult, len(results))
	for i, res := range results {
		toolName := namesByID[res.ToolCallID]
		if toolName == "" && i < len(toolCalls) {
			toolName = toolCalls[i].Name
		}
		guarded[i] = guardToolResult(guard, toolName, res, resolver)
	}
	return guarded
}

type sessionLock struct {
	mu   sync.Mutex
	refs int
}

func (r *Runtime) lockSession(sessionID string) func() {
	if strings.TrimSpace(sessionID) == "" {
		return func() {}
	}

	r.sessionLocksMu.Lock()
	lock := r.sessionLocks[sessionID]
	if lock == nil {
		lock = &sessionLock{}
		r.sessionLocks[sessionID] = lock
	}
	lock.refs++
	r.sessionLocksMu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		r.sessionLocksMu.Lock()
		lock.refs--
		if lock.refs <= 0 {
			delete(r.sessionLocks, sessionID)
		}
		r.sessionLocksMu.Unlock()
	}
}
