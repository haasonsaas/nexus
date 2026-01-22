# Competition Notes (Clawd + Clawdbot)

## Sources reviewed
- Clawd workspace templates (`AGENTS.md`, `SOUL.md`, `USER.md`, `IDENTITY.md`, `TOOLS.md`, `HEARTBEAT.md`).
- Clawdbot docs: agent workspace + memory concepts, gateway doctor behavior.
- Clawdbot docs: `docs/tools/skills-config.md` (skills config schema + watchers).
- Clawdbot docs: `docs/multi-agent-sandbox-tools.md` (per-agent sandbox + tool allow/deny).
- Clawdbot memory sources: `src/memory/embeddings-openai.ts`, embedding batch tests, vector cache tables.
- GitHub API snapshot (2026-01-22): stars, issues, topics, recent updates.
- GitHub API docs (2026-01-22): exec approvals, sub-agents, skills config.
- GitHub API docs (2026-01-22): plugin manifest schema + per-agent sandbox/tool policy docs.

## Clawd workspace patterns (what they do)
- Workspace = agent home. Standard files seed behavior, persona, user profile, tool notes, and heartbeat checklist.
- `memory/YYYY-MM-DD.md` = daily log; `MEMORY.md` = curated long-term memory.
- Heartbeat checklists are tiny and only report changes; reply `HEARTBEAT_OK` if nothing new.
- Bootstrap ritual (`BOOTSTRAP.md`) creates identity + user profile, then deletes itself.

## Clawdbot patterns that matter for Nexus
- **Doctor-first health and migrations**: strict config validation, doctor-only migrations, and auto doctor dry-run on startup.
- **Repair modes**: `doctor --repair`, `--yes`, `--deep`, optional update-before-doctor for git installs.
- **State integrity checks**: workspace location, permissions, session transcripts, missing directories.
- **Gateway diagnostics**: channel status probes, service config audits, port collision checks, runtime best practices.
- **Memory system**: daily logs + optional long-term memory file, optional vector search, pre-compaction memory flush.
- **Embeddings pipeline**: OpenAI-compatible embeddings with default `text-embedding-3-small`, `/v1/embeddings` base URL normalization, batching + retry logic, and on-disk/DB cache tables.
- **Skills config**: allowlist for bundled skills, extraDirs + watch debounce, install preferences, per-skill enabled/env overrides.
- **Exec approvals**: per-agent allowlists, ask fallback when UI is unavailable, safe-bins for stdin-only tools, and auto-allow skill CLIs.
- **Sub-agents**: `sessions_spawn` tool with announce step, per-agent auth resolution, and concurrency controls.
- **Profiles**: profile-derived paths for daemon/service configs (`CLAWDBOT_PROFILE`) and auth profiles file protections.
- **Multi-agent sandboxing**: per-agent sandbox overrides + per-agent tool allow/deny lists.
- **Per-agent auth isolation**: each agent reads its own `auth-profiles.json` under `~/.clawdbot/agents/<agentId>/agent/`.
- **Plugin manifests**: required JSON schema even for empty config; manifest missing/invalid blocks config validation.

## Nexus adoption status (high-level)
- Implemented: strict config validation, doctor CLI, system prompt assembly, heartbeat + tool-notes injection, memory log recall, runtime plugin loader.
- Implemented (this iteration): workspace bootstrap file loading into the system prompt; docs updated to match strict config.
- Implemented: doctor repair/migration pipeline (config migrations + workspace repairs) and channel health probes.
- Implemented: service/daemon audits, memory search tool, and memory flush reminders.
- Implemented: remote embeddings + cache for memory search, service auto-restart hooks, profile/skills/channel login CLI.
- Implemented: tool lifecycle events, tool retry/timeout controls, async jobs with `job_status`, and approval pattern gating.
- Pending: automated compaction + post-flush confirmation.
- Pending: exec allowlist + ask-fallback approvals, sub-agent spawn/announce flow, job persistence.

## Follow-up ideas (small/medium scope)
1. Automated compaction triggers with post-flush confirmations.
2. Skill watcher + allowlist controls (bundle filtering, extraDirs, and per-skill env overrides).
3. Exec approvals with allowlists + safe-bins and UI fallback.
4. Sub-agent spawn tool + announce UX.
5. Profile-aware state directories + auth-profile permission audits.

## GitHub snapshot (2026-01-22)
- Stars: 5,854 · Forks: 898 · Open issues: 141
- Default branch: main · License: MIT
- Recent commit themes: tighten exec allowlist gating, heartbeat active hours, cache TTL/pruning, channel-specific session policies, and port listener hardening.

## Issue themes (open)
- Heartbeat UX (silent acknowledgments, schedule control, session scoping).
- Memory search reliability (embedding rate limits, extra paths).
- Session overflow/compaction edge cases (context limits, failover behavior).
- Tool call safety (loop detection, exec gating, secret scanning).
- Platform packaging (macOS build stability, universal binaries).
