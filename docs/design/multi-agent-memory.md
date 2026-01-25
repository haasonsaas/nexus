# Multi-Agent Memory & Attention Design

## Overview

This document covers issues #108, #107, #100, and #90: hierarchical memory, shared multi-agent memory, attention feed as active input, and background memory consolidation.

## Goals

1. **Hierarchical recall** across session → agent → channel → global scopes.
2. **Shared memory** that multiple agents can read/write safely.
3. **Attention feed** surfaced to agents for action prioritization.
4. **Memory consolidation** to summarize long conversations into durable long-term memories.

## Non-Goals

- Full semantic deduplication across all stores.
- Complex RBAC for memory sharing (future).
- UI work for attention feed dashboards.

---

## 1) Hierarchical Memory Model

### Concept
Memories are stored with scope identifiers:
- **Session**: tight local context
- **Agent**: persistent agent-specific memory
- **Channel**: per-conversation or per-channel memory
- **Global**: shared memory across all agents

### Retrieval
When hierarchical search is enabled, retrieval merges scope-specific searches with weights:
```
session (1.0) > agent (0.8) > channel (0.7) > global (0.5)
```

### Defaults
- `vector_memory.search.hierarchy.enabled = false` (opt-in)
- Default scope order: session, agent, channel, global

---

## 2) Multi-Agent Shared Memory

### Concept
Global and channel scopes allow shared memory between agents. Agents can write entries tagged as `summary`, `decision`, or `fact` to facilitate collaborative recall.

### Guardrails
- Shared entries should be tagged and marked by source in metadata.
- Future: add allowlist / RBAC for shared scopes.

---

## 3) Attention Feed as Agent Input

### Concept
The attention feed aggregates inbound messages/tickets into an active queue. Agents can:
- Query it via tools (`attention_list`, `attention_get`, etc.)
- Receive a short summary injected into the system prompt

### Prompt Injection
When enabled, a small list of active attention items is included in the system prompt to help the agent prioritize work.

---

## 4) Memory Consolidation

### Concept
A background worker periodically summarizes long sessions into durable memory entries, reducing noise and keeping long-term recall concise.

### Behavior
- Runs on a schedule (`vector_memory.consolidation.interval`)
- Summarizes recent messages
- Stores a single `summary` entry with `source=consolidation`
- Marks session metadata with `memory_consolidated_at`

### LLM Usage
- Uses LLM if available; falls back to heuristic summary if not.

---

## Configuration

```yaml
vector_memory:
  search:
    hierarchy:
      enabled: true
      scopes: ["session", "agent", "channel", "global"]
      weights:
        session: 1.0
        agent: 0.8
        channel: 0.7
        global: 0.5
  consolidation:
    enabled: true
    interval: 6h
    min_messages: 20
    max_messages: 120
    max_sessions: 50
    summary_max_chars: 2000
    summary_max_tokens: 512

attention:
  enabled: true
  inject_in_prompt: true
  max_items: 5
```

---

## Rollout

1. Add hierarchical search and attention prompt injection.
2. Add consolidation worker and metadata tracking.
3. Expand shared-memory policy and tooling.
