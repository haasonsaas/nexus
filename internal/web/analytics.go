package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

// AnalyticsOverview is a lightweight metrics snapshot for a time window.
type AnalyticsOverview struct {
	Period string `json:"period"`

	AgentID string    `json:"agent_id"`
	Since   time.Time `json:"since"`
	Until   time.Time `json:"until"`

	TotalConversations         int64   `json:"total_conversations"`
	TotalMessages              int64   `json:"total_messages"`
	AvgMessagesPerConversation float64 `json:"avg_messages_per_conversation"`

	MessagesPerDay   []TimeSeriesPoint `json:"messages_per_day,omitempty"`
	TopTools         []ToolUsageCount  `json:"top_tools,omitempty"`
	ToolCalls        int64             `json:"tool_calls"`
	ToolResults      int64             `json:"tool_results"`
	ToolErrors       int64             `json:"tool_errors"`
	ToolErrorRatePct float64           `json:"tool_error_rate_pct"`
}

type TimeSeriesPoint struct {
	Day   string `json:"day"`
	Count int64  `json:"count"`
}

type ToolUsageCount struct {
	Tool  string `json:"tool"`
	Count int64  `json:"count"`
}

func (h *Handler) computeAnalyticsOverview(ctx context.Context, agentID string, period string) (*AnalyticsOverview, error) {
	if h == nil || h.config == nil || h.config.SessionStore == nil {
		return nil, fmt.Errorf("session store not configured")
	}

	dur, normalized, err := parseAnalyticsPeriod(period)
	if err != nil {
		return nil, err
	}

	until := time.Now().UTC()
	since := until.Add(-dur)

	store := unwrapSessionStore(h.config.SessionStore)
	if db := sessionStoreDB(store); db != nil {
		return analyticsOverviewFromDB(ctx, db, agentID, normalized, since, until)
	}

	return analyticsOverviewFromStore(ctx, store, agentID, normalized, since, until)
}

func parseAnalyticsPeriod(raw string) (time.Duration, string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		value = "7d"
	}

	if strings.HasSuffix(value, "d") {
		daysRaw := strings.TrimSuffix(value, "d")
		days, err := strconv.Atoi(daysRaw)
		if err != nil || days <= 0 {
			return 0, "", fmt.Errorf("invalid period %q", raw)
		}
		return time.Duration(days) * 24 * time.Hour, fmt.Sprintf("%dd", days), nil
	}

	dur, err := time.ParseDuration(value)
	if err != nil || dur <= 0 {
		return 0, "", fmt.Errorf("invalid period %q", raw)
	}
	return dur, value, nil
}

type storeWrapper interface {
	Store() sessions.Store
}

func unwrapSessionStore(store sessions.Store) sessions.Store {
	for {
		wrapper, ok := store.(storeWrapper)
		if !ok {
			return store
		}
		next := wrapper.Store()
		if next == nil || next == store {
			return store
		}
		store = next
	}
}

type dbSessionStore interface {
	DB() *sql.DB
}

func sessionStoreDB(store sessions.Store) *sql.DB {
	if store == nil {
		return nil
	}
	if dbStore, ok := store.(dbSessionStore); ok {
		return dbStore.DB()
	}
	return nil
}

func analyticsOverviewFromDB(ctx context.Context, db *sql.DB, agentID string, period string, since, until time.Time) (*AnalyticsOverview, error) {
	if db == nil {
		return nil, fmt.Errorf("db is nil")
	}
	if strings.TrimSpace(agentID) == "" {
		return nil, fmt.Errorf("agent id is required")
	}

	where := `
		FROM messages m
		JOIN sessions s ON m.session_id = s.id
		WHERE s.agent_id = $1 AND m.created_at >= $2 AND m.created_at < $3
	`

	var totalMessages int64
	if err := db.QueryRowContext(ctx, "SELECT count(*) "+where, agentID, since, until).Scan(&totalMessages); err != nil {
		return nil, fmt.Errorf("count messages: %w", err)
	}

	var totalConversations int64
	if err := db.QueryRowContext(ctx, "SELECT count(DISTINCT m.session_id) "+where, agentID, since, until).Scan(&totalConversations); err != nil {
		return nil, fmt.Errorf("count conversations: %w", err)
	}

	messagesPerDay, err := dbMessagesPerDay(ctx, db, agentID, since, until)
	if err != nil {
		return nil, err
	}

	toolCalls, toolResults, toolErrors, topTools := dbToolStats(ctx, db, agentID, since, until)

	avgMessages := 0.0
	if totalConversations > 0 {
		avgMessages = float64(totalMessages) / float64(totalConversations)
	}
	toolErrorRatePct := 0.0
	if toolResults > 0 {
		toolErrorRatePct = (float64(toolErrors) / float64(toolResults)) * 100
	}

	return &AnalyticsOverview{
		Period:                     period,
		AgentID:                    agentID,
		Since:                      since,
		Until:                      until,
		TotalConversations:         totalConversations,
		TotalMessages:              totalMessages,
		AvgMessagesPerConversation: avgMessages,
		MessagesPerDay:             messagesPerDay,
		TopTools:                   topTools,
		ToolCalls:                  toolCalls,
		ToolResults:                toolResults,
		ToolErrors:                 toolErrors,
		ToolErrorRatePct:           toolErrorRatePct,
	}, nil
}

func dbMessagesPerDay(ctx context.Context, db *sql.DB, agentID string, since, until time.Time) ([]TimeSeriesPoint, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT date_trunc('day', m.created_at) AS day, count(*)
		FROM messages m
		JOIN sessions s ON m.session_id = s.id
		WHERE s.agent_id = $1 AND m.created_at >= $2 AND m.created_at < $3
		GROUP BY day
		ORDER BY day ASC
	`, agentID, since, until)
	if err != nil {
		return nil, fmt.Errorf("messages per day: %w", err)
	}
	defer rows.Close()

	var points []TimeSeriesPoint
	for rows.Next() {
		var day time.Time
		var count int64
		if err := rows.Scan(&day, &count); err != nil {
			return nil, fmt.Errorf("messages per day scan: %w", err)
		}
		points = append(points, TimeSeriesPoint{
			Day:   day.UTC().Format("2006-01-02"),
			Count: count,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("messages per day rows: %w", err)
	}
	return points, nil
}

func dbToolStats(ctx context.Context, db *sql.DB, agentID string, since, until time.Time) (toolCalls int64, toolResults int64, toolErrors int64, topTools []ToolUsageCount) {
	toolCounts := make(map[string]int64)

	// Tool calls are stored on assistant messages as JSONB.
	callRows, err := db.QueryContext(ctx, `
		SELECT m.tool_calls
		FROM messages m
		JOIN sessions s ON m.session_id = s.id
		WHERE s.agent_id = $1 AND m.created_at >= $2 AND m.created_at < $3 AND m.tool_calls IS NOT NULL
	`, agentID, since, until)
	if err == nil {
		for callRows.Next() {
			var raw []byte
			if err := callRows.Scan(&raw); err != nil {
				continue
			}
			if len(raw) == 0 || string(raw) == "null" {
				continue
			}
			var calls []models.ToolCall
			if err := json.Unmarshal(raw, &calls); err != nil {
				continue
			}
			for _, c := range calls {
				name := strings.TrimSpace(c.Name)
				if name == "" {
					continue
				}
				toolCalls++
				toolCounts[name]++
			}
		}
		callRows.Close()
	}

	// Tool results are stored on tool messages as JSONB.
	resultRows, err := db.QueryContext(ctx, `
		SELECT m.tool_results
		FROM messages m
		JOIN sessions s ON m.session_id = s.id
		WHERE s.agent_id = $1 AND m.created_at >= $2 AND m.created_at < $3 AND m.tool_results IS NOT NULL
	`, agentID, since, until)
	if err == nil {
		for resultRows.Next() {
			var raw []byte
			if err := resultRows.Scan(&raw); err != nil {
				continue
			}
			if len(raw) == 0 || string(raw) == "null" {
				continue
			}
			var results []models.ToolResult
			if err := json.Unmarshal(raw, &results); err != nil {
				continue
			}
			for _, res := range results {
				toolResults++
				if res.IsError {
					toolErrors++
				}
			}
		}
		resultRows.Close()
	}

	topTools = topToolCounts(toolCounts, 10)
	return toolCalls, toolResults, toolErrors, topTools
}

func analyticsOverviewFromStore(ctx context.Context, store sessions.Store, agentID string, period string, since, until time.Time) (*AnalyticsOverview, error) {
	if store == nil {
		return nil, fmt.Errorf("session store not configured")
	}
	if strings.TrimSpace(agentID) == "" {
		return nil, fmt.Errorf("agent id is required")
	}

	// Best-effort scan using store APIs. This can be expensive and may be incomplete
	// for stores that enforce server-side history limits.
	const pageSize = 500
	offset := 0

	activeSessions := make(map[string]struct{})
	messagesPerDay := make(map[string]int64)
	toolCounts := make(map[string]int64)

	var totalMessages int64
	var toolCalls int64
	var toolResults int64
	var toolErrors int64

	for {
		batch, err := store.List(ctx, agentID, sessions.ListOptions{Limit: pageSize, Offset: offset})
		if err != nil {
			return nil, fmt.Errorf("list sessions: %w", err)
		}
		if len(batch) == 0 {
			break
		}
		offset += len(batch)

		for _, session := range batch {
			history, err := store.GetHistory(ctx, session.ID, 100000)
			if err != nil {
				continue
			}
			for _, msg := range history {
				if msg == nil {
					continue
				}
				if msg.CreatedAt.Before(since) || !msg.CreatedAt.Before(until) {
					continue
				}

				activeSessions[session.ID] = struct{}{}
				totalMessages++

				day := msg.CreatedAt.UTC().Format("2006-01-02")
				messagesPerDay[day]++

				for _, call := range msg.ToolCalls {
					name := strings.TrimSpace(call.Name)
					if name == "" {
						continue
					}
					toolCalls++
					toolCounts[name]++
				}

				for _, res := range msg.ToolResults {
					toolResults++
					if res.IsError {
						toolErrors++
					}
				}
			}
		}

		if len(batch) < pageSize {
			break
		}
	}

	totalConversations := int64(len(activeSessions))
	avgMessages := 0.0
	if totalConversations > 0 {
		avgMessages = float64(totalMessages) / float64(totalConversations)
	}
	toolErrorRatePct := 0.0
	if toolResults > 0 {
		toolErrorRatePct = (float64(toolErrors) / float64(toolResults)) * 100
	}

	return &AnalyticsOverview{
		Period:                     period,
		AgentID:                    agentID,
		Since:                      since,
		Until:                      until,
		TotalConversations:         totalConversations,
		TotalMessages:              totalMessages,
		AvgMessagesPerConversation: avgMessages,
		MessagesPerDay:             flattenTimeSeries(messagesPerDay),
		TopTools:                   topToolCounts(toolCounts, 10),
		ToolCalls:                  toolCalls,
		ToolResults:                toolResults,
		ToolErrors:                 toolErrors,
		ToolErrorRatePct:           toolErrorRatePct,
	}, nil
}

func flattenTimeSeries(raw map[string]int64) []TimeSeriesPoint {
	if len(raw) == 0 {
		return nil
	}
	days := make([]string, 0, len(raw))
	for day := range raw {
		days = append(days, day)
	}
	sort.Strings(days)

	out := make([]TimeSeriesPoint, 0, len(days))
	for _, day := range days {
		out = append(out, TimeSeriesPoint{Day: day, Count: raw[day]})
	}
	return out
}

func topToolCounts(raw map[string]int64, limit int) []ToolUsageCount {
	if len(raw) == 0 || limit <= 0 {
		return nil
	}
	items := make([]ToolUsageCount, 0, len(raw))
	for tool, count := range raw {
		items = append(items, ToolUsageCount{Tool: tool, Count: count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count != items[j].Count {
			return items[i].Count > items[j].Count
		}
		return items[i].Tool < items[j].Tool
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}
