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

## High-Priority Patterns to Adopt

### 1. Event-Driven Hooks System

**What Clawdbot has:**
- Event types: `command:new`, `command:reset`, `command:stop`, `agent:bootstrap`, `gateway:startup`, `tool_result_persist`
- HOOK.md metadata discovery in directories
- Hook packs (npm-style packages)
- Per-hook configuration and eligibility checking

**Why adopt:**
- Extends behavior without modifying core code
- Enables session memory snapshots, command logging, boot automation
- Clean separation of concerns

**Implementation plan:**
```go
// internal/hooks/types.go
type HookEvent struct {
    Type       string // "command", "agent", "gateway"
    Action     string // "new", "reset", "stop", "bootstrap", "startup"
    SessionKey string
    Timestamp  time.Time
    Context    HookContext
    Messages   []string // Push messages to send to user
}

type HookHandler func(ctx context.Context, event *HookEvent) error

type HookConfig struct {
    Name        string
    Description string
    Events      []string
    Requires    HookRequirements
    Always      bool
}
```

### 2. Broadcast Groups (Multi-Agent Message Processing)

**What Clawdbot has:**
- Multiple agents process the same message simultaneously
- Parallel (default) or sequential processing strategy
- Session isolation per agent
- Per-peer configuration (WhatsApp group → agent list)

**Why adopt:**
- Specialized agent teams (code reviewer + security auditor + docs generator)
- Multi-language support
- QA workflows (agent + reviewer)

**Implementation plan:**
```go
// internal/gateway/broadcast.go
type BroadcastConfig struct {
    Strategy string            // "parallel" or "sequential"
    Groups   map[string][]string // peer_id -> [agent_ids]
}

func (g *Gateway) processBroadcast(ctx context.Context, msg InboundMessage) error {
    agentIDs := g.cfg.Broadcast.Groups[msg.PeerID]
    if len(agentIDs) == 0 {
        return g.processNormal(ctx, msg) // fallback to normal routing
    }

    if g.cfg.Broadcast.Strategy == "sequential" {
        for _, agentID := range agentIDs {
            g.processForAgent(ctx, msg, agentID)
        }
    } else {
        var wg sync.WaitGroup
        for _, agentID := range agentIDs {
            wg.Add(1)
            go func(aid string) {
                defer wg.Done()
                g.processForAgent(ctx, msg, aid)
            }(agentID)
        }
        wg.Wait()
    }
    return nil
}
```

### 3. Advanced Session Scoping

**What Clawdbot has:**
- `dmScope`: `main` (all DMs share session), `per-peer`, `per-channel-peer`
- `identityLinks`: Map provider-prefixed peer IDs to canonical identity
- Reset policies: daily (at hour), idle (minutes), per-type, per-channel

**Why adopt:**
- Better control over session continuity
- Cross-channel identity linking
- Automatic session expiry

**Implementation plan:**
```go
// internal/config/session.go
type SessionScopeConfig struct {
    DMScope       string            // "main", "per-peer", "per-channel-peer"
    IdentityLinks map[string][]string // canonical_id -> [provider:peer_id, ...]
    Reset         SessionResetConfig
    ResetByType   map[string]SessionResetConfig // "dm", "group", "thread"
    ResetByChannel map[string]SessionResetConfig
}

type SessionResetConfig struct {
    Mode        string // "daily", "idle"
    AtHour      int    // for daily mode
    IdleMinutes int    // for idle mode
}
```

### 4. Tool Groups and Profiles

**What Clawdbot has:**
- Groups: `group:runtime` (exec, bash, process), `group:fs` (read, write, edit), `group:sessions`, `group:memory`, `group:ui`, `group:automation`, `group:messaging`
- Profiles: `coding`, `messaging`
- Per-provider tool restrictions

**Why adopt:**
- Simpler configuration (allow `group:fs` vs listing each tool)
- Consistent security profiles across agents
- Provider-specific restrictions

**Implementation plan:**
```go
// internal/tools/policy/groups.go
var ToolGroups = map[string][]string{
    "group:runtime":    {"exec", "bash", "process"},
    "group:fs":         {"read", "write", "edit", "apply_patch"},
    "group:sessions":   {"sessions_list", "sessions_history", "sessions_send", "sessions_spawn", "session_status"},
    "group:memory":     {"memory_search", "memory_get"},
    "group:ui":         {"browser", "canvas"},
    "group:automation": {"cron", "gateway"},
    "group:messaging":  {"message"},
}

func ExpandGroups(patterns []string) []string {
    var result []string
    for _, p := range patterns {
        if expanded, ok := ToolGroups[p]; ok {
            result = append(result, expanded...)
        } else {
            result = append(result, p)
        }
    }
    return result
}
```

### 5. Multi-Agent Sandbox Modes

**What Clawdbot has:**
- `mode`: `off`, `all`, `non-main`
- `scope`: `agent` (one container per agent), `session` (one per session), `shared`
- Per-agent sandbox overrides

**Why adopt:**
- Different security profiles for different agents
- Resource sharing options
- Main agent unsandboxed, others sandboxed

**Implementation plan:**
```go
// internal/config/sandbox.go
type SandboxConfig struct {
    Mode            string // "off", "all", "non-main"
    Scope           string // "agent", "session", "shared"
    WorkspaceRoot   string
    WorkspaceAccess string
    Docker          DockerSandboxConfig
    Prune           SandboxPruneConfig
}
```

## Medium-Priority Patterns

### 6. Session Origin Metadata

Track where sessions came from for debugging and UI display:
- `label`: Human-readable label
- `provider`: Channel
- `from`/`to`: Routing IDs
- `accountId`: Provider account
- `threadId`: Thread/topic ID

### 7. Runtime Commands

In-chat commands for session control:
- `/new [model]` - Start new session (optional model switch)
- `/reset` - Reset current session
- `/status` - Show context usage, toggles, cred freshness
- `/context list|detail` - Show system prompt contents
- `/stop` - Abort current run
- `/compact [instructions]` - Manual compaction
- `/send on|off|inherit` - Override send policy

### 8. Session Pruning (vs Compaction)

Separate in-memory tool result trimming from persistent compaction:
- Pruning: Trim old tool results per-request, doesn't persist
- Compaction: Summarize and persist

### 9. Elevated Mode

Sender-based allowlist for elevated tool access:
- Global `tools.elevated` baseline
- Per-agent `tools.elevated` override
- Can be disabled globally or per-agent

## Low-Priority / Future Patterns

### 10. Hook Packs (npm-style)

Allow installing hooks as packages with dependencies.

### 11. WebChat and Remote Mode

UI clients querying gateway for session state.

### 12. Telegram Forum Topic Support

Thread isolation for forum-style chats.

## Implementation Order Recommendation

1. **Tool Groups** - Quick win, improves config ergonomics
2. **Event Hooks** - Foundation for extensibility
3. **Broadcast Groups** - Enables multi-agent workflows
4. **Advanced Session Scoping** - Better session control
5. **Runtime Commands** - User-facing improvements
6. **Sandbox Modes** - Security flexibility
7. **Session Pruning** - Performance optimization
8. **Elevated Mode** - Fine-grained access control

## Notes

- Nexus already has strong foundations (approval, compaction, sub-agents, job persistence)
- Priority should be features that unlock new use cases (hooks, broadcast)
- Tool groups are a quick ergonomic win
- Session scoping improvements help with multi-user scenarios
