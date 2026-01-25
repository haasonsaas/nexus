// Package ssrf provides utilities for validating URLs and IP addresses
// to prevent Server-Side Request Forgery (SSRF) attacks.
package ssrf

// SSRFBlockedError is returned when a hostname or IP address is blocked
// due to SSRF protection rules.
type SSRFBlockedError struct {
	Message string
}

// Error implements the error interface.
func (e *SSRFBlockedError) Error() string {
	return e.Message
}

// NewSSRFBlockedError creates a new SSRFBlockedError with the given message.
func NewSSRFBlockedError(message string) *SSRFBlockedError {
	return &SSRFBlockedError{Message: message}
}
