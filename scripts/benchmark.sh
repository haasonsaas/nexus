#!/usr/bin/env bash
# Run benchmarks and optionally compare with previous results
# Usage: ./scripts/benchmark.sh [package-pattern] [--compare]

set -euo pipefail

PKG_PATTERN="${1:-./...}"
COMPARE="${2:-}"
BENCH_DIR="benchmarks"
BENCH_FILE="$BENCH_DIR/bench-$(date +%Y%m%d-%H%M%S).txt"
BENCH_LATEST="$BENCH_DIR/bench-latest.txt"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

mkdir -p "$BENCH_DIR"

echo "==> Running benchmarks for: $PKG_PATTERN"
echo ""

# Run benchmarks
go test -bench=. -benchmem -benchtime=2s -count=5 -run=^$ "$PKG_PATTERN" | tee "$BENCH_FILE"

# Copy to latest
cp "$BENCH_FILE" "$BENCH_LATEST"

echo ""
echo -e "${GREEN}==> Benchmarks saved to:${NC} $BENCH_FILE"

# Compare with previous if requested and benchstat is available
if [[ "$COMPARE" == "--compare" ]] && command -v benchstat &> /dev/null; then
    # Find previous benchmark file
    PREV_FILE=$(ls -t "$BENCH_DIR"/bench-*.txt 2>/dev/null | head -2 | tail -1)

    if [[ -n "$PREV_FILE" ]] && [[ "$PREV_FILE" != "$BENCH_FILE" ]]; then
        echo ""
        echo -e "${YELLOW}==> Comparing with previous benchmark:${NC} $PREV_FILE"
        echo ""
        benchstat "$PREV_FILE" "$BENCH_FILE"
    else
        echo ""
        echo "No previous benchmark to compare with."
    fi
elif [[ "$COMPARE" == "--compare" ]]; then
    echo ""
    echo "Install benchstat for comparison: go install golang.org/x/perf/cmd/benchstat@latest"
fi

# Summary
echo ""
echo "==> Benchmark files in $BENCH_DIR:"
ls -la "$BENCH_DIR"/*.txt 2>/dev/null | tail -5
