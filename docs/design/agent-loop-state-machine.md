# Agent Loop State Machine

## Summary
Define an explicit agent loop state machine with checkpoints, parallel tool execution, and crash recovery.

## Goals
- Model loop phases (plan, act, observe, reflect) with explicit state transitions
- Persist checkpoints to resume after crashes or restarts
- Allow safe parallel tool execution with deterministic ordering

## Non-goals
- Build a UI for state visualization (tracked elsewhere)

## Proposed Design
- Introduce a LoopState struct persisted per session/iteration
- Checkpoint at phase boundaries and after tool result aggregation
- Use stable tool-call IDs and ordered merge rules for parallelism
- Replay from latest checkpoint with idempotent tool execution guards

## Open Questions
- Do we need versioned checkpoint schemas for backward compatibility?
- What is the policy for retrying failed tools during recovery?
