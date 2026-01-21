# Shortcomings Remediation Plan (Nexus)

## Goals
- Turn the gateway into a functional message pipeline (session → agent runtime → response).
- Add missing security gates for gRPC access.
- Reduce reliability risks (goroutine leaks, blocking channels).
- Bring provider error handling in line with the structured failover model.
- Adopt proven patterns from Clawdbot where they fit Nexus.

## Inputs
- `TODO.md` (existing prioritized backlog).
- Clawdbot reference repo (local clone) for inspiration.
- Claude CLI synthesis (2026-01-21).

## Workstreams & Status
1) Critical gateway functionality
   - [x] Implement message handling in `internal/gateway/server.go`.
   - [x] Add gRPC auth interceptors (unary + stream).
   - [x] Wire session persistence into gateway flow.

2) Reliability & shutdown safety
   - [x] Track gateway goroutines with WaitGroups + cancellation.
   - [x] Buffer `Runtime.Process` channel to avoid producer hangs.
   - [x] Fix LatencyHistogram ring buffer to avoid O(n) churn.

3) Provider error integration
   - [x] Integrate `ProviderError` into Anthropic provider.
   - [x] Integrate `ProviderError` into OpenAI provider.

4) Clawdbot-inspired architecture
   - [x] Add identity/user-backed system prompt assembly.
   - [x] Add local daily memory logging (optional).
   - [x] Add tool-notes injection into system prompt.
   - [x] Add safety defaults to the system prompt (no secrets, no destructive actions, no partials).
   - [x] Add bootstrap guidance when identity/user details are missing.
   - [x] Add memory recall (read today/yesterday logs into the prompt when enabled).
   - [x] Add optional heartbeat checklist integration.
   - [x] Add tool notes file support (path-based, not just inline config).
   - [x] Split the monolithic channel adapter interface into focused contracts.
   - [x] Add a plugin registry with lazy loading.
   - [x] Add schema-driven config validation + explicit defaults pipeline.

5) Testing improvements
   - [x] Add unit test for buffered runtime response channel.
   - [x] Add unit test for gateway shutdown waiting on handlers.
   - [x] Add unit tests for provider error wrapping.
   - [x] Add gateway tests for auth interceptors + message flow wiring.
   - [x] Add gateway integration tests for message flow.
   - [x] Add context-cancellation tests for nested goroutines.

6) Validation
   - [x] `go test ./...`
   - [x] `go build ./cmd/nexus`

## TDD Approach
- For each change: add/extend tests first, confirm failure, then implement.
- Prefer targeted package tests before running the full suite.

## Subagents
- Use Claude CLI for backlog triage and architectural cross-checks.

## Progress Notes
- 2026-01-21: Plan created from `TODO.md` + Clawdbot reference + Claude CLI.
- 2026-01-21: Added gateway WaitGroup tracking + buffered runtime channel with tests (go test ./internal/agent, ./internal/gateway).
- 2026-01-21: Integrated ProviderError into Anthropic/OpenAI providers with unit tests; fixed websearch schema marshal handling.
- 2026-01-21: Reworked channel latency histogram into a true ring buffer with tests.
- 2026-01-21: Implemented gateway message flow, auth interceptors, session store wiring, tool/adapter registration, and default-model propagation with unit tests.
- 2026-01-21: Added session scoping config, base URL wiring for providers, gateway processing integration tests, and context-cancellation coverage.
- 2026-01-21: Added identity + user config, system prompt assembly, memory logger, tool notes prompt injection, and safety/bootstrap guidance.
- 2026-01-21: Claude CLI timeout traced to shell command timeout; resolved by running with longer timeout and shorter prompts.
- 2026-01-21: Added per-request system prompt overrides with memory recall, heartbeat checklist injection, and tool-notes file support.
- 2026-01-21: Added strict config parsing (unknown keys fail), validation checks, and explicit defaults pipeline.
- 2026-01-21: Added channel plugin registry with lazy loading and split channel adapter interfaces into focused contracts.
