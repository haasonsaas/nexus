# Plugin Sandboxing Design

## Overview

This document describes isolating third-party plugin execution.

## Goals

1. Isolate plugin execution to prevent filesystem or network abuse.
2. Provide configurable sandbox backends (Docker / Firecracker).
3. Preserve existing plugin API for compatibility.

## Non-Goals

- Full multi-tenant isolation (future).
- Automatic privilege escalation of plugins (explicit allow only).

---

## Proposed Architecture

### Phase 0: Capability Declaration + Enforcement (Implemented)
Runtime plugin registration is now gated by its manifest:

- `nexus.plugin.json` supports optional allowlists for:
  - `tools`
  - `channels`
  - `commands` (CLI command paths like `plugins.install`)
  - `services` (service IDs)
  - `hooks` (hook event types)
- Runtime registration rejects undeclared registrations when an allowlist is present.
  - Empty allowlists continue to allow all (backwards compatible).
  - `commands` is validated for nested subcommands too (declare each dotted path you register).
- Optional `capabilities.required` / `capabilities.optional` can additionally gate registrations with wildcard matching
  (e.g. `tool:*`, `cli:plugins.*`).

This does **not** provide strong isolation (plugins are still in-process), but it reduces surprise and
makes “what this plugin can do” explicit and enforceable.

See `docs/plugins.md` for manifest structure and allowlist details.

### Sandbox Wrapper
Wrap plugin entrypoints in a sandbox runner that:
- Starts the plugin in a container/VM
- Mounts only approved paths
- Applies network constraints
- Enforces CPU/memory/timeouts

### Config
Config lives under `plugins.isolation` (intentionally not `plugins.sandbox`):
- `enabled`
- `backend` (daytona | docker | firecracker)
- `network_enabled`
- `timeout`
- `limits` (`max_cpu`, `max_memory`)

### Failure Modes
- If sandbox fails to start, skip plugin and log a warning.
- Support allowlist to force sandbox for untrusted plugins.

Current implementation: Daytona backend supports tool-only runtime plugin execution; unsupported plugins are skipped with a warning. Enabling other backends will skip plugin loading (fail-closed) and log a warning.

---

## Rollout

1. Enforce manifest-declared capabilities (tools/channels/commands/services/hooks) for runtime plugin registration. ✅
2. Add config + runner abstraction. ✅ (config scaffold + loader selection; isolation flag fails closed when backend unavailable)
3. Implement Daytona backend (tool-only runtime plugin execution). ✅
4. Implement Docker backend (out-of-process plugin host).
5. Extend with Firecracker backend + snapshots (reuse existing sandbox pool patterns).
