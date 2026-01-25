# Nexus Makefile
# Comprehensive build, test, and development automation

.PHONY: all build build-all test test-unit test-integration test-race test-coverage \
        test-bench clean fmt lint lint-fix vet proto proto-lint proto-breaking \
        install-tools dev run docker-build docker-run docker-compose-up \
        docker-compose-down migrate migrate-status migrate-down docs gen \
        check ci pre-commit version help

# ============================================================================
# Variables
# ============================================================================

# Version info
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

# Build configuration
BINARY_NAME := nexus
BUILD_DIR   := bin
CMD_DIR     := ./cmd/nexus
LDFLAGS     := -ldflags "-s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

# Go configuration
GOPATH      ?= $(shell go env GOPATH)
GOOS        ?= $(shell go env GOOS)
GOARCH      ?= $(shell go env GOARCH)
CGO_ENABLED ?= 0

# Docker configuration
DOCKER_IMAGE     := nexus
DOCKER_TAG       := $(VERSION)
DOCKER_REGISTRY  ?= ghcr.io/haasonsaas

# Test configuration
COVERAGE_DIR     := coverage
COVERAGE_FILE    := $(COVERAGE_DIR)/coverage.out
COVERAGE_HTML    := $(COVERAGE_DIR)/coverage.html
TEST_TIMEOUT     := 10m
BENCH_COUNT      := 5
BENCH_TIME       := 2s

# Tool versions
GOLANGCI_LINT_VERSION := v1.64.8
BUF_VERSION           := 1.50.0

# ============================================================================
# Default target
# ============================================================================

all: check build

# ============================================================================
# Build targets
# ============================================================================

## build: Build the binary for current platform
build:
	@echo "==> Building $(BINARY_NAME) $(VERSION)..."
	@mkdir -p $(BUILD_DIR)
	@CGO_ENABLED=$(CGO_ENABLED) go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)
	@echo "==> Built $(BUILD_DIR)/$(BINARY_NAME)"

## build-all: Build for all supported platforms
build-all: build-linux-amd64 build-linux-arm64 build-darwin-amd64 build-darwin-arm64
	@echo "==> Built all platforms"

build-linux-amd64:
	@echo "==> Building for linux/amd64..."
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(CMD_DIR)

build-linux-arm64:
	@echo "==> Building for linux/arm64..."
	@GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 $(CMD_DIR)

build-darwin-amd64:
	@echo "==> Building for darwin/amd64..."
	@GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 $(CMD_DIR)

build-darwin-arm64:
	@echo "==> Building for darwin/arm64..."
	@GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 $(CMD_DIR)

## install: Install binary to GOPATH/bin
install: build
	@echo "==> Installing to $(GOPATH)/bin..."
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(GOPATH)/bin/$(BINARY_NAME)

# ============================================================================
# Test targets
# ============================================================================

## test: Run all tests
test: test-unit

## test-unit: Run unit tests
test-unit:
	@echo "==> Running unit tests..."
	@go test -timeout $(TEST_TIMEOUT) ./...

## test-race: Run tests with race detector
test-race:
	@echo "==> Running tests with race detector..."
	@go test -race -timeout $(TEST_TIMEOUT) ./...

## test-integration: Run integration tests (requires Docker + Playwright)
test-integration:
	@echo "==> Running integration tests..."
	@NEXUS_DOCKER_TESTS=1 NEXUS_DOCKER_PULL=1 NEXUS_BROWSER_TESTS=1 \
		go test -v -timeout $(TEST_TIMEOUT) ./...

## test-coverage: Run tests with coverage report
test-coverage:
	@echo "==> Running tests with coverage..."
	@mkdir -p $(COVERAGE_DIR)
	@go test -race -coverprofile=$(COVERAGE_FILE) -covermode=atomic -timeout $(TEST_TIMEOUT) ./...
	@go tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "==> Coverage report: $(COVERAGE_HTML)"
	@go tool cover -func=$(COVERAGE_FILE) | tail -n 1

## test-bench: Run benchmarks
test-bench:
	@echo "==> Running benchmarks..."
	@go test -bench=. -benchmem -benchtime=$(BENCH_TIME) -count=$(BENCH_COUNT) -run=^$$ ./... | tee benchmark-results.txt

## test-short: Run short tests only
test-short:
	@echo "==> Running short tests..."
	@go test -short -timeout 5m ./...

## test-verbose: Run tests with verbose output
test-verbose:
	@echo "==> Running tests (verbose)..."
	@go test -v -timeout $(TEST_TIMEOUT) ./...

## test-package: Run tests for a specific package (use PKG=./internal/foo)
test-package:
ifndef PKG
	$(error PKG is required, e.g., make test-package PKG=./internal/gateway)
endif
	@echo "==> Running tests for $(PKG)..."
	@go test -v -race -coverprofile=coverage-pkg.out $(PKG)

# ============================================================================
# Code quality targets
# ============================================================================

## fmt: Format code with gofmt
fmt:
	@echo "==> Formatting code..."
	@gofmt -s -w .
	@echo "==> Code formatted"

## lint: Run linters
lint: vet
	@echo "==> Running linters..."
	@files=$$(gofmt -l .); \
	if [ -n "$$files" ]; then \
		echo "gofmt needed on:"; \
		echo "$$files"; \
		exit 1; \
	fi
	@if command -v golangci-lint > /dev/null 2>&1; then \
		golangci-lint run --timeout=5m; \
	else \
		echo "Warning: golangci-lint not installed. Run 'make install-tools'"; \
	fi

## lint-fix: Run linters with auto-fix
lint-fix:
	@echo "==> Running linters with auto-fix..."
	@golangci-lint run --fix --timeout=5m

## vet: Run go vet
vet:
	@echo "==> Running go vet..."
	@go vet ./...

## check: Run all quality checks (fmt, vet, lint, test)
check: fmt vet lint test-unit
	@echo "==> All checks passed"

## ci: Run CI checks (used in GitHub Actions)
ci: lint test-race test-coverage
	@echo "==> CI checks completed"

## pre-commit: Run before committing (fmt, lint, test-short)
pre-commit: fmt lint test-short
	@echo "==> Pre-commit checks passed"

# ============================================================================
# Proto targets
# ============================================================================

## proto: Generate protobuf code
proto:
	@echo "==> Generating protobuf code..."
	@if command -v buf > /dev/null 2>&1; then \
		echo "Using buf..."; \
		PATH=$$PATH:$(GOPATH)/bin buf generate; \
	else \
		echo "Using protoc..."; \
		mkdir -p pkg/proto; \
		PATH=$$PATH:$(GOPATH)/bin protoc --go_out=. --go_opt=paths=source_relative \
			--go-grpc_out=. --go-grpc_opt=paths=source_relative \
			pkg/proto/nexus.proto; \
	fi
	@echo "==> Protobuf code generated"

## proto-lint: Lint proto files
proto-lint:
	@echo "==> Linting proto files..."
	@buf lint

## proto-breaking: Check for breaking changes
proto-breaking:
	@echo "==> Checking for breaking changes..."
	@buf breaking --against '.git#branch=main'

# ============================================================================
# Development targets
# ============================================================================

## dev: Start development server with hot reload (requires air)
dev:
	@echo "==> Starting development server..."
	@if command -v air > /dev/null 2>&1; then \
		air; \
	else \
		echo "Error: air not installed. Run: go install github.com/air-verse/air@latest"; \
		exit 1; \
	fi

## run: Build and run the server
run: build
	@echo "==> Running $(BINARY_NAME)..."
	@$(BUILD_DIR)/$(BINARY_NAME) serve

## run-debug: Run with debug logging
run-debug: build
	@echo "==> Running $(BINARY_NAME) with debug logging..."
	@NEXUS_LOG_LEVEL=debug $(BUILD_DIR)/$(BINARY_NAME) serve

# ============================================================================
# Migration targets
# ============================================================================

## migrate: Run database migrations
migrate: build
	@echo "==> Running migrations..."
	@$(BUILD_DIR)/$(BINARY_NAME) migrate up

## migrate-status: Show migration status
migrate-status: build
	@echo "==> Migration status..."
	@$(BUILD_DIR)/$(BINARY_NAME) migrate status

## migrate-down: Rollback last migration
migrate-down: build
	@echo "==> Rolling back migration..."
	@$(BUILD_DIR)/$(BINARY_NAME) migrate down

## migrate-create: Create new migration (use NAME=migration_name)
migrate-create:
ifndef NAME
	$(error NAME is required, e.g., make migrate-create NAME=add_users_table)
endif
	@echo "==> Creating migration $(NAME)..."
	@$(BUILD_DIR)/$(BINARY_NAME) migrate create $(NAME)

# ============================================================================
# Docker targets
# ============================================================================

## docker-build: Build Docker image
docker-build:
	@echo "==> Building Docker image $(DOCKER_IMAGE):$(DOCKER_TAG)..."
	@docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg DATE=$(DATE) \
		.
	@docker tag $(DOCKER_IMAGE):$(DOCKER_TAG) $(DOCKER_IMAGE):latest

## docker-push: Push Docker image to registry
docker-push:
	@echo "==> Pushing Docker image..."
	@docker tag $(DOCKER_IMAGE):$(DOCKER_TAG) $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(DOCKER_TAG)
	@docker tag $(DOCKER_IMAGE):latest $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest
	@docker push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(DOCKER_TAG)
	@docker push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest

## docker-run: Run Docker container
docker-run:
	@echo "==> Running Docker container..."
	@docker run --rm -it \
		-p 8080:8080 \
		-p 50051:50051 \
		-v $(PWD)/config.yaml:/app/config.yaml:ro \
		$(DOCKER_IMAGE):$(DOCKER_TAG)

## docker-compose-up: Start services with docker-compose
docker-compose-up:
	@echo "==> Starting services..."
	@docker-compose up -d

## docker-compose-down: Stop services
docker-compose-down:
	@echo "==> Stopping services..."
	@docker-compose down

## docker-compose-logs: View service logs
docker-compose-logs:
	@docker-compose logs -f

# ============================================================================
# Documentation targets
# ============================================================================

## docs: Generate documentation
docs:
	@echo "==> Generating documentation..."
	@if command -v godoc > /dev/null 2>&1; then \
		echo "Starting godoc server at http://localhost:6060"; \
		godoc -http=:6060; \
	else \
		go install golang.org/x/tools/cmd/godoc@latest; \
		godoc -http=:6060; \
	fi

## docs-markdown: Generate markdown docs (requires gomarkdoc)
docs-markdown:
	@echo "==> Generating markdown docs..."
	@if command -v gomarkdoc > /dev/null 2>&1; then \
		gomarkdoc --output docs/api.md ./...; \
	else \
		echo "Installing gomarkdoc..."; \
		go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest; \
		gomarkdoc --output docs/api.md ./...; \
	fi

# ============================================================================
# Code generation targets
# ============================================================================

## gen: Run all code generators
gen: proto
	@echo "==> Running go generate..."
	@go generate ./...

## gen-mocks: Generate test mocks (requires mockgen)
gen-mocks:
	@echo "==> Generating mocks..."
	@if command -v mockgen > /dev/null 2>&1; then \
		go generate ./...; \
	else \
		echo "Installing mockgen..."; \
		go install go.uber.org/mock/mockgen@latest; \
		go generate ./...; \
	fi

# ============================================================================
# Tool installation
# ============================================================================

## install-tools: Install required development tools
install-tools:
	@echo "==> Installing development tools..."
	@echo "Installing buf..."
	@go install github.com/bufbuild/buf/cmd/buf@latest
	@echo "Installing protoc plugins..."
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@echo "Installing golangci-lint..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@echo "Installing air (hot reload)..."
	@go install github.com/air-verse/air@latest
	@echo "Installing govulncheck..."
	@go install golang.org/x/vuln/cmd/govulncheck@latest
	@echo "Installing mockgen..."
	@go install go.uber.org/mock/mockgen@latest
	@echo "==> All tools installed"

## install-tools-ci: Install minimal tools for CI
install-tools-ci:
	@echo "==> Installing CI tools..."
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	@go install golang.org/x/vuln/cmd/govulncheck@latest

# ============================================================================
# Security targets
# ============================================================================

## security: Run security checks
security:
	@echo "==> Running security checks..."
	@govulncheck ./...

## security-audit: Run comprehensive security audit
security-audit: security
	@echo "==> Checking for hardcoded secrets..."
	@if command -v gitleaks > /dev/null 2>&1; then \
		gitleaks detect --source=. --verbose; \
	else \
		echo "Warning: gitleaks not installed"; \
	fi

# ============================================================================
# Cleanup targets
# ============================================================================

## clean: Clean build artifacts
clean:
	@echo "==> Cleaning..."
	@rm -rf $(BUILD_DIR)
	@rm -rf $(COVERAGE_DIR)
	@rm -rf pkg/proto/*.pb.go
	@rm -f benchmark-results.txt
	@rm -f coverage-pkg.out
	@go clean -cache -testcache
	@echo "==> Cleaned"

## clean-docker: Remove Docker images
clean-docker:
	@echo "==> Removing Docker images..."
	@docker rmi $(DOCKER_IMAGE):$(DOCKER_TAG) $(DOCKER_IMAGE):latest 2>/dev/null || true

## clean-all: Clean everything including tools cache
clean-all: clean clean-docker
	@echo "==> Deep cleaning..."
	@go clean -modcache

# ============================================================================
# Info targets
# ============================================================================

## version: Show version information
version:
	@echo "Version:  $(VERSION)"
	@echo "Commit:   $(COMMIT)"
	@echo "Date:     $(DATE)"
	@echo "Go:       $(shell go version)"
	@echo "Platform: $(GOOS)/$(GOARCH)"

## env: Show environment information
env:
	@echo "GOPATH:      $(GOPATH)"
	@echo "GOOS:        $(GOOS)"
	@echo "GOARCH:      $(GOARCH)"
	@echo "CGO_ENABLED: $(CGO_ENABLED)"
	@echo "BUILD_DIR:   $(BUILD_DIR)"
	@echo "VERSION:     $(VERSION)"

## deps: Show module dependencies
deps:
	@echo "==> Module dependencies..."
	@go list -m all

## deps-update: Update all dependencies
deps-update:
	@echo "==> Updating dependencies..."
	@go get -u ./...
	@go mod tidy

## deps-tidy: Tidy go.mod
deps-tidy:
	@echo "==> Tidying go.mod..."
	@go mod tidy
	@git diff --exit-code go.mod go.sum || echo "Warning: go.mod/go.sum changed"

# ============================================================================
# Help target
# ============================================================================

## help: Show this help message
help:
	@echo ""
	@echo "Nexus Makefile - Build and Development Automation"
	@echo "=================================================="
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Build targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | grep -E '^(build|install)' | column -t -s ':' | sed 's/^/  /'
	@echo ""
	@echo "Test targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | grep -E '^test' | column -t -s ':' | sed 's/^/  /'
	@echo ""
	@echo "Code quality:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | grep -E '^(fmt|lint|vet|check|ci|pre-commit)' | column -t -s ':' | sed 's/^/  /'
	@echo ""
	@echo "Proto targets:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | grep -E '^proto' | column -t -s ':' | sed 's/^/  /'
	@echo ""
	@echo "Development:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | grep -E '^(dev|run|migrate)' | column -t -s ':' | sed 's/^/  /'
	@echo ""
	@echo "Docker:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | grep -E '^docker' | column -t -s ':' | sed 's/^/  /'
	@echo ""
	@echo "Documentation:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | grep -E '^docs' | column -t -s ':' | sed 's/^/  /'
	@echo ""
	@echo "Tools & Security:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | grep -E '^(install-tools|security|gen)' | column -t -s ':' | sed 's/^/  /'
	@echo ""
	@echo "Cleanup:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | grep -E '^clean' | column -t -s ':' | sed 's/^/  /'
	@echo ""
	@echo "Info:"
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | grep -E '^(version|env|deps|help)' | column -t -s ':' | sed 's/^/  /'
	@echo ""
