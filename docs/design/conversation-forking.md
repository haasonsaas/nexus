# Conversation Forking

## Summary
Parallel exploration via session branches.

## Goals
- Fork a session at a branch point
- Allow parallel exploration with branch-aware memory
- Provide merge tooling for selected branches

## Non-goals
- Real-time multi-user branch editing

## Proposed Design
- Use existing session branch store as backing model
- Expose APIs to create, list, and merge branches
- Track branch lineage and metadata

## Open Questions
- How should merges reconcile tool outputs and memory writes?
