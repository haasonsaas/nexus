# How to Debug a Failed Run

This guide explains how to trace and debug issues when agent runs fail or behave unexpectedly.

## Quick Start

```bash
# Show events for a specific run
nexus events show run_abc123

# List recent events
nexus events list --limit 20

# Filter to specific event types
nexus events list --type tool.error
nexus events list --type llm.error

# Filter by session
nexus events list --session sess_xyz789
```

## Understanding Event Types

Nexus records events throughout agent execution with correlation IDs for tracing:

### Run Events
- `run.start` - Agent run started
- `run.end` - Agent run completed successfully
- `run.error` - Agent run failed with error

### Tool Events
- `tool.start` - Tool execution started
- `tool.end` - Tool execution completed
- `tool.error` - Tool execution failed
- `tool.progress` - Tool progress update (for long-running tools)

### LLM Events
- `llm.request` - Request sent to LLM provider
- `llm.response` - Response received from LLM
- `llm.error` - LLM request failed

### Edge Events
- `edge.connect` - Edge daemon connected
- `edge.disconnect` - Edge daemon disconnected
- `edge.heartbeat` - Edge heartbeat received

### Approval Events
- `approval.required` - Tool requires user approval
- `approval.decided` - Approval decision made

## Correlation IDs

Every event includes correlation IDs for tracing across boundaries:

- `run_id` - Unique ID for this agent run/turn
- `session_id` - Session ID (conversation context)
- `tool_call_id` - ID of the specific tool call
- `edge_id` - ID of the edge daemon (for edge tools)
- `agent_id` - ID of the agent configuration
- `message_id` - ID of the triggering message

## Debugging Workflow

### 1. Identify the Failed Run

If you have a session ID from the error context:
```bash
nexus events list --session sess_xyz789 --limit 50
```

This shows all events for that session, helping you identify which run failed.

### 2. View the Timeline

Once you have the run ID:
```bash
nexus events show run_abc123
```

This displays a formatted timeline showing:
- Total duration
- Event counts by type
- Chronological event list with timestamps
- Error messages and their context

### 3. Investigate Specific Events

Filter to the event types relevant to your issue:

```bash
# Tool execution problems
nexus events list --type tool.error

# LLM issues (rate limits, API errors)
nexus events list --type llm.error

# Edge daemon connectivity
nexus events list --type edge.disconnect
```

### 4. Check Edge Daemon Status

If the issue involves edge tools:
```bash
# Check if edges are connected
nexus edge list

# Check specific edge status
nexus edge status edge_id

# List available edge tools
nexus edge tools
```

## Common Issues

### Tool Timeouts

**Symptoms**: `tool.error` event with timeout message

**Debug**:
```bash
nexus events show run_xxx --format json | jq '.events[] | select(.type == "tool.error")'
```

**Common causes**:
- Network issues to edge daemon
- Tool taking longer than configured timeout
- Edge daemon overloaded

### LLM Rate Limits

**Symptoms**: `llm.error` with rate limit message

**Debug**:
```bash
nexus events list --type llm.error --limit 10
```

**Solutions**:
- Check provider dashboard for quota
- Implement request backoff
- Consider using a different provider

### Edge Disconnections

**Symptoms**: Tools fail with "edge not found" or similar

**Debug**:
```bash
nexus edge list
nexus events list --type edge.disconnect
```

**Solutions**:
- Check edge daemon logs
- Verify network connectivity
- Restart edge daemon if needed

## Using JSONL Traces

For detailed low-level debugging, use JSONL trace files:

```bash
# Validate trace structure
nexus trace validate ./traces/run_abc.jsonl

# View statistics
nexus trace stats ./traces/run_abc.jsonl

# Replay events
nexus trace replay ./traces/run_abc.jsonl --speed 0
```

## Getting Help

If you can't resolve the issue:

1. Export the event timeline: `nexus events show run_xxx --format json > debug.json`
2. Check the logs: `journalctl -u nexus -n 100`
3. Open an issue with the event timeline and logs

## See Also

- [Observability Overview](./overview.md#observability)
- [Edge Daemon Guide](./components.md#edge-daemon)
- [Tool Execution](./components.md#tool-execution)
