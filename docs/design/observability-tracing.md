# Observability Tracing Design

## Overview

This document defines OpenTelemetry tracing and cost instrumentation for Nexus. It addresses issue #79 by wiring tracing configuration into the gateway and exposing span data for message processing, tool execution, and LLM usage.

## Goals

1. **Distributed tracing**: Emit spans via OTLP to common backends (Tempo, Jaeger).
2. **Low-friction config**: Simple YAML-based setup with defaults.
3. **Key spans**: Message processing, tool execution, LLM requests.
4. **Cost context**: Attach usage + cost attributes where available.
5. **Safe defaults**: No tracing unless explicitly enabled.

## Non-Goals

- Full metrics overhaul (existing metrics stay).
- Trace visualizations in the UI (future).

---

## 1. Configuration

```yaml
observability:
  tracing:
    enabled: true
    endpoint: "localhost:4317"
    service_name: "nexus"
    service_version: "dev"
    environment: "local"
    sampling_rate: 0.2
    insecure: true
    attributes:
      deployment: "dev"
```

### 1.1 Derived Config

```go
type ObservabilityConfig struct {
    Tracing TracingConfig `yaml:"tracing"`
}

type TracingConfig struct {
    Enabled       bool               `yaml:"enabled"`
    Endpoint      string             `yaml:"endpoint"`
    ServiceName   string             `yaml:"service_name"`
    ServiceVersion string            `yaml:"service_version"`
    Environment   string             `yaml:"environment"`
    SamplingRate  float64            `yaml:"sampling_rate"`
    Insecure      bool               `yaml:"insecure"`
    Attributes    map[string]string  `yaml:"attributes"`
}
```

---

## 2. Span Taxonomy

- `message.process`: channel, session_id, direction
- `llm.request`: provider, model, usage tokens, cost_usd
- `tool.execute`: tool_name, duration_ms, error

Spans are additive and optional; they should not block processing.

---

## 3. Implementation Plan

1. Add tracing config to `internal/config`.
2. Initialize `observability.NewTracer` on gateway startup when enabled.
3. Wrap message handling and LLM/tool hooks with tracing spans.
4. Flush tracer on shutdown.

---

## 4. Risks

- OTLP endpoint misconfiguration: should fail open (no panic).
- Excessive span volume: mitigated by sampling rate.

