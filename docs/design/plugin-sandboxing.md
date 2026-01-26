# Plugin Sandboxing Design

## Overview

This document addresses issue #97: isolating third-party plugin execution.

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

- `nexus.plugin.json` supports an optional `tools` allowlist.
- Channel/tool registration rejects undeclared capabilities when an allowlist is present.
  - Empty allowlists continue to allow all (backwards compatible).

This does **not** provide strong isolation (plugins are still in-process), but it reduces surprise and
makes “what this plugin can do” explicit and enforceable.

### Sandbox Wrapper
Wrap plugin entrypoints in a sandbox runner that:
- Starts the plugin in a container/VM
- Mounts only approved paths
- Applies network constraints
- Enforces CPU/memory/timeouts

### Config
Add a dedicated plugin isolation config (avoid colliding with legacy `plugins.sandbox -> tools.sandbox` migrations):
- `enabled`
- `backend` (docker/firecracker)
- `network_enabled`
- `limits` (cpu, memory, timeout)

### Failure Modes
- If sandbox fails to start, skip plugin and log a warning.
- Support allowlist to force sandbox for untrusted plugins.

---

## Rollout

1. Enforce manifest-declared capabilities (tools/channels) for runtime plugin registration. ✅
2. Add config + runner abstraction.
3. Implement Docker backend (out-of-process plugin host).
4. Extend with Firecracker backend + snapshots (reuse existing sandbox pool patterns).
