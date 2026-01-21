package doctor

import (
	"context"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
)

type fakeHealthAdapter struct {
	channel models.ChannelType
	status  channels.HealthStatus
}

func (f *fakeHealthAdapter) Type() models.ChannelType { return f.channel }

func (f *fakeHealthAdapter) Status() channels.Status { return channels.Status{} }

func (f *fakeHealthAdapter) HealthCheck(ctx context.Context) channels.HealthStatus {
	select {
	case <-ctx.Done():
		return channels.HealthStatus{Healthy: false, Message: "timeout", LastCheck: time.Now()}
	default:
	}
	return f.status
}

func (f *fakeHealthAdapter) Metrics() channels.MetricsSnapshot { return channels.MetricsSnapshot{} }

func TestProbeChannelHealthReturnsResults(t *testing.T) {
	registry := channels.NewRegistry()
	registry.Register(&fakeHealthAdapter{
		channel: models.ChannelType("alpha"),
		status:  channels.HealthStatus{Healthy: true, Message: "ok"},
	})
	registry.Register(&fakeHealthAdapter{
		channel: models.ChannelType("beta"),
		status:  channels.HealthStatus{Healthy: false, Message: "down"},
	})

	results := ProbeChannelHealth(context.Background(), registry)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Channel != models.ChannelType("alpha") {
		t.Fatalf("expected sorted results, got %s", results[0].Channel)
	}
}
