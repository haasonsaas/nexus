package eval

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/pkg/models"
)

const (
	defaultJudgeScoreTokens  = 256
	defaultJudgeAnswerTokens = 1024
)

var scorePattern = regexp.MustCompile(`[-+]?[0-9]*\.?[0-9]+`)

// LLMJudge scores answer quality using an LLM provider.
type LLMJudge struct {
	provider        agent.LLMProvider
	defaultModel    string
	scoreMaxTokens  int
	answerMaxTokens int
}

// NewLLMJudge creates a new LLM judge with a default model and token limits.
func NewLLMJudge(provider agent.LLMProvider, defaultModel string) *LLMJudge {
	return &LLMJudge{
		provider:        provider,
		defaultModel:    defaultModel,
		scoreMaxTokens:  defaultJudgeScoreTokens,
		answerMaxTokens: defaultJudgeAnswerTokens,
	}
}

// SetScoreMaxTokens overrides the max tokens for judge scoring prompts.
func (j *LLMJudge) SetScoreMaxTokens(tokens int) {
	if tokens > 0 {
		j.scoreMaxTokens = tokens
	}
}

// SetAnswerMaxTokens overrides the max tokens for answer generation.
func (j *LLMJudge) SetAnswerMaxTokens(tokens int) {
	if tokens > 0 {
		j.answerMaxTokens = tokens
	}
}

// GenerateAnswer produces an answer using the retrieved context.
func (j *LLMJudge) GenerateAnswer(ctx context.Context, query, context, model string, maxTokens int) (string, error) {
	if j == nil || j.provider == nil {
		return "", fmt.Errorf("llm judge provider is nil")
	}
	if maxTokens <= 0 {
		maxTokens = j.answerMaxTokens
	}
	req := &agent.CompletionRequest{
		Model: j.resolveModel(model),
		System: "You are a concise assistant. Answer only using the provided context. " +
			"If the context does not contain the answer, say you don't know.",
		Messages: []agent.CompletionMessage{{
			Role:    "user",
			Content: fmt.Sprintf("Question:\n%s\n\nContext:\n%s\n\nAnswer:", query, context),
		}},
		MaxTokens: maxTokens,
	}
	return j.completeText(ctx, req)
}

// JudgeRelevance scores how well the answer addresses the query.
func (j *LLMJudge) JudgeRelevance(ctx context.Context, query, answer string) (float64, error) {
	if strings.TrimSpace(answer) == "" {
		return 0, nil
	}
	req := &agent.CompletionRequest{
		Model: j.resolveModel(""),
		System: "You are a strict evaluator. Return only a single number between 0 and 1. " +
			"0 means the answer is unrelated. 1 means it fully answers the question.",
		Messages: []agent.CompletionMessage{{
			Role: "user",
			Content: fmt.Sprintf("Question:\n%s\n\nAnswer:\n%s\n\nScore (0-1):",
				query, answer),
		}},
		MaxTokens: j.scoreMaxTokens,
	}
	text, err := j.completeText(ctx, req)
	if err != nil {
		return 0, err
	}
	return parseScore(text)
}

// JudgeFaithfulness scores how well the answer is supported by the retrieved context.
func (j *LLMJudge) JudgeFaithfulness(ctx context.Context, answer string, results []*models.DocumentSearchResult) (float64, error) {
	if strings.TrimSpace(answer) == "" {
		return 0, nil
	}
	context := BuildContext(results)
	req := &agent.CompletionRequest{
		Model: j.resolveModel(""),
		System: "You are a strict evaluator. Return only a single number between 0 and 1. " +
			"0 means the answer is not supported by the context. 1 means all claims are fully supported.",
		Messages: []agent.CompletionMessage{{
			Role: "user",
			Content: fmt.Sprintf("Context:\n%s\n\nAnswer:\n%s\n\nScore (0-1):",
				context, answer),
		}},
		MaxTokens: j.scoreMaxTokens,
	}
	text, err := j.completeText(ctx, req)
	if err != nil {
		return 0, err
	}
	return parseScore(text)
}

// JudgeContextRecall scores how much of the retrieved context is reflected in the answer.
func (j *LLMJudge) JudgeContextRecall(ctx context.Context, answer string, results []*models.DocumentSearchResult) (float64, error) {
	if strings.TrimSpace(answer) == "" {
		return 0, nil
	}
	context := BuildContext(results)
	req := &agent.CompletionRequest{
		Model: j.resolveModel(""),
		System: "You are a strict evaluator. Return only a single number between 0 and 1. " +
			"0 means the answer ignores the context. 1 means it captures all key facts from the context.",
		Messages: []agent.CompletionMessage{{
			Role: "user",
			Content: fmt.Sprintf("Context:\n%s\n\nAnswer:\n%s\n\nScore (0-1):",
				context, answer),
		}},
		MaxTokens: j.scoreMaxTokens,
	}
	text, err := j.completeText(ctx, req)
	if err != nil {
		return 0, err
	}
	return parseScore(text)
}

func (j *LLMJudge) resolveModel(model string) string {
	model = strings.TrimSpace(model)
	if model != "" {
		return model
	}
	if strings.TrimSpace(j.defaultModel) != "" {
		return strings.TrimSpace(j.defaultModel)
	}
	models := j.provider.Models()
	if len(models) > 0 {
		return models[0].ID
	}
	return ""
}

func (j *LLMJudge) completeText(ctx context.Context, req *agent.CompletionRequest) (string, error) {
	if j.provider == nil {
		return "", fmt.Errorf("llm judge provider is nil")
	}
	ch, err := j.provider.Complete(ctx, req)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for chunk := range ch {
		if chunk == nil {
			continue
		}
		if chunk.Error != nil {
			return "", chunk.Error
		}
		if chunk.ToolCall != nil {
			return "", fmt.Errorf("llm judge requested a tool call")
		}
		if chunk.Text != "" {
			sb.WriteString(chunk.Text)
		}
		if chunk.Done {
			break
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

func parseScore(text string) (float64, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return 0, fmt.Errorf("empty judge response")
	}
	match := scorePattern.FindString(trimmed)
	if match == "" {
		return 0, fmt.Errorf("no numeric score in response: %q", trimmed)
	}
	val, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid score %q: %w", match, err)
	}
	if val < 0 {
		return 0, fmt.Errorf("score out of range: %v", val)
	}
	if val > 1 {
		if val <= 100 && strings.Contains(trimmed, "%") {
			val = val / 100
		} else {
			return 0, fmt.Errorf("score out of range: %v", val)
		}
	}
	return val, nil
}
