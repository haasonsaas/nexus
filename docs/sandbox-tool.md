# Sandbox Code Execution Tool

The Nexus sandbox tool provides secure, isolated code execution for multiple programming languages.

Backends: Docker (default), optional Firecracker microVM backend on supported Linux hosts, and Daytona for remote sandbox execution.

## Quick Start

### Registering the Tool

```go
package main

import (
    "github.com/haasonsaas/nexus/internal/agent"
    "github.com/haasonsaas/nexus/internal/tools/sandbox"
    "github.com/haasonsaas/nexus/internal/sessions"
)

func main() {
    // Create agent runtime
    provider := ... // your LLM provider
    store := sessions.NewMemoryStore()
    runtime := agent.NewRuntime(provider, store)

    // Register sandbox tool
    sandbox.MustRegister(runtime)

    // Now the agent can execute code via the execute_code tool
}
```

### Direct Usage

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "github.com/haasonsaas/nexus/internal/tools/sandbox"
)

func main() {
    // Create executor
    executor, err := sandbox.NewExecutor()
    if err != nil {
        panic(err)
    }
    defer executor.Close()

    // Execute Python code
    params := sandbox.ExecuteParams{
        Language: "python",
        Code:     `print("Hello, World!")`,
        Timeout:  10,
    }

    paramsJSON, _ := json.Marshal(params)
    result, err := executor.Execute(context.Background(), paramsJSON)
    if err != nil {
        panic(err)
    }

    fmt.Println(result.Content)
}
```

## Supported Languages

The sandbox supports the following languages out of the box:

- **Python 3.11** - Full standard library
- **Node.js 20** - Modern JavaScript/ES2020+
- **Go 1.24** - Complete Go toolchain
- **Bash 5** - Shell scripting

## Features

### Security

- **Network Isolation**: Containers run with `--network none`
- **Resource Limits**: CPU, memory, and time constraints
- **Read-only Workspace**: Code cannot modify the host
- **Process Limits**: Maximum 100 processes
- **Timeout Enforcement**: Hard timeout on execution

### Performance

- **Pre-warmed Pool**: Containers are ready before execution
- **Concurrent Execution**: Multiple sandboxes run in parallel
- **Fast Startup**: Docker-based execution starts in <100ms
- **Automatic Scaling**: Pool grows and shrinks based on demand

### Flexibility

- **File Mounting**: Provide additional files to the sandbox
- **Stdin Support**: Pass input data to programs
- **Output Capture**: Full stdout, stderr, and exit code
- **Custom Limits**: Configure CPU, memory, and timeout per execution

## Examples

### Data Analysis

```go
params := sandbox.ExecuteParams{
    Language: "python",
    Code: `
import json

with open('sales.json', 'r') as f:
    data = json.load(f)

total = sum(sale['amount'] for sale in data)
avg = total / len(data)

print(f"Total Sales: ${total:,.2f}")
print(f"Average: ${avg:,.2f}")
print(f"Transactions: {len(data)}")
`,
    Files: map[string]string{
        "sales.json": `[
            {"id": 1, "amount": 150.00},
            {"id": 2, "amount": 230.50},
            {"id": 3, "amount": 89.99}
        ]`,
    },
    Timeout: 30,
}
```

### API Response Processing

```go
params := sandbox.ExecuteParams{
    Language: "nodejs",
    Code: `
const data = require('./response.json');

const filtered = data.items
    .filter(item => item.active)
    .map(item => ({
        id: item.id,
        name: item.name,
        value: item.value * 1.1
    }));

console.log(JSON.stringify(filtered, null, 2));
`,
    Files: map[string]string{
        "response.json": `{
            "items": [
                {"id": 1, "name": "A", "value": 100, "active": true},
                {"id": 2, "name": "B", "value": 200, "active": false},
                {"id": 3, "name": "C", "value": 300, "active": true}
            ]
        }`,
    },
    Timeout: 30,
}
```

### System Administration

```go
params := sandbox.ExecuteParams{
    Language: "bash",
    Code: `
#!/bin/bash

# Process log file
grep "ERROR" app.log | \
    awk '{print $3}' | \
    sort | uniq -c | \
    sort -rn | head -10

echo "---"
echo "Total errors: $(grep -c ERROR app.log)"
`,
    Files: map[string]string{
        "app.log": `[INFO] 12:00:00 App started
[ERROR] 12:01:00 Database connection failed
[ERROR] 12:01:05 Database connection failed
[INFO] 12:02:00 Request processed
[ERROR] 12:03:00 Invalid input
`,
    },
    Timeout: 30,
}
```

### Algorithm Implementation

```go
params := sandbox.ExecuteParams{
    Language: "go",
    Code: `
package main

import (
    "fmt"
    "sort"
)

func quicksort(arr []int) []int {
    if len(arr) <= 1 {
        return arr
    }

    pivot := arr[len(arr)/2]
    var left, right []int

    for i := 0; i < len(arr); i++ {
        if i == len(arr)/2 {
            continue
        }
        if arr[i] < pivot {
            left = append(left, arr[i])
        } else {
            right = append(right, arr[i])
        }
    }

    left = quicksort(left)
    right = quicksort(right)

    return append(append(left, pivot), right...)
}

func main() {
    data := []int{64, 34, 25, 12, 22, 11, 90}
    fmt.Println("Original:", data)

    sorted := quicksort(data)
    fmt.Println("Sorted:", sorted)

    // Verify
    if sort.IntsAreSorted(sorted) {
        fmt.Println("✓ Correctly sorted")
    }
}
`,
    Timeout: 30,
}
```

## Configuration

### Pool Configuration

```go
executor, err := sandbox.NewExecutor(
    // Backend selection (auto-falls back to Docker)
    sandbox.WithBackend(sandbox.BackendFirecracker),

    // Initial pool size per language
    sandbox.WithPoolSize(3),

    // Maximum pool size per language
    sandbox.WithMaxPoolSize(10),

    // Default execution timeout
    sandbox.WithDefaultTimeout(60 * time.Second),

    // Enable network access (disabled by default)
    sandbox.WithNetworkEnabled(false),

    // Optional workspace root and access defaults
    sandbox.WithWorkspaceRoot("/var/lib/nexus/sandboxes"),
    sandbox.WithDefaultWorkspaceAccess(sandbox.WorkspaceReadOnly),
)
```

`WorkspaceRoot` controls where sandbox workspaces are staged on disk (defaults to the system temp dir).
`WorkspaceAccess` controls how the workspace is provided to the sandbox: read-only (`ro`, default),
read-write (`rw`), or `none` to copy files into an isolated tmpfs without a host mount.

### Daytona Backend

```go
executor, err := sandbox.NewExecutor(
    sandbox.WithBackend(sandbox.BackendDaytona),
    sandbox.WithDaytonaConfig(sandbox.DaytonaConfig{
        APIKey:   "your-api-key", // or rely on DAYTONA_API_KEY
        APIURL:   "https://app.daytona.io/api",
        Snapshot: "default", // optional
        // ReuseSandbox keeps a sandbox alive across executions (faster, less isolated).
        ReuseSandbox: false,
    }),
)
```

### Per-Execution Configuration

```go
params := sandbox.ExecuteParams{
    Language: "python",
    Code:     "...",

    // Execution timeout in seconds
    Timeout:  120,

    // CPU limit in millicores (1000 = 1 core)
    CPULimit: 2000,

    // Memory limit in MB
    MemLimit: 1024,

    // Additional files
    Files: map[string]string{
        "config.ini": "...",
        "data.csv":   "...",
    },

    // Standard input
    Stdin: "input data",
}
```

## Resource Limits

| Resource | Default | Maximum | Notes |
|----------|---------|---------|-------|
| Timeout | 30s | 300s | Hard timeout, process is killed |
| CPU | 1000 millicores | No limit | 1000 = 1 full core |
| Memory | 512 MB | No limit | Container is OOM-killed if exceeded |
| PIDs | 100 | 100 | Process limit |
| File Descriptors | 1024 | 1024 | Open file limit |

## Error Handling

The executor returns structured errors with detailed information:

```go
result, err := executor.Execute(ctx, paramsJSON)
if err != nil {
    // Fatal error (network, invalid params, etc.)
    log.Fatal(err)
}

if result.IsError {
    // Code execution failed (syntax error, runtime error, timeout)
    fmt.Println("Execution failed:")
    fmt.Println(result.Content)
}
```

### Error Types

1. **Invalid Parameters**: Missing required fields, invalid language
2. **Timeout**: Execution exceeded time limit
3. **Runtime Error**: Code threw exception or had syntax error
4. **Resource Limit**: Memory or CPU limit exceeded
5. **System Error**: Docker unavailable, workspace creation failed

## Testing

The sandbox includes comprehensive tests:

```bash
# Run all tests (requires Docker)
go test -v ./internal/tools/sandbox

# Run unit tests only (no Docker required)
go test -short ./internal/tools/sandbox

# Run specific test
go test -v -run TestExecutor_PythonExecution ./internal/tools/sandbox

# Run with timeout
go test -v -timeout 5m ./internal/tools/sandbox
```

## Monitoring

### Pool Statistics

```go
// Get current pool stats
stats := executor.pool.Stats()

for language, stat := range stats {
    fmt.Printf("Language: %s\n", language)
    fmt.Printf("  Available: %d\n", stat.Available)
    fmt.Printf("  Active: %d\n", stat.Active)
    fmt.Printf("  Max Size: %d\n", stat.MaxSize)
}
```

### Health Checks

```go
// Check if pool is healthy
if err := executor.pool.Health(); err != nil {
    log.Printf("Pool unhealthy: %v", err)
}
```

## Best Practices

1. **Reuse Executors**: Create one executor and reuse it for multiple executions
2. **Set Appropriate Timeouts**: Balance between allowing enough time and preventing hangs
3. **Limit Resource Usage**: Set CPU and memory limits based on expected workload
4. **Handle Errors Gracefully**: Check both err and result.IsError
5. **Close When Done**: Always defer executor.Close() to cleanup resources
6. **Use Short Tests**: Skip integration tests in CI with -short flag
7. **Monitor Pool Stats**: Track pool utilization for capacity planning

## Troubleshooting

### Docker Not Found

```
Error: failed to create pool: exec: "docker": executable file not found
```

**Solution**: Install Docker and ensure it's in PATH

### Permission Denied

```
Error: Got permission denied while trying to connect to the Docker daemon
```

**Solution**: Add user to docker group
```bash
sudo usermod -aG docker $USER
newgrp docker
```

### Timeout Issues

```
Error: Execution timeout
```

**Solution**: Increase timeout for long-running operations
```go
params.Timeout = 120 // 2 minutes
```

### Memory Errors

```
Error: Container was OOM killed
```

**Solution**: Increase memory limit
```go
params.MemLimit = 1024 // 1 GB
```

## Architecture

```
┌─────────────────────────────────────────┐
│          Agent Runtime                   │
├─────────────────────────────────────────┤
│      Sandbox Executor (Tool)            │
├─────────────────────────────────────────┤
│            Pool Manager                  │
│  ┌──────────────────────────────────┐   │
│  │ Language Pool (Python)           │   │
│  │ Language Pool (Node.js)          │   │
│  │ Language Pool (Go)               │   │
│  │ Language Pool (Bash)             │   │
│  └──────────────────────────────────┘   │
├─────────────────────────────────────────┤
│   Docker* / Firecracker Runtime Executor │
├─────────────────────────────────────────┤
│      Docker Engine* / Firecracker        │
└─────────────────────────────────────────┘
```

Docker is the default backend; Firecracker is optional (Linux-only).

## Future Roadmap

- [x] Firecracker backend (experimental; Linux-only)
- [ ] Support for Rust, Ruby, and PHP
- [ ] Package installation (pip, npm, go get)
- [ ] Persistent workspaces for multi-step execution
- [ ] GPU access for ML workloads
- [ ] Network access with domain allowlist
- [ ] WebAssembly runtime support
- [ ] Kubernetes-native execution

## License

Part of the Nexus project. See LICENSE file for details.
