package web

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/cron"
)

// CronJobSummary is a safe representation of a cron job for UI/API.
type CronJobSummary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Enabled   bool      `json:"enabled"`
	Schedule  string    `json:"schedule"`
	NextRun   time.Time `json:"next_run"`
	LastRun   time.Time `json:"last_run"`
	LastError string    `json:"last_error,omitempty"`
}

type cronExecutionsResponse struct {
	Executions []*cron.JobExecution `json:"executions"`
}

// apiCron handles GET /api/cron.
func (h *Handler) apiCron(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jobs := h.listCronJobs()
	h.jsonResponse(w, apiCronResponse{
		Enabled: h.config != nil && h.config.GatewayConfig != nil && h.config.GatewayConfig.Cron.Enabled,
		Jobs:    jobs,
	})
}

// apiCronExecutions handles GET /api/cron/executions.
func (h *Handler) apiCronExecutions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.config == nil || h.config.CronScheduler == nil {
		h.jsonResponse(w, cronExecutionsResponse{})
		return
	}
	jobID := strings.TrimSpace(clampQueryParam(r, "job_id"))
	limit := parseIntParam(r, "limit", 50)
	if limit < 1 || limit > 200 {
		limit = 50
	}
	offset := parseIntParam(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	executions, err := h.config.CronScheduler.Executions(ctx, jobID, limit, offset)
	if err != nil {
		h.jsonError(w, "Failed to fetch cron executions", http.StatusInternalServerError)
		return
	}
	h.jsonResponse(w, cronExecutionsResponse{Executions: executions})
}

func (h *Handler) listCronJobs() []*CronJobSummary {
	if h == nil || h.config == nil || h.config.CronScheduler == nil {
		return nil
	}
	jobs := h.config.CronScheduler.Jobs()
	out := make([]*CronJobSummary, 0, len(jobs))
	for _, job := range jobs {
		if job == nil {
			continue
		}
		out = append(out, &CronJobSummary{
			ID:        job.ID,
			Name:      job.Name,
			Type:      string(job.Type),
			Enabled:   job.Enabled,
			Schedule:  formatSchedule(job.Schedule),
			NextRun:   job.NextRun,
			LastRun:   job.LastRun,
			LastError: job.LastError,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func formatSchedule(schedule cron.Schedule) string {
	switch schedule.Kind {
	case "cron":
		return fmt.Sprintf("cron: %s", schedule.CronExpr)
	case "every":
		if schedule.Timezone != "" {
			return fmt.Sprintf("every %s (%s)", schedule.Every, schedule.Timezone)
		}
		return fmt.Sprintf("every %s", schedule.Every)
	case "at":
		if schedule.Timezone != "" {
			return fmt.Sprintf("at %s (%s)", schedule.At.Format(time.RFC3339), schedule.Timezone)
		}
		return fmt.Sprintf("at %s", schedule.At.Format(time.RFC3339))
	default:
		return schedule.Kind
	}
}
