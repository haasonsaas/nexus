package models

import (
	"testing"
)

func TestCatalog_Get(t *testing.T) {
	c := NewCatalog()

	// Get by ID
	model, ok := c.Get("claude-opus-4")
	if !ok {
		t.Fatal("expected to find claude-opus-4")
	}
	if model.Name != "Claude Opus 4" {
		t.Errorf("Name = %s, want Claude Opus 4", model.Name)
	}

	// Get by alias
	model, ok = c.Get("sonnet")
	if !ok {
		t.Fatal("expected to find sonnet alias")
	}
	if model.ID != "claude-3-5-sonnet-latest" {
		t.Errorf("ID = %s, want claude-3-5-sonnet-latest", model.ID)
	}

	// Get unknown
	_, ok = c.Get("unknown-model")
	if ok {
		t.Error("should not find unknown-model")
	}
}

func TestModel_Capabilities(t *testing.T) {
	model := &Model{
		ID:           "test",
		Capabilities: []Capability{CapVision, CapTools, CapStreaming},
	}

	if !model.HasCapability(CapVision) {
		t.Error("should have vision capability")
	}
	if !model.SupportsVision() {
		t.Error("should support vision")
	}
	if !model.SupportsTools() {
		t.Error("should support tools")
	}
	if !model.SupportsStreaming() {
		t.Error("should support streaming")
	}
	if model.HasCapability(CapReasoning) {
		t.Error("should not have reasoning capability")
	}
}

func TestCatalog_List(t *testing.T) {
	c := NewCatalog()

	// List all
	all := c.List(nil)
	if len(all) == 0 {
		t.Error("expected some models")
	}

	// List by provider
	anthropic := c.ListByProvider(ProviderAnthropic)
	for _, m := range anthropic {
		if m.Provider != ProviderAnthropic {
			t.Errorf("expected anthropic provider, got %s", m.Provider)
		}
	}

	// List by capability
	vision := c.ListByCapability(CapVision)
	for _, m := range vision {
		if !m.HasCapability(CapVision) {
			t.Errorf("model %s should have vision capability", m.ID)
		}
	}
}

func TestFilter_Matches(t *testing.T) {
	model := &Model{
		ID:            "test",
		Provider:      ProviderAnthropic,
		Tier:          TierStandard,
		ContextWindow: 200000,
		Capabilities:  []Capability{CapVision, CapTools},
		Deprecated:    false,
	}

	tests := []struct {
		name   string
		filter *Filter
		want   bool
	}{
		{
			name:   "nil filter matches all",
			filter: nil,
			want:   true,
		},
		{
			name:   "empty filter matches all",
			filter: &Filter{},
			want:   true,
		},
		{
			name: "provider match",
			filter: &Filter{
				Providers: []Provider{ProviderAnthropic},
			},
			want: true,
		},
		{
			name: "provider no match",
			filter: &Filter{
				Providers: []Provider{ProviderOpenAI},
			},
			want: false,
		},
		{
			name: "tier match",
			filter: &Filter{
				Tiers: []Tier{TierStandard, TierFast},
			},
			want: true,
		},
		{
			name: "tier no match",
			filter: &Filter{
				Tiers: []Tier{TierFlagship},
			},
			want: false,
		},
		{
			name: "capability match",
			filter: &Filter{
				RequiredCapabilities: []Capability{CapVision, CapTools},
			},
			want: true,
		},
		{
			name: "capability no match",
			filter: &Filter{
				RequiredCapabilities: []Capability{CapVision, CapReasoning},
			},
			want: false,
		},
		{
			name: "context window match",
			filter: &Filter{
				MinContextWindow: 100000,
			},
			want: true,
		},
		{
			name: "context window no match",
			filter: &Filter{
				MinContextWindow: 500000,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.filter.Matches(model)
			if got != tt.want {
				t.Errorf("Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilter_Deprecated(t *testing.T) {
	deprecated := &Model{
		ID:         "old-model",
		Deprecated: true,
	}

	// Default excludes deprecated
	filter := &Filter{}
	if filter.Matches(deprecated) {
		t.Error("should not match deprecated by default")
	}

	// Explicitly include deprecated
	filter = &Filter{IncludeDeprecated: true}
	if !filter.Matches(deprecated) {
		t.Error("should match when IncludeDeprecated is true")
	}
}

func TestDefaultCatalog(t *testing.T) {
	// Test global functions
	model, ok := Get("gpt-4o")
	if !ok {
		t.Fatal("expected to find gpt-4o in default catalog")
	}
	if model.Provider != ProviderOpenAI {
		t.Errorf("provider = %s, want openai", model.Provider)
	}

	// List all
	all := List(nil)
	if len(all) < 5 {
		t.Errorf("expected at least 5 models, got %d", len(all))
	}
}
