# Clawdbot Patterns for Nexus Adoption

Analysis date: 2026-01-22

## Summary

After reviewing Clawdbot documentation (hooks, multi-agent sandbox, broadcast groups, session management, compaction), this document identifies patterns Nexus could adopt to improve functionality.

## What Nexus Already Has (from recent implementation)

| Pattern | Location | Notes |
|---------|----------|-------|
| Approval system | `internal/agent/approval.go` | Allowlist, denylist, safe_bins, skill_allowlist, ask fallback |
| Compaction with memory flush | `internal/agent/compaction.go` | Context monitoring, flush prompts, confirmation flow |
| Sub-agent spawn tool | `internal/tools/subagent/spawn.go` | Manager with concurrency controls, announce callback |
| Tool cancellation | `internal/tools/jobs/cancel.go` | Cancel running jobs, context propagation |
| Job persistence | `internal/jobs/cockroach.go` | DB storage with pruning |
| Tool policy resolver | `internal/tools/policy/resolver.go` | Per-agent policies |
| Memory search | `internal/tools/memorysearch/` | Lexical/vector/hybrid search |
| Session store | `internal/sessions/` | Memory + CockroachDB backends |
| Plugin system | `internal/plugins/` | Manifest discovery, lazy loading |
| Skills | `internal/skills/` | Parser, gating, types |
| Workspace bootstrap | `internal/workspace/bootstrap.go` | AGENTS/SOUL/USER/IDENTITY/MEMORY files |
| System prompt assembly | `internal/gateway/system_prompt.go` | Identity, safety, tool notes injection |

## High-Priority Patterns - IMPLEMENTED ✅

### 1. Event-Driven Hooks System ✅

**Status:** Implemented at `internal/hooks/types.go`

**Implementation:**
- Event types: `message.*`, `session.*`, `command.*`, `tool.*`, `agent.*`, `gateway.*`
- Hook registration with priority and filtering
- Async triggering via registry
- Gateway lifecycle hooks (startup/shutdown)

### 2. Broadcast Groups (Multi-Agent Message Processing) ✅

**Status:** Implemented at `internal/gateway/broadcast.go`

**Implementation:**
- `BroadcastManager` with parallel/sequential strategies
- `BroadcastGroup` configuration per-peer
- Session isolation per agent
- Config: `gateway.broadcast.groups` and `gateway.broadcast.strategy`

### 3. Advanced Session Scoping ✅

**Status:** Implemented at `internal/sessions/scoping.go` and `internal/config/config.go`

**Implementation:**
- `DMScope`: `main`, `per-peer`, `per-channel-peer`
- `IdentityLinks` with `SessionKeyBuilder.ResolveIdentity()`
- Reset modes: `never`, `daily`, `idle`, `daily+idle`
- Per-type and per-channel reset overrides
- Config validation for scope settings

### 4. Tool Groups and Profiles ✅

**Status:** Implemented at `internal/tools/policy/groups.go`

**Implementation:**
- Groups: `group:runtime`, `group:fs`, `group:sessions`, `group:memory`, `group:ui`, `group:automation`, `group:messaging`
- Profiles: `coding`, `messaging`, `readonly`, `full`, `minimal`
- `ExpandGroups()` function for pattern expansion
- Integration with tool policy resolver

### 5. Multi-Agent Sandbox Modes ✅

**Status:** Implemented at `internal/tools/sandbox/modes.go` and `internal/config/config.go`

**Implementation:**
- Modes: `off`, `all`, `non-main`
- Scopes: `agent`, `session`, `shared`
- `ShouldSandbox(agentID, isMainAgent)` helper
- `SandboxKey(agentID, sessionID)` for isolation
- Config: `tools.sandbox.mode`, `tools.sandbox.scope`, `tools.sandbox.workspace_root`, `tools.sandbox.workspace_access`

## Medium-Priority Patterns

### 6. Session Origin Metadata

Track where sessions came from for debugging and UI display:
- `label`: Human-readable label
- `provider`: Channel
- `from`/`to`: Routing IDs
- `accountId`: Provider account
- `threadId`: Thread/topic ID

### 7. Runtime Commands ✅

**Status:** Implemented at `internal/commands/builtin.go`

**Implemented commands:**
- `/new [model]` - Start new session with optional model
- `/reset`, `/clear` - Aliases for /new
- `/status` - Show session status
- `/context [list|detail]` - Show system prompt contents
- `/stop`, `/abort`, `/cancel` - Abort current run
- `/compact`, `/summarize` - Manual compaction
- `/send [on|off|inherit]` - Override send policy
- `/model [name]` - Show or change model
- `/think [budget|off]` - Extended thinking control
- `/memory [query]` - Memory search
- `/undo` - Undo last message
- `/whoami` - Show sender identity
- `/help [command]` - Command help

### 8. Session Pruning (vs Compaction) ✅

**Status:** Implemented at `internal/agent/context/pruning.go` and `internal/agent/runtime.go`

Implementation notes:
- Prunes tool results in-memory per request (no persistence)
- Keeps recent assistant turns and protects bootstrap messages
- Soft trims oversized tool outputs before hard clearing

### 9. Elevated Mode ✅

**Status:** Implemented at `internal/gateway/elevated.go`

**Implementation:**
- Global `tools.elevated.enabled` toggle
- `tools.elevated.allow_from` per-channel sender allowlists
- `tools.elevated.tools` list of elevated tool names
- Directive parsing in messages (`/elevate`, etc.)
- Permission resolution with sender matching
- Security audit warnings for missing allowlists

## Low-Priority / Future Patterns

### 10. Hook Packs (npm-style)

Allow installing hooks as packages with dependencies.

### 11. WebChat and Remote Mode

UI clients querying gateway for session state.

### 12. Telegram Forum Topic Support ✅

**Status:** Implemented in Telegram adapter + gateway session routing.

## Implementation Status Summary

| Pattern | Status | Location |
|---------|--------|----------|
| Event-Driven Hooks | ✅ Done | `internal/hooks/` |
| Broadcast Groups | ✅ Done | `internal/gateway/broadcast.go` |
| Session Scoping | ✅ Done | `internal/sessions/scoping.go` |
| Tool Groups | ✅ Done | `internal/tools/policy/groups.go` |
| Sandbox Modes | ✅ Done | `internal/tools/sandbox/modes.go` |
| Runtime Commands | ✅ Done | `internal/commands/builtin.go` |
| Elevated Mode | ✅ Done | `internal/gateway/elevated.go` |
| Security Audits | ✅ Done | `internal/doctor/security_audit.go` |
| AgentDir Collision | ✅ Done | `internal/multiagent/config.go` |
| Telegram Forums | ✅ Done | `internal/channels/telegram/adapter.go` |
| Session Pruning | ✅ Done | `internal/agent/context/pruning.go` |
| Hook Packs | ⏳ Future | - |

## Remaining Work

1. **Hook Packs** - npm-style hook package installation

## Notes

- All high-priority Clawdbot patterns have been adopted
- Nexus has strong foundations (approval, compaction, sub-agents, persistence)
- Security audits extended with channel/elevated/sandbox checks
- Multi-agent support is comprehensive with broadcast groups and sandbox isolation
