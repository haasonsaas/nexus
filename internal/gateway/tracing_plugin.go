package gateway

import (
	"context"
	"sync"

	"github.com/haasonsaas/nexus/internal/observability"
	"github.com/haasonsaas/nexus/pkg/models"
	"go.opentelemetry.io/otel/trace"
)

// TracingPlugin emits OpenTelemetry spans for agent events.
type TracingPlugin struct {
	tracer *observability.Tracer

	mu        sync.Mutex
	toolSpans map[string]map[string]trace.Span
	iterSpans map[string]map[int]trace.Span
}

// NewTracingPlugin creates a new tracing plugin.
func NewTracingPlugin(tracer *observability.Tracer) *TracingPlugin {
	return &TracingPlugin{
		tracer:    tracer,
		toolSpans: make(map[string]map[string]trace.Span),
		iterSpans: make(map[string]map[int]trace.Span),
	}
}

// OnEvent translates agent events into spans.
func (p *TracingPlugin) OnEvent(ctx context.Context, e models.AgentEvent) {
	if p == nil || p.tracer == nil {
		return
	}
	if e.RunID == "" {
		return
	}

	switch e.Type {
	case models.AgentEventIterStarted:
		p.startIterSpan(ctx, e.RunID, e.IterIndex)
	case models.AgentEventModelCompleted:
		p.finishIterSpan(ctx, e)
	case models.AgentEventIterFinished:
		p.endIterSpan(e.RunID, e.IterIndex)
	case models.AgentEventToolStarted:
		p.startToolSpan(ctx, e)
	case models.AgentEventToolFinished:
		p.finishToolSpan(e)
	case models.AgentEventToolTimedOut:
		p.timeoutToolSpan(e)
	case models.AgentEventRunError, models.AgentEventRunCancelled, models.AgentEventRunTimedOut, models.AgentEventRunFinished:
		p.endRunSpans(e.RunID)
	}
}

func (p *TracingPlugin) startIterSpan(ctx context.Context, runID string, iter int) {
	_, span := p.tracer.Start(ctx, "llm.request", observability.SpanOptions{Kind: trace.SpanKindClient})
	p.tracer.SetAttributes(span, "run_id", runID, "iteration", iter)

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.iterSpans[runID] == nil {
		p.iterSpans[runID] = make(map[int]trace.Span)
	}
	p.iterSpans[runID][iter] = span
}

func (p *TracingPlugin) finishIterSpan(ctx context.Context, e models.AgentEvent) {
	span := p.popIterSpan(e.RunID, e.IterIndex)
	if span == nil {
		return
	}
	if e.Stream != nil {
		if e.Stream.Provider != "" {
			p.tracer.SetAttributes(span, "llm.provider", e.Stream.Provider)
		}
		if e.Stream.Model != "" {
			p.tracer.SetAttributes(span, "llm.model", e.Stream.Model)
		}
		if e.Stream.InputTokens > 0 {
			p.tracer.SetAttributes(span, "llm.input_tokens", e.Stream.InputTokens)
		}
		if e.Stream.OutputTokens > 0 {
			p.tracer.SetAttributes(span, "llm.output_tokens", e.Stream.OutputTokens)
		}
	}
	span.End()
}

func (p *TracingPlugin) endIterSpan(runID string, iter int) {
	span := p.popIterSpan(runID, iter)
	if span == nil {
		return
	}
	span.End()
}

func (p *TracingPlugin) popIterSpan(runID string, iter int) trace.Span {
	p.mu.Lock()
	defer p.mu.Unlock()
	iters := p.iterSpans[runID]
	if iters == nil {
		return nil
	}
	span, ok := iters[iter]
	if !ok {
		return nil
	}
	delete(iters, iter)
	if len(iters) == 0 {
		delete(p.iterSpans, runID)
	}
	return span
}

func (p *TracingPlugin) startToolSpan(ctx context.Context, e models.AgentEvent) {
	if e.Tool == nil || e.Tool.CallID == "" {
		return
	}
	_, span := p.tracer.TraceToolExecution(ctx, e.Tool.Name)
	p.tracer.SetAttributes(span, "run_id", e.RunID, "tool.call_id", e.Tool.CallID)

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.toolSpans[e.RunID] == nil {
		p.toolSpans[e.RunID] = make(map[string]trace.Span)
	}
	p.toolSpans[e.RunID][e.Tool.CallID] = span
}

func (p *TracingPlugin) finishToolSpan(e models.AgentEvent) {
	if e.Tool == nil || e.Tool.CallID == "" {
		return
	}
	span := p.popToolSpan(e.RunID, e.Tool.CallID)
	if span == nil {
		return
	}
	p.tracer.SetAttributes(span,
		"tool.success", e.Tool.Success,
		"tool.elapsed_ms", e.Tool.Elapsed.Milliseconds(),
	)
	span.End()
}

func (p *TracingPlugin) timeoutToolSpan(e models.AgentEvent) {
	if e.Tool == nil || e.Tool.CallID == "" {
		return
	}
	span := p.popToolSpan(e.RunID, e.Tool.CallID)
	if span == nil {
		return
	}
	if e.Error != nil && e.Error.Message != "" {
		p.tracer.RecordError(span, context.DeadlineExceeded)
		p.tracer.SetAttributes(span, "tool.error", e.Error.Message)
	}
	p.tracer.SetAttributes(span,
		"tool.success", false,
		"tool.elapsed_ms", e.Tool.Elapsed.Milliseconds(),
	)
	span.End()
}

func (p *TracingPlugin) popToolSpan(runID, callID string) trace.Span {
	p.mu.Lock()
	defer p.mu.Unlock()
	tools := p.toolSpans[runID]
	if tools == nil {
		return nil
	}
	span, ok := tools[callID]
	if !ok {
		return nil
	}
	delete(tools, callID)
	if len(tools) == 0 {
		delete(p.toolSpans, runID)
	}
	return span
}

func (p *TracingPlugin) endRunSpans(runID string) {
	p.mu.Lock()
	iterSpans := p.iterSpans[runID]
	toolSpans := p.toolSpans[runID]
	delete(p.iterSpans, runID)
	delete(p.toolSpans, runID)
	p.mu.Unlock()

	for _, span := range iterSpans {
		span.End()
	}
	for _, span := range toolSpans {
		span.End()
	}
}

// GetTracingPlugin returns a Plugin that emits OpenTelemetry spans if tracing is enabled.
func (s *Server) GetTracingPlugin() *TracingPlugin {
	if s == nil || s.tracer == nil || s.config == nil {
		return nil
	}
	if !s.config.Observability.Tracing.Enabled {
		return nil
	}
	return NewTracingPlugin(s.tracer)
}
