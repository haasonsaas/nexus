package routing

import (
	"regexp"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
)

var (
	codeRegex    = regexp.MustCompile("(?i)\\b(func|class|def|package|import|SELECT|INSERT|UPDATE|DELETE)\\b")
	reasonRegex  = regexp.MustCompile("(?i)\\b(analyze|reason|think through|derive|prove|why|tradeoff)\\b")
	quickRegex   = regexp.MustCompile("(?i)\\b(what is|define|quick|brief|summary)\\b")
	markdownCode = regexp.MustCompile("```")
)

// HeuristicClassifier tags requests using simple content heuristics.
type HeuristicClassifier struct{}

// Classify returns a list of tags for the request.
func (c *HeuristicClassifier) Classify(req *agent.CompletionRequest) []string {
	content := strings.TrimSpace(lastUserContent(req))
	if content == "" {
		return nil
	}
	lower := strings.ToLower(content)
	var tags []string

	if markdownCode.MatchString(lower) || codeRegex.MatchString(lower) {
		tags = append(tags, "code")
	}
	if reasonRegex.MatchString(lower) {
		tags = append(tags, "reasoning")
	}
	if quickRegex.MatchString(lower) || len(lower) < 80 {
		tags = append(tags, "quick")
	}

	return tags
}
