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
- Clawd workspace templates (`/home/developer/clawd`).
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
   - [x] Add plugin manifest + schema validation and public plugin SDK types.
   - [x] Add schema-driven config validation + explicit defaults pipeline.
   - [x] Add runtime plugin hooks (in-process register + optional .so loading).
   - [x] Add operator UX for memory/heartbeat prompt preview and config doctor.
   - [x] Load workspace bootstrap files (AGENTS/SOUL/USER/IDENTITY/MEMORY) into the system prompt.
   - [x] Align example config/docs with strict schema (tools vs plugins, remove unsupported keys).
   - [x] Add workspace bootstrap CLI (`nexus setup`) to seed AGENTS/SOUL/USER/IDENTITY/TOOLS/HEARTBEAT/MEMORY.
   - [x] Add `doctor --repair` with config migrations + workspace repairs.
   - [x] Add doctor channel health probes (opt-in) for enabled adapters.
   - [x] Add service install/repair workflows (systemd/launchd templates + audit).
   - [x] Add onboarding + auth CLI flows.
   - [x] Add channel policy checks in doctor.
   - [x] Add memory search tool (lexical/vector/hybrid) and memory flush confirmations.
   - [x] Add remote embeddings + cache for memory search (OpenAI-compatible).
   - [x] Add profile-aware config selection + profile CLI.
   - [x] Add skills CLI (list/enable/disable) and channel login checks.
   - [x] Auto-restart services after install/repair.
   - [x] Implement auth service + API key/JWT gRPC middleware (design doc section 6).
   - [x] Add OAuth provider scaffolding + callback handler and env overrides for config.
   - [x] Implement gRPC services (stream + session/agent/channel/health) and in-memory session store.
   - [x] Add DB-backed storage for agents/channel connections/users with migrations and OAuth user linkage.
   - [x] Implement migration runner (`nexus migrate`) with embedded migrations + schema_migrations table.
   - [x] Ship cron scheduler MVP (webhook jobs, config-driven, tests) per design doc.
   - [x] Wire MCP manager into gateway lifecycle and bridge MCP tools into the runtime (safe tool names).

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
- 2026-01-21: Added plugin manifest discovery + schema validation and published a minimal plugin SDK surface.
- 2026-01-21: Added runtime plugin hooks (.so loading), plus doctor/prompt CLI UX for memory/heartbeat.
- 2026-01-21: Added sample external plugin (`examples/plugins/echo`) with manifest + build instructions.
- 2026-01-21: Added workspace bootstrap file loading, fallback tool notes from workspace, and documentation fixes for strict config.
- 2026-01-21: Added `nexus setup`, doctor repairs/migrations, and channel health probes.
- 2026-01-21: Added memory search tool, memory flush reminders, service/daemon audits, and startup migration checks.
- 2026-01-21: Added service install/repair CLI, onboarding/auth flows, channel policy checks, vector/hybrid memory search, and memory flush confirmations.
- 2026-01-21: Claude CLI blocked by updated Terms prompt when run non-interactively; requires interactive run to accept.
- 2026-01-21: Added profiles + skills + channel login CLI, NEXUS_PROFILE support, memory search remote embeddings + cache, and service auto-restart hooks.
- 2026-01-21: Implemented auth service package (JWT + API keys) and gRPC middleware wiring per design docs.
- 2026-01-21: Added OAuth provider scaffolding, callback handler, and config env overrides.
- 2026-01-21: Implemented gRPC services + in-memory session store; added tests.
- 2026-01-22: Reviewed Clawdbot cron implementation + docs via GH API; translated into a Nexus cron MVP plan.
- 2026-01-22: Added storage layer for agents/channel connections/users (memory + Cockroach) and OAuth user linkage.
- 2026-01-22: Added embedded migration runner + schema_migrations table and core table migrations.
- 2026-01-22: Implemented cron scheduler MVP (webhook jobs, config schedules, tests) and wired into gateway start/stop.
- 2026-01-22: Added MCP tool bridge with safe tool names, plus gateway lifecycle wiring and MCP integration docs update.
