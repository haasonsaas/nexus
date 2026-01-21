# Competition Notes (Clawd + Clawdbot)

## Sources reviewed
- Clawd workspace templates (`AGENTS.md`, `SOUL.md`, `USER.md`, `IDENTITY.md`, `TOOLS.md`, `HEARTBEAT.md`).
- Clawdbot docs: agent workspace + memory concepts, gateway doctor behavior.
- GitHub API snapshot (2026-01-21): stars, issues, topics, recent updates.

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

## Nexus adoption status (high-level)
- Implemented: strict config validation, doctor CLI, system prompt assembly, heartbeat + tool-notes injection, memory log recall, runtime plugin loader.
- Implemented (this iteration): workspace bootstrap file loading into the system prompt; docs updated to match strict config.
- Implemented: doctor repair/migration pipeline (config migrations + workspace repairs) and channel health probes.
- Implemented: service/daemon audits, memory search tool, and memory flush reminders.
- Pending: vector/semantic memory search, automated compaction + post-flush confirmation.

## Follow-up ideas (small/medium scope)
1. Service install/repair workflows (launchd/systemd writers + restart hooks).
2. Vector memory search + compaction triggers with automatic flush confirmations.

## GitHub snapshot (2026-01-21)
- Stars: 5,761 · Forks: 891 · Open issues: 151
- Topics: ai, assistant, clawd, crustacean, own-your-data, personal
- Default branch: main · License: MIT
- Recent commit themes: tighten exec allowlist gating, heartbeat active hours, cache TTL/pruning, channel-specific session policies, and port listener hardening.
