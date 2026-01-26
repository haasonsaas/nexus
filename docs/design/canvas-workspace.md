# Canvas Workspace

## Summary
Canvas as an agent workspace for visual reasoning and collaboration.

## Goals
- Represent a shared canvas with nodes, edges, and artifacts
- Allow agents to read/write canvas state during a session
- Support collaboration and live updates

## Non-goals
- Full whiteboard UI implementation in this phase

## Proposed Design
- Store canvas state as a versioned document tied to session
- Expose tools to add/update nodes, edges, and attachments
- Emit change events for real-time sync

## Open Questions
- Should canvas history be stored in sessions or a separate store?
