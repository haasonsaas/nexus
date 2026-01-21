# Sample Echo Plugin

This example shows how to build an external runtime plugin for Nexus using Go's plugin system.

## Build

From the plugin directory:

```bash
go build -buildmode=plugin -o plugin.so
```

## Configure

In `nexus.yaml`:

```yaml
plugins:
  load:
    paths:
      - ./examples/plugins/echo
  entries:
    sample-echo:
      enabled: true
      path: ./examples/plugins/echo
      config:
        prefix: "[echo] "
```

Restart the gateway. The `echo` tool will be available for tool calls.

## Notes

- The loader looks for `plugin.so` or `<id>.so` in the plugin directory.
- The runtime symbol must be exported as `NexusPlugin` and implement `pluginsdk.RuntimePlugin`.
- Go plugins require the same Go toolchain version as the host binary.
