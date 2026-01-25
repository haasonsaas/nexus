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

### Sandbox Wrapper
Wrap plugin entrypoints in a sandbox runner that:
- Starts the plugin in a container/VM
- Mounts only approved paths
- Applies network constraints
- Enforces CPU/memory/timeouts

### Config
Add `plugins.sandbox` config:
- `enabled`
- `backend` (docker/firecracker)
- `network_enabled`
- `limits` (cpu, memory, timeout)

### Failure Modes
- If sandbox fails to start, skip plugin and log a warning.
- Support allowlist to force sandbox for untrusted plugins.

---

## Rollout

1. Add config + runner abstraction.
2. Implement Docker backend.
3. Extend with Firecracker backend.
