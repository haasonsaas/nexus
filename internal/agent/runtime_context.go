package agent

import (
	"context"
	"strings"

	"github.com/haasonsaas/nexus/internal/tools/policy"
	"github.com/haasonsaas/nexus/pkg/models"
)

type systemPromptKey struct{}
type chunksChanKey struct{}
type sessionKey struct{}
type runtimeOptsKey struct{}
type elevatedKey struct{}
type modelKey struct{}

const contextPruningCacheTouchKey = "context_pruning_cache_ttl_at"

// WithSession stores a session in the context.
func WithSession(ctx context.Context, session *models.Session) context.Context {
	if session == nil {
		return ctx
	}
	return context.WithValue(ctx, sessionKey{}, session)
}

// SessionFromContext retrieves the session from context.
func SessionFromContext(ctx context.Context) *models.Session {
	session, ok := ctx.Value(sessionKey{}).(*models.Session)
	if !ok {
		return nil
	}
	return session
}

// WithRuntimeOptions stores per-request runtime option overrides in the context.
func WithRuntimeOptions(ctx context.Context, opts RuntimeOptions) context.Context {
	return context.WithValue(ctx, runtimeOptsKey{}, opts)
}

func runtimeOptionsFromContext(ctx context.Context) (RuntimeOptions, bool) {
	opts, ok := ctx.Value(runtimeOptsKey{}).(RuntimeOptions)
	return opts, ok
}

// ElevatedMode controls elevated execution semantics for a request.
type ElevatedMode string

const (
	ElevatedOff  ElevatedMode = "off"
	ElevatedAsk  ElevatedMode = "ask"
	ElevatedFull ElevatedMode = "full"
)

// MaxResponseTextSize is the maximum size of accumulated response text (1MB).
// This prevents memory exhaustion from malicious or buggy model responses.
const MaxResponseTextSize = 1 << 20 // 1MB

// MaxToolCallsPerIteration is the maximum number of tool calls allowed in a single iteration.
// This prevents DOS attacks where the model returns excessive tool calls.
const MaxToolCallsPerIteration = 100

// ParseElevatedMode normalizes a user-facing directive to an ElevatedMode.
func ParseElevatedMode(value string) (ElevatedMode, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "on", "ask":
		return ElevatedAsk, true
	case "full":
		return ElevatedFull, true
	case "off":
		return ElevatedOff, true
	default:
		return ElevatedOff, false
	}
}

// WithElevated stores an elevated mode override in the context.
func WithElevated(ctx context.Context, mode ElevatedMode) context.Context {
	return context.WithValue(ctx, elevatedKey{}, mode)
}

// ElevatedFromContext retrieves the elevated mode from context (default: off).
func ElevatedFromContext(ctx context.Context) ElevatedMode {
	mode, ok := ctx.Value(elevatedKey{}).(ElevatedMode)
	if !ok {
		return ElevatedOff
	}
	return mode
}

// WithSystemPrompt stores a request-scoped system prompt override in the context.
func WithSystemPrompt(ctx context.Context, prompt string) context.Context {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ctx
	}
	return context.WithValue(ctx, systemPromptKey{}, prompt)
}

func systemPromptFromContext(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(systemPromptKey{}).(string)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}

// WithModel stores a request-scoped model override in the context.
func WithModel(ctx context.Context, model string) context.Context {
	model = strings.TrimSpace(model)
	if model == "" {
		return ctx
	}
	return context.WithValue(ctx, modelKey{}, model)
}

func modelFromContext(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(modelKey{}).(string)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}

type toolPolicyKey struct{}
type toolResolverKey struct{}

// WithToolPolicy stores a tool policy override in the context.
func WithToolPolicy(ctx context.Context, resolver *policy.Resolver, toolPolicy *policy.Policy) context.Context {
	if resolver == nil || toolPolicy == nil {
		return ctx
	}
	ctx = context.WithValue(ctx, toolResolverKey{}, resolver)
	return context.WithValue(ctx, toolPolicyKey{}, toolPolicy)
}

func toolPolicyFromContext(ctx context.Context) (*policy.Resolver, *policy.Policy, bool) {
	resolver, ok := ctx.Value(toolResolverKey{}).(*policy.Resolver)
	if !ok || resolver == nil {
		return nil, nil, false
	}
	pol, ok := ctx.Value(toolPolicyKey{}).(*policy.Policy)
	if !ok || pol == nil {
		return nil, nil, false
	}
	return resolver, pol, true
}
