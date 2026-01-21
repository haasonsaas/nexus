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
   - [ ] Implement message handling in `internal/gateway/server.go`.
   - [ ] Add gRPC auth interceptors (unary + stream).
   - [ ] Wire session persistence into gateway flow.

2) Reliability & shutdown safety
   - [x] Track gateway goroutines with WaitGroups + cancellation.
   - [x] Buffer `Runtime.Process` channel to avoid producer hangs.

3) Provider error integration
   - [x] Integrate `ProviderError` into Anthropic provider.
   - [x] Integrate `ProviderError` into OpenAI provider.

4) Clawdbot-inspired architecture
   - [ ] Split the monolithic channel adapter interface into focused contracts.
   - [ ] Add a plugin registry with lazy loading.
   - [ ] Add schema-driven config validation + explicit defaults pipeline.

5) Testing improvements
   - [x] Add unit test for buffered runtime response channel.
   - [x] Add unit test for gateway shutdown waiting on handlers.
   - [x] Add unit tests for provider error wrapping.
   - [ ] Add gateway integration tests for message flow.
   - [ ] Add context-cancellation tests for nested goroutines.

6) Validation
   - [ ] `go test ./...`
   - [ ] `go build ./cmd/nexus`

## TDD Approach
- For each change: add/extend tests first, confirm failure, then implement.
- Prefer targeted package tests before running the full suite.

## Subagents
- Use Claude CLI for backlog triage and architectural cross-checks.

## Progress Notes
- 2026-01-21: Plan created from `TODO.md` + Clawdbot reference + Claude CLI.
- 2026-01-21: Added gateway WaitGroup tracking + buffered runtime channel with tests (go test ./internal/agent, ./internal/gateway).
- 2026-01-21: Integrated ProviderError into Anthropic/OpenAI providers with unit tests; fixed websearch schema marshal handling.
