package experiments

import "testing"

func TestResolve(t *testing.T) {
	cfg := Config{
		Experiments: []Experiment{
			{
				ID:         "exp1",
				Status:     "active",
				Allocation: 100,
				Variants: []Variant{
					{ID: "a", Weight: 50, Config: VariantConfig{SystemPrompt: "A"}},
					{ID: "b", Weight: 50, Config: VariantConfig{SystemPrompt: "B"}},
				},
			},
		},
	}
	mgr := NewManager(cfg)
	out := mgr.Resolve("subject-1")
	if len(out.Assignments) != 1 {
		t.Fatalf("expected assignment")
	}
	if out.SystemPrompt == "" {
		t.Fatalf("expected system prompt override")
	}
}
