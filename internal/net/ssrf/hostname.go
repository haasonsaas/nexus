package ssrf

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// blockedHostnames contains hostnames that are always blocked.
var blockedHostnames = map[string]bool{
	"localhost":                true,
	"metadata.google.internal": true,
}

// dangerousSuffixes contains hostname suffixes that indicate internal/local resources.
var dangerousSuffixes = []string{
	".localhost",
	".local",
	".internal",
}

// IsBlockedHostname checks if a hostname is blocked due to SSRF protection rules.
// This includes explicitly blocked hostnames and dangerous suffixes.
func IsBlockedHostname(hostname string) bool {
	normalized := normalizeHostname(hostname)
	if normalized == "" {
		return false
	}

	// Check explicitly blocked hostnames
	if blockedHostnames[normalized] {
		return true
	}

	// Check dangerous suffixes
	for _, suffix := range dangerousSuffixes {
		if strings.HasSuffix(normalized, suffix) {
			return true
		}
	}

	return false
}

// ValidatePublicHostname validates that a hostname is safe for external requests.
// It checks that the hostname is not blocked and does not resolve to a private IP address.
func ValidatePublicHostname(hostname string) error {
	normalized := normalizeHostname(hostname)
	if normalized == "" {
		return errors.New("invalid hostname: empty after normalization")
	}

	// Check if hostname is blocked
	if IsBlockedHostname(normalized) {
		return NewSSRFBlockedError(fmt.Sprintf("blocked hostname: %s", hostname))
	}

	// Check if hostname is already a private IP address
	if IsPrivateIPAddress(normalized) {
		return NewSSRFBlockedError("blocked: private/internal IP address")
	}

	// Perform DNS lookup
	ips, err := net.LookupIP(normalized)
	if err != nil {
		return fmt.Errorf("unable to resolve hostname: %s: %w", hostname, err)
	}

	if len(ips) == 0 {
		return fmt.Errorf("unable to resolve hostname: %s", hostname)
	}

	// Check each resolved IP address
	for _, ip := range ips {
		if IsPrivateIPAddress(ip.String()) {
			return NewSSRFBlockedError("blocked: resolves to private/internal IP address")
		}
	}

	return nil
}
