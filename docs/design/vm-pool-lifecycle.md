# VM Pool Lifecycle Design

## Overview

This document defines lifecycle management for the Firecracker VM pool. It addresses issue #111 by introducing snapshot-aware warmup, idle recycling, and auto-maintenance of pool health.

## Goals

1. **Faster cold start**: Prefer snapshot-based boot when available.
2. **Predictable warm pool**: Maintain a minimum idle count.
3. **Safe recycling**: Retire VMs by uptime, exec count, and idle age.
4. **Configurable cadence**: Tune maintenance intervals and snapshot refresh.
5. **Observable health**: Expose pool stats and snapshot inventory.

## Non-Goals

- Full VM live migration across hosts (future).
- Hypervisor-level autoscaling (outside Nexus).

---

## 1. Architecture

```
               ┌──────────────────────────────────┐
               │        Firecracker Backend        │
               │  - Snapshot Manager               │
               │  - Overlay Manager                │
               └──────────────┬────────────────────┘
                              │
                        ┌─────▼─────┐
                        │  VM Pool  │
                        │ warmup    │
                        │ recycle   │
                        │ refresh   │
                        └─────┬─────┘
                              │
                     ┌────────▼────────┐
                     │   MicroVMs      │
                     └─────────────────┘
```

---

## 2. Configuration

```yaml
tools:
  sandbox:
    backend: firecracker
    pool_size: 3
    max_pool_size: 10
    min_idle: 2
    max_idle_time: 5m
    snapshots:
      enabled: true
      refresh_interval: 30m
      max_age: 6h
```

### 2.1 Derived Config

Add to `SandboxConfig`:

```go
type SandboxConfig struct {
    // existing fields...
    MinIdle        int           `yaml:"min_idle"`
    MaxIdleTime    time.Duration `yaml:"max_idle_time"`
    Snapshots      SnapshotConfig `yaml:"snapshots"`
}

type SnapshotConfig struct {
    Enabled          bool          `yaml:"enabled"`
    RefreshInterval time.Duration `yaml:"refresh_interval"`
    MaxAge           time.Duration `yaml:"max_age"`
}
```

---

## 3. Lifecycle Policies

### 3.1 Warmup
- Pre-create `pool_size` VMs at startup.
- If snapshots enabled and available, boot from snapshot first.

### 3.2 Recycling
Recycle VMs when any of the following are true:
- `exec_count >= max_exec_count`
- `uptime >= max_uptime`
- `idle_time >= max_idle_time`

### 3.3 Snapshot Refresh
- Maintain a recent snapshot per language.
- Refresh snapshot if older than `max_age`.
- Use idle VM for snapshotting to avoid impact on active runs.

---

## 4. Phased Delivery

**Phase 1 (this iteration)**:
- Persist snapshot metadata on disk.
- Snapshot-aware VM creation.
- Idle-time based recycling (last-used tracking).

**Phase 2**:
- Snapshot warm pool refresh scheduling with observability hooks.
- Image rotation + version pinning for reproducible builds.

---

## 5. Risks

- Snapshot staleness if refresh disabled; mitigate with max_age.
- Idle recycling thrash under bursty load; tune min_idle and max_idle_time.
- Snapshot availability only on Linux Firecracker environments.

