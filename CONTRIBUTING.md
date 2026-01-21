# Contributing to Nexus

Thank you for your interest in contributing to Nexus! This document provides guidelines and information for contributors.

## Code of Conduct

Please be respectful and constructive in all interactions. We aim to maintain a welcoming and inclusive community.

## Getting Started

### Prerequisites

- Go 1.24 or later
- Docker (for running CockroachDB and sandbox tests)
- Make (optional, but recommended)
- Playwright system dependencies (for browser tool tests)

Optional integration test requirements:
- Docker images: `python:3.11-alpine`, `node:20-alpine`, `golang:1.22-alpine`, `bash:5-alpine`
- Playwright browser dependencies (GTK/GStreamer/etc.)

### Development Setup

1. Fork and clone the repository:
   ```bash
   git clone https://github.com/YOUR_USERNAME/nexus.git
   cd nexus
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

3. Start the development database:
   ```bash
   docker run -d --name cockroach \
     -p 26257:26257 -p 8080:8080 \
     cockroachdb/cockroach:v23.2.0 start-single-node --insecure
   ```

4. Run the tests:
   ```bash
   make test
   ```

### Integration Tests

Some tests require Docker and Playwright browser dependencies. By default, those tests will **skip** if the environment is missing the prerequisites. To force them to run (and fail if missing), set:

```bash
NEXUS_DOCKER_TESTS=1 NEXUS_DOCKER_PULL=1 NEXUS_BROWSER_TESTS=1 go test ./...
```

- `NEXUS_DOCKER_TESTS=1` forces sandbox integration tests to run.
- `NEXUS_DOCKER_PULL=1` allows tests to `docker pull` required images if missing.
- `NEXUS_BROWSER_TESTS=1` forces browser integration tests to run.

5. Build the binary:
   ```bash
   make build
   ```

## Development Workflow

### Branch Naming

- `feature/` - New features
- `fix/` - Bug fixes
- `docs/` - Documentation changes
- `refactor/` - Code refactoring
- `test/` - Test additions or fixes

Example: `feature/add-matrix-channel`

### Commit Messages

We follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <description>

[optional body]

[optional footer]
```

Types:
- `feat`: A new feature
- `fix`: A bug fix
- `docs`: Documentation changes
- `style`: Code style changes (formatting, etc.)
- `refactor`: Code refactoring
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

Examples:
```
feat(telegram): add inline keyboard support
fix(agent): handle rate limiting from Anthropic API
docs(readme): update installation instructions
```

### Pull Requests

1. Create a feature branch from `main`
2. Make your changes
3. Ensure tests pass: `make test`
4. Ensure code is formatted: `make fmt`
5. Ensure linting passes: `make lint`
6. Push your branch and create a PR

### PR Checklist

- [ ] Tests added/updated for changes
- [ ] Documentation updated if needed
- [ ] Code formatted (`make fmt`)
- [ ] Linting passes (`make lint`)
- [ ] Commit messages follow conventions
- [ ] PR description explains the changes

## Code Style

### Go

- Follow [Effective Go](https://golang.org/doc/effective_go)
- Use `gofmt` for formatting
- Keep functions small and focused
- Write clear, descriptive variable names
- Add comments for exported functions and types
- Handle errors explicitly

### Testing

- Write tests for all new functionality
- Use table-driven tests where appropriate
- Mock external dependencies
- Aim for >80% code coverage on new code

Example test structure:
```go
func TestFunctionName(t *testing.T) {
    tests := []struct {
        name    string
        input   InputType
        want    OutputType
        wantErr bool
    }{
        {
            name:  "valid input",
            input: validInput,
            want:  expectedOutput,
        },
        {
            name:    "invalid input",
            input:   invalidInput,
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := FunctionName(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("FunctionName() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if !reflect.DeepEqual(got, tt.want) {
                t.Errorf("FunctionName() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

## Project Structure

```
nexus/
├── cmd/nexus/           # CLI entry point
├── internal/            # Private packages
│   ├── gateway/         # gRPC gateway server
│   ├── channels/        # Channel adapters (telegram, discord, slack)
│   ├── agent/           # LLM orchestration and providers
│   ├── tools/           # Tool implementations (sandbox, browser, search)
│   ├── sessions/        # Session persistence
│   ├── auth/            # Authentication
│   └── config/          # Configuration
├── pkg/                 # Public packages
│   ├── models/          # Shared data types
│   └── proto/           # gRPC definitions
├── deployments/         # Deployment configurations
├── docs/                # Documentation
└── scripts/             # Utility scripts
```

## Adding a New Channel

1. Create a new package under `internal/channels/`
2. Implement the `channels.Adapter` interface
3. Add configuration in `internal/config/config.go`
4. Register the adapter in the gateway
5. Add tests
6. Update documentation

## Adding a New LLM Provider

1. Create a new file under `internal/agent/providers/`
2. Implement the `agent.LLMProvider` interface
3. Add configuration support
4. Add tests with mocked API responses
5. Update documentation

## Adding a New Tool

1. Create a new package under `internal/tools/`
2. Implement the `agent.Tool` interface
3. Add configuration support
4. Add tests
5. Update the tool documentation

## Running Integration Tests

Integration tests require external services. Use Docker Compose:

```bash
# Start test services
docker-compose -f deployments/docker/docker-compose.yaml up -d

# Run integration tests
make test-integration

# Stop services
docker-compose -f deployments/docker/docker-compose.yaml down
```

## Documentation

- Update README.md for user-facing changes
- Update docs/ for architectural changes
- Add godoc comments to exported types and functions
- Include examples where helpful

## Getting Help

- Open an issue for bugs or feature requests
- Start a discussion for questions or ideas
- Tag maintainers if you need a review

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
