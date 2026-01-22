// Package models provides a catalog of LLM models and their capabilities.
package models

import (
	"sort"
	"strings"
	"sync"
)

// Provider identifies an LLM provider.
type Provider string

const (
	ProviderAnthropic Provider = "anthropic"
	ProviderOpenAI    Provider = "openai"
	ProviderGoogle    Provider = "google"
	ProviderMistral   Provider = "mistral"
	ProviderCohere    Provider = "cohere"
	ProviderOllama    Provider = "ollama"
	ProviderAzure     Provider = "azure"
	ProviderBedrock   Provider = "bedrock"
	ProviderVertex    Provider = "vertex"
)

// Capability identifies a model capability.
type Capability string

const (
	CapVision       Capability = "vision"         // Can process images
	CapTools        Capability = "tools"          // Supports function calling
	CapStreaming    Capability = "streaming"      // Supports streaming responses
	CapJSON         Capability = "json"           // Supports JSON mode
	CapCode         Capability = "code"           // Optimized for code
	CapReasoning    Capability = "reasoning"      // Extended reasoning (o1, etc)
	CapAudio        Capability = "audio"          // Can process audio
	CapVideo        Capability = "video"          // Can process video
	CapEmbeddings   Capability = "embeddings"     // Can generate embeddings
	CapFineTunable  Capability = "fine_tunable"   // Can be fine-tuned
	CapPDFInput     Capability = "pdf_input"      // Can process PDFs directly
	CapLongContext  Capability = "long_context"   // 100k+ context window
	CapBatch        Capability = "batch"          // Supports batch API
	CapCaching      Capability = "caching"        // Supports prompt caching
)

// Tier identifies a model's quality/cost tier.
type Tier string

const (
	TierFlagship Tier = "flagship" // Best quality, highest cost
	TierStandard Tier = "standard" // Good balance
	TierFast     Tier = "fast"     // Faster, cheaper
	TierMini     Tier = "mini"     // Smallest/cheapest
)

// Model represents an LLM model with its capabilities and metadata.
type Model struct {
	// ID is the model identifier used in API calls
	ID string `json:"id"`

	// Name is a human-readable name
	Name string `json:"name"`

	// Provider is the LLM provider
	Provider Provider `json:"provider"`

	// Tier is the quality/cost tier
	Tier Tier `json:"tier"`

	// ContextWindow is the maximum context size in tokens
	ContextWindow int `json:"context_window"`

	// MaxOutputTokens is the maximum output size
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`

	// Capabilities lists what the model can do
	Capabilities []Capability `json:"capabilities"`

	// Aliases are alternative names for this model
	Aliases []string `json:"aliases,omitempty"`

	// Deprecated indicates if this model is deprecated
	Deprecated bool `json:"deprecated,omitempty"`

	// ReplacedBy is the recommended replacement for deprecated models
	ReplacedBy string `json:"replaced_by,omitempty"`

	// ReleaseDate is when the model was released (YYYY-MM-DD)
	ReleaseDate string `json:"release_date,omitempty"`

	// Description is a brief description
	Description string `json:"description,omitempty"`

	// InputPrice is the price per million input tokens (USD)
	InputPrice float64 `json:"input_price,omitempty"`

	// OutputPrice is the price per million output tokens (USD)
	OutputPrice float64 `json:"output_price,omitempty"`
}

// HasCapability checks if the model has a specific capability.
func (m *Model) HasCapability(cap Capability) bool {
	for _, c := range m.Capabilities {
		if c == cap {
			return true
		}
	}
	return false
}

// SupportsVision returns true if the model can process images.
func (m *Model) SupportsVision() bool {
	return m.HasCapability(CapVision)
}

// SupportsTools returns true if the model supports function calling.
func (m *Model) SupportsTools() bool {
	return m.HasCapability(CapTools)
}

// SupportsStreaming returns true if the model supports streaming.
func (m *Model) SupportsStreaming() bool {
	return m.HasCapability(CapStreaming)
}

// Catalog manages a collection of models.
type Catalog struct {
	models   map[string]*Model // id -> model
	aliases  map[string]string // alias -> id
	mu       sync.RWMutex
}

// NewCatalog creates a new model catalog.
func NewCatalog() *Catalog {
	c := &Catalog{
		models:  make(map[string]*Model),
		aliases: make(map[string]string),
	}

	// Register built-in models
	c.registerBuiltinModels()

	return c
}

// Register adds a model to the catalog.
func (c *Catalog) Register(model *Model) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.models[model.ID] = model

	// Register aliases
	for _, alias := range model.Aliases {
		c.aliases[strings.ToLower(alias)] = model.ID
	}
}

// Get retrieves a model by ID or alias.
func (c *Catalog) Get(id string) (*Model, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Try direct lookup
	if model, ok := c.models[id]; ok {
		return model, true
	}

	// Try alias lookup
	if realID, ok := c.aliases[strings.ToLower(id)]; ok {
		return c.models[realID], true
	}

	return nil, false
}

// List returns all models, optionally filtered.
func (c *Catalog) List(filter *Filter) []*Model {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var result []*Model
	for _, model := range c.models {
		if filter == nil || filter.Matches(model) {
			result = append(result, model)
		}
	}

	// Sort by provider, then tier, then name
	sort.Slice(result, func(i, j int) bool {
		if result[i].Provider != result[j].Provider {
			return result[i].Provider < result[j].Provider
		}
		if result[i].Tier != result[j].Tier {
			return tierRank(result[i].Tier) < tierRank(result[j].Tier)
		}
		return result[i].Name < result[j].Name
	})

	return result
}

// ListByProvider returns all models for a provider.
func (c *Catalog) ListByProvider(provider Provider) []*Model {
	return c.List(&Filter{Providers: []Provider{provider}})
}

// ListByCapability returns models with a specific capability.
func (c *Catalog) ListByCapability(cap Capability) []*Model {
	return c.List(&Filter{RequiredCapabilities: []Capability{cap}})
}

// Filter for querying models.
type Filter struct {
	// Providers to include
	Providers []Provider

	// Tiers to include
	Tiers []Tier

	// Required capabilities (all must be present)
	RequiredCapabilities []Capability

	// Minimum context window
	MinContextWindow int

	// Include deprecated models
	IncludeDeprecated bool
}

// Matches checks if a model matches the filter.
func (f *Filter) Matches(m *Model) bool {
	if f == nil {
		return true
	}

	// Check provider
	if len(f.Providers) > 0 {
		found := false
		for _, p := range f.Providers {
			if p == m.Provider {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check tier
	if len(f.Tiers) > 0 {
		found := false
		for _, t := range f.Tiers {
			if t == m.Tier {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check capabilities
	for _, cap := range f.RequiredCapabilities {
		if !m.HasCapability(cap) {
			return false
		}
	}

	// Check context window
	if f.MinContextWindow > 0 && m.ContextWindow < f.MinContextWindow {
		return false
	}

	// Check deprecated
	if !f.IncludeDeprecated && m.Deprecated {
		return false
	}

	return true
}

func tierRank(t Tier) int {
	switch t {
	case TierFlagship:
		return 0
	case TierStandard:
		return 1
	case TierFast:
		return 2
	case TierMini:
		return 3
	default:
		return 4
	}
}

func (c *Catalog) registerBuiltinModels() {
	// Anthropic models
	c.Register(&Model{
		ID:            "claude-opus-4",
		Name:          "Claude Opus 4",
		Provider:      ProviderAnthropic,
		Tier:          TierFlagship,
		ContextWindow: 200000,
		MaxOutputTokens: 32000,
		Capabilities: []Capability{
			CapVision, CapTools, CapStreaming, CapJSON, CapCode,
			CapLongContext, CapCaching, CapPDFInput,
		},
		Aliases:     []string{"claude-opus-4-5-20251101", "opus"},
		ReleaseDate: "2025-11-01",
		InputPrice:  15.0,
		OutputPrice: 75.0,
	})

	c.Register(&Model{
		ID:            "claude-3-5-sonnet-latest",
		Name:          "Claude 3.5 Sonnet",
		Provider:      ProviderAnthropic,
		Tier:          TierStandard,
		ContextWindow: 200000,
		MaxOutputTokens: 8192,
		Capabilities: []Capability{
			CapVision, CapTools, CapStreaming, CapJSON, CapCode,
			CapLongContext, CapCaching, CapPDFInput,
		},
		Aliases:     []string{"claude-3-5-sonnet", "sonnet"},
		ReleaseDate: "2024-10-22",
		InputPrice:  3.0,
		OutputPrice: 15.0,
	})

	c.Register(&Model{
		ID:            "claude-3-5-haiku-latest",
		Name:          "Claude 3.5 Haiku",
		Provider:      ProviderAnthropic,
		Tier:          TierFast,
		ContextWindow: 200000,
		MaxOutputTokens: 8192,
		Capabilities: []Capability{
			CapVision, CapTools, CapStreaming, CapJSON, CapCode,
			CapLongContext, CapCaching,
		},
		Aliases:     []string{"claude-3-5-haiku", "haiku"},
		ReleaseDate: "2024-11-04",
		InputPrice:  0.8,
		OutputPrice: 4.0,
	})

	// OpenAI models
	c.Register(&Model{
		ID:            "gpt-4o",
		Name:          "GPT-4o",
		Provider:      ProviderOpenAI,
		Tier:          TierStandard,
		ContextWindow: 128000,
		MaxOutputTokens: 16384,
		Capabilities: []Capability{
			CapVision, CapTools, CapStreaming, CapJSON, CapCode,
			CapLongContext, CapAudio,
		},
		Aliases:     []string{"gpt-4o-2024-11-20"},
		ReleaseDate: "2024-05-13",
		InputPrice:  2.5,
		OutputPrice: 10.0,
	})

	c.Register(&Model{
		ID:            "gpt-4o-mini",
		Name:          "GPT-4o Mini",
		Provider:      ProviderOpenAI,
		Tier:          TierFast,
		ContextWindow: 128000,
		MaxOutputTokens: 16384,
		Capabilities: []Capability{
			CapVision, CapTools, CapStreaming, CapJSON, CapCode,
			CapLongContext,
		},
		Aliases:     []string{"gpt-4o-mini-2024-07-18"},
		ReleaseDate: "2024-07-18",
		InputPrice:  0.15,
		OutputPrice: 0.6,
	})

	c.Register(&Model{
		ID:            "o1",
		Name:          "o1",
		Provider:      ProviderOpenAI,
		Tier:          TierFlagship,
		ContextWindow: 200000,
		MaxOutputTokens: 100000,
		Capabilities: []Capability{
			CapVision, CapTools, CapReasoning, CapJSON, CapCode,
			CapLongContext,
		},
		Aliases:     []string{"o1-2024-12-17"},
		ReleaseDate: "2024-12-17",
		InputPrice:  15.0,
		OutputPrice: 60.0,
	})

	c.Register(&Model{
		ID:            "o3-mini",
		Name:          "o3-mini",
		Provider:      ProviderOpenAI,
		Tier:          TierStandard,
		ContextWindow: 200000,
		MaxOutputTokens: 100000,
		Capabilities: []Capability{
			CapTools, CapReasoning, CapJSON, CapCode, CapLongContext,
		},
		Aliases:     []string{"o3-mini-2025-01-31"},
		ReleaseDate: "2025-01-31",
		InputPrice:  1.1,
		OutputPrice: 4.4,
	})

	// Google models
	c.Register(&Model{
		ID:            "gemini-2.0-flash-exp",
		Name:          "Gemini 2.0 Flash",
		Provider:      ProviderGoogle,
		Tier:          TierFast,
		ContextWindow: 1048576,
		MaxOutputTokens: 8192,
		Capabilities: []Capability{
			CapVision, CapTools, CapStreaming, CapJSON, CapCode,
			CapLongContext, CapAudio, CapVideo,
		},
		Aliases:     []string{"gemini-2.0-flash"},
		ReleaseDate: "2024-12-11",
		InputPrice:  0.0,
		OutputPrice: 0.0,
	})

	c.Register(&Model{
		ID:            "gemini-1.5-pro-latest",
		Name:          "Gemini 1.5 Pro",
		Provider:      ProviderGoogle,
		Tier:          TierStandard,
		ContextWindow: 2097152,
		MaxOutputTokens: 8192,
		Capabilities: []Capability{
			CapVision, CapTools, CapStreaming, CapJSON, CapCode,
			CapLongContext, CapAudio, CapVideo,
		},
		Aliases:     []string{"gemini-1.5-pro"},
		ReleaseDate: "2024-05-14",
		InputPrice:  1.25,
		OutputPrice: 5.0,
	})
}

// DefaultCatalog is the global model catalog.
var DefaultCatalog = NewCatalog()

// Get retrieves a model from the default catalog.
func Get(id string) (*Model, bool) {
	return DefaultCatalog.Get(id)
}

// List returns models from the default catalog.
func List(filter *Filter) []*Model {
	return DefaultCatalog.List(filter)
}

// ListByProvider returns models from the default catalog for a provider.
func ListByProvider(provider Provider) []*Model {
	return DefaultCatalog.ListByProvider(provider)
}

// ListByCapability returns models from the default catalog with a capability.
func ListByCapability(cap Capability) []*Model {
	return DefaultCatalog.ListByCapability(cap)
}
