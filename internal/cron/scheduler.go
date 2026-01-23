package cron

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
)

// Scheduler runs cron jobs from configuration.
type Scheduler struct {
	jobs         []*Job
	logger       *slog.Logger
	httpClient   *http.Client
	now          func() time.Time
	tickInterval time.Duration

	mu      sync.Mutex
	started bool
	wg      sync.WaitGroup
}

// Option configures the scheduler.
type Option func(*Scheduler)

// WithLogger configures the scheduler logger.
func WithLogger(logger *slog.Logger) Option {
	return func(s *Scheduler) {
		if logger != nil {
			s.logger = logger
		}
	}
}

// WithHTTPClient configures the HTTP client used for webhook jobs.
func WithHTTPClient(client *http.Client) Option {
	return func(s *Scheduler) {
		if client != nil {
			s.httpClient = client
		}
	}
}

// WithNow overrides the clock for tests.
func WithNow(now func() time.Time) Option {
	return func(s *Scheduler) {
		if now != nil {
			s.now = now
		}
	}
}

// WithTickInterval overrides the scheduler tick interval.
func WithTickInterval(interval time.Duration) Option {
	return func(s *Scheduler) {
		if interval > 0 {
			s.tickInterval = interval
		}
	}
}

// NewScheduler creates a scheduler from config.
func NewScheduler(cfg config.CronConfig, opts ...Option) (*Scheduler, error) {
	scheduler := &Scheduler{
		logger:       slog.Default().With("component", "cron"),
		httpClient:   http.DefaultClient,
		now:          time.Now,
		tickInterval: time.Second,
	}
	for _, opt := range opts {
		opt(scheduler)
	}

	jobs := make([]*Job, 0, len(cfg.Jobs))
	now := scheduler.now()
	for _, entry := range cfg.Jobs {
		job, err := scheduler.buildJob(entry, now)
		if err != nil {
			scheduler.logger.Warn("cron job skipped", "id", entry.ID, "error", err)
			continue
		}
		jobs = append(jobs, job)
	}
	scheduler.jobs = jobs
	return scheduler, nil
}

// Start begins running cron jobs until the context is cancelled.
func (s *Scheduler) Start(ctx context.Context) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = true
	s.mu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.tickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runDue(ctx)
			}
		}
	}()
	return nil
}

// Stop waits for the scheduler loop to stop.
func (s *Scheduler) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RunOnce executes due jobs immediately (primarily for tests).
func (s *Scheduler) RunOnce(ctx context.Context) int {
	if s == nil {
		return 0
	}
	return s.runDue(ctx)
}

// Jobs returns a snapshot of configured cron jobs.
func (s *Scheduler) Jobs() []*Job {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]*Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		if job == nil {
			continue
		}
		copyJob := *job
		if job.Message != nil {
			msgCopy := *job.Message
			copyJob.Message = &msgCopy
		}
		if job.Webhook != nil {
			webhookCopy := *job.Webhook
			if job.Webhook.Headers != nil {
				headers := make(map[string]string, len(job.Webhook.Headers))
				for k, v := range job.Webhook.Headers {
					headers[k] = v
				}
				webhookCopy.Headers = headers
			}
			copyJob.Webhook = &webhookCopy
		}
		out = append(out, &copyJob)
	}
	return out
}

func (s *Scheduler) runDue(ctx context.Context) int {
	now := s.now()
	count := 0
	s.mu.Lock()
	jobs := make([]*Job, len(s.jobs))
	copy(jobs, s.jobs)
	s.mu.Unlock()

	for _, job := range jobs {
		if job == nil {
			continue
		}
		s.mu.Lock()
		if !job.Enabled || job.NextRun.IsZero() || now.Before(job.NextRun) {
			s.mu.Unlock()
			continue
		}
		job.LastRun = now
		schedule := job.Schedule
		jobID := job.ID
		s.mu.Unlock()

		err := s.executeJob(ctx, job)
		if err != nil {
			s.logger.Warn("cron job failed", "id", jobID, "error", err)
		}

		next, ok, nextErr := schedule.Next(now)

		s.mu.Lock()
		if err != nil {
			job.LastError = err.Error()
		} else {
			job.LastError = ""
		}
		if nextErr != nil {
			job.LastError = nextErr.Error()
			job.NextRun = time.Time{}
			job.Enabled = false
		} else if ok {
			job.NextRun = next
		} else {
			job.NextRun = time.Time{}
			job.Enabled = false
		}
		s.mu.Unlock()
		count++
	}
	return count
}

func (s *Scheduler) buildJob(cfg config.CronJobConfig, now time.Time) (*Job, error) {
	if strings.TrimSpace(cfg.ID) == "" {
		return nil, fmt.Errorf("job id required")
	}
	if !cfg.Enabled {
		return nil, fmt.Errorf("job disabled")
	}
	schedule, err := NewSchedule(cfg.Schedule)
	if err != nil {
		return nil, err
	}
	jobType := JobType(strings.ToLower(strings.TrimSpace(cfg.Type)))
	switch jobType {
	case JobTypeWebhook:
		if cfg.Webhook == nil || strings.TrimSpace(cfg.Webhook.URL) == "" {
			return nil, fmt.Errorf("webhook job missing url")
		}
	case JobTypeMessage, JobTypeAgent:
		return nil, fmt.Errorf("job type %s not implemented", jobType)
	default:
		return nil, fmt.Errorf("unsupported job type %q", cfg.Type)
	}

	next, ok, err := schedule.Next(now)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("no next run scheduled")
	}

	return &Job{
		ID:       cfg.ID,
		Name:     cfg.Name,
		Type:     jobType,
		Enabled:  cfg.Enabled,
		Schedule: schedule,
		Message:  cfg.Message,
		Webhook:  cfg.Webhook,
		NextRun:  next,
	}, nil
}

func (s *Scheduler) executeJob(ctx context.Context, job *Job) error {
	if job == nil {
		return errors.New("job is nil")
	}
	switch job.Type {
	case JobTypeWebhook:
		return s.executeWebhook(ctx, job)
	default:
		return fmt.Errorf("job type %s not implemented", job.Type)
	}
}

func (s *Scheduler) executeWebhook(ctx context.Context, job *Job) error {
	cfg := job.Webhook
	if cfg == nil {
		return errors.New("missing webhook config")
	}
	method := strings.ToUpper(strings.TrimSpace(cfg.Method))
	if method == "" {
		method = http.MethodPost
	}
	requestBody := strings.NewReader(cfg.Body)
	req, err := http.NewRequestWithContext(ctx, method, cfg.URL, requestBody)
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	for key, value := range cfg.Headers {
		req.Header.Set(key, value)
	}

	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
		req = req.WithContext(ctx)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}
