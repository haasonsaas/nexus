# Plugins

Nexus supports in-process runtime plugins (Go `.so`) that can register tools, channels, CLI commands, services, and hooks.

## Plugin Manifest (`nexus.plugin.json`)

Each plugin ships a manifest file (`nexus.plugin.json`, or legacy `clawdbot.plugin.json`) that describes metadata, configuration
schema, and (optionally) what the plugin is allowed to register.

Minimal example:

```json
{
  "id": "example.echo",
  "name": "Echo",
  "version": "1.0.0",
  "configSchema": {
    "type": "object",
    "properties": {
      "prefix": { "type": "string" }
    }
  }
}
```

### Registration Allowlists (Phase 0)

The manifest may include optional allowlists:

- `tools` (tool names)
- `channels` (channel IDs like `telegram`)
- `commands` (CLI command paths like `plugins.install`)
- `services` (service IDs)
- `hooks` (hook event types)

Semantics:

- Omitted or empty allowlists allow all registrations (backwards compatible).
- When an allowlist is non-empty, the gateway rejects any runtime registrations not declared in the allowlist.
- `commands` uses dotted paths for nested subcommands; the full command tree is validated (declare each path you register).

### Capabilities (Optional)

The manifest may also include a `capabilities` object:

```json
{
  "capabilities": {
    "required": ["tool:stub", "channel:telegram"],
    "optional": ["cli:plugins.*"]
  }
}
```

Semantics:

- If `capabilities` is omitted/empty, capability checks are disabled (backwards compatible).
- When present, runtime registrations require matching capabilities:
  - Tools: `tool:<name>`
  - Channels: `channel:<type>`
  - CLI: `cli:<dotted-path>` (e.g. `cli:plugins.install`)
  - Services: `service:<id>`
  - Hooks: `hook:<eventType>`
- Matching supports exact, `*`, and prefix wildcards like `tool:*` or `cli:plugins.*`.
- Today `required` and `optional` are treated the same by the runtime; `optional` is reserved for future user-approval flows.

## Gateway Config (`nexus.yaml`)

Configure plugins under `plugins.entries`:

```yaml
plugins:
  entries:
    example.echo:
      enabled: true
      path: ./plugins/echo # contains nexus.plugin.json + plugin.so (or <id>.so)
      config:
        prefix: "[echo] "
```

`path` may point at a directory containing the manifest + `.so`, the manifest file itself, or a direct `.so` path.

### Isolation (Future)

The `plugins.isolation` config block is reserved for out-of-process plugin execution (Docker / Firecracker). Today, it only
controls config parsing; true isolation is not implemented yet.

## Security Notes

Registration allowlists and manifest capabilities do not provide isolation (plugins are still in-process). True sandboxing
(out-of-process execution with an allowlisted API surface) is planned.
