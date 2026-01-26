#!/usr/bin/env bash
# Development environment setup script
# Usage: ./scripts/setup-dev.sh

set -euo pipefail

echo "==> Setting up Nexus development environment..."

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check Go version
GO_VERSION=$(go version 2>/dev/null | awk '{print $3}' | sed 's/go//')
REQUIRED_GO="1.24"

check_command() {
    if command -v "$1" &> /dev/null; then
        echo -e "${GREEN}✓${NC} $1 is installed"
        return 0
    else
        echo -e "${RED}✗${NC} $1 is NOT installed"
        return 1
    fi
}

install_go_tool() {
    local name=$1
    local package=$2
    echo -e "${YELLOW}→${NC} Installing $name..."
    go install "$package" 2>/dev/null || {
        echo -e "${RED}✗${NC} Failed to install $name"
        return 1
    }
    echo -e "${GREEN}✓${NC} Installed $name"
}

# Check prerequisites
echo ""
echo "==> Checking prerequisites..."

check_command "go" || {
    echo -e "${RED}Error: Go is required. Please install Go $REQUIRED_GO or later.${NC}"
    exit 1
}

echo -e "${GREEN}✓${NC} Go version: $GO_VERSION"

check_command "git" || {
    echo -e "${RED}Error: git is required.${NC}"
    exit 1
}

# Optional tools check
echo ""
echo "==> Checking optional tools..."
check_command "docker" || echo "  (Docker is optional but recommended for integration tests)"
check_command "buf" || echo "  (buf is optional but recommended for protobuf development)"

# Install development tools
echo ""
echo "==> Installing development tools..."

install_go_tool "golangci-lint" "github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
install_go_tool "air" "github.com/air-verse/air@latest"
install_go_tool "buf" "github.com/bufbuild/buf/cmd/buf@latest"
install_go_tool "protoc-gen-go" "google.golang.org/protobuf/cmd/protoc-gen-go@latest"
install_go_tool "protoc-gen-go-grpc" "google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest"
install_go_tool "govulncheck" "golang.org/x/vuln/cmd/govulncheck@latest"
install_go_tool "mockgen" "go.uber.org/mock/mockgen@latest"

# Download dependencies
echo ""
echo "==> Downloading Go dependencies..."
go mod download

# Build the project
echo ""
echo "==> Building project..."
make build

# Run tests
echo ""
echo "==> Running tests..."
make test-short

# Setup git hooks (optional)
if [ -d ".git" ]; then
    echo ""
    echo "==> Setting up git hooks..."

    # Create pre-commit hook
    cat > .git/hooks/pre-commit << 'EOF'
#!/bin/sh
# Pre-commit hook - run quick checks before commit

echo "Running pre-commit checks..."

# Format check
files=$(gofmt -l .)
if [ -n "$files" ]; then
    echo "Error: gofmt needed on:"
    echo "$files"
    echo "Run 'make fmt' to fix."
    exit 1
fi

# Go vet
go vet ./... || exit 1

# Quick test
go test -short -timeout 2m ./... || exit 1

echo "Pre-commit checks passed!"
EOF
    chmod +x .git/hooks/pre-commit
    echo -e "${GREEN}✓${NC} Git pre-commit hook installed"
fi

echo ""
echo -e "${GREEN}==> Development environment setup complete!${NC}"
echo ""
echo "Quick start commands:"
echo "  make build        - Build the binary"
echo "  make test         - Run tests"
echo "  make dev          - Start with hot reload (requires air)"
echo "  make help         - Show all available commands"
echo ""
