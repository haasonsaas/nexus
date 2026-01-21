package channels

import (
	"context"
	"testing"

	"github.com/haasonsaas/nexus/pkg/models"
)

type inboundOnlyAdapter struct {
	messages chan *models.Message
}

func (a *inboundOnlyAdapter) Type() models.ChannelType { return models.ChannelTelegram }

func (a *inboundOnlyAdapter) Messages() <-chan *models.Message { return a.messages }

type outboundOnlyAdapter struct{}

func (outboundOnlyAdapter) Type() models.ChannelType { return models.ChannelDiscord }

func (outboundOnlyAdapter) Send(ctx context.Context, msg *models.Message) error { return nil }

func TestRegistryGetOutbound(t *testing.T) {
	registry := NewRegistry()
	registry.Register(outboundOnlyAdapter{})

	if _, ok := registry.GetOutbound(models.ChannelDiscord); !ok {
		t.Fatalf("expected outbound adapter to be registered")
	}
}

func TestAggregateMessagesUsesInboundAdapters(t *testing.T) {
	registry := NewRegistry()
	inbound := &inboundOnlyAdapter{messages: make(chan *models.Message, 1)}
	registry.Register(inbound)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := registry.AggregateMessages(ctx)
	msg := &models.Message{Role: models.RoleUser, Content: "hi"}
	inbound.messages <- msg

	got := <-out
	if got != msg {
		t.Fatalf("expected message to pass through, got %#v", got)
	}
}
