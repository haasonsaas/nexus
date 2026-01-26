package cron

import (
	"context"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
)

// JobType identifies the handler for a cron job.
type JobType string

const (
	JobTypeMessage JobType = "message"
	JobTypeAgent   JobType = "agent"
	JobTypeWebhook JobType = "webhook"
	JobTypeCustom  JobType = "custom"
)

// Schedule represents a parsed schedule.
type Schedule struct {
	Kind     string
	CronExpr string
	Every    time.Duration
	At       time.Time
	Timezone string
}

// Job represents a scheduled job.
type Job struct {
	ID       string
	Name     string
	Type     JobType
	Enabled  bool
	Schedule Schedule

	Message *config.CronMessageConfig
	Webhook *config.CronWebhookConfig
	Custom  *config.CronCustomConfig
	Retry   config.CronRetryConfig

	NextRun    time.Time
	LastRun    time.Time
	LastError  string
	RetryCount int
}

// MessageSender executes outbound cron message jobs.
type MessageSender interface {
	Send(ctx context.Context, message *config.CronMessageConfig) error
}

// MessageSenderFunc adapts a function to a MessageSender.
type MessageSenderFunc func(ctx context.Context, message *config.CronMessageConfig) error

// Send executes the message sender function.
func (f MessageSenderFunc) Send(ctx context.Context, message *config.CronMessageConfig) error {
	return f(ctx, message)
}

// AgentRunner executes agent cron jobs.
type AgentRunner interface {
	Run(ctx context.Context, job *Job) error
}

// AgentRunnerFunc adapts a function to an AgentRunner.
type AgentRunnerFunc func(ctx context.Context, job *Job) error

// Run executes the agent runner function.
func (f AgentRunnerFunc) Run(ctx context.Context, job *Job) error {
	return f(ctx, job)
}

// CustomHandler executes custom cron jobs.
type CustomHandler interface {
	Handle(ctx context.Context, job *Job, args map[string]any) error
}

// CustomHandlerFunc adapts a function to a CustomHandler.
type CustomHandlerFunc func(ctx context.Context, job *Job, args map[string]any) error

// Handle executes the custom handler function.
func (f CustomHandlerFunc) Handle(ctx context.Context, job *Job, args map[string]any) error {
	return f(ctx, job, args)
}
