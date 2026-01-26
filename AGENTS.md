# Nexus Repository Agent Guide

## Overview
- Go monorepo for Nexus server, edge daemon, tools, and docs.
- Primary entrypoints: `cmd/nexus` and `cmd/nexus-edge`.
- Config example: `nexus.example.yaml`.

## Common Commands
- Run all tests: `go test ./...`
- Targeted tests: `go test ./internal/<package>`
- Format Go code: `gofmt -w <files>`

## Migrations
- Session/schema migrations live in `internal/sessions/migrations`.
- That directory is gitignored; use `git add -f` for new migrations.
- Apply DB migrations via `nexus migrate`.

## Conventions
- Keep changes minimal and backwards-compatible unless requested.
- Prefer smaller, testable increments with explicit config defaults.
- Update `nexus.example.yaml` when adding new config fields.

## Open Issues Plan
- The open-issues execution tracker lives in `docs/plan-2026-01-25-open-issues.md`.
- Design docs referenced there should be kept in sync with implementation progress.

## CI Notes
- `go test ./...` is the baseline expectation.
- Keep platform assumptions explicit (tests should not depend on host OS unless intended).
