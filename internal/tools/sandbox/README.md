# Sandbox Code Executor

A secure code execution sandbox for the Nexus project that implements the `agent.Tool` interface.

## Features

- **Multiple Runtime Support**: Python 3, Node.js, Go, and Bash
- **Resource Limits**: CPU, memory, and time constraints
- **Network Isolation**: No network access by default
- **Secure Execution**: Docker-based sandboxing (with Firecracker fallback support)
- **VM Pool Management**: Pre-warmed container pool for fast execution
- **Stream Capture**: Full stdout, stderr, and exit code capture
- **File Mounting**: Support for mounting additional files

## Architecture

### Components

1. **Executor** (`executor.go`): Main tool implementation that integrates with the agent runtime
2. **Pool** (`pool.go`): Manages a pool of runtime executors for efficient reuse
3. **RuntimeExecutor**: Interface for language-specific execution backends

### Backends

- **Docker** (default): Uses Docker containers with gVisor for isolation
- **Firecracker**: Lightweight VM-based execution (auto-fallback to Docker if unavailable)

## Usage

### Basic Usage

```go
import (
    "context"
    "encoding/json"
    "github.com/haasonsaas/nexus/internal/tools/sandbox"
)

// Create executor
executor, err := sandbox.NewExecutor()
if err != nil {
    log.Fatal(err)
}
defer executor.Close()

// Prepare execution parameters
params := sandbox.ExecuteParams{
    Language: "python",
    Code:     `print("Hello, World!")`,
    Timeout:  30,
}

paramsJSON, _ := json.Marshal(params)

// Execute code
result, err := executor.Execute(context.Background(), paramsJSON)
if err != nil {
    log.Fatal(err)
}

fmt.Println(result.Content)
```

### Advanced Configuration

```go
executor, err := sandbox.NewExecutor(
    sandbox.WithBackend(sandbox.BackendDocker),
    sandbox.WithPoolSize(5),
    sandbox.WithMaxPoolSize(10),
    sandbox.WithDefaultTimeout(60 * time.Second),
    sandbox.WithNetworkEnabled(false),
)
```

### With File Mounting

```go
params := sandbox.ExecuteParams{
    Language: "python",
    Code: `
with open('data.json', 'r') as f:
    import json
    data = json.load(f)
    print(data['message'])
`,
    Files: map[string]string{
        "data.json": `{"message": "Hello from file!"}`,
    },
    Timeout: 30,
}
```

### With Stdin

```go
params := sandbox.ExecuteParams{
    Language: "python",
    Code: `
import sys
data = sys.stdin.read()
print(f"Received: {data}")
`,
    Stdin: "test input",
    Timeout: 30,
}
```

## Tool Interface

The executor implements the `agent.Tool` interface:

```go
type Tool interface {
    Name() string
    Description() string
    Schema() json.RawMessage
    Execute(ctx context.Context, params json.RawMessage) (*ToolResult, error)
}
```

### Tool Name
`execute_code`

### Schema

```json
{
  "type": "object",
  "properties": {
    "language": {
      "type": "string",
      "enum": ["python", "nodejs", "go", "bash"],
      "description": "Programming language to execute"
    },
    "code": {
      "type": "string",
      "description": "The code to execute"
    },
    "stdin": {
      "type": "string",
      "description": "Optional standard input"
    },
    "files": {
      "type": "object",
      "description": "Optional additional files (filename -> content)"
    },
    "timeout": {
      "type": "integer",
      "description": "Timeout in seconds (default: 30, max: 300)"
    },
    "cpu_limit": {
      "type": "integer",
      "description": "CPU limit in millicores (default: 1000)"
    },
    "mem_limit": {
      "type": "integer",
      "description": "Memory limit in MB (default: 512)"
    }
  },
  "required": ["language", "code"]
}
```

## Supported Languages

### Python 3
- Image: `python:3.11-alpine`
- File: `main.py`
- Command: `python main.py`

### Node.js
- Image: `node:20-alpine`
- File: `main.js`
- Command: `node main.js`

### Go
- Image: `golang:1.22-alpine`
- File: `main.go`
- Command: `go run main.go`

### Bash
- Image: `bash:5-alpine`
- File: `main.sh`
- Command: `bash main.sh`

## Resource Limits

### Default Limits
- **CPU**: 1000 millicores (1 core)
- **Memory**: 512 MB
- **Timeout**: 30 seconds (max: 300 seconds)
- **PIDs**: 100
- **File Descriptors**: 1024

### Custom Limits

```go
params := sandbox.ExecuteParams{
    Language: "python",
    Code:     "print('test')",
    CPULimit: 500,   // 0.5 cores
    MemLimit: 256,   // 256 MB
    Timeout:  60,    // 60 seconds
}
```

## Pool Management

The executor uses a pool of pre-warmed containers for fast execution:

```go
// Get pool statistics
stats := executor.pool.Stats()
for lang, stat := range stats {
    fmt.Printf("%s: %d available, %d active\n",
        lang, stat.Available, stat.Active)
}

// Manually warmup a language pool
err := executor.pool.Warmup(context.Background(), "python", 5)

// Shrink a pool
err := executor.pool.Shrink("python", 2)

// Check pool health
err := executor.pool.Health()
```

## Security

### Isolation
- Containers run with `--network none` (no network access)
- Read-only workspace mounting
- Restricted PID and file descriptor limits
- Memory and CPU limits enforced

### Safety
- Code runs in isolated containers/VMs
- No access to host filesystem (except mounted workspace)
- Timeout enforcement prevents infinite loops
- Resource limits prevent resource exhaustion

## Testing

```bash
# Run all tests
go test ./internal/tools/sandbox

# Run with integration tests
go test -v ./internal/tools/sandbox

# Run only unit tests (skip integration)
go test -short ./internal/tools/sandbox
```

### Test Coverage

- Language execution (Python, Node.js, Go, Bash)
- Timeout handling
- Resource limit enforcement
- Output capture (stdout/stderr)
- File mounting
- Pool management
- Error handling

## Examples

### Python Data Processing

```go
params := sandbox.ExecuteParams{
    Language: "python",
    Code: `
import json

data = [1, 2, 3, 4, 5]
result = sum(data) / len(data)
print(f"Average: {result}")
`,
    Timeout: 10,
}
```

### Node.js Async Processing

```go
params := sandbox.ExecuteParams{
    Language: "nodejs",
    Code: `
async function process() {
    const data = [1, 2, 3, 4, 5];
    const sum = data.reduce((a, b) => a + b, 0);
    console.log('Sum:', sum);
}
process();
`,
    Timeout: 10,
}
```

### Go Computation

```go
params := sandbox.ExecuteParams{
    Language: "go",
    Code: `
package main
import "fmt"

func fibonacci(n int) int {
    if n <= 1 {
        return n
    }
    return fibonacci(n-1) + fibonacci(n-2)
}

func main() {
    fmt.Println("Fib(10):", fibonacci(10))
}
`,
    Timeout: 15,
}
```

## Limitations

1. **Network Access**: Disabled by default for security
2. **Execution Time**: Maximum 300 seconds
3. **Memory**: Limited to prevent host exhaustion
4. **File System**: Read-only workspace, no write access
5. **Dependencies**: Only standard library packages available

## Future Enhancements

- [ ] Firecracker integration for faster cold starts
- [ ] Support for additional languages (Ruby, PHP, Rust)
- [ ] Package installation support (pip, npm, etc.)
- [ ] Persistent workspace for multi-step execution
- [ ] Network access with allowlist
- [ ] GPU support for ML workloads

## Troubleshooting

### Docker not available
Ensure Docker is installed and running:
```bash
docker ps
```

### Permission denied
Ensure the user has Docker permissions:
```bash
sudo usermod -aG docker $USER
```

### Timeout issues
Increase timeout for long-running operations:
```go
params.Timeout = 120 // 2 minutes
```

### Memory errors
Increase memory limit:
```go
params.MemLimit = 1024 // 1 GB
```
