# Nexus - Deployment Guide

This guide covers deploying Nexus in both managed (Kubernetes) and self-hosted configurations.

## Prerequisites

- Go 1.22+
- Docker
- kubectl (for Kubernetes deployment)
- Helm 3.x (optional, for Helm chart deployment)
- CockroachDB cluster (or single node for dev)

## Quick Start (Local Development)

```bash
# Clone the repo
git clone https://github.com/yourorg/nexus.git
cd nexus

# Install dependencies
go mod download

# Start CockroachDB (single node)
docker run -d --name cockroach \
  -p 26257:26257 -p 8080:8080 \
  cockroachdb/cockroach:v23.2.0 start-single-node --insecure

# Create database
docker exec cockroach cockroach sql --insecure -e "CREATE DATABASE nexus;"

# Copy and configure
cp nexus.example.yaml nexus.yaml
# Edit nexus.yaml with your API keys

# Run migrations
go run ./cmd/nexus migrate up

# Start the server
go run ./cmd/nexus serve
```

## Building

### Binary

```bash
# Build for current platform
go build -o bin/nexus ./cmd/nexus

# Cross-compile
GOOS=linux GOARCH=amd64 go build -o bin/nexus-linux-amd64 ./cmd/nexus
GOOS=darwin GOARCH=arm64 go build -o bin/nexus-darwin-arm64 ./cmd/nexus
```

### Docker Image

```dockerfile
# Dockerfile
FROM golang:1.22-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /nexus ./cmd/nexus

FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /nexus /usr/local/bin/nexus
ENTRYPOINT ["nexus"]
CMD ["serve"]
```

```bash
# Build image
docker build -t nexus:latest .

# Push to registry
docker tag nexus:latest ghcr.io/yourorg/nexus:latest
docker push ghcr.io/yourorg/nexus:latest
```

---

## Kubernetes Deployment

### Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                    Kubernetes Cluster                           │
│                                                                 │
│  ┌─────────────┐   ┌─────────────┐   ┌─────────────┐          │
│  │   Ingress   │──▶│   Nexus     │──▶│ CockroachDB │          │
│  │  (nginx)    │   │  (3 pods)   │   │  (3 nodes)  │          │
│  └─────────────┘   └──────┬──────┘   └─────────────┘          │
│                           │                                    │
│                           ▼                                    │
│                    ┌─────────────┐   ┌─────────────┐          │
│                    │  Sandbox    │   │   SearXNG   │          │
│                    │  (Pool)     │   │             │          │
│                    └─────────────┘   └─────────────┘          │
│                                                                 │
│  ┌─────────────┐   ┌─────────────┐   ┌─────────────┐          │
│  │ Prometheus  │   │  Grafana    │   │   Jaeger    │          │
│  └─────────────┘   └─────────────┘   └─────────────┘          │
└─────────────────────────────────────────────────────────────────┘
```

### Namespace and Secrets

```yaml
# deployments/kubernetes/namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: nexus
---
# deployments/kubernetes/secrets.yaml
apiVersion: v1
kind: Secret
metadata:
  name: nexus-secrets
  namespace: nexus
type: Opaque
stringData:
  JWT_SECRET: "your-jwt-secret-here"
  ANTHROPIC_API_KEY: "sk-ant-..."
  OPENAI_API_KEY: "sk-..."
  GOOGLE_CLIENT_ID: "..."
  GOOGLE_CLIENT_SECRET: "..."
  GITHUB_CLIENT_ID: "..."
  GITHUB_CLIENT_SECRET: "..."
```

### ConfigMap

```yaml
# deployments/kubernetes/configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: nexus-config
  namespace: nexus
data:
  nexus.yaml: |
    server:
      host: 0.0.0.0
      grpc_port: 50051
      http_port: 8080
      metrics_port: 9090

    database:
      url: postgres://root@cockroachdb-public:26257/nexus?sslmode=disable
      max_connections: 25

    auth:
      jwt_secret: ${JWT_SECRET}
      token_expiry: 24h
      oauth:
        google:
          client_id: ${GOOGLE_CLIENT_ID}
          client_secret: ${GOOGLE_CLIENT_SECRET}
          redirect_url: https://nexus.example.com/auth/google/callback
        github:
          client_id: ${GITHUB_CLIENT_ID}
          client_secret: ${GITHUB_CLIENT_SECRET}
          redirect_url: https://nexus.example.com/auth/github/callback

    session:
      default_agent_id: main
      slack_scope: thread
      discord_scope: thread
      memory:
        enabled: false
        directory: memory
        max_lines: 20
        days: 2
        scope: session
      heartbeat:
        enabled: false
        file: HEARTBEAT.md
        mode: always

    workspace:
      enabled: false
      path: .
      max_chars: 20000
      agents_file: AGENTS.md
      soul_file: SOUL.md
      user_file: USER.md
      identity_file: IDENTITY.md
      tools_file: TOOLS.md
      memory_file: MEMORY.md

    identity:
      name: ""
      creature: ""
      vibe: ""
      emoji: ""

    user:
      name: ""
      preferred_address: ""
      pronouns: ""
      timezone: ""
      notes: ""

    channels:
      telegram:
        enabled: true
      discord:
        enabled: true
      slack:
        enabled: true

    llm:
      default_provider: anthropic
      providers:
        anthropic:
          api_key: ${ANTHROPIC_API_KEY}
          default_model: claude-sonnet-4-20250514
        openai:
          api_key: ${OPENAI_API_KEY}
          default_model: gpt-4o

    tools:
      notes: ""
      notes_file: ""
      sandbox:
        enabled: true
        pool_size: 10
        timeout: 60s
        limits:
          max_cpu: 1
          max_memory: 512MB
      browser:
        enabled: true
        headless: true
        url: http://playwright:3000
      websearch:
        enabled: true
        provider: searxng
        url: http://searxng:8080

    plugins:
      load:
        paths: []
      entries: {}

    logging:
      level: info
      format: json
```

Notes:
- Config parsing is strict; unknown keys will fail validation.
- Plugin entries require a manifest file (`nexus.plugin.json` or `clawdbot.plugin.json`) with a JSON schema.

Validate configuration:
```bash
nexus doctor -c nexus.yaml
```

Apply config migrations + workspace repairs:
```bash
nexus doctor --repair -c nexus.yaml
```

Run channel health probes:
```bash
nexus doctor --probe -c nexus.yaml
```

Initialize a workspace:
```bash
nexus setup --workspace ./clawd
```

### Deployment

```yaml
# deployments/kubernetes/deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nexus
  namespace: nexus
  labels:
    app: nexus
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nexus
  template:
    metadata:
      labels:
        app: nexus
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9090"
    spec:
      serviceAccountName: nexus
      containers:
        - name: nexus
          image: ghcr.io/yourorg/nexus:latest
          args: ["serve", "--config", "/etc/nexus/nexus.yaml"]
          ports:
            - name: grpc
              containerPort: 50051
            - name: http
              containerPort: 8080
            - name: metrics
              containerPort: 9090
          envFrom:
            - secretRef:
                name: nexus-secrets
          volumeMounts:
            - name: config
              mountPath: /etc/nexus
          resources:
            requests:
              cpu: 500m
              memory: 512Mi
            limits:
              cpu: 2000m
              memory: 2Gi
          livenessProbe:
            grpc:
              port: 50051
            initialDelaySeconds: 10
            periodSeconds: 10
          readinessProbe:
            grpc:
              port: 50051
            initialDelaySeconds: 5
            periodSeconds: 5
      volumes:
        - name: config
          configMap:
            name: nexus-config
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: nexus
  namespace: nexus
```

### Services

```yaml
# deployments/kubernetes/service.yaml
apiVersion: v1
kind: Service
metadata:
  name: nexus
  namespace: nexus
spec:
  selector:
    app: nexus
  ports:
    - name: grpc
      port: 50051
      targetPort: 50051
    - name: http
      port: 8080
      targetPort: 8080
    - name: metrics
      port: 9090
      targetPort: 9090
---
apiVersion: v1
kind: Service
metadata:
  name: nexus-public
  namespace: nexus
spec:
  type: LoadBalancer
  selector:
    app: nexus
  ports:
    - name: grpc
      port: 443
      targetPort: 50051
    - name: http
      port: 80
      targetPort: 8080
```

### Ingress

```yaml
# deployments/kubernetes/ingress.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: nexus
  namespace: nexus
  annotations:
    kubernetes.io/ingress.class: nginx
    cert-manager.io/cluster-issuer: letsencrypt-prod
    nginx.ingress.kubernetes.io/backend-protocol: "GRPC"
spec:
  tls:
    - hosts:
        - nexus.example.com
        - grpc.nexus.example.com
      secretName: nexus-tls
  rules:
    - host: nexus.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: nexus
                port:
                  number: 8080
    - host: grpc.nexus.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: nexus
                port:
                  number: 50051
```

### CockroachDB (StatefulSet)

```yaml
# deployments/kubernetes/cockroachdb.yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: cockroachdb
  namespace: nexus
spec:
  serviceName: cockroachdb
  replicas: 3
  selector:
    matchLabels:
      app: cockroachdb
  template:
    metadata:
      labels:
        app: cockroachdb
    spec:
      containers:
        - name: cockroachdb
          image: cockroachdb/cockroach:v23.2.0
          ports:
            - containerPort: 26257
              name: grpc
            - containerPort: 8080
              name: http
          command:
            - /cockroach/cockroach
            - start
            - --logtostderr
            - --insecure
            - --advertise-host=$(POD_NAME).cockroachdb
            - --http-addr=0.0.0.0
            - --join=cockroachdb-0.cockroachdb,cockroachdb-1.cockroachdb,cockroachdb-2.cockroachdb
            - --cache=25%
            - --max-sql-memory=25%
          env:
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
          volumeMounts:
            - name: datadir
              mountPath: /cockroach/cockroach-data
          resources:
            requests:
              cpu: 500m
              memory: 2Gi
            limits:
              cpu: 2000m
              memory: 4Gi
  volumeClaimTemplates:
    - metadata:
        name: datadir
      spec:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: 100Gi
---
apiVersion: v1
kind: Service
metadata:
  name: cockroachdb-public
  namespace: nexus
spec:
  ports:
    - port: 26257
      targetPort: 26257
      name: grpc
    - port: 8080
      targetPort: 8080
      name: http
  selector:
    app: cockroachdb
---
apiVersion: v1
kind: Service
metadata:
  name: cockroachdb
  namespace: nexus
spec:
  ports:
    - port: 26257
      targetPort: 26257
      name: grpc
    - port: 8080
      targetPort: 8080
      name: http
  clusterIP: None
  selector:
    app: cockroachdb
```

### HorizontalPodAutoscaler

```yaml
# deployments/kubernetes/hpa.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: nexus
  namespace: nexus
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: nexus
  minReplicas: 3
  maxReplicas: 20
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 80
```

### Apply All Resources

```bash
# Apply in order
kubectl apply -f deployments/kubernetes/namespace.yaml
kubectl apply -f deployments/kubernetes/secrets.yaml
kubectl apply -f deployments/kubernetes/configmap.yaml
kubectl apply -f deployments/kubernetes/cockroachdb.yaml

# Wait for CockroachDB to be ready
kubectl wait --for=condition=ready pod -l app=cockroachdb -n nexus --timeout=300s

# Initialize CockroachDB cluster
kubectl exec -it cockroachdb-0 -n nexus -- cockroach init --insecure

# Create database
kubectl exec -it cockroachdb-0 -n nexus -- cockroach sql --insecure -e "CREATE DATABASE nexus;"

# Deploy Nexus
kubectl apply -f deployments/kubernetes/deployment.yaml
kubectl apply -f deployments/kubernetes/service.yaml
kubectl apply -f deployments/kubernetes/ingress.yaml
kubectl apply -f deployments/kubernetes/hpa.yaml
```

---

## Self-Hosted Deployment

### Docker Compose

```yaml
# deployments/docker/docker-compose.yaml
version: '3.8'

services:
  nexus:
    image: ghcr.io/yourorg/nexus:latest
    command: ["serve", "--config", "/etc/nexus/nexus.yaml"]
    ports:
      - "50051:50051"  # gRPC
      - "8080:8080"    # HTTP
      - "9090:9090"    # Metrics
    environment:
      - JWT_SECRET=${JWT_SECRET}
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
      - OPENAI_API_KEY=${OPENAI_API_KEY}
    volumes:
      - ./nexus.yaml:/etc/nexus/nexus.yaml:ro
    depends_on:
      cockroachdb:
        condition: service_healthy
    restart: unless-stopped

  cockroachdb:
    image: cockroachdb/cockroach:v23.2.0
    command: start-single-node --insecure --advertise-addr=cockroachdb
    ports:
      - "26257:26257"
      - "8081:8080"
    volumes:
      - cockroach-data:/cockroach/cockroach-data
    healthcheck:
      test: ["CMD", "cockroach", "sql", "--insecure", "-e", "SELECT 1"]
      interval: 10s
      timeout: 5s
      retries: 5
    restart: unless-stopped

  searxng:
    image: searxng/searxng:latest
    ports:
      - "8888:8080"
    volumes:
      - ./searxng:/etc/searxng:ro
    restart: unless-stopped

  playwright:
    image: mcr.microsoft.com/playwright:v1.42.0-jammy
    command: npx playwright run-server --port 3000
    ports:
      - "3000:3000"
    restart: unless-stopped

volumes:
  cockroach-data:
```

### Systemd Service (Direct Binary)

```ini
# /etc/systemd/system/nexus.service
[Unit]
Description=Nexus AI Agent Gateway
After=network.target cockroachdb.service
Requires=network.target

[Service]
Type=simple
User=nexus
Group=nexus
WorkingDirectory=/opt/nexus
ExecStart=/opt/nexus/bin/nexus serve --config /etc/nexus/nexus.yaml
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal

# Environment
EnvironmentFile=/etc/nexus/env

# Security
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/var/lib/nexus

[Install]
WantedBy=multi-user.target
```

```bash
# Install
sudo mkdir -p /opt/nexus/bin /etc/nexus /var/lib/nexus
sudo cp bin/nexus /opt/nexus/bin/
sudo cp nexus.yaml /etc/nexus/
sudo useradd -r -s /bin/false nexus

# Configure
sudo cat > /etc/nexus/env << EOF
JWT_SECRET=your-secret
ANTHROPIC_API_KEY=sk-ant-...
OPENAI_API_KEY=sk-...
EOF
sudo chmod 600 /etc/nexus/env

# Enable and start
sudo systemctl daemon-reload
sudo systemctl enable nexus
sudo systemctl start nexus
sudo systemctl status nexus
```

---

## Database Migrations

```bash
# Run all pending migrations
nexus migrate up

# Rollback last migration
nexus migrate down

# Show migration status
nexus migrate status

# Create new migration
nexus migrate create add_user_preferences
```

---

## Monitoring

### Prometheus Metrics

Nexus exposes metrics on the configured metrics port (default 9090):

```
# Request metrics
nexus_requests_total{method="Stream",status="ok"} 1234
nexus_request_duration_seconds{method="Stream",quantile="0.99"} 0.45

# LLM metrics
nexus_llm_requests_total{provider="anthropic",model="claude-sonnet-4-20250514"} 567
nexus_llm_tokens_total{provider="anthropic",type="input"} 123456
nexus_llm_tokens_total{provider="anthropic",type="output"} 78901

# Tool metrics
nexus_tool_executions_total{tool="code_sandbox",status="success"} 89
nexus_tool_duration_seconds{tool="browser",quantile="0.95"} 2.3

# Session metrics
nexus_active_sessions 45
nexus_messages_total 12345
```

### Grafana Dashboard

Import the provided dashboard from `deployments/grafana/nexus-dashboard.json`.

### Health Checks

```bash
# gRPC health check
grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check

# HTTP health check
curl http://localhost:8080/health

# Detailed status
curl http://localhost:8080/status
```

---

## Troubleshooting

### Common Issues

**CockroachDB connection refused:**
```bash
# Check if CockroachDB is running
docker ps | grep cockroach
# or
kubectl get pods -l app=cockroachdb -n nexus
```

**Channel not connecting:**
```bash
# Check logs for specific channel
nexus logs --channel telegram

# Verify credentials
nexus channels status
```

**High memory usage:**
```bash
# Check Go memory stats
curl http://localhost:8080/debug/pprof/heap > heap.prof
go tool pprof heap.prof
```

### Debug Mode

```bash
# Enable debug logging
NEXUS_LOG_LEVEL=debug nexus serve

# Enable request tracing
nexus serve --trace
```
