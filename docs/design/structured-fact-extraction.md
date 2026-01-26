# Structured Fact Extraction

## Summary
Schema-driven extraction of facts beyond summarization.

## Goals
- Extract structured entities and relationships from conversations
- Attach confidence and provenance to each extracted fact
- Store facts into memory for retrieval and consolidation

## Non-goals
- Replacing existing summarization pipeline

## Proposed Design
- Define extraction schemas per domain (contacts, tasks, prefs)
- Run extraction on compaction and explicit tool calls
- Track provenance with message IDs and timestamps

## Open Questions
- How to resolve conflicts between extracted facts and existing memory?
