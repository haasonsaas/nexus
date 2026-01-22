# Nexus Iteration Plan (2026-01-22, Iteration 3)

## Context
- Pull latest main and address CI failures (tests + gofmt).
- Adopt Clawdbot command patterns: allowlisted command-only and inline shortcuts.
- Add local developer ergonomics (lint target, docs/examples).
- Re-check Claude CLI timeouts and document mitigations.

## Goals
1) CI green after the large test drop (approval defaults, race fix, gofmt).
2) Command allowlists + inline shortcuts match Clawdbot semantics.
3) Update config examples + docs to reflect new commands config.
4) Keep changes test-driven where possible.

## Workstreams
### A) CI Stabilization
- Fix approval default decision behavior for partial policies.
- Remove data race in executor retry/cancel test.
- Run gofmt on newly added tests.

### B) Command Controls
- Add `commands` config (enabled, allow_from, inline_allow_from, inline_commands).
- Implement command-only allowlist gating.
- Implement inline command shortcuts with stripping.
- Share allowlist normalization between elevated and commands.
- Tests: allowlist gating + inline command stripping.

### C) Docs & Examples
- Update `nexus.example.yaml` for commands section.
- Update `docs/components.md` + `docs/deployment.md`.

## TDD Targets
- Inline command execution removes tokens and still runs runtime.
- Inline commands blocked when not allowlisted.
- Command-only messages ignored when not allowlisted.

## Validation
- gofmt on touched files.
- `go test ./internal/agent ./internal/gateway` at minimum.

## Notes
- Claude CLI timeouts: likely command timeout defaults; confirm by running with longer `timeout_ms` and smaller prompts.
