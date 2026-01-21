package channels

import (
	"context"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// Adapter is the minimal contract for a channel connector.
type Adapter interface {
	// Type returns the channel type (telegram, discord, slack, etc.).
	Type() models.ChannelType
}

// LifecycleAdapter represents adapters that can start and stop.
type LifecycleAdapter interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// OutboundAdapter represents adapters that can send messages.
type OutboundAdapter interface {
	Send(ctx context.Context, msg *models.Message) error
}

// InboundAdapter represents adapters that emit inbound messages.
type InboundAdapter interface {
	Messages() <-chan *models.Message
}

// HealthAdapter represents adapters that expose status and metrics.
type HealthAdapter interface {
	Status() Status
	HealthCheck(ctx context.Context) HealthStatus
	Metrics() MetricsSnapshot
}

// FullAdapter aggregates all adapter capabilities for convenience.
type FullAdapter interface {
	Adapter
	LifecycleAdapter
	OutboundAdapter
	InboundAdapter
	HealthAdapter
}

// Status represents the connection status of a channel.
type Status struct {
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
	LastPing  int64  `json:"last_ping,omitempty"` // Unix timestamp
}

// HealthStatus represents the health check result for an adapter.
type HealthStatus struct {
	// Healthy indicates whether the adapter is functioning correctly
	Healthy bool `json:"healthy"`

	// Latency is the time taken to perform the health check
	Latency time.Duration `json:"latency"`

	// Message provides additional context about the health status
	Message string `json:"message,omitempty"`

	// LastCheck is the timestamp of this health check
	LastCheck time.Time `json:"last_check"`

	// Degraded indicates the service is operational but with reduced functionality
	Degraded bool `json:"degraded,omitempty"`
}

// Registry manages multiple channel adapters.
type Registry struct {
	adapters  map[models.ChannelType]Adapter
	inbound   map[models.ChannelType]InboundAdapter
	outbound  map[models.ChannelType]OutboundAdapter
	lifecycle map[models.ChannelType]LifecycleAdapter
	health    map[models.ChannelType]HealthAdapter
}

// NewRegistry creates a new channel registry.
func NewRegistry() *Registry {
	return &Registry{
		adapters:  make(map[models.ChannelType]Adapter),
		inbound:   make(map[models.ChannelType]InboundAdapter),
		outbound:  make(map[models.ChannelType]OutboundAdapter),
		lifecycle: make(map[models.ChannelType]LifecycleAdapter),
		health:    make(map[models.ChannelType]HealthAdapter),
	}
}

// Register adds an adapter to the registry.
func (r *Registry) Register(adapter Adapter) {
	channelType := adapter.Type()
	r.adapters[channelType] = adapter

	if inbound, ok := adapter.(InboundAdapter); ok {
		r.inbound[channelType] = inbound
	} else {
		delete(r.inbound, channelType)
	}

	if outbound, ok := adapter.(OutboundAdapter); ok {
		r.outbound[channelType] = outbound
	} else {
		delete(r.outbound, channelType)
	}

	if lifecycle, ok := adapter.(LifecycleAdapter); ok {
		r.lifecycle[channelType] = lifecycle
	} else {
		delete(r.lifecycle, channelType)
	}

	if health, ok := adapter.(HealthAdapter); ok {
		r.health[channelType] = health
	} else {
		delete(r.health, channelType)
	}
}

// Get returns an adapter by channel type.
func (r *Registry) Get(channelType models.ChannelType) (Adapter, bool) {
	adapter, ok := r.adapters[channelType]
	return adapter, ok
}

// GetOutbound returns an adapter that can send messages for the channel.
func (r *Registry) GetOutbound(channelType models.ChannelType) (OutboundAdapter, bool) {
	adapter, ok := r.outbound[channelType]
	return adapter, ok
}

// HealthAdapters returns a copy of registered health adapters.
func (r *Registry) HealthAdapters() map[models.ChannelType]HealthAdapter {
	out := make(map[models.ChannelType]HealthAdapter, len(r.health))
	for channelType, adapter := range r.health {
		out[channelType] = adapter
	}
	return out
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
	for _, adapter := range r.lifecycle {
		if err := adapter.Start(ctx); err != nil {
			return err
		}
	}
	return nil
}

// StopAll stops all registered adapters.
func (r *Registry) StopAll(ctx context.Context) error {
	var lastErr error
	for _, adapter := range r.lifecycle {
		if err := adapter.Stop(ctx); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// AggregateMessages returns a channel that receives messages from all adapters.
// The returned channel is closed when the context is cancelled or all adapters close.
func (r *Registry) AggregateMessages(ctx context.Context) <-chan *models.Message {
	out := make(chan *models.Message)
	var wg sync.WaitGroup

	for _, adapter := range r.inbound {
		wg.Add(1)
		go func(a InboundAdapter) {
			defer wg.Done()
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

	// Close output channel when all adapter goroutines complete
	go func() {
		wg.Wait()
		close(out)
	}()

	return out
}
