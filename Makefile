.PHONY: all build test test-integration clean fmt lint proto proto-lint proto-breaking install-tools help

# Default target
all: build

# Build the project
build:
	@echo "Building Nexus..."
	@go build -o bin/nexus ./cmd/nexus

# Run tests
test:
	@echo "Running tests..."
	@go test -v ./...

# Run integration tests (Docker + Playwright required)
test-integration:
	@echo "Running integration tests..."
	@NEXUS_DOCKER_TESTS=1 NEXUS_DOCKER_PULL=1 NEXUS_BROWSER_TESTS=1 go test -v ./...

# Format code
fmt:
	@echo "Formatting..."
	@gofmt -w .

# Lint code
lint:
	@echo "Linting..."
	@files=$$(gofmt -l .); \
	if [ -n "$$files" ]; then \
		echo "gofmt needed on:"; \
		echo "$$files"; \
		exit 1; \
	fi
	@go vet ./...
	@if command -v golangci-lint > /dev/null 2>&1; then \
		golangci-lint run --timeout=5m; \
	else \
		echo "Error: golangci-lint is not installed. Run 'make install-tools' to install it."; \
		exit 1; \
	fi

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf bin/
	@rm -rf pkg/proto/*.pb.go

# Generate protobuf code
proto:
	@echo "Generating protobuf code..."
	@if command -v buf > /dev/null 2>&1; then \
		echo "Using buf for code generation..."; \
		PATH=$$PATH:$$HOME/go/bin buf generate; \
	else \
		echo "Using protoc for code generation..."; \
		mkdir -p pkg/proto; \
		PATH=$$PATH:$$HOME/go/bin protoc --go_out=. --go_opt=paths=source_relative \
			--go-grpc_out=. --go-grpc_opt=paths=source_relative \
			pkg/proto/nexus.proto; \
	fi
	@echo "Protobuf code generated successfully!"

# Lint proto files (requires buf)
proto-lint:
	@echo "Linting protobuf files..."
	@if command -v buf > /dev/null 2>&1; then \
		buf lint; \
	else \
		echo "Error: buf is not installed. Run 'make install-tools' to install it."; \
		exit 1; \
	fi

# Check for breaking changes (requires buf)
proto-breaking:
	@echo "Checking for breaking changes..."
	@if command -v buf > /dev/null 2>&1; then \
		buf breaking --against '.git#branch=main'; \
	else \
		echo "Error: buf is not installed. Run 'make install-tools' to install it."; \
		exit 1; \
	fi

# Install required tools
install-tools:
	@echo "Installing required tools..."
	@echo "Installing buf..."
	@go install github.com/bufbuild/buf/cmd/buf@latest
	@echo "Installing protoc-gen-go..."
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@echo "Installing protoc-gen-go-grpc..."
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@echo "Installing golangci-lint..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "All tools installed successfully!"

# Show help
help:
	@echo "Nexus Makefile Commands:"
	@echo "  make build          - Build the project"
	@echo "  make test           - Run tests"
	@echo "  make test-integration - Run integration tests (Docker + Playwright)"
	@echo "  make clean          - Clean build artifacts"
	@echo "  make fmt            - Format code with gofmt"
	@echo "  make lint           - Run gofmt/go vet/golangci-lint"
	@echo "  make proto          - Generate protobuf code"
	@echo "  make proto-lint     - Lint protobuf files (requires buf)"
	@echo "  make proto-breaking - Check for breaking changes (requires buf)"
	@echo "  make install-tools  - Install required development tools"
	@echo "  make help           - Show this help message"
