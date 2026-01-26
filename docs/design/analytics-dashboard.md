# Analytics Dashboard

## Summary
Conversation analytics and insights dashboard.

## Goals
- Track usage, latency, quality, and cost metrics
- Provide per-agent and per-channel breakdowns
- Support export for offline analysis

## Non-goals
- Advanced BI tooling in the first iteration

## Proposed Design
- Aggregate metrics from existing observability pipeline
- Store timeseries in a metrics store
- Expose read-only API endpoints for dashboard UI

## Open Questions
- Which metrics are most critical for MVP dashboard?
