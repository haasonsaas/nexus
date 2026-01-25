package routing

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/haasonsaas/nexus/internal/agent"
)

type stubProvider struct {
	name          string
	supportsTools bool
	calls         int
	lastModel     string
}

type dummyTool struct{}

func (dummyTool) Name() string            { return "dummy" }
func (dummyTool) Description() string     { return "dummy tool" }
func (dummyTool) Schema() json.RawMessage { return nil }
func (dummyTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	return &agent.ToolResult{}, nil
}

func (p *stubProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	p.calls++
	p.lastModel = req.Model
	ch := make(chan *agent.CompletionChunk, 1)
	ch <- &agent.CompletionChunk{Done: true}
	close(ch)
	return ch, nil
}

func (p *stubProvider) Name() string {
	return p.name
}

func (p *stubProvider) Models() []agent.Model {
	return nil
}

func (p *stubProvider) SupportsTools() bool {
	return p.supportsTools
}

func TestRouterRuleMatch(t *testing.T) {
	fast := &stubProvider{name: "fast"}
	code := &stubProvider{name: "code"}
	providers := map[string]agent.LLMProvider{
		"fast": fast,
		"code": code,
	}

	router := NewRouter(Config{
		DefaultProvider: "fast",
		Rules: []Rule{{
			Name:  "code",
			Match: Match{Tags: []string{"code"}},
			Target: Target{
				Provider: "code",
				Model:    "gpt-4o",
			},
		}},
		Classifier: &HeuristicClassifier{},
	}, providers)

	req := &agent.CompletionRequest{
		Messages: []agent.CompletionMessage{{Role: "user", Content: "Write a Go function: func main() {}"}},
	}
	_, err := router.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if code.calls != 1 {
		t.Fatalf("expected code provider to be called")
	}
	if code.lastModel != "gpt-4o" {
		t.Fatalf("expected model override, got %q", code.lastModel)
	}
}

func TestRouterPreferLocal(t *testing.T) {
	local := &stubProvider{name: "ollama"}
	defaultP := &stubProvider{name: "anthropic"}
	providers := map[string]agent.LLMProvider{
		"ollama":    local,
		"anthropic": defaultP,
	}

	router := NewRouter(Config{
		DefaultProvider: "anthropic",
		PreferLocal:     true,
		LocalProviders:  []string{"ollama"},
	}, providers)

	req := &agent.CompletionRequest{
		Messages: []agent.CompletionMessage{{Role: "user", Content: "hello"}},
	}
	_, err := router.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if local.calls != 1 {
		t.Fatalf("expected local provider to be called")
	}
}

func TestRouterToolFallback(t *testing.T) {
	noTools := &stubProvider{name: "ollama", supportsTools: false}
	withTools := &stubProvider{name: "openai", supportsTools: true}
	providers := map[string]agent.LLMProvider{
		"ollama": noTools,
		"openai": withTools,
	}

	router := NewRouter(Config{
		DefaultProvider: "ollama",
	}, providers)

	req := &agent.CompletionRequest{
		Messages: []agent.CompletionMessage{{Role: "user", Content: "use tool"}},
		Tools:    []agent.Tool{dummyTool{}},
	}
	_, err := router.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if withTools.calls != 1 {
		t.Fatalf("expected tool-capable provider to be called")
	}
}
