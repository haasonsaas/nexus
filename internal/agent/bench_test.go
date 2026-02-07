package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	agentctx "github.com/haasonsaas/nexus/internal/agent/context"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/internal/tools/policy"
	"github.com/haasonsaas/nexus/pkg/models"
)

// BenchmarkToolRegistryGet measures tool lookup performance.
func BenchmarkToolRegistryGet(b *testing.B) {
	reg := NewToolRegistry()
	for i := 0; i < 50; i++ {
		reg.Register(&benchTool{name: fmt.Sprintf("tool_%d", i)})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg.Get("tool_25")
	}
}

// BenchmarkToolRegistryGetParallel measures concurrent tool lookup.
func BenchmarkToolRegistryGetParallel(b *testing.B) {
	reg := NewToolRegistry()
	for i := 0; i < 50; i++ {
		reg.Register(&benchTool{name: fmt.Sprintf("tool_%d", i)})
	}
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			reg.Get("tool_25")
		}
	})
}

// BenchmarkToolRegistryExecute measures tool execution overhead.
func BenchmarkToolRegistryExecute(b *testing.B) {
	reg := NewToolRegistry()
	reg.Register(&benchTool{name: "bench"})
	params := json.RawMessage(`{"key":"value"}`)
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg.Execute(ctx, "bench", params)
	}
}

// BenchmarkToolRegistryAsLLMTools measures tool list construction.
func BenchmarkToolRegistryAsLLMTools(b *testing.B) {
	reg := NewToolRegistry()
	for i := 0; i < 50; i++ {
		reg.Register(&benchTool{name: fmt.Sprintf("tool_%d", i)})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg.AsLLMTools()
	}
}

// BenchmarkMatchToolPattern measures pattern matching for tool policies.
func BenchmarkMatchToolPattern(b *testing.B) {
	b.Run("exact", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			matchToolPattern("websearch", "websearch")
		}
	})
	b.Run("wildcard", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			matchToolPattern("mcp:*", "mcp:github.search")
		}
	})
	b.Run("prefix", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			matchToolPattern("sandbox.*", "sandbox.exec")
		}
	})
	b.Run("mismatch", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			matchToolPattern("websearch", "sandbox")
		}
	})
}

// BenchmarkMatchesToolPatterns measures multi-pattern matching.
func BenchmarkMatchesToolPatterns(b *testing.B) {
	patterns := []string{"websearch", "sandbox.*", "mcp:*", "browser", "exec"}
	b.Run("hit_first", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			matchesToolPatterns(patterns, "websearch", nil)
		}
	})
	b.Run("hit_last", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			matchesToolPatterns(patterns, "exec", nil)
		}
	})
	b.Run("miss", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			matchesToolPatterns(patterns, "calculator", nil)
		}
	})
}

// BenchmarkFilterToolsByPolicy measures tool filtering with policy.
func BenchmarkFilterToolsByPolicy(b *testing.B) {
	tools := make([]Tool, 20)
	for i := range tools {
		tools[i] = &benchTool{name: fmt.Sprintf("tool_%d", i)}
	}
	resolver := policy.NewResolver()
	pol := &policy.Policy{
		Profile: policy.ProfileFull,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		filterToolsByPolicy(resolver, pol, tools)
	}
}

// BenchmarkBuildCompletionMessages measures message conversion.
func BenchmarkBuildCompletionMessages(b *testing.B) {
	rt := NewRuntime(stubProvider{}, sessions.NewMemoryStore())
	history := make([]*models.Message, 0, 60)
	for i := 0; i < 60; i++ {
		role := models.RoleUser
		if i%2 == 1 {
			role = models.RoleAssistant
		}
		history = append(history, &models.Message{
			ID:      fmt.Sprintf("msg-%d", i),
			Role:    role,
			Content: fmt.Sprintf("Message content %d with some text to process", i),
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rt.buildCompletionMessages(history)
	}
}

// BenchmarkBuildCompletionMessagesWithToolCalls measures message conversion with tool calls.
func BenchmarkBuildCompletionMessagesWithToolCalls(b *testing.B) {
	rt := NewRuntime(stubProvider{}, sessions.NewMemoryStore())
	history := make([]*models.Message, 0, 30)
	for i := 0; i < 10; i++ {
		history = append(history, &models.Message{
			ID:      fmt.Sprintf("user-%d", i),
			Role:    models.RoleUser,
			Content: "What is the weather?",
		})
		history = append(history, &models.Message{
			ID:   fmt.Sprintf("asst-%d", i),
			Role: models.RoleAssistant,
			ToolCalls: []models.ToolCall{
				{ID: fmt.Sprintf("tc-%d", i), Name: "websearch", Input: json.RawMessage(`{"q":"weather"}`)},
			},
		})
		history = append(history, &models.Message{
			ID:   fmt.Sprintf("tool-%d", i),
			Role: models.RoleTool,
			ToolResults: []models.ToolResult{
				{ToolCallID: fmt.Sprintf("tc-%d", i), Content: "Sunny, 72F"},
			},
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rt.buildCompletionMessages(history)
	}
}

// BenchmarkContextPack measures context packing performance.
func BenchmarkContextPack(b *testing.B) {
	packer := agentctx.NewPacker(agentctx.DefaultPackOptions())
	history := make([]*models.Message, 100)
	for i := range history {
		role := models.RoleUser
		if i%2 == 1 {
			role = models.RoleAssistant
		}
		history[i] = &models.Message{
			ID:      fmt.Sprintf("msg-%d", i),
			Role:    role,
			Content: fmt.Sprintf("This is message number %d with enough content to be realistic for testing context packing performance.", i),
		}
	}
	incoming := &models.Message{
		ID:      "incoming",
		Role:    models.RoleUser,
		Content: "What should I do next?",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		packer.Pack(history, incoming, nil)
	}
}

// BenchmarkContextPackWithSummary measures packing with summary message.
func BenchmarkContextPackWithSummary(b *testing.B) {
	packer := agentctx.NewPacker(agentctx.DefaultPackOptions())
	history := make([]*models.Message, 100)
	for i := range history {
		role := models.RoleUser
		if i%2 == 1 {
			role = models.RoleAssistant
		}
		history[i] = &models.Message{
			ID:      fmt.Sprintf("msg-%d", i),
			Role:    role,
			Content: fmt.Sprintf("This is message number %d with enough content to be realistic.", i),
		}
	}
	incoming := &models.Message{
		ID:      "incoming",
		Role:    models.RoleUser,
		Content: "What next?",
	}
	summary := &models.Message{
		ID:      "summary",
		Role:    models.RoleAssistant,
		Content: "Previously, we discussed project architecture and decided on a microservices approach with Go backends.",
		Metadata: map[string]any{
			agentctx.SummaryMetadataKey: "true",
		},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		packer.Pack(history, incoming, summary)
	}
}

// BenchmarkSessionLock measures session lock acquisition/release.
func BenchmarkSessionLock(b *testing.B) {
	rt := NewRuntime(stubProvider{}, sessions.NewMemoryStore())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		unlock := rt.lockSession("session-1")
		unlock()
	}
}

// BenchmarkSessionLockParallel measures concurrent session lock contention across different sessions.
func BenchmarkSessionLockParallel(b *testing.B) {
	rt := NewRuntime(stubProvider{}, sessions.NewMemoryStore())
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			// Different sessions = no contention
			unlock := rt.lockSession(fmt.Sprintf("session-%d", i%100))
			unlock()
			i++
		}
	})
}

// benchTool is a minimal tool implementation for benchmarks.
type benchTool struct {
	name string
}

func (t *benchTool) Name() string             { return t.name }
func (t *benchTool) Description() string       { return "bench tool" }
func (t *benchTool) Schema() json.RawMessage   { return json.RawMessage(`{"type":"object"}`) }
func (t *benchTool) Execute(_ context.Context, _ json.RawMessage) (*ToolResult, error) {
	return &ToolResult{Content: "ok"}, nil
}
