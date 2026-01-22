package hooks

import (
	"context"
	"log/slog"
	"sync"
)

var (
	globalRegistry *Registry
	globalOnce     sync.Once
)

// Global returns the global hook registry.
// The registry is created lazily on first access.
func Global() *Registry {
	globalOnce.Do(func() {
		globalRegistry = NewRegistry(nil)
	})
	return globalRegistry
}

// SetGlobalRegistry replaces the global registry.
// This should only be called during initialization.
func SetGlobalRegistry(r *Registry) {
	globalRegistry = r
}

// SetGlobalLogger sets the logger for the global registry.
func SetGlobalLogger(logger *slog.Logger) {
	Global().logger = logger.With("component", "hooks")
}

// Register adds a handler to the global registry.
func Register(eventKey string, handler Handler, opts ...RegisterOption) string {
	return Global().Register(eventKey, handler, opts...)
}

// Unregister removes a handler from the global registry.
func Unregister(id string) bool {
	return Global().Unregister(id)
}

// Trigger dispatches an event through the global registry.
func Trigger(ctx context.Context, event *Event) error {
	return Global().Trigger(ctx, event)
}

// TriggerAsync dispatches an event asynchronously through the global registry.
func TriggerAsync(ctx context.Context, event *Event) {
	Global().TriggerAsync(ctx, event)
}

// On is a convenience function to register a handler for an event type.
func On(eventType EventType, handler Handler, opts ...RegisterOption) string {
	return Register(string(eventType), handler, opts...)
}

// OnAction registers a handler for a specific event type and action.
func OnAction(eventType EventType, action string, handler Handler, opts ...RegisterOption) string {
	eventKey := string(eventType) + ":" + action
	return Register(eventKey, handler, opts...)
}

// Emit is a convenience function to trigger an event.
func Emit(ctx context.Context, eventType EventType, action string) error {
	return Trigger(ctx, NewEvent(eventType, action))
}

// EmitAsync is a convenience function to trigger an event asynchronously.
func EmitAsync(ctx context.Context, eventType EventType, action string) {
	TriggerAsync(ctx, NewEvent(eventType, action))
}
