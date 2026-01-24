# Canvas RFC: Centralized Canvas for Slack

## Summary
A centralized, Slack-first canvas surface gives teams a shared, live workspace that agents can update and users can interact with. The canvas is session-scoped (channel or thread), secured with signed links, and backed by a persistent state + event log.

## Goals
- Provide a shared, live workspace per Slack channel/thread.
- Allow agents to push updates and the UI to send actions back.
- Support 500+ employees with predictable performance and auditability.
- Enforce role-based access control and signed, expiring access links.

## Non-goals
- A full-fledged dashboard builder or BI system.
- Permanent document editing or file storage.

## Use cases
- Incident/status dashboards (live updates, timeline, owners).
- Workflow tracking (checklists, approvals, escalations).
- On-call handoffs (live runbooks + current state).
- Team-specific context (shared notes, active tasks).

## Canvas session model
A **canvas session** is the unit of state + event history and maps to a Slack context.

### Mapping rules
- **Default**: one canvas session per Slack channel.
- **Thread override**: if a canvas is opened from a thread, use a session keyed by `channel_id + thread_ts`.
- **Workspace**: include Slack workspace ID in the key to avoid collisions.

### Session key format
```
slack:{workspace_id}:{channel_id}[:thread_ts]
```

### Lifecycle
- Created on first access (slash command or link open).
- Persisted with state snapshot + event log.
- Automatically expires after inactivity based on retention configuration.

## Authentication and authorization
### Access model
- Canvas endpoints require authentication plus session token validation when configured.
- Canvas links are signed, expiring URLs (HMAC) scoped to a session and role.
- Canvas host requires either `canvas.tokens.secret` or `auth` (JWT/API keys) to be configured.
- Tokens embed session id, workspace id, channel/thread scope, and role.

### Roles
- **Viewer**: read-only access.
- **Editor**: can submit canvas actions.
- **Admin**: can manage sessions and revoke tokens.

### Role assignment
- Primary source: configuration (by Slack workspace and user).
- Optional future: dynamic roles from an identity provider.

Example role configuration:
```
channels:
  slack:
    canvas:
      default_role: editor
      workspace_roles:
        T1234567890: admin
      user_roles:
        T1234567890:
          U1234567890: viewer
```

## Data model and persistence
- **canvas_sessions**: id, workspace, channel, thread, created_at, updated_at
- **canvas_state**: session_id, state_json, updated_at
- **canvas_events**: session_id, type, payload_json, created_at

State is stored as a full JSON snapshot, and events are appended for replay.

### Retention
- State snapshot retained for N days (default 30).
- Event log retained for M days (default 7) or capped by size.

## API surface
### Canvas host
- Session-specific routes:
  - `/__nexus__/canvas/{session_id}/...`
- Auth middleware validates token and enforces role.

### Realtime updates
- Stream endpoint:
  - `/__nexus__/canvas/api/stream?session=...&token=...`
- Message envelope:
```
{ "type": "state" | "event" | "reset", "session_id": "...", "payload": {}, "ts": "..." }
```

### UI actions
- Action endpoint:
  - `POST /__nexus__/canvas/api/action`
- Payload includes action name, source component id, and context.

## Slack entrypoints
- `/canvas` slash command replies with an ephemeral link to the canvas for the current channel/thread.
- Optional message shortcut for threads.

## Scale assumptions
- Up to 500 concurrent viewers per workspace.
- Update cadence: 1–5 updates/sec per session.
- UI should handle bursts with backpressure and server-side throttling.

## Metrics and audit
- Metrics: active viewers, updates/sec, actions/sec, stream fanout.
- Audit events for all state changes and user actions.

## Security review checklist
- [ ] Signed token validation on every canvas endpoint.
- [ ] Path traversal protection for session-based routing.
- [ ] Role enforcement for read/write operations.
- [ ] Audit log contains user, session, action, and timestamp.
- [ ] Configurable retention and data deletion.
- [ ] Rate limiting on action endpoints.

## Open questions (resolved for MVP)
- **Session mapping**: default channel-level, thread-specific override.
- **Auth model**: signed URLs + server-side RBAC.
- **Persistence**: JSON snapshots + append-only event log.
