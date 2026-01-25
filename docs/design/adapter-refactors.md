# Adapter & Provider Refactors Design

## Overview

This document consolidates refactor work for issues #84–#89. The goal is to DRY up common adapter/provider patterns while keeping behavior stable and migrations incremental.

## Goals

1. **Base health/metrics adapter** for consistent status, metrics, and degraded-state handling.
2. **Reusable reconnection loop** with standardized backoff and observability hooks.
3. **Stream manager** for buffered streaming updates (throttling + chunk splitting).
4. **Base provider utilities** to reduce duplicated retry/error/stream handling in LLM providers.
5. **Unified tool schema** with provider-specific converters in one place.

## Non-Goals

- Full rewrite of all adapters/providers in one pass.
- Large behavior changes to streaming semantics or rate limits.
- Changing public APIs for channel adapters or LLM providers.

---

## 1) BaseHealthAdapter (Issue #89)

### Problem
Many channel adapters implement their own `status`, `metrics`, and `degraded` state tracking. This leads to inconsistent behavior and duplicated code.

### Proposal
Introduce `channels.BaseHealthAdapter` with:
- `Status()`, `SetStatus(connected, err)`
- `Metrics()` snapshot
- `RecordMessageSent/Received/Failed`, `RecordError`, `RecordReconnectAttempt`
- `SetDegraded`, `IsDegraded`
- `HealthCheck(ctx)` default implementation

Adapters embed this base type and replace custom status/metrics tracking.

### Migration Plan
Phase 1: Adopt in `personal.BaseAdapter` + one or two core adapters (Telegram, Email).
Phase 2: Migrate remaining adapters in batches.

---

## 2) ReconnectingAdapter Wrapper (Issue #86)

### Problem
Adapters implement reconnection loops differently, with inconsistent backoff, logging, and metrics.

### Proposal
Add `channels.Reconnector` helper:
- Configurable backoff (initial delay, max delay, factor, jitter)
- Max attempts + cancellation aware
- Optional hook to update health/metrics

Adapters use `Reconnector.Run(ctx, func(ctx) error)` instead of hand-rolled loops.

---

## 3) StreamManager (Issue #87)

### Problem
Streaming responses need throttling and buffering to avoid edit rate limits and "message not modified" errors.

### Proposal
Extract a `StreamManager` from gateway streaming logic:
- Accepts streaming adapter + behavior config
- Buffers and throttles updates
- Handles first-message creation and final update

Gateway `StreamingHandler` delegates buffered updates to StreamManager.

---

## 4) BaseProvider Utilities (Issue #85)

### Problem
LLM providers duplicate error handling, retry, and stream aggregation logic.

### Proposal
Add `providers.BaseProvider` utilities:
- Common retry helpers
- Standardized error wrapping
- Shared stream accumulation helpers for tests

Providers adopt helpers incrementally without changing their public APIs.

---

## 5) Unified Tool Schema (Issue #88)

### Problem
Tool conversion to provider-specific schema is duplicated across providers.

### Proposal
Introduce `agent/toolconv` package:
- Single canonical tool schema (existing `agent.Tool`)
- Provider-specific converters (OpenAI, Anthropic, Google, etc.)
- Centralized tests for schema transformation

Providers switch to using converters instead of local logic.

---

## Testing

- Unit tests for BaseHealthAdapter (status, metrics, degraded).
- Reconnector tests for backoff + cancellation.
- StreamManager tests for throttling and final update behavior.
- Provider converter golden tests for tool schema transforms.

---

## Rollout

1. Add BaseHealthAdapter + Reconnector + StreamManager scaffolding.
2. Migrate a first set of adapters (personal base + Telegram + Email).
3. Add tool converters and start migrating providers.
4. Continue adapter/provider migrations in batches.
