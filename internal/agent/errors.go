package agent

import (
	"errors"
	"fmt"
	"strings"
)

// Common sentinel errors for agent operations
var (
	// ErrMaxIterations indicates the agentic loop exceeded its iteration limit
	ErrMaxIterations = errors.New("max iterations exceeded")

	// ErrContextCancelled indicates the context was cancelled
	ErrContextCancelled = errors.New("context cancelled")

	// ErrNoProvider indicates no LLM provider is configured
	ErrNoProvider = errors.New("no provider configured")

	// ErrToolNotFound indicates a requested tool doesn't exist
	ErrToolNotFound = errors.New("tool not found")

	// ErrToolTimeout indicates a tool execution timed out
	ErrToolTimeout = errors.New("tool execution timed out")

	// ErrToolPanic indicates a tool panicked during execution
	ErrToolPanic = errors.New("tool panicked")

	// ErrBackpressure indicates the system is overloaded
	ErrBackpressure = errors.New("backpressure: system overloaded")
)

// ToolErrorType categorizes tool execution errors for retry logic and error handling.
type ToolErrorType string

const (
	// ToolErrorNotFound indicates the tool doesn't exist
	ToolErrorNotFound ToolErrorType = "not_found"

	// ToolErrorInvalidInput indicates invalid parameters were passed
	ToolErrorInvalidInput ToolErrorType = "invalid_input"

	// ToolErrorTimeout indicates the tool timed out
	ToolErrorTimeout ToolErrorType = "timeout"

	// ToolErrorNetwork indicates a network error
	ToolErrorNetwork ToolErrorType = "network"

	// ToolErrorPermission indicates a permission error
	ToolErrorPermission ToolErrorType = "permission"

	// ToolErrorRateLimit indicates the tool was rate limited
	ToolErrorRateLimit ToolErrorType = "rate_limit"

	// ToolErrorExecution indicates a runtime error during execution
	ToolErrorExecution ToolErrorType = "execution"

	// ToolErrorPanic indicates the tool panicked
	ToolErrorPanic ToolErrorType = "panic"

	// ToolErrorUnknown indicates an unclassified error
	ToolErrorUnknown ToolErrorType = "unknown"
)

// IsRetryable returns true if this error type suggests retrying the operation may succeed.
// Timeout, network, and rate limit errors are considered retryable.
func (t ToolErrorType) IsRetryable() bool {
	switch t {
	case ToolErrorTimeout, ToolErrorNetwork, ToolErrorRateLimit:
		return true
	default:
		return false
	}
}

// ToolError represents a structured error from tool execution with categorization
// for retry logic and detailed context about the failure.
type ToolError struct {
	// Type categorizes the error for retry logic
	Type ToolErrorType

	// ToolName is the name of the tool that failed
	ToolName string

	// ToolCallID is the ID of the tool call that failed
	ToolCallID string

	// Message is the human-readable error message
	Message string

	// Cause is the underlying error
	Cause error

	// Retryable indicates if this error should be retried
	Retryable bool

	// Attempts is the number of attempts made
	Attempts int
}

// Error implements the error interface.
func (e *ToolError) Error() string {
	var parts []string

	parts = append(parts, fmt.Sprintf("[tool:%s]", e.Type))

	if e.ToolName != "" {
		parts = append(parts, e.ToolName)
	}

	if e.Message != "" {
		parts = append(parts, e.Message)
	} else if e.Cause != nil {
		parts = append(parts, e.Cause.Error())
	}

	if e.Attempts > 1 {
		parts = append(parts, fmt.Sprintf("(attempts=%d)", e.Attempts))
	}

	return strings.Join(parts, " ")
}

// Unwrap returns the underlying error.
func (e *ToolError) Unwrap() error {
	return e.Cause
}

// NewToolError creates a new ToolError with automatic error classification.
// The error type is inferred from the cause's error message.
func NewToolError(toolName string, cause error) *ToolError {
	err := &ToolError{
		ToolName: toolName,
		Cause:    cause,
		Type:     ToolErrorUnknown,
		Attempts: 1,
	}

	if cause != nil {
		err.Message = cause.Error()
		err.Type = classifyToolError(cause)
		err.Retryable = err.Type.IsRetryable()
	}

	return err
}

// WithType sets the error type and updates retryable status accordingly.
func (e *ToolError) WithType(t ToolErrorType) *ToolError {
	e.Type = t
	e.Retryable = t.IsRetryable()
	return e
}

// WithToolCallID sets the tool call ID for correlating errors with specific calls.
func (e *ToolError) WithToolCallID(id string) *ToolError {
	e.ToolCallID = id
	return e
}

// WithMessage sets a custom human-readable error message.
func (e *ToolError) WithMessage(msg string) *ToolError {
	e.Message = msg
	return e
}

// WithAttempts sets the number of execution attempts that were made.
func (e *ToolError) WithAttempts(n int) *ToolError {
	e.Attempts = n
	return e
}

// classifyToolError determines the error type from the error content.
func classifyToolError(err error) ToolErrorType {
	if err == nil {
		return ToolErrorUnknown
	}

	// Check for sentinel errors
	if errors.Is(err, ErrToolNotFound) {
		return ToolErrorNotFound
	}
	if errors.Is(err, ErrToolTimeout) {
		return ToolErrorTimeout
	}
	if errors.Is(err, ErrToolPanic) {
		return ToolErrorPanic
	}

	errStr := strings.ToLower(err.Error())

	// Timeout patterns
	if strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "deadline exceeded") ||
		strings.Contains(errStr, "context deadline") {
		return ToolErrorTimeout
	}

	// Network patterns
	if strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "network") ||
		strings.Contains(errStr, "dns") ||
		strings.Contains(errStr, "refused") ||
		strings.Contains(errStr, "unreachable") {
		return ToolErrorNetwork
	}

	// Rate limit patterns
	if strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "rate_limit") ||
		strings.Contains(errStr, "too many requests") ||
		strings.Contains(errStr, "429") {
		return ToolErrorRateLimit
	}

	// Permission patterns
	if strings.Contains(errStr, "permission") ||
		strings.Contains(errStr, "forbidden") ||
		strings.Contains(errStr, "unauthorized") ||
		strings.Contains(errStr, "access denied") {
		return ToolErrorPermission
	}

	// Invalid input patterns
	if strings.Contains(errStr, "invalid") ||
		strings.Contains(errStr, "validation") ||
		strings.Contains(errStr, "required") ||
		strings.Contains(errStr, "missing") {
		return ToolErrorInvalidInput
	}

	return ToolErrorExecution
}

// IsToolError checks if an error is or wraps a ToolError.
func IsToolError(err error) bool {
	var toolErr *ToolError
	return errors.As(err, &toolErr)
}

// GetToolError extracts a ToolError from an error chain using errors.As.
func GetToolError(err error) (*ToolError, bool) {
	var toolErr *ToolError
	if errors.As(err, &toolErr) {
		return toolErr, true
	}
	return nil, false
}

// IsToolRetryable checks if a tool error should be retried based on its type.
func IsToolRetryable(err error) bool {
	if toolErr, ok := GetToolError(err); ok {
		return toolErr.Retryable
	}
	return classifyToolError(err).IsRetryable()
}

// LoopError represents an error that occurred during the agentic loop execution
// with context about which phase and iteration the error occurred in.
type LoopError struct {
	// Phase is the loop phase where the error occurred
	Phase LoopPhase

	// Iteration is the loop iteration where the error occurred
	Iteration int

	// Message is the human-readable error message
	Message string

	// Cause is the underlying error
	Cause error
}

// Error implements the error interface.
func (e *LoopError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("loop error at %s (iteration %d): %s", e.Phase, e.Iteration, e.Message)
	}
	if e.Cause != nil {
		return fmt.Sprintf("loop error at %s (iteration %d): %v", e.Phase, e.Iteration, e.Cause)
	}
	return fmt.Sprintf("loop error at %s (iteration %d)", e.Phase, e.Iteration)
}

// Unwrap returns the underlying error.
func (e *LoopError) Unwrap() error {
	return e.Cause
}

// LoopPhase represents a distinct phase in the agentic loop lifecycle.
type LoopPhase string

const (
	// PhaseInit is the initialization phase
	PhaseInit LoopPhase = "init"

	// PhaseStream is the LLM streaming phase
	PhaseStream LoopPhase = "stream"

	// PhaseExecuteTools is the tool execution phase
	PhaseExecuteTools LoopPhase = "execute_tools"

	// PhaseContinue is the continuation phase after tool results
	PhaseContinue LoopPhase = "continue"

	// PhaseComplete is the completion phase
	PhaseComplete LoopPhase = "complete"
)
