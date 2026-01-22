package context

// TruncationStrategy defines how to reduce context when it gets too long.
type TruncationStrategy string

const (
	// TruncateOldest removes the oldest messages first
	TruncateOldest TruncationStrategy = "oldest"

	// TruncateMiddle keeps the first and last messages, removes middle
	TruncateMiddle TruncationStrategy = "middle"

	// TruncateSummarize summarizes older messages into a single message
	TruncateSummarize TruncationStrategy = "summarize"

	// TruncateNone returns an error instead of truncating
	TruncateNone TruncationStrategy = "none"
)

// TruncationResult holds the result of a truncation operation.
type TruncationResult struct {
	// Original message count
	OriginalCount int `json:"original_count"`

	// New message count after truncation
	NewCount int `json:"new_count"`

	// Messages removed
	RemovedCount int `json:"removed_count"`

	// Tokens freed
	TokensFreed int `json:"tokens_freed"`

	// Strategy used
	Strategy TruncationStrategy `json:"strategy"`

	// Summary if summarization was used
	Summary string `json:"summary,omitempty"`
}

// Message represents a conversation message for truncation purposes.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Tokens  int    `json:"tokens"`

	// Pinned messages are never truncated
	Pinned bool `json:"pinned,omitempty"`

	// System messages have special handling
	IsSystem bool `json:"is_system,omitempty"`
}

// Truncator handles message truncation strategies.
type Truncator struct {
	strategy  TruncationStrategy
	maxTokens int
	keepFirst int // Number of messages to always keep at start
	keepLast  int // Number of messages to always keep at end
}

// NewTruncator creates a new truncator with the given strategy.
func NewTruncator(strategy TruncationStrategy, maxTokens int) *Truncator {
	return &Truncator{
		strategy:  strategy,
		maxTokens: maxTokens,
		keepFirst: 1, // Keep system prompt
		keepLast:  2, // Keep recent context
	}
}

// SetKeepFirst sets how many messages to keep at the start.
func (t *Truncator) SetKeepFirst(n int) {
	if n >= 0 {
		t.keepFirst = n
	}
}

// SetKeepLast sets how many messages to keep at the end.
func (t *Truncator) SetKeepLast(n int) {
	if n >= 0 {
		t.keepLast = n
	}
}

// Truncate reduces messages to fit within the token limit.
func (t *Truncator) Truncate(messages []Message) ([]Message, *TruncationResult) {
	result := &TruncationResult{
		OriginalCount: len(messages),
		Strategy:      t.strategy,
	}

	// Calculate current total tokens
	totalTokens := 0
	for _, msg := range messages {
		if msg.Tokens == 0 {
			msg.Tokens = EstimateTokens(msg.Content)
		}
		totalTokens += msg.Tokens
	}

	// Check if truncation is needed
	if totalTokens <= t.maxTokens {
		result.NewCount = len(messages)
		return messages, result
	}

	switch t.strategy {
	case TruncateOldest:
		return t.truncateOldest(messages, result)
	case TruncateMiddle:
		return t.truncateMiddle(messages, result)
	case TruncateNone:
		result.NewCount = len(messages)
		return messages, result
	default:
		return t.truncateOldest(messages, result)
	}
}

func (t *Truncator) truncateOldest(messages []Message, result *TruncationResult) ([]Message, *TruncationResult) {
	if len(messages) == 0 {
		return messages, result
	}

	// Identify pinned and system messages
	var kept []Message
	var candidates []Message

	for i, msg := range messages {
		// Always keep first N messages
		if i < t.keepFirst {
			kept = append(kept, msg)
			continue
		}

		// Always keep last N messages
		if i >= len(messages)-t.keepLast {
			kept = append(kept, msg)
			continue
		}

		// Keep pinned messages
		if msg.Pinned || msg.IsSystem {
			kept = append(kept, msg)
			continue
		}

		candidates = append(candidates, msg)
	}

	// Calculate tokens for kept messages
	keptTokens := 0
	for _, msg := range kept {
		keptTokens += msg.Tokens
	}

	// Remove oldest candidates until we fit
	for len(candidates) > 0 && keptTokens+sumTokens(candidates) > t.maxTokens {
		result.TokensFreed += candidates[0].Tokens
		candidates = candidates[1:]
		result.RemovedCount++
	}

	// Merge kept and remaining candidates, preserving order
	final := make([]Message, 0, len(kept)+len(candidates))
	candidateIdx := 0

	for i, msg := range messages {
		if i < t.keepFirst {
			final = append(final, msg)
			continue
		}
		if i >= len(messages)-t.keepLast {
			final = append(final, msg)
			continue
		}
		if msg.Pinned || msg.IsSystem {
			final = append(final, msg)
			continue
		}

		// Check if this message is in remaining candidates
		if candidateIdx < len(candidates) {
			final = append(final, candidates[candidateIdx])
			candidateIdx++
		}
	}

	result.NewCount = len(final)
	return final, result
}

func (t *Truncator) truncateMiddle(messages []Message, result *TruncationResult) ([]Message, *TruncationResult) {
	if len(messages) <= t.keepFirst+t.keepLast {
		result.NewCount = len(messages)
		return messages, result
	}

	// Keep first N and last N messages
	first := messages[:t.keepFirst]
	last := messages[len(messages)-t.keepLast:]
	middle := messages[t.keepFirst : len(messages)-t.keepLast]

	// Calculate tokens
	firstTokens := sumTokens(first)
	lastTokens := sumTokens(last)
	targetMiddleTokens := t.maxTokens - firstTokens - lastTokens

	if targetMiddleTokens <= 0 {
		// Can't fit any middle messages
		result.RemovedCount = len(middle)
		for _, msg := range middle {
			result.TokensFreed += msg.Tokens
		}
		result.NewCount = t.keepFirst + t.keepLast

		final := make([]Message, 0, result.NewCount)
		final = append(final, first...)
		final = append(final, last...)
		return final, result
	}

	// Remove from middle, keeping pinned messages
	var keptMiddle []Message
	middleTokens := 0

	for _, msg := range middle {
		if msg.Pinned || msg.IsSystem {
			keptMiddle = append(keptMiddle, msg)
			middleTokens += msg.Tokens
		} else if middleTokens+msg.Tokens <= targetMiddleTokens {
			keptMiddle = append(keptMiddle, msg)
			middleTokens += msg.Tokens
		} else {
			result.RemovedCount++
			result.TokensFreed += msg.Tokens
		}
	}

	final := make([]Message, 0, t.keepFirst+len(keptMiddle)+t.keepLast)
	final = append(final, first...)
	final = append(final, keptMiddle...)
	final = append(final, last...)

	result.NewCount = len(final)
	return final, result
}

func sumTokens(messages []Message) int {
	total := 0
	for _, msg := range messages {
		total += msg.Tokens
	}
	return total
}
