package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/google/uuid"
)

// Registry manages hook registrations and event dispatch.
type Registry struct {
	handlers map[string][]*Registration // eventKey -> handlers
	byID     map[string]*Registration   // id -> registration
	logger   *slog.Logger
	mu       sync.RWMutex
}

// NewRegistry creates a new hook registry.
func NewRegistry(logger *slog.Logger) *Registry {
	if logger == nil {
		logger = slog.Default()
	}
	return &Registry{
		handlers: make(map[string][]*Registration),
		byID:     make(map[string]*Registration),
		logger:   logger.With("component", "hooks"),
	}
}

// Register adds a handler for an event type.
// Returns the registration ID for later unregistration.
func (r *Registry) Register(eventKey string, handler Handler, opts ...RegisterOption) string {
	reg := &Registration{
		ID:       uuid.New().String(),
		EventKey: eventKey,
		Handler:  handler,
		Priority: PriorityNormal,
	}

	for _, opt := range opts {
		opt(reg)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers[eventKey] = append(r.handlers[eventKey], reg)
	r.byID[reg.ID] = reg

	// Sort by priority
	sort.Slice(r.handlers[eventKey], func(i, j int) bool {
		return r.handlers[eventKey][i].Priority < r.handlers[eventKey][j].Priority
	})

	r.logger.Debug("registered hook",
		"id", reg.ID,
		"event_key", eventKey,
		"name", reg.Name,
		"priority", reg.Priority)

	return reg.ID
}

// RegisterOption configures a registration.
type RegisterOption func(*Registration)

// WithPriority sets the handler priority.
func WithPriority(p Priority) RegisterOption {
	return func(r *Registration) {
		r.Priority = p
	}
}

// WithName sets the handler name for debugging.
func WithName(name string) RegisterOption {
	return func(r *Registration) {
		r.Name = name
	}
}

// WithSource sets the handler source (plugin name, etc).
func WithSource(source string) RegisterOption {
	return func(r *Registration) {
		r.Source = source
	}
}

// Unregister removes a handler by its registration ID.
func (r *Registry) Unregister(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	reg, exists := r.byID[id]
	if !exists {
		return false
	}

	delete(r.byID, id)

	handlers := r.handlers[reg.EventKey]
	for i, h := range handlers {
		if h.ID == id {
			r.handlers[reg.EventKey] = append(handlers[:i], handlers[i+1:]...)
			break
		}
	}

	r.logger.Debug("unregistered hook", "id", id, "event_key", reg.EventKey)
	return true
}

// Clear removes all registered handlers.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.handlers = make(map[string][]*Registration)
	r.byID = make(map[string]*Registration)
	r.logger.Debug("cleared all hooks")
}

// Trigger dispatches an event to all matching handlers.
// Handlers are called in priority order. Errors are logged but
// don't prevent other handlers from running.
func (r *Registry) Trigger(ctx context.Context, event *Event) error {
	if event == nil {
		return fmt.Errorf("event is nil")
	}

	r.mu.RLock()
	// Collect handlers for both general type and specific type:action
	typeHandlers := r.handlers[string(event.Type)]
	var specificHandlers []*Registration
	if event.Action != "" {
		specificKey := fmt.Sprintf("%s:%s", event.Type, event.Action)
		specificHandlers = r.handlers[specificKey]
	}
	r.mu.RUnlock()

	// Merge and sort all handlers
	allHandlers := make([]*Registration, 0, len(typeHandlers)+len(specificHandlers))
	allHandlers = append(allHandlers, typeHandlers...)
	allHandlers = append(allHandlers, specificHandlers...)

	sort.Slice(allHandlers, func(i, j int) bool {
		return allHandlers[i].Priority < allHandlers[j].Priority
	})

	if len(allHandlers) == 0 {
		return nil
	}

	var firstErr error
	for _, handler := range allHandlers {
		if err := r.callHandler(ctx, handler, event); err != nil {
			r.logger.Warn("hook handler error",
				"event_type", event.Type,
				"event_action", event.Action,
				"handler_id", handler.ID,
				"handler_name", handler.Name,
				"error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	return firstErr
}

func (r *Registry) callHandler(ctx context.Context, reg *Registration, event *Event) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("hook panic: %v", p)
		}
	}()

	return reg.Handler(ctx, event)
}

// TriggerAsync dispatches an event asynchronously.
// Returns immediately; handlers run in a goroutine.
func (r *Registry) TriggerAsync(ctx context.Context, event *Event) {
	go func() {
		if err := r.Trigger(ctx, event); err != nil {
			r.logger.Warn("async hook trigger error",
				"event_type", event.Type,
				"error", err)
		}
	}()
}

// RegisteredEvents returns all event keys with registered handlers.
func (r *Registry) RegisteredEvents() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	keys := make([]string, 0, len(r.handlers))
	for k := range r.handlers {
		keys = append(keys, k)
	}
	return keys
}

// HandlerCount returns the number of handlers for an event key.
func (r *Registry) HandlerCount(eventKey string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.handlers[eventKey])
}

// GetRegistration returns a registration by ID.
func (r *Registry) GetRegistration(id string) (*Registration, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	reg, ok := r.byID[id]
	return reg, ok
}

// ListRegistrations returns all registrations for an event key.
func (r *Registry) ListRegistrations(eventKey string) []*Registration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	handlers := r.handlers[eventKey]
	result := make([]*Registration, len(handlers))
	copy(result, handlers)
	return result
}
