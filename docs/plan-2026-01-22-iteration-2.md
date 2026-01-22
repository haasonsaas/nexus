# Nexus Iteration Plan (2026-01-22, Iteration 2)

## Context
- Close remaining gaps from the recent feature drops (RAG, branching, marketplace).
- Incorporate Clawdbot learnings (GH docs) and Claude CLI review.
- Resolve Claude CLI timeout confusion and document practical mitigations.

## Inputs
- GH API review: `docs/plugins/manifest.md`, `docs/multi-agent-sandbox-tools.md`.
- Claude CLI review: RAG + marketplace gaps (2026-01-22).
- Existing Nexus shortcomings plan + design docs.

## Goals
1) RAG is usable out of the box (default parsers + scope-aware tools).
2) Branching is actually wired into runtime persistence and history.
3) Embedding dimension mismatches fail early with clear errors.
4) Marketplace store is safer on corrupted index + partial installs.
5) Claude CLI timeouts have a documented, practical mitigation.

## Workstreams
### A) RAG usability + correctness
- Register default parsers (text + markdown) once per manager.
- Resolve scope ID from session context in `document_search`.
- Stamp agent/session/channel metadata on `document_upload`.
- Add embedding dimension validation to store/search/update paths.

### B) Branch persistence wiring
- Add BranchStore to runtime and gateway initialization.
- Persist messages via BranchStore when configured.
- Ensure primary branch exists for new sessions.

### C) Marketplace resilience
- Protect against corrupted plugin index by backing up (not silently wiping).
- Add minimal cleanup on install failure.

### D) Claude CLI timeout mitigation
- Root cause: long `claude -p` prompts exceed default command timeout.
- Mitigation: increase command timeout (e.g., 180s+), split prompts, or use streaming output.
- Document in plan/notes for team repeatability.

## TDD Plan
- Add targeted failing tests:
  - `document_search` should set `ScopeID` from session context.
  - `document_upload` should stamp scope metadata from session context.
  - pgvector store should reject invalid embedding dimensions before DB ops.
  - runtime should persist messages via BranchStore when configured.

## Commit Plan
1) RAG scope + parser registration + tests.
2) BranchStore wiring + tests.
3) Marketplace index safety + docs note.

## Risks / Notes
- BranchStore changes touch runtime persistence; keep opt-in to avoid regressions.
- Marketplace install staging is a larger refactor; start with minimal safety.
- Claude CLI lacks explicit timeout flag; only wrapper timeout is adjustable.
