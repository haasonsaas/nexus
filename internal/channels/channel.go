package channels

import (
	"context"

	"github.com/yourorg/nexus/pkg/models"
)

// Adapter is the interface that all channel adapters must implement.
type Adapter interface {
	// Start begins listening for messages from the channel.
	Start(ctx context.Context) error

	// Stop gracefully shuts down the adapter.
	Stop(ctx context.Context) error

	// Send delivers a message to the channel.
	Send(ctx context.Context, msg *models.Message) error

	// Messages returns a channel of inbound messages.
	Messages() <-chan *models.Message

	// Type returns the channel type.
	Type() models.ChannelType

	// Status returns the current connection status.
	Status() Status
}

// Status represents the connection status of a channel.
type Status struct {
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
	LastPing  int64  `json:"last_ping,omitempty"` // Unix timestamp
}

// Registry manages multiple channel adapters.
type Registry struct {
	adapters map[models.ChannelType]Adapter
}

// NewRegistry creates a new channel registry.
func NewRegistry() *Registry {
	return &Registry{
		adapters: make(map[models.ChannelType]Adapter),
	}
}

// Register adds an adapter to the registry.
func (r *Registry) Register(adapter Adapter) {
	r.adapters[adapter.Type()] = adapter
}

// Get returns an adapter by channel type.
func (r *Registry) Get(channelType models.ChannelType) (Adapter, bool) {
	adapter, ok := r.adapters[channelType]
	return adapter, ok
}

// All returns all registered adapters.
func (r *Registry) All() []Adapter {
	adapters := make([]Adapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		adapters = append(adapters, a)
	}
	return adapters
}

// StartAll starts all registered adapters.
func (r *Registry) StartAll(ctx context.Context) error {
	for _, adapter := range r.adapters {
		if err := adapter.Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

// StopAll stops all registered adapters.
func (r *Registry) StopAll(ctx context.Context) error {
	var lastErr error
	for _, adapter := range r.adapters {
		if err := adapter.Stop(ctx); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// AggregateMessages returns a channel that receives messages from all adapters.
func (r *Registry) AggregateMessages(ctx context.Context) <-chan *models.Message {
	out := make(chan *models.Message)

	for _, adapter := range r.adapters {
		go func(a Adapter) {
			for {
				select {
				case <-ctx.Done():
					return
				case msg, ok := <-a.Messages():
					if !ok {
						return
					}
					select {
					case out <- msg:
					case <-ctx.Done():
						return
					}
				}
			}
		}(adapter)
	}

	return out
}
