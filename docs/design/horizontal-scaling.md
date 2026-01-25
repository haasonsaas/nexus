# Horizontal Scaling Architecture Design

## Overview

This document defines the horizontal scaling approach for Nexus gateways. It addresses issue #102 by introducing cluster identity, distributed session locks, and safe multi-gateway operation while preserving existing single-node defaults.

## Goals

1. **Multi-gateway support**: Allow multiple gateway instances to run against the same database.
2. **Session safety**: Prevent concurrent session processing across nodes.
3. **Minimal disruption**: Keep single-node defaults and opt-in scaling.
4. **Observable identity**: Surface node identity in health snapshots and logs.
5. **Configurable locking**: Provide tunable lock TTL/refresh/poll settings.

## Non-Goals

- Global service discovery or service mesh integration.
- Full leader election or active-active broadcast replication.
- Automatic sharding of sessions across nodes (future phase).

---

## 1. Architecture

```
      ┌────────────────────────────────────────────────────┐
      │                     CockroachDB                     │
      │  sessions / messages / session_locks / jobs / tasks │
      └────────────────────────────────────────────────────┘
                ▲                         ▲
                │                         │
      ┌─────────┴─────────┐     ┌─────────┴─────────┐
      │  Gateway Node A   │     │  Gateway Node B   │
      │  node_id: a1      │     │  node_id: b1      │
      └─────────┬─────────┘     └─────────┬─────────┘
                │                         │
                └───────── Session Locks ─┘
```

### 1.1 Key Components

- **Cluster config**: Enables multi-node mode and defines node identity.
- **Distributed session locks**: DB-backed lease table (`session_locks`).
- **Gateway lock bypass**: Allow multi-gateway when cluster enabled.
- **Observability**: node_id included in health and logs.

---

## 2. Configuration

```yaml
cluster:
  enabled: true
  node_id: "gateway-us-east-1a"
  allow_multiple_gateways: true
  session_locks:
    enabled: true
    ttl: 2m
    refresh_interval: 30s
    acquire_timeout: 10s
    poll_interval: 200ms
```

### 2.1 Derived Config

Add to `internal/config`:

```go
type ClusterConfig struct {
    Enabled                bool              `yaml:"enabled"`
    NodeID                 string            `yaml:"node_id"`
    AllowMultipleGateways  bool              `yaml:"allow_multiple_gateways"`
    SessionLocks           SessionLockConfig `yaml:"session_locks"`
}

type SessionLockConfig struct {
    Enabled         bool          `yaml:"enabled"`
    TTL             time.Duration `yaml:"ttl"`
    RefreshInterval time.Duration `yaml:"refresh_interval"`
    AcquireTimeout  time.Duration `yaml:"acquire_timeout"`
    PollInterval    time.Duration `yaml:"poll_interval"`
}
```

---

## 3. Distributed Session Locks

### 3.1 Table

```
session_locks(
  session_id TEXT PRIMARY KEY,
  owner_id   TEXT,
  acquired_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ
)
```

### 3.2 Lock Semantics

- Acquire uses an UPSERT with conditional update when the lock is expired.
- A lock is renewed periodically by the owner to avoid expiry during long runs.
- Unlock deletes the row only if the owner matches.

---

## 4. Behavior

1. **On inbound message**: gateway acquires distributed lock for the session.
2. **Processing**: lock is held + renewed during the run.
3. **Completion**: lock is released, allowing another node to process.

---

## 5. Phased Delivery

**Phase 1 (this iteration)**:
- Cluster config + node_id defaults.
- Distributed session lock implementation (DB-backed lease).
- Gateway lock bypass when cluster enabled.

**Phase 2**:
- Session routing (consistent hashing) and leader-style control plane.
- Cross-node broadcast of presence and metrics.

---

## 6. Risks

- Lease expiry during long runs: mitigated with renew loop.
- Misconfigured node_id collisions: default to hostname + random suffix.
- Multi-gateway without locks: warn and keep default off.

