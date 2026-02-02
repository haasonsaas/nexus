# Nexus Cron Scheduling Design

## Overview

This document specifies the design for a cron scheduling system in Nexus, supporting scheduled messages, agent tasks, webhooks, and custom handlers.

## Goals

1. **Flexible scheduling**: Cron expressions and interval-based scheduling
2. **Multiple job types**: Messages, agent tasks, webhooks, custom handlers
3. **Persistence**: Jobs survive restarts
4. **Reliability**: Retry logic, failure tracking, graceful shutdown

---

## Current Implementation (MVP, as of 2026-01-26)

The shipped cron system in Nexus is config-driven and implemented in `internal/cron` and `internal/gateway`.

- **Config schema**: `config.CronConfig` / `config.CronJobConfig` in `internal/config/config.go`.
- **Supported job types**: `message`, `webhook`, `agent`.
- **Message jobs**: executed via an injected `cron.MessageSender` (wired in `internal/gateway/server.go` via proactive messaging).
- **Agent jobs**: executed via an injected `cron.AgentRunner` (wired in `internal/gateway/server.go` by injecting an inbound message into the runtime). For `agent` jobs, `message.channel`/`message.channel_id` are optional; when both are omitted, the gateway defaults to `channel=api` and `channel_id=cron:<job-id>`.
- **Webhook jobs**: executed with an HTTP client; default method is `POST`; a default timeout of 30 seconds is enforced when `timeout` is unset.

Example configuration:

```yaml
cron:
  enabled: true
  jobs:
    - id: daily-reminder
      name: Daily reminder
      type: message
      enabled: true
      schedule:
        cron: "0 9 * * 1-5"
        timezone: "America/New_York"
      message:
        channel: slack
        channel_id: U123456
        content: "Standup in 10 minutes."

    - id: weekly-digest
      name: Weekly digest
      type: agent
      enabled: true
      schedule:
        cron: "0 18 * * 5"
        timezone: "America/New_York"
      message:
        channel: slack
        channel_id: C123456
        content: "Generate a weekly digest of key discussions and action items."

    - id: ping-webhook
      name: Ping webhook
      type: webhook
      enabled: true
      schedule:
        every: 1h
      webhook:
        url: https://example.com/ping
        method: POST
        headers:
          Content-Type: application/json
        body: "{\"source\":\"nexus\"}"
        timeout: 30s
```

## 1. Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                        Cron Scheduler                            │
│   (job registry, scheduling, execution, monitoring)             │
└─────────────────────────────────────────────────────────────────┘
                              │
              ┌───────────────┼───────────────┬───────────────┐
              ▼               ▼               ▼               ▼
      ┌───────────┐   ┌───────────┐   ┌───────────┐   ┌───────────┐
      │  Message  │   │  Agent    │   │  Webhook  │   │  Custom   │
      │   Job     │   │   Task    │   │   Job     │   │  Handler  │
      └───────────┘   └───────────┘   └───────────┘   └───────────┘
```

---

## 2. Proposed Data Model (Future)

### 2.1 Job Definition

```go
// Proposed shape (not the shipped MVP implementation).

type Job struct {
    ID          string            `json:"id" yaml:"id"`
    Name        string            `json:"name" yaml:"name"`
    Description string            `json:"description,omitempty" yaml:"description"`
    Type        JobType           `json:"type" yaml:"type"`
    Schedule    Schedule          `json:"schedule" yaml:"schedule"`
    Config      JobConfig         `json:"config" yaml:"config"`
    Enabled     bool              `json:"enabled" yaml:"enabled"`
    Retry       *RetryConfig      `json:"retry,omitempty" yaml:"retry"`
    Timeout     time.Duration     `json:"timeout,omitempty" yaml:"timeout"`
    Tags        []string          `json:"tags,omitempty" yaml:"tags"`
    CreatedAt   time.Time         `json:"created_at"`
    UpdatedAt   time.Time         `json:"updated_at"`
}

type JobType string

const (
    JobTypeMessage  JobType = "message"
    JobTypeAgent    JobType = "agent"
    JobTypeWebhook  JobType = "webhook"
    JobTypeCustom   JobType = "custom"
)

type Schedule struct {
    // Cron expression (e.g., "0 9 * * 1-5" for 9am weekdays)
    Cron string `json:"cron,omitempty" yaml:"cron"`

    // Interval-based (e.g., "1h", "30m")
    Every time.Duration `json:"every,omitempty" yaml:"every"`

    // Fixed times
    At []string `json:"at,omitempty" yaml:"at"`  // ["09:00", "17:00"]

    // Timezone
    Timezone string `json:"timezone,omitempty" yaml:"timezone"`
}

type JobConfig struct {
    // Message job
    Message *MessageJobConfig `json:"message,omitempty" yaml:"message"`

    // Agent job
    Agent *AgentJobConfig `json:"agent,omitempty" yaml:"agent"`

    // Webhook job
    Webhook *WebhookJobConfig `json:"webhook,omitempty" yaml:"webhook"`

    // Custom job
    Custom *CustomJobConfig `json:"custom,omitempty" yaml:"custom"`
}

type RetryConfig struct {
    MaxRetries int           `json:"max_retries" yaml:"max_retries"`
    Backoff    time.Duration `json:"backoff" yaml:"backoff"`
    MaxBackoff time.Duration `json:"max_backoff" yaml:"max_backoff"`
}
```

### 2.2 Job Type Configs

```go
// Message Job: Send a message to a channel
type MessageJobConfig struct {
    Channel   models.ChannelType `json:"channel" yaml:"channel"`
    ChannelID string             `json:"channel_id" yaml:"channel_id"`  // User/group ID
    Content   string             `json:"content" yaml:"content"`
    Template  string             `json:"template,omitempty" yaml:"template"`  // Go template
    Data      map[string]any     `json:"data,omitempty" yaml:"data"`  // Template data
}

// Agent Job: Run an agent with a prompt
type AgentJobConfig struct {
    AgentID      string         `json:"agent_id" yaml:"agent_id"`
    Prompt       string         `json:"prompt" yaml:"prompt"`
    Template     string         `json:"template,omitempty" yaml:"template"`
    Data         map[string]any `json:"data,omitempty" yaml:"data"`
    OutputTo     *OutputConfig  `json:"output_to,omitempty" yaml:"output_to"`
    MaxTokens    int            `json:"max_tokens,omitempty" yaml:"max_tokens"`
    Tools        []string       `json:"tools,omitempty" yaml:"tools"`  // Tool allowlist
}

type OutputConfig struct {
    Channel   models.ChannelType `json:"channel,omitempty" yaml:"channel"`
    ChannelID string             `json:"channel_id,omitempty" yaml:"channel_id"`
    File      string             `json:"file,omitempty" yaml:"file"`
    Webhook   string             `json:"webhook,omitempty" yaml:"webhook"`
}

// Webhook Job: Call an HTTP endpoint
type WebhookJobConfig struct {
    URL     string            `json:"url" yaml:"url"`
    Method  string            `json:"method" yaml:"method"`  // GET, POST, etc.
    Headers map[string]string `json:"headers,omitempty" yaml:"headers"`
    Body    string            `json:"body,omitempty" yaml:"body"`
    Auth    *WebhookAuth      `json:"auth,omitempty" yaml:"auth"`
}

type WebhookAuth struct {
    Type   string `json:"type" yaml:"type"`  // bearer, basic, api_key
    Token  string `json:"token,omitempty" yaml:"token"`
    User   string `json:"user,omitempty" yaml:"user"`
    Pass   string `json:"pass,omitempty" yaml:"pass"`
    Header string `json:"header,omitempty" yaml:"header"`  // For api_key
}

// Custom Job: Call a registered handler
type CustomJobConfig struct {
    Handler string         `json:"handler" yaml:"handler"`
    Args    map[string]any `json:"args,omitempty" yaml:"args"`
}
```

### 2.3 Job Execution

```go
type JobExecution struct {
    ID          string           `json:"id"`
    JobID       string           `json:"job_id"`
    Status      ExecutionStatus  `json:"status"`
    StartedAt   time.Time        `json:"started_at"`
    CompletedAt *time.Time       `json:"completed_at,omitempty"`
    Duration    time.Duration    `json:"duration,omitempty"`
    Output      string           `json:"output,omitempty"`
    Error       string           `json:"error,omitempty"`
    RetryCount  int              `json:"retry_count"`
}

type ExecutionStatus string

const (
    StatusPending   ExecutionStatus = "pending"
    StatusRunning   ExecutionStatus = "running"
    StatusCompleted ExecutionStatus = "completed"
    StatusFailed    ExecutionStatus = "failed"
    StatusRetrying  ExecutionStatus = "retrying"
    StatusCancelled ExecutionStatus = "cancelled"
)
```

---

## 3. Scheduler Implementation

### 3.1 Scheduler Interface

```go
// internal/cron/scheduler.go

type Scheduler interface {
    // Job management
    Register(job *Job) error
    Unregister(jobID string) error
    Update(job *Job) error
    Get(jobID string) (*Job, bool)
    List() []*Job

    // Control
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Pause(jobID string) error
    Resume(jobID string) error

    // Execution
    RunNow(ctx context.Context, jobID string) (*JobExecution, error)

    // History
    GetExecutions(jobID string, limit int) ([]*JobExecution, error)
    GetLastExecution(jobID string) (*JobExecution, error)
}
```

### 3.2 Scheduler Implementation

```go
// internal/cron/scheduler.go

import "github.com/robfig/cron/v3"

type CronScheduler struct {
    cron     *cron.Cron
    jobs     map[string]*Job
    entries  map[string]cron.EntryID
    handlers map[string]JobHandler
    store    ExecutionStore
    logger   *slog.Logger
    mu       sync.RWMutex

    // Dependencies
    channels *channels.Manager
    runtime  *agent.Runtime
    http     *http.Client
}

func NewScheduler(cfg *config.CronConfig, deps Dependencies) *CronScheduler {
    parser := cron.NewParser(
        cron.SecondOptional |
        cron.Minute |
        cron.Hour |
        cron.Dom |
        cron.Month |
        cron.Dow |
        cron.Descriptor,
    )

    return &CronScheduler{
        cron:     cron.New(cron.WithParser(parser), cron.WithLogger(cronLogger{})),
        jobs:     make(map[string]*Job),
        entries:  make(map[string]cron.EntryID),
        handlers: make(map[string]JobHandler),
        store:    NewMemoryStore(), // or NewDBStore(db)
        logger:   slog.Default().With("component", "cron"),
        channels: deps.Channels,
        runtime:  deps.Runtime,
        http:     &http.Client{Timeout: 30 * time.Second},
    }
}

func (s *CronScheduler) Start(ctx context.Context) error {
    s.cron.Start()
    s.logger.Info("cron scheduler started")
    return nil
}

func (s *CronScheduler) Stop(ctx context.Context) error {
    stopCtx := s.cron.Stop()
    select {
    case <-stopCtx.Done():
        s.logger.Info("cron scheduler stopped")
    case <-ctx.Done():
        s.logger.Warn("cron scheduler stop timed out")
    }
    return nil
}

func (s *CronScheduler) Register(job *Job) error {
    s.mu.Lock()
    defer s.mu.Unlock()

    // Validate job
    if err := s.validateJob(job); err != nil {
        return fmt.Errorf("invalid job: %w", err)
    }

    // Build schedule
    schedule, err := s.buildSchedule(job.Schedule)
    if err != nil {
        return fmt.Errorf("invalid schedule: %w", err)
    }

    // Create cron entry
    entryID, err := s.cron.AddFunc(schedule, func() {
        s.executeJob(context.Background(), job)
    })
    if err != nil {
        return fmt.Errorf("failed to add cron entry: %w", err)
    }

    s.jobs[job.ID] = job
    s.entries[job.ID] = entryID

    s.logger.Info("registered job",
        "id", job.ID,
        "name", job.Name,
        "type", job.Type,
        "schedule", schedule)

    return nil
}

func (s *CronScheduler) buildSchedule(sched Schedule) (string, error) {
    if sched.Cron != "" {
        return sched.Cron, nil
    }

    if sched.Every > 0 {
        return fmt.Sprintf("@every %s", sched.Every), nil
    }

    if len(sched.At) > 0 {
        // Convert "09:00" to cron format
        // This is simplified; real implementation would handle multiple times
        parts := strings.Split(sched.At[0], ":")
        return fmt.Sprintf("%s %s * * *", parts[1], parts[0]), nil
    }

    return "", fmt.Errorf("no schedule specified")
}

func (s *CronScheduler) executeJob(ctx context.Context, job *Job) {
    execution := &JobExecution{
        ID:        uuid.New().String(),
        JobID:     job.ID,
        Status:    StatusRunning,
        StartedAt: time.Now(),
    }

    // Apply timeout
    if job.Timeout > 0 {
        var cancel context.CancelFunc
        ctx, cancel = context.WithTimeout(ctx, job.Timeout)
        defer cancel()
    }

    s.logger.Info("executing job", "id", job.ID, "name", job.Name)

    var err error
    var output string

    switch job.Type {
    case JobTypeMessage:
        output, err = s.executeMessageJob(ctx, job)
    case JobTypeAgent:
        output, err = s.executeAgentJob(ctx, job)
    case JobTypeWebhook:
        output, err = s.executeWebhookJob(ctx, job)
    case JobTypeCustom:
        output, err = s.executeCustomJob(ctx, job)
    default:
        err = fmt.Errorf("unknown job type: %s", job.Type)
    }

    now := time.Now()
    execution.CompletedAt = &now
    execution.Duration = now.Sub(execution.StartedAt)
    execution.Output = output

    if err != nil {
        execution.Status = StatusFailed
        execution.Error = err.Error()
        s.logger.Error("job failed", "id", job.ID, "error", err)

        // Handle retry
        if job.Retry != nil && execution.RetryCount < job.Retry.MaxRetries {
            s.scheduleRetry(ctx, job, execution)
        }
    } else {
        execution.Status = StatusCompleted
        s.logger.Info("job completed", "id", job.ID, "duration", execution.Duration)
    }

    // Store execution
    s.store.Save(execution)
}

func (s *CronScheduler) scheduleRetry(ctx context.Context, job *Job, exec *JobExecution) {
    backoff := job.Retry.Backoff * time.Duration(1<<exec.RetryCount)
    if backoff > job.Retry.MaxBackoff {
        backoff = job.Retry.MaxBackoff
    }

    exec.Status = StatusRetrying
    exec.RetryCount++

    s.logger.Info("scheduling retry",
        "job", job.ID,
        "attempt", exec.RetryCount,
        "backoff", backoff)

    time.AfterFunc(backoff, func() {
        s.executeJob(context.Background(), job)
    })
}

func (s *CronScheduler) RunNow(ctx context.Context, jobID string) (*JobExecution, error) {
    s.mu.RLock()
    job, ok := s.jobs[jobID]
    s.mu.RUnlock()

    if !ok {
        return nil, fmt.Errorf("job not found: %s", jobID)
    }

    execution := &JobExecution{
        ID:        uuid.New().String(),
        JobID:     job.ID,
        Status:    StatusPending,
        StartedAt: time.Now(),
    }

    go func() {
        execution.Status = StatusRunning
        s.executeJob(ctx, job)
    }()

    return execution, nil
}
```

---

## 4. Job Executors

### 4.1 Message Job

**Implementation note (updated 2026-02-01):**
- Message jobs are supported by the shipped scheduler (`internal/cron/scheduler.go`) and require a configured `MessageSender`.
- Template rendering is implemented via `Scheduler.renderMessageContent`.
- Message delivery is routed through the gateway message service (default channel adapters).

```go
// Proposed: internal/cron/executors/message.go

func (s *CronScheduler) executeMessageJob(ctx context.Context, job *Job) (string, error) {
    cfg := job.Config.Message
    if cfg == nil {
        return "", fmt.Errorf("message config is nil")
    }

    // Render template if provided
    content := cfg.Content
    if cfg.Template != "" {
        tmpl, err := template.New("message").Parse(cfg.Template)
        if err != nil {
            return "", fmt.Errorf("invalid template: %w", err)
        }

        data := cfg.Data
        if data == nil {
            data = make(map[string]any)
        }
        data["now"] = time.Now()

        var buf bytes.Buffer
        if err := tmpl.Execute(&buf, data); err != nil {
            return "", fmt.Errorf("template execution failed: %w", err)
        }
        content = buf.String()
    }

    // Get channel adapter
    adapter, ok := s.channels.Get(cfg.Channel)
    if !ok {
        return "", fmt.Errorf("channel not found: %s", cfg.Channel)
    }

    // Send message
    msg := &models.Message{
        ID:        uuid.New().String(),
        Channel:   cfg.Channel,
        Direction: models.DirectionOutbound,
        Role:      models.RoleAssistant,
        Content:   content,
        Metadata: map[string]any{
            "peer_id": cfg.ChannelID,
            "cron_job": job.ID,
        },
        CreatedAt: time.Now(),
    }

    if err := adapter.Send(ctx, msg); err != nil {
        return "", fmt.Errorf("send failed: %w", err)
    }

    return fmt.Sprintf("sent message to %s:%s", cfg.Channel, cfg.ChannelID), nil
}
```

### 4.2 Agent Job

**Implementation note (updated 2026-02-01):**
- Agent jobs are supported in the shipped scheduler and use the `message` payload (`content`, optional `channel`/`channel_id`) rather than a dedicated agent block.
- The gateway `cron.AgentRunner` builds a synthetic inbound message and routes it through standard message handling (default channel is `api` with `cron:<job.ID>` when none is provided).
- Template rendering is implemented via `Scheduler.renderMessageContent` before invoking the runner.

```go
// internal/cron/executors/agent.go

func (s *CronScheduler) executeAgentJob(ctx context.Context, job *Job) (string, error) {
    cfg := job.Config.Agent
    if cfg == nil {
        return "", fmt.Errorf("agent config is nil")
    }

    // Render prompt template
    prompt := cfg.Prompt
    if cfg.Template != "" {
        tmpl, err := template.New("prompt").Parse(cfg.Template)
        if err != nil {
            return "", fmt.Errorf("invalid template: %w", err)
        }

        data := cfg.Data
        if data == nil {
            data = make(map[string]any)
        }
        data["now"] = time.Now()
        data["date"] = time.Now().Format("2006-01-02")
        data["time"] = time.Now().Format("15:04")

        var buf bytes.Buffer
        if err := tmpl.Execute(&buf, data); err != nil {
            return "", fmt.Errorf("template execution failed: %w", err)
        }
        prompt = buf.String()
    }

    // Create session for this job
    session := &models.Session{
        ID:        fmt.Sprintf("cron-%s-%d", job.ID, time.Now().Unix()),
        AgentID:   cfg.AgentID,
        Metadata: map[string]any{
            "cron_job": job.ID,
        },
    }

    // Build message
    msg := &models.Message{
        ID:        uuid.New().String(),
        SessionID: session.ID,
        Role:      models.RoleUser,
        Content:   prompt,
    }

    // Process with runtime
    chunks, err := s.runtime.Process(ctx, session, msg)
    if err != nil {
        return "", fmt.Errorf("runtime error: %w", err)
    }

    // Collect response
    var response strings.Builder
    for chunk := range chunks {
        if chunk.Error != nil {
            return response.String(), chunk.Error
        }
        if chunk.Text != "" {
            response.WriteString(chunk.Text)
        }
    }

    output := response.String()

    // Handle output routing
    if cfg.OutputTo != nil {
        if err := s.routeOutput(ctx, cfg.OutputTo, output); err != nil {
            s.logger.Error("failed to route output", "error", err)
        }
    }

    return output, nil
}

func (s *CronScheduler) routeOutput(ctx context.Context, cfg *OutputConfig, output string) error {
    if cfg.Channel != "" && cfg.ChannelID != "" {
        adapter, ok := s.channels.Get(cfg.Channel)
        if ok {
            msg := &models.Message{
                ID:        uuid.New().String(),
                Channel:   cfg.Channel,
                Direction: models.DirectionOutbound,
                Content:   output,
                Metadata: map[string]any{
                    "peer_id": cfg.ChannelID,
                },
            }
            return adapter.Send(ctx, msg)
        }
    }

    if cfg.File != "" {
        return os.WriteFile(cfg.File, []byte(output), 0644)
    }

    if cfg.Webhook != "" {
        body, _ := json.Marshal(map[string]string{"output": output})
        resp, err := s.http.Post(cfg.Webhook, "application/json", bytes.NewReader(body))
        if err != nil {
            return err
        }
        defer resp.Body.Close()
        if resp.StatusCode >= 400 {
            return fmt.Errorf("webhook returned %d", resp.StatusCode)
        }
    }

    return nil
}
```

### 4.3 Webhook Job

**MVP implementation note (as of 2026-01-26):**
- The shipped cron scheduler lives in `internal/cron/scheduler.go` and uses `config.CronWebhookConfig` (url/method/headers/body/timeout).
- Webhook auth helpers are implemented (bearer/basic/api_key) via `config.CronWebhookAuth` + scheduler auth helpers.
- Webhook calls enforce a default timeout (currently 30s) when `timeout` is not set.

```go
// internal/cron/executors/webhook.go

func (s *CronScheduler) executeWebhookJob(ctx context.Context, job *Job) (string, error) {
    cfg := job.Config.Webhook
    if cfg == nil {
        return "", fmt.Errorf("webhook config is nil")
    }

    method := cfg.Method
    if method == "" {
        method = "POST"
    }

    var bodyReader io.Reader
    if cfg.Body != "" {
        bodyReader = strings.NewReader(cfg.Body)
    }

    req, err := http.NewRequestWithContext(ctx, method, cfg.URL, bodyReader)
    if err != nil {
        return "", fmt.Errorf("failed to create request: %w", err)
    }

    // Set headers
    for k, v := range cfg.Headers {
        req.Header.Set(k, v)
    }

    // Apply auth
    if cfg.Auth != nil {
        switch cfg.Auth.Type {
        case "bearer":
            req.Header.Set("Authorization", "Bearer "+cfg.Auth.Token)
        case "basic":
            req.SetBasicAuth(cfg.Auth.User, cfg.Auth.Pass)
        case "api_key":
            header := cfg.Auth.Header
            if header == "" {
                header = "X-API-Key"
            }
            req.Header.Set(header, cfg.Auth.Token)
        }
    }

    resp, err := s.http.Do(req)
    if err != nil {
        return "", fmt.Errorf("request failed: %w", err)
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB safety cap

    if resp.StatusCode >= 400 {
        return string(body), fmt.Errorf("HTTP %d: %s", resp.StatusCode, body)
    }

    return fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncate(string(body), 500)), nil
}
```

### 4.4 Custom Job Handler

```go
// internal/cron/executors/custom.go

type JobHandler func(ctx context.Context, args map[string]any) (string, error)

func (s *CronScheduler) RegisterHandler(name string, handler JobHandler) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.handlers[name] = handler
}

func (s *CronScheduler) executeCustomJob(ctx context.Context, job *Job) (string, error) {
    cfg := job.Config.Custom
    if cfg == nil {
        return "", fmt.Errorf("custom config is nil")
    }

    s.mu.RLock()
    handler, ok := s.handlers[cfg.Handler]
    s.mu.RUnlock()

    if !ok {
        return "", fmt.Errorf("handler not found: %s", cfg.Handler)
    }

    return handler(ctx, cfg.Args)
}

// Example: Register built-in handlers
func (s *CronScheduler) registerBuiltinHandlers() {
    // Database cleanup handler
    s.RegisterHandler("db_cleanup", func(ctx context.Context, args map[string]any) (string, error) {
        days := 30
        if d, ok := args["days"].(float64); ok {
            days = int(d)
        }
        // Cleanup old data...
        return fmt.Sprintf("cleaned up data older than %d days", days), nil
    })

    // Memory compaction handler
    s.RegisterHandler("memory_compact", func(ctx context.Context, args map[string]any) (string, error) {
        // Compact memory store...
        return "memory compaction completed", nil
    })

    // Health check handler
    s.RegisterHandler("health_check", func(ctx context.Context, args map[string]any) (string, error) {
        // Run health checks...
        return "all systems healthy", nil
    })
}
```

---

## 5. Proposed Configuration (Future)

> The configuration below matches the *proposed* (future) cron job model in this design doc. For the shipped MVP
> configuration, see **Current Implementation (MVP, as of 2026-01-26)** at the top of this document.

```yaml
# nexus.yaml
cron:
  enabled: true

  jobs:
    # Daily summary message
    - id: daily-summary
      name: Daily Summary
      type: agent
      schedule:
        cron: "0 9 * * *"  # 9am daily
        timezone: America/New_York
      config:
        agent:
          agent_id: main
          template: |
            Generate a brief daily summary for {{.date}}.
            Include:
            - Weather forecast
            - Top news headlines
            - Calendar reminders
          output_to:
            channel: telegram
            channel_id: "123456789"
      enabled: true
      timeout: 5m

    # Hourly health check
    - id: health-check
      name: System Health Check
      type: webhook
      schedule:
        every: 1h
      config:
        webhook:
          url: https://healthchecks.io/ping/abc123
          method: GET
      enabled: true

    # Weekly report
    - id: weekly-report
      name: Weekly Activity Report
      type: agent
      schedule:
        cron: "0 18 * * 5"  # 6pm Friday
      config:
        agent:
          agent_id: main
          prompt: |
            Generate a weekly activity report summarizing:
            - Messages processed
            - Tool usage statistics
            - Notable conversations
          output_to:
            file: /var/log/nexus/weekly-report-{{.date}}.md
      retry:
        max_retries: 3
        backoff: 1m
        max_backoff: 10m
      enabled: true

    # Custom cleanup job
    - id: db-cleanup
      name: Database Cleanup
      type: custom
      schedule:
        cron: "0 3 * * 0"  # 3am Sunday
      config:
        custom:
          handler: db_cleanup
          args:
            days: 30
      enabled: true
```

---

## 6. CLI Commands

> CLI commands are not shipped yet (as of 2026-01-26). This section documents a proposed future interface.

```bash
# List all jobs
nexus cron list [--enabled] [--type <type>]

# Show job details
nexus cron show <job-id>

# Run job immediately
nexus cron run <job-id>

# Enable/disable job
nexus cron enable <job-id>
nexus cron disable <job-id>

# Show execution history
nexus cron history <job-id> [--limit 10]

# Show next scheduled runs
nexus cron next [--limit 5]

# Validate cron expression
nexus cron validate "0 9 * * 1-5"
```

---

## 7. Execution Store

> Execution history persistence is not shipped yet (as of 2026-01-26). The shipped MVP tracks `last_run` and `last_error`
> in memory only.

```go
// internal/cron/store.go

type ExecutionStore interface {
    Save(exec *JobExecution) error
    Get(execID string) (*JobExecution, error)
    ListByJob(jobID string, limit int) ([]*JobExecution, error)
    GetLast(jobID string) (*JobExecution, error)
    Prune(olderThan time.Duration) error
}

// In-memory store (for simple deployments)
type MemoryStore struct {
    executions map[string][]*JobExecution
    mu         sync.RWMutex
}

// Database store (for persistent history)
type DBStore struct {
    db *sql.DB
}

func (s *DBStore) Save(exec *JobExecution) error {
    _, err := s.db.Exec(`
        INSERT INTO cron_executions
        (id, job_id, status, started_at, completed_at, duration_ms, output, error, retry_count)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
    `, exec.ID, exec.JobID, exec.Status, exec.StartedAt, exec.CompletedAt,
        exec.Duration.Milliseconds(), exec.Output, exec.Error, exec.RetryCount)
    return err
}
```

---

## 8. Implementation Phases

### Phase 1: Core Scheduler (Week 1)
- [x] Job data model (config-driven MVP)
- [x] Cron scheduler with robfig/cron (parsing + next-run computation)
- [ ] Job registration/unregistration (beyond config)
- [x] Basic execution tracking (last run + last error)

### Phase 2: Job Executors (Week 2)
- [x] Message job executor
- [x] Webhook job executor
- [ ] Custom handler system
- [ ] Template rendering

### Phase 3: Agent Jobs (Week 3)
- [x] Agent job executor
- [x] Output routing
- [ ] Tool allowlisting for jobs
- [ ] Error handling & retry

### Phase 4: Persistence & CLI (Week 4)
- [ ] Execution store (memory + DB)
- [ ] CLI commands
- [ ] History pruning
- [ ] Documentation

---

## Appendix: Cron Expression Reference

| Field | Values | Special Characters |
|-------|--------|-------------------|
| Second (optional) | 0-59 | * / , - |
| Minute | 0-59 | * / , - |
| Hour | 0-23 | * / , - |
| Day of month | 1-31 | * / , - ? L W |
| Month | 1-12 or JAN-DEC | * / , - |
| Day of week | 0-6 or SUN-SAT | * / , - ? L # |

**Examples:**
- `0 9 * * *` - Daily at 9:00 AM
- `0 9 * * 1-5` - Weekdays at 9:00 AM
- `*/15 * * * *` - Every 15 minutes
- `0 0 1 * *` - First day of every month
- `0 18 * * 5` - Fridays at 6:00 PM
