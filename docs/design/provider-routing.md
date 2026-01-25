# Provider Routing & Local Discovery Design

## Overview

This document specifies intelligent provider routing for Nexus, combining cost/quality heuristics, predictive health, and local-provider auto-discovery. It addresses issues #105 (intelligent routing), #76 (smart routing by complexity), and #77 (auto-detect local Ollama).

## Goals

1. **Local-first routing**: Prefer on-device or LAN inference when available.
2. **Quality-aware routing**: Route complex tasks to higher-quality providers.
3. **Cost-aware routing**: Reduce spend for trivial prompts.
4. **Health-aware routing**: Avoid unstable providers proactively.
5. **Configurable & safe**: Deterministic rules with clear observability.

## Non-Goals

- Building a full provider marketplace.
- Automatic finetune selection.
- Replacing the existing failover orchestrator (we will build on it).

---

## 1. Architecture

```
            ┌───────────────────────────────┐
            │        Routing Layer          │
            │  (policy + metrics + rules)   │
            └───────────────┬───────────────┘
                            │ selects
                    ┌───────┴────────┐
                    ▼                ▼
         ┌────────────────┐  ┌────────────────┐
         │ Provider (A)   │  │ Provider (B)   │
         │ (local/cheap)  │  │ (quality/high) │
         └────────────────┘  └────────────────┘
                            │
                            ▼
                  ┌──────────────────────┐
                  │ Failover Orchestrator│
                  └──────────────────────┘
```

### 1.1 Components

- **Router**: Selects a provider (and optional model) per request.
- **Discovery Service**: Probes for local providers (Ollama first).
- **Metrics Store**: Tracks latency, cost, error rate, and health.
- **Policy Engine**: Applies configured rules and fallbacks.

---

## 2. Configuration

```yaml
llm:
  default_provider: anthropic
  routing:
    enabled: true
    classifier: heuristic # heuristic | local | remote
    prefer_local: true
    rules:
      - name: quick
        match:
          patterns: ["what is", "define", "quick"]
        target:
          provider: ollama
          model: qwen2.5:1.5b
      - name: reasoning
        match:
          patterns: ["analyze", "reason", "think through"]
        target:
          provider: anthropic
          model: claude-sonnet-4-20250514
      - name: code
        match:
          tags: ["code"]
        target:
          provider: openai
          model: gpt-4o
    fallback:
      provider: anthropic
  auto_discover:
    ollama:
      enabled: true
      prefer_local: true
      probe_locations:
        - http://localhost:11434
        - http://ollama:11434
        - http://ollama.ollama.svc.cluster.local:11434
```

### 2.1 Derived Config

Add to `internal/config`:

```go
type LLMRoutingConfig struct {
    Enabled      bool            `yaml:"enabled"`
    Classifier   string          `yaml:"classifier"`
    PreferLocal  bool            `yaml:"prefer_local"`
    Rules        []RoutingRule   `yaml:"rules"`
    Fallback     RoutingTarget   `yaml:"fallback"`
}

type RoutingRule struct {
    Name   string        `yaml:"name"`
    Match  RoutingMatch  `yaml:"match"`
    Target RoutingTarget `yaml:"target"`
}

type RoutingMatch struct {
    Patterns []string `yaml:"patterns"`
    Tags     []string `yaml:"tags"`
}

type RoutingTarget struct {
    Provider string `yaml:"provider"`
    Model    string `yaml:"model"`
}

type LLMAutoDiscoverConfig struct {
    Ollama OllamaDiscoverConfig `yaml:"ollama"`
}

type OllamaDiscoverConfig struct {
    Enabled        bool     `yaml:"enabled"`
    PreferLocal    bool     `yaml:"prefer_local"`
    ProbeLocations []string `yaml:"probe_locations"`
}
```

---

## 3. Routing Pipeline

1. **Enrich request**: Extract tags (code/analysis) via heuristic classifier.
2. **Local-first check**: If local provider is healthy and `prefer_local`, route there.
3. **Rule matching**: Apply first matching rule by order.
4. **Fallback**: Use configured fallback if no rule matches.
5. **Failover**: Existing failover orchestrator handles provider-level errors.

---

## 4. Local Provider Discovery (Ollama)

### 4.1 Probe Strategy

- HTTP GET `/<api>/tags` or `/api/version` to detect Ollama.
- Record available models and latency.
- Create or update provider entry for `ollama`.

### 4.2 Provider Registration

- Extend provider registry to allow dynamic providers.
- Store discovered providers in-memory (optionally persisted later).

---

## 5. Observability

Emit events/metrics:
- `llm.routing.decision` with rule name + provider.
- `llm.discovery.probe` with outcome + latency.
- `llm.provider.health` periodic snapshot.

---

## 6. Rollout Plan

1. Phase 1: Routing with simple heuristics and static rules.
2. Phase 2: Ollama auto-discovery and prefer-local.
3. Phase 3: Predictive health + cost-aware decisions.

---

## 7. Testing

- Unit tests for rule matching and classifier.
- Integration tests for discovery probes using test HTTP server.
- Routing + failover integration tests using mock providers.

---

## Open Questions

- Should discovery data be persisted across restarts?
- How to score provider “quality” without human labels?
- Should routing be per-session sticky or per-request?
