package pluginsdk

import (
	"context"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

// ChannelAdapter is the minimal interface for plugin-provided channels.
type ChannelAdapter interface {
	Type() models.ChannelType
}

type InboundAdapter interface {
	Messages() <-chan *models.Message
}

type OutboundAdapter interface {
	Send(ctx context.Context, msg *models.Message) error
}

type LifecycleAdapter interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

type HealthAdapter interface {
	Status() Status
	HealthCheck(ctx context.Context) HealthStatus
}

// Status represents the connection status for plugin adapters.
type Status struct {
	Connected bool
	Error     string
	LastPing  int64
}

// HealthStatus mirrors a lightweight health check result for plugin adapters.
type HealthStatus struct {
	Healthy   bool
	Latency   time.Duration
	Message   string
	LastCheck time.Time
	Degraded  bool
}
