package gateway

import (
	"strings"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/pkg/models"
)

func TestSteeringForMessageMatchesByChannelAndTag(t *testing.T) {
	cfg := &config.Config{
		Steering: config.SteeringConfig{
			Enabled: true,
			Rules: []config.SteeringRule{
				{
					ID:       "priority",
					Prompt:   "Be extra concise.",
					Channels: []string{"slack"},
					Tags:     []string{"vip"},
				},
			},
		},
	}
	server := &Server{config: cfg}

	session := &models.Session{AgentID: "main"}
	msg := &models.Message{
		Channel: models.ChannelSlack,
		Role:    models.RoleUser,
		Content: "hello",
		Metadata: map[string]any{
			"tags": []string{"vip"},
		},
	}

	prompt, trace := server.steeringForMessage(session, msg)
	if !strings.Contains(prompt, "extra concise") {
		t.Fatalf("expected steering prompt, got %q", prompt)
	}
	if len(trace) != 1 || trace[0].ID != "priority" {
		t.Fatalf("expected steering trace for rule, got %#v", trace)
	}
	if !trace[0].Matched {
		t.Fatalf("expected rule to be marked matched")
	}
}

func TestSteeringForMessageRespectsPriority(t *testing.T) {
	enabled := true
	cfg := &config.Config{
		Steering: config.SteeringConfig{
			Enabled: true,
			Rules: []config.SteeringRule{
				{
					ID:       "low",
					Prompt:   "Low priority",
					Priority: 1,
					Enabled:  &enabled,
				},
				{
					ID:       "high",
					Prompt:   "High priority",
					Priority: 10,
					Enabled:  &enabled,
				},
			},
		},
	}
	server := &Server{config: cfg}

	msg := &models.Message{
		Channel: models.ChannelAPI,
		Role:    models.RoleUser,
		Content: "ping",
	}

	prompt, trace := server.steeringForMessage(&models.Session{AgentID: "main"}, msg)
	if prompt != "High priority\nLow priority" {
		t.Fatalf("expected priority ordering, got %q", prompt)
	}
	if len(trace) != 2 {
		t.Fatalf("expected 2 traces, got %d", len(trace))
	}
	if trace[0].ID != "high" {
		t.Fatalf("expected high priority trace first, got %#v", trace)
	}
}

func TestSteeringForMessageHonorsTimeWindow(t *testing.T) {
	now := time.Now()
	after := now.Add(-time.Minute).Format(time.RFC3339)
	before := now.Add(time.Minute).Format(time.RFC3339)

	cfg := &config.Config{
		Steering: config.SteeringConfig{
			Enabled: true,
			Rules: []config.SteeringRule{
				{
					ID:     "window",
					Prompt: "Within window",
					TimeWindow: config.SteeringTimeWindow{
						After:  after,
						Before: before,
					},
				},
			},
		},
	}
	server := &Server{config: cfg}

	msg := &models.Message{
		Channel: models.ChannelAPI,
		Role:    models.RoleUser,
		Content: "ping",
	}

	prompt, trace := server.steeringForMessage(&models.Session{AgentID: "main"}, msg)
	if prompt == "" || len(trace) != 1 {
		t.Fatalf("expected time window rule to match, prompt=%q trace=%v", prompt, trace)
	}
}
