package routing

import (
	"context"
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
)

// Router selects an LLM provider for each request based on rules and heuristics.
type Router struct {
	defaultProvider string
	providers       map[string]agent.LLMProvider
	rules           []Rule
	preferLocal     bool
	localProviders  map[string]struct{}
	classifier      Classifier
}

// Rule defines a routing rule.
type Rule struct {
	Name   string
	Match  Match
	Target Target
}

// Match defines rule matching conditions.
type Match struct {
	Patterns []string
	Tags     []string
}

// Target defines the destination provider and model.
type Target struct {
	Provider string
	Model    string
}

// Classifier assigns tags to a request.
type Classifier interface {
	Classify(req *agent.CompletionRequest) []string
}

// Config configures a Router.
type Config struct {
	DefaultProvider string
	PreferLocal     bool
	LocalProviders  []string
	Rules           []Rule
	Classifier      Classifier
}

// NewRouter creates a new Router.
func NewRouter(cfg Config, providers map[string]agent.LLMProvider) *Router {
	lp := make(map[string]struct{})
	for _, name := range cfg.LocalProviders {
		if n := normalizeID(name); n != "" {
			lp[n] = struct{}{}
		}
	}

	classifier := cfg.Classifier
	if classifier == nil {
		classifier = &HeuristicClassifier{}
	}

	return &Router{
		defaultProvider: normalizeID(cfg.DefaultProvider),
		providers:       providers,
		rules:           cfg.Rules,
		preferLocal:     cfg.PreferLocal,
		localProviders:  lp,
		classifier:      classifier,
	}
}

// Complete routes the request to the selected provider.
func (r *Router) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	if req == nil {
		return nil, errInvalidRequest("request is nil")
	}
	providerName, model := r.selectProvider(req)
	provider := r.lookupProvider(providerName)
	if provider == nil {
		provider = r.lookupProvider(r.defaultProvider)
	}
	if provider == nil {
		return nil, errInvalidRequest("no providers configured")
	}
	if len(req.Tools) > 0 && !provider.SupportsTools() {
		provider = r.findToolProvider()
	}
	if provider == nil {
		return nil, errInvalidRequest("no tool-capable providers available")
	}

	copyReq := *req
	if copyReq.Model == "" && model != "" {
		copyReq.Model = model
	}
	return provider.Complete(ctx, &copyReq)
}

// Name returns the router name.
func (r *Router) Name() string {
	if r.defaultProvider == "" {
		return "router"
	}
	return "router:" + r.defaultProvider
}

// Models returns a union of available models across providers.
func (r *Router) Models() []agent.Model {
	var models []agent.Model
	seen := make(map[string]struct{})
	for _, provider := range r.providers {
		for _, model := range provider.Models() {
			if _, ok := seen[model.ID]; ok {
				continue
			}
			seen[model.ID] = struct{}{}
			models = append(models, model)
		}
	}
	return models
}

// SupportsTools returns true if any provider supports tools.
func (r *Router) SupportsTools() bool {
	for _, provider := range r.providers {
		if provider.SupportsTools() {
			return true
		}
	}
	return false
}

func (r *Router) selectProvider(req *agent.CompletionRequest) (string, string) {
	tags := r.classifier.Classify(req)

	// Rule matching (first match wins).
	for _, rule := range r.rules {
		if ruleMatches(rule.Match, tags, req) {
			return normalizeID(rule.Target.Provider), rule.Target.Model
		}
	}

	// Prefer local provider if configured and available.
	if r.preferLocal && len(r.localProviders) > 0 && len(req.Tools) == 0 {
		for name := range r.localProviders {
			if r.lookupProvider(name) != nil {
				return name, ""
			}
		}
	}

	return r.defaultProvider, ""
}

func (r *Router) lookupProvider(name string) agent.LLMProvider {
	if name == "" {
		return nil
	}
	if provider, ok := r.providers[normalizeID(name)]; ok {
		return provider
	}
	return nil
}

func (r *Router) findToolProvider() agent.LLMProvider {
	if defaultProvider := r.lookupProvider(r.defaultProvider); defaultProvider != nil && defaultProvider.SupportsTools() {
		return defaultProvider
	}
	for _, provider := range r.providers {
		if provider.SupportsTools() {
			return provider
		}
	}
	return nil
}

func ruleMatches(match Match, tags []string, req *agent.CompletionRequest) bool {
	if len(match.Patterns) == 0 && len(match.Tags) == 0 {
		return false
	}
	content := lastUserContent(req)
	contentLower := strings.ToLower(content)

	if len(match.Patterns) > 0 {
		patternMatch := false
		for _, pattern := range match.Patterns {
			p := strings.ToLower(strings.TrimSpace(pattern))
			if p == "" {
				continue
			}
			if strings.Contains(contentLower, p) {
				patternMatch = true
				break
			}
		}
		if !patternMatch {
			return false
		}
	}

	if len(match.Tags) > 0 {
		for _, tag := range match.Tags {
			if containsTag(tags, tag) {
				return true
			}
		}
		return false
	}

	return true
}

func containsTag(tags []string, tag string) bool {
	needle := strings.ToLower(strings.TrimSpace(tag))
	if needle == "" {
		return false
	}
	for _, t := range tags {
		if strings.EqualFold(t, needle) {
			return true
		}
	}
	return false
}

func lastUserContent(req *agent.CompletionRequest) string {
	if req == nil {
		return ""
	}
	for i := len(req.Messages) - 1; i >= 0; i-- {
		msg := req.Messages[i]
		if msg.Role == "user" {
			return msg.Content
		}
	}
	if len(req.Messages) == 0 {
		return ""
	}
	return req.Messages[len(req.Messages)-1].Content
}

func normalizeID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func errInvalidRequest(msg string) error {
	return fmt.Errorf("routing: %s", msg)
}
