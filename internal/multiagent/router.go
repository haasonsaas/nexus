package multiagent

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"github.com/haasonsaas/nexus/pkg/models"
)

// Router handles agent selection and routing based on message content,
// triggers, and handoff rules.
//
// The router evaluates routing triggers in priority order:
//  1. Explicit handoff requests
//  2. Pattern/keyword matches
//  3. Intent classification (if enabled)
//  4. Tool usage triggers
//  5. Fallback rules
type Router struct {
	// orchestrator provides access to agents and configuration.
	orchestrator *Orchestrator

	// compiledPatterns caches compiled regex patterns.
	compiledPatterns map[string]*regexp.Regexp

	// intentClassifier is an optional LLM-based intent classifier.
	intentClassifier IntentClassifier
}

// IntentClassifier classifies message intent for routing.
type IntentClassifier interface {
	// Classify returns the detected intent and confidence score.
	Classify(ctx context.Context, message string, candidates []string) (intent string, confidence float64, err error)
}

// NewRouter creates a new message router.
func NewRouter(orchestrator *Orchestrator) *Router {
	return &Router{
		orchestrator:     orchestrator,
		compiledPatterns: make(map[string]*regexp.Regexp),
	}
}

// SetIntentClassifier sets the intent classifier for intent-based routing.
func (r *Router) SetIntentClassifier(classifier IntentClassifier) {
	r.intentClassifier = classifier
}

// Route determines which agent should handle a message.
// Returns the target agent ID and whether routing should occur.
func (r *Router) Route(ctx context.Context, session *models.Session, msg *models.Message, currentAgentID string) (string, bool) {
	// Collect all matching rules
	matches := r.findMatchingRules(ctx, msg, currentAgentID)
	if len(matches) == 0 {
		return "", false
	}

	// Sort by priority (highest first)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Priority > matches[j].Priority
	})

	// Return the highest priority match
	return matches[0].TargetAgentID, true
}

// RouteMatch represents a routing rule match.
type RouteMatch struct {
	// TargetAgentID is the agent to route to.
	TargetAgentID string

	// Priority is the rule's priority.
	Priority int

	// TriggerType is the type of trigger that matched.
	TriggerType TriggerType

	// Confidence is the match confidence (0.0 to 1.0).
	Confidence float64

	// Rule is the matching handoff rule.
	Rule *HandoffRule
}

// findMatchingRules finds all rules that match the message.
func (r *Router) findMatchingRules(ctx context.Context, msg *models.Message, currentAgentID string) []RouteMatch {
	var matches []RouteMatch

	// Get the current agent's rules (if any)
	if currentAgentID != "" {
		if agent, ok := r.orchestrator.GetAgent(currentAgentID); ok {
			for i := range agent.HandoffRules {
				rule := &agent.HandoffRules[i]
				if match := r.evaluateRule(ctx, msg, rule); match != nil {
					matches = append(matches, *match)
				}
			}
		}
	}

	// Check global handoff rules
	for i := range r.orchestrator.config.GlobalHandoffRules {
		rule := &r.orchestrator.config.GlobalHandoffRules[i]
		if match := r.evaluateRule(ctx, msg, rule); match != nil {
			matches = append(matches, *match)
		}
	}

	// Check all agents for fallback and always triggers
	for _, agent := range r.orchestrator.ListAgents() {
		if agent.ID == currentAgentID {
			continue // Already checked
		}
		if !agent.CanReceiveHandoffs {
			continue
		}

		for i := range agent.HandoffRules {
			rule := &agent.HandoffRules[i]
			for _, trigger := range rule.Triggers {
				if trigger.Type == TriggerFallback && len(matches) == 0 {
					matches = append(matches, RouteMatch{
						TargetAgentID: rule.TargetAgentID,
						Priority:      rule.Priority - 1000, // Lower priority for fallbacks
						TriggerType:   TriggerFallback,
						Confidence:    1.0,
						Rule:          rule,
					})
				}
			}
		}
	}

	return matches
}

// evaluateRule checks if a rule's triggers match the message.
func (r *Router) evaluateRule(ctx context.Context, msg *models.Message, rule *HandoffRule) *RouteMatch {
	for _, trigger := range rule.Triggers {
		confidence := r.evaluateTrigger(ctx, msg, &trigger)
		if confidence > 0 {
			// Check threshold if specified
			if trigger.Threshold > 0 && confidence < trigger.Threshold {
				continue
			}

			return &RouteMatch{
				TargetAgentID: rule.TargetAgentID,
				Priority:      rule.Priority,
				TriggerType:   trigger.Type,
				Confidence:    confidence,
				Rule:          rule,
			}
		}
	}
	return nil
}

// evaluateTrigger evaluates a single trigger against a message.
// Returns confidence score (0.0 = no match, 1.0 = perfect match).
func (r *Router) evaluateTrigger(ctx context.Context, msg *models.Message, trigger *RoutingTrigger) float64 {
	content := strings.ToLower(msg.Content)

	switch trigger.Type {
	case TriggerKeyword:
		return r.evaluateKeywordTrigger(content, trigger)

	case TriggerPattern:
		return r.evaluatePatternTrigger(content, trigger)

	case TriggerIntent:
		return r.evaluateIntentTrigger(ctx, msg.Content, trigger)

	case TriggerToolUse:
		return r.evaluateToolUseTrigger(msg, trigger)

	case TriggerExplicit:
		return r.evaluateExplicitTrigger(content, trigger)

	case TriggerAlways:
		return 1.0

	case TriggerFallback:
		// Fallback is handled specially in findMatchingRules
		return 0

	case TriggerTaskComplete:
		return r.evaluateTaskCompleteTrigger(msg, trigger)

	case TriggerError:
		return r.evaluateErrorTrigger(msg, trigger)

	default:
		return 0
	}
}

// evaluateKeywordTrigger checks for keyword matches.
func (r *Router) evaluateKeywordTrigger(content string, trigger *RoutingTrigger) float64 {
	keywords := trigger.Values
	if trigger.Value != "" {
		keywords = append(keywords, trigger.Value)
	}

	matchCount := 0
	for _, keyword := range keywords {
		if strings.Contains(content, strings.ToLower(keyword)) {
			matchCount++
		}
	}

	if matchCount == 0 {
		return 0
	}

	// Return confidence based on how many keywords matched
	return float64(matchCount) / float64(len(keywords))
}

// evaluatePatternTrigger checks for regex pattern matches.
func (r *Router) evaluatePatternTrigger(content string, trigger *RoutingTrigger) float64 {
	pattern := trigger.Value
	if pattern == "" {
		return 0
	}

	// Get or compile the pattern
	re, ok := r.compiledPatterns[pattern]
	if !ok {
		var err error
		re, err = regexp.Compile("(?i)" + pattern) // Case insensitive
		if err != nil {
			return 0
		}
		r.compiledPatterns[pattern] = re
	}

	if re.MatchString(content) {
		return 1.0
	}
	return 0
}

// evaluateIntentTrigger uses LLM classification for intent matching.
func (r *Router) evaluateIntentTrigger(ctx context.Context, content string, trigger *RoutingTrigger) float64 {
	if r.intentClassifier == nil {
		return 0
	}

	candidates := trigger.Values
	if trigger.Value != "" {
		candidates = append(candidates, trigger.Value)
	}
	if len(candidates) == 0 {
		return 0
	}

	intent, confidence, err := r.intentClassifier.Classify(ctx, content, candidates)
	if err != nil {
		return 0
	}

	// Check if the classified intent matches any of our candidates
	for _, candidate := range candidates {
		if strings.EqualFold(intent, candidate) {
			return confidence
		}
	}

	return 0
}

// evaluateToolUseTrigger checks if specific tools were used.
func (r *Router) evaluateToolUseTrigger(msg *models.Message, trigger *RoutingTrigger) float64 {
	if len(msg.ToolCalls) == 0 {
		return 0
	}

	tools := trigger.Values
	if trigger.Value != "" {
		tools = append(tools, trigger.Value)
	}

	for _, tc := range msg.ToolCalls {
		for _, tool := range tools {
			if tc.Name == tool {
				return 1.0
			}
		}
	}

	return 0
}

// evaluateExplicitTrigger checks for explicit handoff requests.
func (r *Router) evaluateExplicitTrigger(content string, trigger *RoutingTrigger) float64 {
	// Check for explicit handoff patterns
	explicitPatterns := []string{
		"hand off to",
		"handoff to",
		"transfer to",
		"switch to",
		"let .* handle",
		"ask .* to help",
		"@\\w+", // @mention style
	}

	for _, pattern := range explicitPatterns {
		re, err := regexp.Compile("(?i)" + pattern)
		if err != nil {
			continue
		}
		if re.MatchString(content) {
			// If trigger specifies a value, check for agent name/ID match
			if trigger.Value != "" {
				if strings.Contains(content, strings.ToLower(trigger.Value)) {
					return 1.0
				}
			} else {
				return 1.0
			}
		}
	}

	return 0
}

// evaluateTaskCompleteTrigger checks for task completion indicators.
func (r *Router) evaluateTaskCompleteTrigger(msg *models.Message, trigger *RoutingTrigger) float64 {
	content := strings.ToLower(msg.Content)

	completionPhrases := []string{
		"task complete",
		"task completed",
		"task done",
		"i'm done",
		"i am done",
		"finished",
		"completed successfully",
		"task is complete",
	}

	for _, phrase := range completionPhrases {
		if strings.Contains(content, phrase) {
			return 1.0
		}
	}

	// Check metadata for completion signal
	if msg.Metadata != nil {
		if complete, ok := msg.Metadata["task_complete"].(bool); ok && complete {
			return 1.0
		}
	}

	return 0
}

// evaluateErrorTrigger checks for error conditions.
func (r *Router) evaluateErrorTrigger(msg *models.Message, trigger *RoutingTrigger) float64 {
	// Check tool results for errors
	for _, tr := range msg.ToolResults {
		if tr.IsError {
			return 1.0
		}
	}

	// Check metadata for error signals
	if msg.Metadata != nil {
		if _, ok := msg.Metadata["error"]; ok {
			return 1.0
		}
	}

	content := strings.ToLower(msg.Content)
	errorIndicators := []string{
		"error",
		"failed",
		"cannot",
		"unable to",
		"i don't know how",
		"out of my expertise",
		"need help with",
	}

	for _, indicator := range errorIndicators {
		if strings.Contains(content, indicator) {
			return 0.5 // Lower confidence for text-based error detection
		}
	}

	return 0
}

// FindAgentByName finds an agent by name (case-insensitive).
func (r *Router) FindAgentByName(name string) (*AgentDefinition, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, agent := range r.orchestrator.ListAgents() {
		if strings.ToLower(agent.Name) == name || strings.ToLower(agent.ID) == name {
			return agent, true
		}
	}
	return nil, false
}

// GetCandidateAgents returns agents that could handle a message based on their descriptions.
func (r *Router) GetCandidateAgents(ctx context.Context, msg *models.Message) []*AgentDefinition {
	var candidates []*AgentDefinition

	for _, agent := range r.orchestrator.ListAgents() {
		if !agent.CanReceiveHandoffs {
			continue
		}

		// Check if any of the agent's triggers would match
		for i := range agent.HandoffRules {
			rule := &agent.HandoffRules[i]
			if r.evaluateRule(ctx, msg, rule) != nil {
				candidates = append(candidates, agent)
				break
			}
		}
	}

	// If no specific matches, return all agents that can receive handoffs
	if len(candidates) == 0 {
		for _, agent := range r.orchestrator.ListAgents() {
			if agent.CanReceiveHandoffs {
				candidates = append(candidates, agent)
			}
		}
	}

	return candidates
}

// BuildAgentDescriptions creates a summary of available agents for LLM context.
func (r *Router) BuildAgentDescriptions() string {
	var sb strings.Builder
	sb.WriteString("Available agents:\n\n")

	for _, agent := range r.orchestrator.ListAgents() {
		sb.WriteString("- **")
		sb.WriteString(agent.Name)
		sb.WriteString("** (")
		sb.WriteString(agent.ID)
		sb.WriteString("): ")
		sb.WriteString(agent.Description)
		if len(agent.Tools) > 0 {
			sb.WriteString("\n  Tools: ")
			sb.WriteString(strings.Join(agent.Tools, ", "))
		}
		sb.WriteString("\n\n")
	}

	return sb.String()
}
