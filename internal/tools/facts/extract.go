package facts

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
)

// Fact represents a structured fact extracted from text.
type Fact struct {
	Type       string  `json:"type"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
	Source     string  `json:"source,omitempty"`
}

// ExtractTool extracts simple structured facts from text using heuristics.
type ExtractTool struct {
	maxFacts int
}

// NewExtractTool creates a new fact extraction tool.
func NewExtractTool(maxFacts int) *ExtractTool {
	if maxFacts <= 0 {
		maxFacts = 10
	}
	return &ExtractTool{maxFacts: maxFacts}
}

// Name returns the tool name.
func (t *ExtractTool) Name() string {
	return "facts_extract"
}

// Description describes the tool.
func (t *ExtractTool) Description() string {
	return "Extracts structured facts (emails, URLs, phone numbers) from text."
}

// Schema defines the tool parameters.
func (t *ExtractTool) Schema() json.RawMessage {
	return json.RawMessage(`{
  "type": "object",
  "properties": {
    "text": {"type": "string", "description": "Input text to extract facts from"},
    "max_facts": {"type": "integer", "description": "Maximum number of facts to return"}
  },
  "required": ["text"]
}`)
}

// Execute runs fact extraction.
func (t *ExtractTool) Execute(_ context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input struct {
		Text     string `json:"text"`
		MaxFacts int    `json:"max_facts"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("invalid params: %v", err), IsError: true}, nil
	}
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return &agent.ToolResult{Content: "text is required", IsError: true}, nil
	}

	limit := t.maxFacts
	if input.MaxFacts > 0 {
		limit = input.MaxFacts
	}

	facts := extractFacts(text, limit)
	payload, err := json.MarshalIndent(struct {
		Facts []Fact `json:"facts"`
	}{
		Facts: facts,
	}, "", "  ")
	if err != nil {
		return &agent.ToolResult{Content: fmt.Sprintf("failed to encode results: %v", err), IsError: true}, nil
	}

	return &agent.ToolResult{Content: string(payload)}, nil
}

var (
	emailRegex = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	urlRegex   = regexp.MustCompile(`https?://[^\s]+`)
	phoneRegex = regexp.MustCompile(`\+?[0-9][0-9()\-\s.]{6,}[0-9]`)
)

func extractFacts(text string, limit int) []Fact {
	seen := map[string]struct{}{}
	out := make([]Fact, 0, 8)

	add := func(f Fact) {
		if limit > 0 && len(out) >= limit {
			return
		}
		key := f.Type + ":" + f.Value
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, f)
	}

	for _, match := range emailRegex.FindAllString(text, -1) {
		add(Fact{Type: "email", Value: match, Confidence: 0.9, Source: "regex"})
	}
	for _, match := range urlRegex.FindAllString(text, -1) {
		add(Fact{Type: "url", Value: match, Confidence: 0.8, Source: "regex"})
	}
	for _, match := range phoneRegex.FindAllString(text, -1) {
		clean := strings.TrimSpace(match)
		add(Fact{Type: "phone", Value: clean, Confidence: 0.6, Source: "regex"})
	}

	return out
}
