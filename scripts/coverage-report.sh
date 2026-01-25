#!/usr/bin/env bash
# Generate and display test coverage report
# Usage: ./scripts/coverage-report.sh [package-pattern]

set -euo pipefail

COVERAGE_DIR="coverage"
COVERAGE_FILE="$COVERAGE_DIR/coverage.out"
COVERAGE_HTML="$COVERAGE_DIR/coverage.html"
COVERAGE_JSON="$COVERAGE_DIR/coverage.json"

# Package pattern (default: all)
PKG_PATTERN="${1:-./...}"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo "==> Running tests with coverage for: $PKG_PATTERN"

# Create coverage directory
mkdir -p "$COVERAGE_DIR"

# Run tests with coverage
go test -race -coverprofile="$COVERAGE_FILE" -covermode=atomic -timeout 10m "$PKG_PATTERN"

# Generate HTML report
go tool cover -html="$COVERAGE_FILE" -o "$COVERAGE_HTML"

# Display coverage summary
echo ""
echo "==> Coverage Summary:"
echo "-----------------------------------"

# Per-package coverage
go tool cover -func="$COVERAGE_FILE" | while read -r line; do
    # Extract percentage
    pct=$(echo "$line" | awk '{print $NF}' | tr -d '%')
    name=$(echo "$line" | awk '{print $1}')

    # Color based on coverage
    if [[ "$pct" =~ ^[0-9]+\.?[0-9]*$ ]]; then
        if (( $(echo "$pct >= 80" | bc -l) )); then
            echo -e "${GREEN}$line${NC}"
        elif (( $(echo "$pct >= 50" | bc -l) )); then
            echo -e "${YELLOW}$line${NC}"
        else
            echo -e "${RED}$line${NC}"
        fi
    else
        echo "$line"
    fi
done

echo "-----------------------------------"
echo ""
echo -e "HTML report: ${GREEN}$COVERAGE_HTML${NC}"
echo ""

# Open HTML report if on macOS
if [[ "$(uname)" == "Darwin" ]]; then
    read -p "Open HTML report in browser? [y/N] " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        open "$COVERAGE_HTML"
    fi
fi
