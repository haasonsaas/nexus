# Edge Mesh Architecture Design

## Overview

This document defines the edge mesh architecture for distributed tool execution across connected edge daemons. It addresses issues #106, #94, and #80 by introducing capability-based routing, selection strategies, and a future-ready mesh topology that can expand beyond a single core instance.

## Goals

1. **Capability routing**: Select edges based on tool availability, capabilities, channel support, and metadata tags.
2. **Load-aware selection**: Prefer less-loaded edges to avoid hot spots.
3. **Composable routing**: Provide clear APIs for selection + execution (manual or automatic).
4. **Mesh-ready**: Design for multi-core/federated routing, even if phase 1 is local-only.
5. **Observable**: Emit routing decisions and edge selection outcomes.

## Non-Goals

- Full multi-core federation in phase 1 (routing remains local to a core instance).
- Cross-region failover with WAN health probes (deferred).
- Edge-to-edge direct tool routing (core remains the control plane).

---

## 1. Architecture

```
                 ┌─────────────────────────────────────────┐
                 │              Nexus Core                 │
                 │ ┌─────────────────────────────────────┐ │
                 │ │   Edge Mesh Router (new)            │ │
                 │ │ - selection criteria               │ │
                 │ │ - load-aware strategy              │ │
                 │ │ - observability hooks              │ │
                 │ └───────────────┬─────────────────────┘ │
                 └─────────────────┼───────────────────────┘
                                   │
              ┌────────────────────┼────────────────────┐
              ▼                    ▼                    ▼
         ┌───────────┐        ┌───────────┐        ┌───────────┐
         │  Edge A   │        │  Edge B   │        │  Edge C   │
         └───────────┘        └───────────┘        └───────────┘
```

### 1.1 Router Inputs

- **Tool availability**: edge tool registry (name + schema).
- **Capabilities**: `EdgeCapabilities` (tools/channels/streaming/artifacts).
- **Channel support**: registered channel types (iMessage, WhatsApp, etc).
- **Metadata tags**: edge metadata (region, device class, GPU, etc).
- **Load metrics**: `active_tool_count`, optional CPU/memory.

### 1.2 Selection Strategies

- `least_busy` (default): choose the lowest `active_tool_count` / `max_concurrent_tools`.
- `round_robin`: deterministic fairness across candidates.
- `random`: simple load spread for small meshes.

---

## 2. API Additions

### 2.1 Manager Selection API

Add selection methods to the edge manager:

```go
type SelectionCriteria struct {
    ToolName    string
    ChannelType string
    Capabilities *pb.EdgeCapabilities
    Metadata    map[string]string
    Strategy    SelectionStrategy // least_busy | round_robin | random
}

func (m *Manager) SelectEdge(criteria SelectionCriteria) (*EdgeConnection, error)
func (m *Manager) ExecuteToolAny(ctx context.Context, criteria SelectionCriteria, input string, opts ExecuteOptions) (*ToolExecutionResult, error)
```

### 2.2 Nodes Tool Extensions

Expose routing via the `nodes` tool:

- `action: route` -> return candidate edges and selected edge.
- `action: invoke_any` -> select edge + execute tool.

---

## 3. Configuration

No new mandatory config for phase 1. Optional parameters may be added later:

```yaml
edge:
  routing:
    default_strategy: least_busy
    metadata_keys: ["region", "device_class"]
```

---

## 4. Observability

Emit routing events with:

- `edge_id` selected
- criteria summary
- strategy used
- candidate count
- selection latency

These events can flow through existing `event_timeline` plumbing.

---

## 5. Phased Delivery

**Phase 1 (this iteration)**:
- Local-only routing in `edge.Manager`.
- `nodes` tool gains `route` + `invoke_any`.
- Selection strategy `least_busy` and simple metadata filtering.

**Phase 2**:
- Mesh peers (remote core instances) expose virtual edge inventories.
- Router aggregates local + remote candidates with latency weighting.

**Phase 3**:
- Cross-core failover, global registry, and per-tenant routing policy.

---

## 6. Risks

- Ambiguous selection when multiple edges match; mitigated by explicit strategies.
- Incomplete metadata leading to suboptimal routing; provide defaults and logging.
- Edge load metrics may be stale; selection should tolerate jitter.

