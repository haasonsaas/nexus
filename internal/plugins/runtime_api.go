package plugins

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

var closedMessages <-chan *models.Message = func() <-chan *models.Message {
	ch := make(chan *models.Message)
	close(ch)
	return ch
}()

type runtimeChannelRegistry struct {
	registry *channels.Registry
}

func (r *runtimeChannelRegistry) RegisterChannel(adapter pluginsdk.ChannelAdapter) error {
	if r.registry == nil {
		return fmt.Errorf("channel registry is nil")
	}
	if adapter == nil {
		return fmt.Errorf("plugin adapter is nil")
	}
	r.registry.Register(pluginAdapterWrapper{adapter: adapter})
	return nil
}

type runtimeToolRegistry struct {
	runtime *agent.Runtime
}

func (r *runtimeToolRegistry) RegisterTool(def pluginsdk.ToolDefinition, handler pluginsdk.ToolHandler) error {
	if r.runtime == nil {
		return fmt.Errorf("runtime is nil")
	}
	if handler == nil {
		return fmt.Errorf("tool handler is nil")
	}
	if def.Name == "" {
		return fmt.Errorf("tool name is required")
	}
	tool := &pluginTool{
		definition: def,
		handler:    handler,
	}
	r.runtime.RegisterTool(tool)
	return nil
}

type pluginTool struct {
	definition pluginsdk.ToolDefinition
	handler    pluginsdk.ToolHandler
}

func (t *pluginTool) Name() string {
	return t.definition.Name
}

func (t *pluginTool) Description() string {
	return t.definition.Description
}

func (t *pluginTool) Schema() json.RawMessage {
	return t.definition.Schema
}

func (t *pluginTool) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	result, err := t.handler(ctx, params)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return &agent.ToolResult{Content: ""}, nil
	}
	return &agent.ToolResult{
		Content: result.Content,
		IsError: result.IsError,
	}, nil
}

type pluginAdapterWrapper struct {
	adapter pluginsdk.ChannelAdapter
}

func (p pluginAdapterWrapper) Type() models.ChannelType {
	return p.adapter.Type()
}

func (p pluginAdapterWrapper) Start(ctx context.Context) error {
	if adapter, ok := p.adapter.(pluginsdk.LifecycleAdapter); ok {
		return adapter.Start(ctx)
	}
	return nil
}

func (p pluginAdapterWrapper) Stop(ctx context.Context) error {
	if adapter, ok := p.adapter.(pluginsdk.LifecycleAdapter); ok {
		return adapter.Stop(ctx)
	}
	return nil
}

func (p pluginAdapterWrapper) Send(ctx context.Context, msg *models.Message) error {
	if adapter, ok := p.adapter.(pluginsdk.OutboundAdapter); ok {
		return adapter.Send(ctx, msg)
	}
	return fmt.Errorf("outbound not supported")
}

func (p pluginAdapterWrapper) Messages() <-chan *models.Message {
	if adapter, ok := p.adapter.(pluginsdk.InboundAdapter); ok {
		return adapter.Messages()
	}
	return closedMessages
}

func (p pluginAdapterWrapper) Status() channels.Status {
	if adapter, ok := p.adapter.(pluginsdk.HealthAdapter); ok {
		return toChannelStatus(adapter.Status())
	}
	return channels.Status{}
}

func (p pluginAdapterWrapper) HealthCheck(ctx context.Context) channels.HealthStatus {
	if adapter, ok := p.adapter.(pluginsdk.HealthAdapter); ok {
		return toChannelHealth(adapter.HealthCheck(ctx))
	}
	return channels.HealthStatus{}
}

func (p pluginAdapterWrapper) Metrics() channels.MetricsSnapshot {
	return channels.MetricsSnapshot{ChannelType: p.adapter.Type()}
}

func toChannelStatus(status pluginsdk.Status) channels.Status {
	return channels.Status{
		Connected: status.Connected,
		Error:     status.Error,
		LastPing:  status.LastPing,
	}
}

func toChannelHealth(status pluginsdk.HealthStatus) channels.HealthStatus {
	return channels.HealthStatus{
		Healthy:   status.Healthy,
		Latency:   status.Latency,
		Message:   status.Message,
		LastCheck: status.LastCheck,
		Degraded:  status.Degraded,
	}
}
