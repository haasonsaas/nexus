package cron

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/google/uuid"

	"github.com/haasonsaas/nexus/internal/config"
)

var defaultWebhookTimeout = 30 * time.Second

// Scheduler runs cron jobs from configuration.
type Scheduler struct {
	jobs           []*Job
	logger         *slog.Logger
	httpClient     *http.Client
	messageSender  MessageSender
	agentRunner    AgentRunner
	customHandlers map[string]CustomHandler
	executionStore ExecutionStore
	now            func() time.Time
	tickInterval   time.Duration

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

// WithMessageSender configures the message sender used for message jobs.
func WithMessageSender(sender MessageSender) Option {
	return func(s *Scheduler) {
		if sender != nil {
			s.messageSender = sender
		}
	}
}

// WithAgentRunner configures the agent runner used for agent jobs.
func WithAgentRunner(runner AgentRunner) Option {
	return func(s *Scheduler) {
		if runner != nil {
			s.agentRunner = runner
		}
	}
}

// WithCustomHandler registers a custom handler by name.
func WithCustomHandler(name string, handler CustomHandler) Option {
	return func(s *Scheduler) {
		s.RegisterCustomHandler(name, handler)
	}
}

// WithExecutionStore configures the execution history store.
func WithExecutionStore(store ExecutionStore) Option {
	return func(s *Scheduler) {
		if store != nil {
			s.executionStore = store
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

// SetMessageSender updates the sender for message jobs after initialization.
func (s *Scheduler) SetMessageSender(sender MessageSender) {
	if s == nil || sender == nil {
		return
	}
	s.mu.Lock()
	s.messageSender = sender
	s.mu.Unlock()
}

// SetAgentRunner updates the runner for agent jobs after initialization.
func (s *Scheduler) SetAgentRunner(runner AgentRunner) {
	if s == nil || runner == nil {
		return
	}
	s.mu.Lock()
	s.agentRunner = runner
	s.mu.Unlock()
}

// SetExecutionStore updates the execution store after initialization.
func (s *Scheduler) SetExecutionStore(store ExecutionStore) {
	if s == nil || store == nil {
		return
	}
	s.mu.Lock()
	s.executionStore = store
	s.mu.Unlock()
}

// RegisterCustomHandler registers a handler for custom cron jobs.
func (s *Scheduler) RegisterCustomHandler(name string, handler CustomHandler) {
	if s == nil || handler == nil {
		return
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return
	}
	s.mu.Lock()
	if s.customHandlers == nil {
		s.customHandlers = make(map[string]CustomHandler)
	}
	s.customHandlers[name] = handler
	s.mu.Unlock()
}

// NewScheduler creates a scheduler from config.
func NewScheduler(cfg config.CronConfig, opts ...Option) (*Scheduler, error) {
	scheduler := &Scheduler{
		logger:         slog.Default().With("component", "cron"),
		httpClient:     http.DefaultClient,
		customHandlers: make(map[string]CustomHandler),
		executionStore: NewMemoryExecutionStore(),
		now:            time.Now,
		tickInterval:   time.Second,
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
		if job.Custom != nil {
			customCopy := *job.Custom
			if job.Custom.Args != nil {
				args := make(map[string]any, len(job.Custom.Args))
				for k, v := range job.Custom.Args {
					args[k] = v
				}
				customCopy.Args = args
			}
			copyJob.Custom = &customCopy
		}
		out = append(out, &copyJob)
	}
	return out
}

// RegisterJob adds or replaces a cron job at runtime.
func (s *Scheduler) RegisterJob(cfg config.CronJobConfig) (*Job, error) {
	if s == nil {
		return nil, nil
	}
	job, err := s.buildJob(cfg, s.now())
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, existing := range s.jobs {
		if existing != nil && existing.ID == job.ID {
			s.jobs[i] = job
			return job, nil
		}
	}
	s.jobs = append(s.jobs, job)
	return job, nil
}

// UnregisterJob removes a cron job by id.
func (s *Scheduler) UnregisterJob(id string) bool {
	if s == nil {
		return false
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, job := range s.jobs {
		if job != nil && job.ID == id {
			s.jobs = append(s.jobs[:i], s.jobs[i+1:]...)
			return true
		}
	}
	return false
}

// Executions returns execution history for a job.
func (s *Scheduler) Executions(ctx context.Context, jobID string, limit, offset int) ([]*JobExecution, error) {
	if s == nil || s.executionStore == nil {
		return nil, nil
	}
	return s.executionStore.List(ctx, strings.TrimSpace(jobID), limit, offset)
}

// PruneExecutions prunes execution history older than the provided duration.
func (s *Scheduler) PruneExecutions(ctx context.Context, olderThan time.Duration) (int64, error) {
	if s == nil || s.executionStore == nil {
		return 0, nil
	}
	if olderThan <= 0 {
		return 0, nil
	}
	return s.executionStore.Prune(ctx, olderThan)
}

// RunJob executes a specific cron job by id and updates its schedule metadata.
func (s *Scheduler) RunJob(ctx context.Context, id string) error {
	if s == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("job id required")
	}

	var target *Job
	s.mu.Lock()
	for _, job := range s.jobs {
		if job != nil && job.ID == id {
			target = job
			break
		}
	}
	s.mu.Unlock()
	if target == nil {
		return fmt.Errorf("job not found")
	}
	return s.runJob(ctx, target, s.now())
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
		s.mu.Unlock()

		err := s.runJob(ctx, job, now)
		if err != nil {
			s.logger.Warn("cron job failed", "id", job.ID, "error", err)
		}
		count++
	}
	return count
}

func (s *Scheduler) runJob(ctx context.Context, job *Job, now time.Time) error {
	if s == nil || job == nil {
		return errors.New("job is nil")
	}
	s.mu.Lock()
	job.LastRun = now
	retryCount := job.RetryCount
	schedule := job.Schedule
	s.mu.Unlock()

	exec := s.startExecution(ctx, job, retryCount, now)
	err := s.executeJob(ctx, job)
	s.finishExecution(ctx, exec, err, now)

	s.mu.Lock()
	if err != nil {
		job.LastError = err.Error()
	} else {
		job.LastError = ""
	}
	next, disable, nextErr := s.nextRunForJob(job, schedule, now, err)
	if nextErr != nil {
		job.LastError = nextErr.Error()
		job.NextRun = time.Time{}
		job.Enabled = false
	} else if disable {
		job.NextRun = time.Time{}
		job.Enabled = false
	} else {
		job.NextRun = next
	}
	s.mu.Unlock()

	return err
}

func (s *Scheduler) startExecution(ctx context.Context, job *Job, retryCount int, startedAt time.Time) *JobExecution {
	if s == nil || s.executionStore == nil || job == nil {
		return nil
	}
	exec := &JobExecution{
		ID:        uuid.NewString(),
		JobID:     job.ID,
		Status:    ExecutionRunning,
		StartedAt: startedAt,
		Retry:     retryCount,
	}
	if err := s.executionStore.Create(ctx, exec); err != nil && s.logger != nil {
		s.logger.Warn("cron execution create failed", "job_id", job.ID, "error", err)
	}
	return exec
}

func (s *Scheduler) finishExecution(ctx context.Context, exec *JobExecution, err error, finishedAt time.Time) {
	if s == nil || s.executionStore == nil || exec == nil {
		return
	}
	exec.CompletedAt = finishedAt
	exec.Duration = finishedAt.Sub(exec.StartedAt)
	if err != nil {
		exec.Status = ExecutionFailed
		exec.Error = err.Error()
	} else {
		exec.Status = ExecutionSucceeded
		exec.Error = ""
	}
	if updateErr := s.executionStore.Update(ctx, exec); updateErr != nil && s.logger != nil {
		s.logger.Warn("cron execution update failed", "job_id", exec.JobID, "error", updateErr)
	}
}

func (s *Scheduler) nextRunForJob(job *Job, schedule Schedule, now time.Time, err error) (time.Time, bool, error) {
	if job == nil {
		return time.Time{}, true, errors.New("job is nil")
	}
	if err != nil {
		maxRetries := job.Retry.MaxRetries
		if maxRetries > 0 && job.RetryCount < maxRetries {
			job.RetryCount++
			return now.Add(retryDelay(job.Retry, job.RetryCount)), false, nil
		}
	}
	job.RetryCount = 0
	next, ok, nextErr := schedule.Next(now)
	if nextErr != nil {
		return time.Time{}, true, nextErr
	}
	if ok {
		return next, false, nil
	}
	return time.Time{}, true, nil
}

func retryDelay(cfg config.CronRetryConfig, attempt int) time.Duration {
	if attempt <= 0 {
		attempt = 1
	}
	backoff := cfg.Backoff
	if backoff <= 0 {
		backoff = 5 * time.Second
	}
	delay := backoff
	if attempt > 1 {
		factor := 1 << (attempt - 1)
		delay = time.Duration(factor) * backoff
	}
	if cfg.MaxBackoff > 0 && delay > cfg.MaxBackoff {
		return cfg.MaxBackoff
	}
	return delay
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
	case JobTypeMessage:
		if cfg.Message == nil {
			return nil, fmt.Errorf("message job missing payload")
		}
		if strings.TrimSpace(cfg.Message.Channel) == "" || strings.TrimSpace(cfg.Message.ChannelID) == "" {
			return nil, fmt.Errorf("message job missing channel")
		}
		if strings.TrimSpace(cfg.Message.Content) == "" && strings.TrimSpace(cfg.Message.Template) == "" {
			return nil, fmt.Errorf("message job missing content")
		}
		if len(cfg.Message.Tools) > 0 {
			return nil, fmt.Errorf("message job cannot set tools")
		}
	case JobTypeAgent:
		if cfg.Message == nil {
			return nil, fmt.Errorf("agent job missing payload")
		}
		if strings.TrimSpace(cfg.Message.Content) == "" && strings.TrimSpace(cfg.Message.Template) == "" {
			return nil, fmt.Errorf("agent job missing content")
		}
		channel := strings.TrimSpace(cfg.Message.Channel)
		channelID := strings.TrimSpace(cfg.Message.ChannelID)
		if (channel == "" && channelID != "") || (channel != "" && channelID == "") {
			return nil, fmt.Errorf("agent job missing channel")
		}
	case JobTypeCustom:
		if cfg.Custom == nil || strings.TrimSpace(cfg.Custom.Handler) == "" {
			return nil, fmt.Errorf("custom job missing handler")
		}
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
		Custom:   cfg.Custom,
		Retry:    cfg.Retry,
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
	case JobTypeMessage:
		return s.executeMessage(ctx, job)
	case JobTypeAgent:
		return s.executeAgent(ctx, job)
	case JobTypeCustom:
		return s.executeCustom(ctx, job)
	default:
		return fmt.Errorf("job type %s not implemented", job.Type)
	}
}

func (s *Scheduler) executeMessage(ctx context.Context, job *Job) error {
	if s.messageSender == nil {
		return errors.New("message sender not configured")
	}
	if job.Message == nil {
		return errors.New("missing message payload")
	}
	channel := strings.TrimSpace(job.Message.Channel)
	channelID := strings.TrimSpace(job.Message.ChannelID)
	if channel == "" || channelID == "" {
		return errors.New("message payload missing channel")
	}
	content, err := s.renderMessageContent(job.Message)
	if err != nil {
		return err
	}
	if strings.TrimSpace(content) == "" {
		return errors.New("message payload missing content")
	}
	messageCopy := *job.Message
	messageCopy.Content = content
	return s.messageSender.Send(ctx, &messageCopy)
}

func (s *Scheduler) executeAgent(ctx context.Context, job *Job) error {
	if s.agentRunner == nil {
		return errors.New("agent runner not configured")
	}
	if job.Message == nil {
		return errors.New("missing agent payload")
	}
	content, err := s.renderMessageContent(job.Message)
	if err != nil {
		return err
	}
	if strings.TrimSpace(content) == "" {
		return errors.New("agent payload missing content")
	}
	channel := strings.TrimSpace(job.Message.Channel)
	channelID := strings.TrimSpace(job.Message.ChannelID)
	if (channel == "" && channelID != "") || (channel != "" && channelID == "") {
		return errors.New("agent payload missing channel")
	}
	jobCopy := *job
	msgCopy := *job.Message
	msgCopy.Content = content
	jobCopy.Message = &msgCopy
	return s.agentRunner.Run(ctx, &jobCopy)
}

func (s *Scheduler) executeCustom(ctx context.Context, job *Job) error {
	if job.Custom == nil {
		return errors.New("missing custom payload")
	}
	handlerName := strings.ToLower(strings.TrimSpace(job.Custom.Handler))
	if handlerName == "" {
		return errors.New("custom handler missing")
	}
	s.mu.Lock()
	handler := s.customHandlers[handlerName]
	s.mu.Unlock()
	if handler == nil {
		return fmt.Errorf("custom handler not registered: %s", job.Custom.Handler)
	}
	return handler.Handle(ctx, job, job.Custom.Args)
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
	if err := applyWebhookAuth(req, cfg.Auth); err != nil {
		return err
	}

	client := s.httpClient
	if client == nil {
		client = http.DefaultClient
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultWebhookTimeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
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

func applyWebhookAuth(req *http.Request, auth *config.CronWebhookAuth) error {
	if req == nil || auth == nil {
		return nil
	}
	authType := strings.ToLower(strings.TrimSpace(auth.Type))
	switch authType {
	case "":
		return errors.New("webhook auth type is required")
	case "bearer":
		token := strings.TrimSpace(auth.Token)
		if token == "" {
			return errors.New("webhook bearer token is required")
		}
		req.Header.Set("Authorization", "Bearer "+token)
	case "basic":
		user := strings.TrimSpace(auth.User)
		if user == "" {
			return errors.New("webhook basic auth user is required")
		}
		req.SetBasicAuth(user, auth.Pass)
	case "api_key":
		header := strings.TrimSpace(auth.Header)
		if header == "" {
			return errors.New("webhook api_key header is required")
		}
		token := strings.TrimSpace(auth.Token)
		if token == "" {
			return errors.New("webhook api_key token is required")
		}
		req.Header.Set(header, token)
	default:
		return fmt.Errorf("unsupported webhook auth type %q", auth.Type)
	}
	return nil
}

func (s *Scheduler) renderMessageContent(message *config.CronMessageConfig) (string, error) {
	if message == nil {
		return "", errors.New("missing message payload")
	}
	templateText := strings.TrimSpace(message.Template)
	if templateText == "" {
		return message.Content, nil
	}
	now := time.Now()
	if s != nil && s.now != nil {
		now = s.now()
	}
	data := make(map[string]any, len(message.Data)+3)
	for k, v := range message.Data {
		data[k] = v
	}
	data["now"] = now
	data["date"] = now.Format("2006-01-02")
	data["time"] = now.Format("15:04")

	tmpl, err := template.New("cron").Option("missingkey=zero").Parse(templateText)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}
