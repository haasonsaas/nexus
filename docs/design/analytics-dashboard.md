# Analytics Dashboard

## Summary
Conversation analytics and insights dashboard.

## Goals
- Provide a lightweight “what’s happening?” view of conversations and tool usage.
- Support quick time-window filtering (e.g. 24h/7d/30d).
- Make metrics available via a stable JSON API (UI can render via server templates or htmx).

## Non-goals
- Advanced BI tooling in the first iteration

## MVP (Implemented)

### UI
- `/ui/analytics` shows an overview panel with:
  - Conversations (distinct sessions with messages in window)
  - Messages
  - Avg messages / conversation
  - Tool calls / results and error rate
  - Top tools
  - Messages per day

### API
- `/ui/api/v1/analytics/overview?period=7d&agent=main`
  - Returns JSON by default.
  - If `HX-Request: true`, returns the `analytics/overview.html` partial (for htmx refresh).

### Data Sources
- Primary (when available): SQL aggregation over `sessions` + `messages`.
- Tool stats: parsed from `messages.tool_calls` and `messages.tool_results` JSONB.
- Fallback (non-SQL stores): best-effort scan via `sessions.Store.List` + `GetHistory` (may be incomplete if the store enforces history limits).

## Metrics (MVP semantics)

- **Conversations**: count of distinct `session_id` with at least one message in `[since, until)`.
- **Messages**: count of messages in `[since, until)`.
- **Avg messages / conversation**: `messages / conversations` (0 when conversations = 0).
- **Tool calls**: total tool calls found in `messages.tool_calls` within the window.
- **Tool results**: total tool results found in `messages.tool_results` within the window.
- **Tool error rate**: `tool_errors / tool_results` (percent).

## Next Iterations

1) Persist tool calls/results into dedicated tables for faster analytics queries.
2) Add per-channel breakdowns and latency metrics (from tracing/observability).
3) Topic extraction (embeddings + clustering), export endpoints, and retention policies.

## Open Questions
- Which metrics are most critical for MVP dashboard?
