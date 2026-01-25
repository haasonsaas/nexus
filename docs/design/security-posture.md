# Continuous Security Posture Design

## Overview

This document defines a continuous security posture system for Nexus. It addresses issue #112 by adding periodic audits, a posture monitor, and optional auto-remediation to reduce risk in production deployments.

## Goals

1. **Continuous monitoring**: Periodically audit config and filesystem for risks.
2. **Actionable findings**: Emit structured findings with remediation guidance.
3. **Low overhead**: Lightweight checks with configurable intervals.
4. **Optional auto-remediation**: Apply safe, reversible runtime mitigations.
5. **Observable**: Emit events and log posture snapshots.

## Non-Goals

- Full IDS/IPS or real-time malware detection.
- Automatic destructive remediations (e.g., deleting files).
- External policy enforcement (OPA, Kubernetes admission) in this phase.

---

## 1. Architecture

```
          ┌────────────────────────────┐
          │   Security Posture Worker  │
          │  - periodic audit runner   │
          │  - event emission          │
          │  - optional remediation    │
          └───────────────┬────────────┘
                          │
                 ┌────────▼────────┐
                 │ Security Audits │
                 │ config/fs/gw    │
                 └────────┬────────┘
                          │
                 ┌────────▼────────┐
                 │ Observability   │
                 │ events/logs     │
                 └─────────────────┘
```

---

## 2. Configuration

```yaml
security:
  posture:
    enabled: true
    interval: 10m
    include_filesystem: true
    include_gateway: true
    include_config: true
    check_symlinks: true
    allow_group_readable: false
    emit_events: true
    auto_remediation:
      enabled: true
      mode: lockdown # lockdown | warn_only
```

### 2.1 Derived Config

```go
type SecurityConfig struct {
    Posture SecurityPostureConfig `yaml:"posture"`
}

type SecurityPostureConfig struct {
    Enabled             bool          `yaml:"enabled"`
    Interval            time.Duration `yaml:"interval"`
    IncludeFilesystem   bool          `yaml:"include_filesystem"`
    IncludeGateway      bool          `yaml:"include_gateway"`
    IncludeConfig       bool          `yaml:"include_config"`
    CheckSymlinks       bool          `yaml:"check_symlinks"`
    AllowGroupReadable  bool          `yaml:"allow_group_readable"`
    EmitEvents          bool          `yaml:"emit_events"`
    AutoRemediation     RemediationConfig `yaml:"auto_remediation"`
}

type RemediationConfig struct {
    Enabled bool   `yaml:"enabled"`
    Mode    string `yaml:"mode"` // lockdown | warn_only
}
```

---

## 3. Remediation Behavior

**Lockdown mode** (safe, reversible, runtime-only):
- Require approvals for all tool calls.
- Disable elevated bypass.
- Emit an event describing the applied mitigation.

**Warn-only mode**:
- No runtime changes; only logs/events.

Remediation is intentionally conservative and does **not** modify files or write to the config on disk.

---

## 4. Events

Emit a custom event with:
- `summary` counts (critical/warn/info)
- Top findings (check_id, severity, remediation)
- `remediation_applied` flag

---

## 5. Phased Delivery

**Phase 1 (this iteration)**:
- Background posture worker using existing `internal/security` audit.
- Configurable interval + event emission.
- Lockdown-style auto-remediation (runtime only).

**Phase 2**:
- Historical posture retention (DB).
- Remediation policies per environment (dev/staging/prod).
- Alerting integrations (webhooks, Slack, PagerDuty).

