package channels

import (
	"errors"
	"fmt"
)

// ErrorCode represents a specific error condition in channel operations.
// Error codes help with error classification, monitoring, and appropriate retry strategies.
type ErrorCode string

const (
	// ErrCodeConnection indicates network or connection-related failures
	ErrCodeConnection ErrorCode = "CONNECTION_ERROR"

	// ErrCodeAuthentication indicates authentication or authorization failures
	ErrCodeAuthentication ErrorCode = "AUTH_ERROR"

	// ErrCodeRateLimit indicates the operation was rate limited by the upstream service
	ErrCodeRateLimit ErrorCode = "RATE_LIMIT_ERROR"

	// ErrCodeInvalidInput indicates invalid message or configuration data
	ErrCodeInvalidInput ErrorCode = "INVALID_INPUT"

	// ErrCodeNotFound indicates a requested resource was not found
	ErrCodeNotFound ErrorCode = "NOT_FOUND"

	// ErrCodeTimeout indicates an operation timed out
	ErrCodeTimeout ErrorCode = "TIMEOUT_ERROR"

	// ErrCodeInternal indicates an unexpected internal error
	ErrCodeInternal ErrorCode = "INTERNAL_ERROR"

	// ErrCodeUnavailable indicates the service is temporarily unavailable
	ErrCodeUnavailable ErrorCode = "SERVICE_UNAVAILABLE"

	// ErrCodeConfig indicates a configuration error
	ErrCodeConfig ErrorCode = "CONFIG_ERROR"
)

// Error represents a structured error with additional context for channel operations.
// It includes an error code for categorization, the underlying error, and optional context.
type Error struct {
	// Code categorizes the error type for monitoring and handling
	Code ErrorCode

	// Message provides a human-readable error description
	Message string

	// Err is the underlying error that caused this error
	Err error

	// Context provides additional key-value pairs for debugging
	Context map[string]any
}

// Error implements the error interface, returning a formatted error message.
func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the underlying error, allowing errors.Is and errors.As to work.
func (e *Error) Unwrap() error {
	return e.Err
}

// NewError creates a new Error with the given code and message.
func NewError(code ErrorCode, message string, err error) *Error {
	return &Error{
		Code:    code,
		Message: message,
		Err:     err,
		Context: make(map[string]any),
	}
}

// WithContext adds contextual information to the error.
func (e *Error) WithContext(key string, value any) *Error {
	if e.Context == nil {
		e.Context = make(map[string]any)
	}
	e.Context[key] = value
	return e
}

// IsRetryable returns true if the error represents a transient failure
// that may succeed on retry.
func (e *Error) IsRetryable() bool {
	switch e.Code {
	case ErrCodeRateLimit, ErrCodeTimeout, ErrCodeUnavailable, ErrCodeConnection:
		return true
	default:
		return false
	}
}

// Common error constructors for convenience

// ErrConnection creates a connection error.
func ErrConnection(message string, err error) *Error {
	return NewError(ErrCodeConnection, message, err)
}

// ErrAuthentication creates an authentication error.
func ErrAuthentication(message string, err error) *Error {
	return NewError(ErrCodeAuthentication, message, err)
}

// ErrRateLimit creates a rate limit error.
func ErrRateLimit(message string, err error) *Error {
	return NewError(ErrCodeRateLimit, message, err)
}

// ErrInvalidInput creates an invalid input error.
func ErrInvalidInput(message string, err error) *Error {
	return NewError(ErrCodeInvalidInput, message, err)
}

// ErrNotFound creates a not found error.
func ErrNotFound(message string, err error) *Error {
	return NewError(ErrCodeNotFound, message, err)
}

// ErrTimeout creates a timeout error.
func ErrTimeout(message string, err error) *Error {
	return NewError(ErrCodeTimeout, message, err)
}

// ErrInternal creates an internal error.
func ErrInternal(message string, err error) *Error {
	return NewError(ErrCodeInternal, message, err)
}

// ErrUnavailable creates a service unavailable error.
func ErrUnavailable(message string, err error) *Error {
	return NewError(ErrCodeUnavailable, message, err)
}

// ErrConfig creates a configuration error.
func ErrConfig(message string, err error) *Error {
	return NewError(ErrCodeConfig, message, err)
}

// GetErrorCode extracts the ErrorCode from an error if it's a channel Error,
// otherwise returns ErrCodeInternal.
func GetErrorCode(err error) ErrorCode {
	var chErr *Error
	if errors.As(err, &chErr) {
		return chErr.Code
	}
	return ErrCodeInternal
}

// IsRetryable returns true if the error is retryable, checking both
// channel.Error types and context errors.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Check if it's a channel error
	var chErr *Error
	if errors.As(err, &chErr) {
		return chErr.IsRetryable()
	}

	return false
}
