# Competition Notes (Clawd + Clawdbot)

## Sources reviewed
- Clawd workspace templates (`AGENTS.md`, `SOUL.md`, `USER.md`, `IDENTITY.md`, `TOOLS.md`, `HEARTBEAT.md`).
- Clawdbot docs: agent workspace + memory concepts, gateway doctor behavior.

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
- Pending: doctor repair/migration pipeline, channel health probes, service/daemon audits, memory search + pre-compaction flush.

## Follow-up ideas (small/medium scope)
1. Doctor repair mode: add `--fix/--repair` to apply config migrations and common file repairs.
2. Health probes: add a `doctor --probe` that hits running gateway endpoints and reports channel status.
3. Workspace bootstrap CLI: `nexus setup --workspace` to seed AGENTS/SOUL/USER/IDENTITY/TOOLS files.
4. Memory file support: optional `MEMORY.md` ingestion + daily log retention policies.
