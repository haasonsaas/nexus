# Tape System Evolution

## Summary
Diff-based replay and regression testing for agent sessions.

## Goals
- Record tape diffs between runs to identify regressions
- Enable deterministic replay for debugging
- Provide a CLI for replay and comparison

## Non-goals
- Automated UI diffing of external tools

## Proposed Design
- Define a tape format with stable event IDs and hashes
- Store diffs as minimal edits between consecutive tapes
- Replay engine rehydrates state then replays events

## Open Questions
- What inputs are considered nondeterministic and need mocking?
