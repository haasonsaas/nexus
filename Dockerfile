# Build stage
FROM golang:1.24-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build arguments for version info
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE} -s -w" \
    -o /nexus ./cmd/nexus

# Final stage
FROM alpine:3.19

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN addgroup -g 1000 nexus && \
    adduser -u 1000 -G nexus -s /bin/sh -D nexus

# Copy binary from builder
COPY --from=builder /nexus /usr/local/bin/nexus

# Copy example config
COPY --from=builder /app/nexus.example.yaml /etc/nexus/nexus.example.yaml

# Create directories
RUN mkdir -p /var/lib/nexus /var/log/nexus && \
    chown -R nexus:nexus /var/lib/nexus /var/log/nexus /etc/nexus

# Switch to non-root user
USER nexus

# Expose ports
EXPOSE 50051 8080 9090

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:8080/health || exit 1

# Default command
ENTRYPOINT ["nexus"]
CMD ["serve", "--config", "/etc/nexus/nexus.yaml"]
