# Debugging a Failed Run

This guide shows how to capture and inspect a single agent run end‑to‑end.

## 1) Enable trace capture

Set a trace directory before starting the server:

```bash
export NEXUS_TRACE_DIR=./traces
```

The gateway will write one JSONL trace per run to that directory (e.g. `run_id.jsonl`).

## 2) Reproduce the failure

Run your command as usual (API call, CLI, or channel message). This will emit a run trace.

## 3) Find the run ID

The run ID is generated as:

```
<session_id>-<message_id>
```

You can often find it in structured logs, tool events, or the trace header itself.

## 4) Inspect the timeline

Use the CLI to view the timeline:

```bash
nexus events show <run_id> --trace-dir ./traces
```

Or return JSON:

```bash
nexus events show <run_id> --trace-dir ./traces --format json
```

## 5) Replay raw trace events

If you want the raw event stream:

```bash
nexus trace replay ./traces/<run_id>.jsonl
```

## Notes

- Trace files are written synchronously for crash safety.
- If `NEXUS_TRACE_DIR` is not set, `nexus events show` falls back to the in‑memory event store.
