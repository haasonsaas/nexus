// Package providers contains LLM provider implementations.
package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/agent"
	"github.com/haasonsaas/nexus/internal/agent/toolconv"
	"github.com/haasonsaas/nexus/pkg/models"
	openai "github.com/sashabaranov/go-openai"
)

// OllamaConfig configures the Ollama provider.
type OllamaConfig struct {
	BaseURL      string
	DefaultModel string
	Timeout      time.Duration
}

// OllamaProvider implements agent.LLMProvider for Ollama.
type OllamaProvider struct {
	client       *http.Client
	baseURL      string
	defaultModel string
}

var _ agent.LLMProvider = (*OllamaProvider)(nil)

// NewOllamaProvider creates a new Ollama provider.
func NewOllamaProvider(cfg OllamaConfig) *OllamaProvider {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	return &OllamaProvider{
		client:       &http.Client{Timeout: timeout},
		baseURL:      baseURL,
		defaultModel: strings.TrimSpace(cfg.DefaultModel),
	}
}

// Name returns the provider name.
func (p *OllamaProvider) Name() string {
	return "ollama"
}

// Models returns available models (default only when configured).
func (p *OllamaProvider) Models() []agent.Model {
	if p.defaultModel == "" {
		return nil
	}
	return []agent.Model{{ID: p.defaultModel, Name: p.defaultModel}}
}

// SupportsTools returns true when tool definitions can be supplied.
func (p *OllamaProvider) SupportsTools() bool {
	return true
}

// Complete sends a streaming chat request to Ollama.
func (p *OllamaProvider) Complete(ctx context.Context, req *agent.CompletionRequest) (<-chan *agent.CompletionChunk, error) {
	if req == nil {
		return nil, errors.New("request is nil")
	}

	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = p.defaultModel
	}
	if model == "" {
		return nil, NewProviderError("ollama", req.Model, errors.New("model is required"))
	}

	payload := ollamaChatRequest{
		Model:    model,
		Stream:   true,
		Messages: buildOllamaMessages(req),
	}
	if len(req.Tools) > 0 {
		payload.Tools = toolconv.ToOpenAITools(req.Tools)
	}
	if req.MaxTokens > 0 {
		payload.Options = map[string]any{"num_predict": req.MaxTokens}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, NewProviderError("ollama", model, fmt.Errorf("marshal request: %w", err))
	}

	url := p.baseURL + "/api/chat"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, NewProviderError("ollama", model, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, NewProviderError("ollama", model, err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		defer resp.Body.Close()
		errBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		if err != nil {
			return nil, NewProviderError("ollama", model, fmt.Errorf("ollama status %d (read body failed: %w)", resp.StatusCode, err)).WithStatus(resp.StatusCode)
		}
		return nil, NewProviderError("ollama", model, fmt.Errorf("ollama status %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))).WithStatus(resp.StatusCode)
	}

	chunks := make(chan *agent.CompletionChunk)
	go p.streamResponse(ctx, resp.Body, chunks, model)
	return chunks, nil
}

func (p *OllamaProvider) streamResponse(ctx context.Context, body io.ReadCloser, out chan *agent.CompletionChunk, model string) {
	defer close(out)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	buf := make([]byte, 0, 1024*64)
	scanner.Buffer(buf, 1024*1024)

	emitted := map[string]struct{}{}
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			out <- &agent.CompletionChunk{Error: ctx.Err(), Done: true}
			return
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var resp ollamaChatResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			out <- &agent.CompletionChunk{Error: NewProviderError("ollama", model, fmt.Errorf("decode response: %w", err)), Done: true}
			return
		}
		if resp.Error != "" {
			out <- &agent.CompletionChunk{Error: NewProviderError("ollama", model, errors.New(resp.Error)), Done: true}
			return
		}
		if resp.Message != nil {
			if resp.Message.Content != "" {
				out <- &agent.CompletionChunk{Text: resp.Message.Content}
			}
			if len(resp.Message.ToolCalls) > 0 {
				for _, tc := range resp.Message.ToolCalls {
					callID := strings.TrimSpace(tc.ID)
					if callID == "" {
						callID = toolCallKey(tc)
						if callID == "" {
							callID = uuid.NewString()
						}
					}
					if _, ok := emitted[callID]; ok {
						continue
					}
					emitted[callID] = struct{}{}
					toolCall := &models.ToolCall{
						ID:   callID,
						Name: strings.TrimSpace(tc.Function.Name),
					}
					if len(tc.Function.Arguments) > 0 {
						toolCall.Input = tc.Function.Arguments
					} else {
						toolCall.Input = json.RawMessage(`{}`)
					}
					out <- &agent.CompletionChunk{ToolCall: toolCall}
				}
			}
		}
		if resp.Done {
			out <- &agent.CompletionChunk{
				Done:         true,
				InputTokens:  resp.PromptEvalCount,
				OutputTokens: resp.EvalCount,
			}
			return
		}
	}

	if err := scanner.Err(); err != nil {
		out <- &agent.CompletionChunk{Error: NewProviderError("ollama", model, err), Done: true}
		return
	}
}

type ollamaChatRequest struct {
	Model    string              `json:"model"`
	Messages []ollamaChatMessage `json:"messages"`
	Tools    []openai.Tool       `json:"tools,omitempty"`
	Stream   bool                `json:"stream"`
	Options  map[string]any      `json:"options,omitempty"`
}

type ollamaChatMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content,omitempty"`
	ToolCalls []ollamaToolCall `json:"tool_calls,omitempty"`
	ToolName  string           `json:"tool_name,omitempty"`
}

type ollamaChatResponse struct {
	Message         *ollamaChatMessage `json:"message"`
	Done            bool               `json:"done"`
	Error           string             `json:"error"`
	EvalCount       int                `json:"eval_count"`
	PromptEvalCount int                `json:"prompt_eval_count"`
}

type ollamaToolCall struct {
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"`
	Function ollamaToolFunction `json:"function"`
}

type ollamaToolFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

func buildOllamaMessages(req *agent.CompletionRequest) []ollamaChatMessage {
	messages := make([]ollamaChatMessage, 0, len(req.Messages)+1)
	toolNames := map[string]string{}
	for _, msg := range req.Messages {
		for _, tc := range msg.ToolCalls {
			if tc.ID != "" && tc.Name != "" {
				toolNames[tc.ID] = tc.Name
			}
		}
	}
	if system := strings.TrimSpace(req.System); system != "" {
		messages = append(messages, ollamaChatMessage{Role: "system", Content: system})
	}
	for _, msg := range req.Messages {
		role := msg.Role
		if role == "" {
			role = "user"
		}
		switch role {
		case "assistant":
			ollamaMsg := ollamaChatMessage{Role: role, Content: msg.Content}
			if len(msg.ToolCalls) > 0 {
				ollamaMsg.ToolCalls = make([]ollamaToolCall, len(msg.ToolCalls))
				for i, tc := range msg.ToolCalls {
					args := tc.Input
					if len(args) == 0 {
						args = json.RawMessage(`{}`)
					}
					ollamaMsg.ToolCalls[i] = ollamaToolCall{
						ID:   tc.ID,
						Type: "function",
						Function: ollamaToolFunction{
							Name:      tc.Name,
							Arguments: args,
						},
					}
				}
			}
			messages = append(messages, ollamaMsg)
		case "tool":
			if len(msg.ToolResults) > 0 {
				for _, tr := range msg.ToolResults {
					toolName := toolNames[tr.ToolCallID]
					messages = append(messages, ollamaChatMessage{
						Role:     "tool",
						Content:  tr.Content,
						ToolName: toolName,
					})
				}
			} else {
				messages = append(messages, ollamaChatMessage{Role: role, Content: msg.Content})
			}
		default:
			messages = append(messages, ollamaChatMessage{Role: role, Content: msg.Content})
		}
	}
	return messages
}

func toolCallKey(tc ollamaToolCall) string {
	if strings.TrimSpace(tc.ID) != "" {
		return strings.TrimSpace(tc.ID)
	}
	name := strings.TrimSpace(tc.Function.Name)
	args := strings.TrimSpace(string(tc.Function.Arguments))
	if name == "" && args == "" {
		return ""
	}
	return name + ":" + args
}
