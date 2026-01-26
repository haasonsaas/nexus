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

	NextRun   time.Time
	LastRun   time.Time
	LastError string
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
