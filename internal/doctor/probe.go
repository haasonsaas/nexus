package doctor

import (
	"context"
	"sort"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
)

// ChannelProbe captures a channel health probe result.
type ChannelProbe struct {
	Channel models.ChannelType
	Status  channels.HealthStatus
}

// ProbeChannelHealth runs health checks for registered adapters.
func ProbeChannelHealth(ctx context.Context, registry *channels.Registry) []ChannelProbe {
	if registry == nil {
		return nil
	}

	adapters := registry.HealthAdapters()
	if len(adapters) == 0 {
		return nil
	}

	keys := make([]string, 0, len(adapters))
	for channel := range adapters {
		keys = append(keys, string(channel))
	}
	sort.Strings(keys)

	results := make([]ChannelProbe, 0, len(keys))
	for _, key := range keys {
		adapter := adapters[models.ChannelType(key)]
		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		status := adapter.HealthCheck(probeCtx)
		cancel()
		results = append(results, ChannelProbe{Channel: models.ChannelType(key), Status: status})
	}

	return results
}
