package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

// BroadcastStrategy defines how messages are processed across multiple agents.
type BroadcastStrategy string

const (
	// BroadcastParallel processes all agents concurrently.
	BroadcastParallel BroadcastStrategy = "parallel"
	// BroadcastSequential processes agents one at a time in order.
	BroadcastSequential BroadcastStrategy = "sequential"
)

// BroadcastConfig configures broadcast groups for routing messages to multiple agents.
type BroadcastConfig struct {
	// Strategy defines how messages are processed: "parallel" or "sequential".
	Strategy BroadcastStrategy `yaml:"strategy"`

	// Groups maps peer_id to a list of agent_ids that should process messages.
	// When a message arrives from a peer in this map, it will be routed to all
	// specified agents instead of the default single agent.
	Groups map[string][]string `yaml:"groups"`
}

// BroadcastResult contains the result of processing a message by a single agent in a broadcast group.
type BroadcastResult struct {
	AgentID   string
	SessionID string
	Response  string
	Error     error
}

// BroadcastManager handles routing messages to multiple agents in broadcast groups.
type BroadcastManager struct {
	config   BroadcastConfig
	sessions sessions.Store
	runtime  *agent.Runtime
	logger   *slog.Logger
}

// NewBroadcastManager creates a new broadcast manager with the given configuration.
// If logger is nil, slog.Default() is used.
func NewBroadcastManager(config BroadcastConfig, sessionStore sessions.Store, runtime *agent.Runtime, logger *slog.Logger) *BroadcastManager {
	if logger == nil {
		logger = slog.Default()
	}
	return &BroadcastManager{
		config:   config,
		sessions: sessionStore,
		runtime:  runtime,
		logger:   logger,
	}
}

// IsBroadcastPeer returns true if the peer_id is configured for broadcast routing.
func (m *BroadcastManager) IsBroadcastPeer(peerID string) bool {
	if m == nil || m.config.Groups == nil {
		return false
	}
	agents, ok := m.config.Groups[peerID]
	return ok && len(agents) > 0
}

// GetAgentsForPeer returns the list of agent IDs configured for a peer, or nil if not found.
func (m *BroadcastManager) GetAgentsForPeer(peerID string) []string {
	if m == nil || m.config.Groups == nil {
		return nil
	}
	return m.config.Groups[peerID]
}

// BroadcastSessionKey builds a session key that includes the agent ID for isolation.
// This ensures each agent in a broadcast group has its own session.
func BroadcastSessionKey(agentID string, channel models.ChannelType, channelID string) string {
	return sessions.SessionKey(agentID, channel, channelID)
}

// ProcessBroadcast processes a message across all agents configured for the peer.
// Returns results from all agents.
func (m *BroadcastManager) ProcessBroadcast(
	ctx context.Context,
	peerID string,
	msg *models.Message,
	resolveConversationID func(*models.Message) (string, error),
	getSystemPrompt func(context.Context, *models.Session, *models.Message) (string, []SteeringRuleTrace),
) ([]BroadcastResult, error) {
	agents := m.GetAgentsForPeer(peerID)
	if len(agents) == 0 {
		return nil, fmt.Errorf("no agents configured for peer %q", peerID)
	}

	channelID, err := resolveConversationID(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve conversation id: %w", err)
	}

	m.logger.Debug("processing broadcast",
		"peer_id", peerID,
		"agents", agents,
		"strategy", m.config.Strategy,
		"channel", msg.Channel,
		"channel_id", channelID,
	)

	switch m.config.Strategy {
	case BroadcastSequential:
		return m.processSequential(ctx, agents, msg, channelID, getSystemPrompt)
	default:
		// Default to parallel if not specified or invalid
		return m.processParallel(ctx, agents, msg, channelID, getSystemPrompt)
	}
}

// processParallel processes the message with all agents concurrently.
func (m *BroadcastManager) processParallel(
	ctx context.Context,
	agents []string,
	msg *models.Message,
	channelID string,
	getSystemPrompt func(context.Context, *models.Session, *models.Message) (string, []SteeringRuleTrace),
) ([]BroadcastResult, error) {
	results := make([]BroadcastResult, len(agents))
	var wg sync.WaitGroup
	wg.Add(len(agents))

	for i, agentID := range agents {
		go func(idx int, aid string) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					m.logger.Error("panic in broadcast processing",
						"agent_id", aid,
						"panic", r)
					results[idx] = BroadcastResult{
						AgentID: aid,
						Error:   fmt.Errorf("panic during processing: %v", r),
					}
				}
			}()
			result := m.processForAgent(ctx, aid, msg, channelID, getSystemPrompt)
			results[idx] = result
		}(i, agentID)
	}

	wg.Wait()
	return results, nil
}

// processSequential processes the message with agents one at a time.
func (m *BroadcastManager) processSequential(
	ctx context.Context,
	agents []string,
	msg *models.Message,
	channelID string,
	getSystemPrompt func(context.Context, *models.Session, *models.Message) (string, []SteeringRuleTrace),
) ([]BroadcastResult, error) {
	results := make([]BroadcastResult, 0, len(agents))

	for _, agentID := range agents {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		result := m.processForAgent(ctx, agentID, msg, channelID, getSystemPrompt)
		results = append(results, result)

		// If one agent fails, we still continue with the others
		if result.Error != nil {
			m.logger.Warn("agent processing failed in sequential broadcast",
				"agent_id", agentID,
				"error", result.Error,
			)
		}
	}

	return results, nil
}

// processForAgent processes a message for a single agent with session isolation.
func (m *BroadcastManager) processForAgent(
	ctx context.Context,
	agentID string,
	msg *models.Message,
	channelID string,
	getSystemPrompt func(context.Context, *models.Session, *models.Message) (string, []SteeringRuleTrace),
) BroadcastResult {
	result := BroadcastResult{
		AgentID: agentID,
	}

	// Create isolated session key for this agent
	key := BroadcastSessionKey(agentID, msg.Channel, channelID)
	session, err := m.sessions.GetOrCreate(ctx, key, agentID, msg.Channel, channelID)
	if err != nil {
		result.Error = fmt.Errorf("failed to get or create session for agent %s: %w", agentID, err)
		return result
	}

	result.SessionID = session.ID

	// Create a copy of the message with the session ID set
	msgCopy := *msg
	msgCopy.SessionID = session.ID
	if msgCopy.CreatedAt.IsZero() {
		msgCopy.CreatedAt = time.Now()
	}

	// Apply system prompt if available
	promptCtx := ctx
	if getSystemPrompt != nil {
		if systemPrompt, _ := getSystemPrompt(ctx, session, &msgCopy); systemPrompt != "" {
			promptCtx = agent.WithSystemPrompt(promptCtx, systemPrompt)
		}
	}

	// Process with runtime
	chunks, err := m.runtime.Process(promptCtx, session, &msgCopy)
	if err != nil {
		result.Error = fmt.Errorf("runtime processing failed for agent %s: %w", agentID, err)
		return result
	}

	// Collect response
	var response strings.Builder
	for chunk := range chunks {
		if chunk.Error != nil {
			result.Error = fmt.Errorf("runtime stream error for agent %s: %w", agentID, chunk.Error)
			return result
		}
		if chunk.Text != "" {
			response.WriteString(chunk.Text)
		}
	}

	result.Response = response.String()
	return result
}
