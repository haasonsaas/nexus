package gateway

import (
	"context"
	"testing"

	"github.com/haasonsaas/nexus/internal/storage"
	"github.com/haasonsaas/nexus/internal/tools/policy"
	"github.com/haasonsaas/nexus/pkg/models"
)

type stubAgentStore struct {
	agents map[string]*models.Agent
}

func (s *stubAgentStore) Create(ctx context.Context, agent *models.Agent) error { return nil }
func (s *stubAgentStore) Get(ctx context.Context, id string) (*models.Agent, error) {
	if agent, ok := s.agents[id]; ok {
		return agent, nil
	}
	return nil, storage.ErrNotFound
}
func (s *stubAgentStore) List(ctx context.Context, userID string, limit, offset int) ([]*models.Agent, int, error) {
	return nil, 0, nil
}
func (s *stubAgentStore) Update(ctx context.Context, agent *models.Agent) error { return nil }
func (s *stubAgentStore) Delete(ctx context.Context, id string) error           { return nil }

var _ storage.AgentStore = (*stubAgentStore)(nil)

func TestToolPolicyForAgentMergesConfigAndTools(t *testing.T) {
	server := &Server{
		stores: storage.StoreSet{
			Agents: &stubAgentStore{
				agents: map[string]*models.Agent{
					"agent-1": {
						ID:    "agent-1",
						Tools: []string{"read"},
						Config: map[string]any{
							"tool_policy": map[string]any{
								"allow": []any{"websearch"},
								"deny":  []any{"exec"},
							},
						},
					},
				},
			},
		},
	}

	toolPolicy := server.toolPolicyForAgent(context.Background(), "agent-1")
	if toolPolicy == nil {
		t.Fatal("expected tool policy")
	}
	resolver := policy.NewResolver()
	if !resolver.IsAllowed(toolPolicy, "read") {
		t.Fatal("expected agent tools to be allowed")
	}
	if !resolver.IsAllowed(toolPolicy, "websearch") {
		t.Fatal("expected config allow to be allowed")
	}
	if resolver.IsAllowed(toolPolicy, "exec") {
		t.Fatal("expected config deny to be enforced")
	}
}

func TestToolPolicyForAgentProviderOverrides(t *testing.T) {
	server := &Server{
		stores: storage.StoreSet{
			Agents: &stubAgentStore{
				agents: map[string]*models.Agent{
					"agent-1": {
						ID: "agent-1",
						Config: map[string]any{
							"tool_policy": map[string]any{
								"by_provider": map[string]any{
									"mcp:github": map[string]any{
										"allow": []any{"mcp:github.search"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	toolPolicy := server.toolPolicyForAgent(context.Background(), "agent-1")
	if toolPolicy == nil {
		t.Fatal("expected tool policy")
	}

	resolver := policy.NewResolver()
	resolver.RegisterMCPServer("github", []string{"search"})
	if !resolver.IsAllowed(toolPolicy, "mcp:github.search") {
		t.Fatal("expected provider-specific allow to be honored")
	}
	if resolver.IsAllowed(toolPolicy, "mcp:github.other") {
		t.Fatal("expected non-allowed provider tool to be denied")
	}
}
