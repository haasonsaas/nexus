package ssrf

import (
	"errors"
	"testing"
)

func TestSSRFBlockedError(t *testing.T) {
	err := NewSSRFBlockedError("test message")
	if err.Error() != "test message" {
		t.Errorf("expected 'test message', got '%s'", err.Error())
	}

	var ssrfErr *SSRFBlockedError
	if !errors.As(err, &ssrfErr) {
		t.Error("expected error to be SSRFBlockedError")
	}
}

func TestNormalizeHostname(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"example.com", "example.com"},
		{"  example.com  ", "example.com"},
		{"EXAMPLE.COM", "example.com"},
		{"example.com.", "example.com"},
		{"[::1]", "::1"},
		{"[fe80::1]", "fe80::1"},
		{"  EXAMPLE.COM.  ", "example.com"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := normalizeHostname(tc.input)
			if result != tc.expected {
				t.Errorf("normalizeHostname(%q) = %q, expected %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestParseIPv4(t *testing.T) {
	tests := []struct {
		input    string
		expected [4]byte
		hasError bool
	}{
		{"192.168.1.1", [4]byte{192, 168, 1, 1}, false},
		{"0.0.0.0", [4]byte{0, 0, 0, 0}, false},
		{"255.255.255.255", [4]byte{255, 255, 255, 255}, false},
		{"10.0.0.1", [4]byte{10, 0, 0, 1}, false},
		{"127.0.0.1", [4]byte{127, 0, 0, 1}, false},
		{"256.1.1.1", [4]byte{}, true},          // out of range
		{"1.1.1", [4]byte{}, true},              // too few octets
		{"1.1.1.1.1", [4]byte{}, true},          // too many octets
		{"a.b.c.d", [4]byte{}, true},            // invalid chars
		{"-1.1.1.1", [4]byte{}, true},           // negative
		{"1.1.1.1.extra", [4]byte{}, true},      // extra content
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result, err := parseIPv4(tc.input)
			if tc.hasError {
				if err == nil {
					t.Errorf("parseIPv4(%q) expected error, got nil", tc.input)
				}
			} else {
				if err != nil {
					t.Errorf("parseIPv4(%q) unexpected error: %v", tc.input, err)
				}
				if result != tc.expected {
					t.Errorf("parseIPv4(%q) = %v, expected %v", tc.input, result, tc.expected)
				}
			}
		})
	}
}

func TestParseIPv4FromMappedIPv6(t *testing.T) {
	tests := []struct {
		input    string
		expected [4]byte
		hasError bool
	}{
		// Dotted decimal notation
		{"192.168.1.1", [4]byte{192, 168, 1, 1}, false},
		{"10.0.0.1", [4]byte{10, 0, 0, 1}, false},
		// Hex notation with colon
		{"c0a8:0101", [4]byte{192, 168, 1, 1}, false},
		{"0a00:0001", [4]byte{10, 0, 0, 1}, false},
		// Single hex value
		{"c0a80101", [4]byte{192, 168, 1, 1}, false},
		// Invalid cases
		{"invalid", [4]byte{}, true},
		{"::::", [4]byte{}, true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result, err := parseIPv4FromMappedIPv6(tc.input)
			if tc.hasError {
				if err == nil {
					t.Errorf("parseIPv4FromMappedIPv6(%q) expected error, got nil", tc.input)
				}
			} else {
				if err != nil {
					t.Errorf("parseIPv4FromMappedIPv6(%q) unexpected error: %v", tc.input, err)
				}
				if result != tc.expected {
					t.Errorf("parseIPv4FromMappedIPv6(%q) = %v, expected %v", tc.input, result, tc.expected)
				}
			}
		})
	}
}

func TestIsPrivateIPv4(t *testing.T) {
	tests := []struct {
		input    [4]byte
		expected bool
		name     string
	}{
		// Private ranges
		{[4]byte{0, 0, 0, 0}, true, "0.0.0.0/8 current network"},
		{[4]byte{0, 255, 255, 255}, true, "0.0.0.0/8 boundary"},
		{[4]byte{10, 0, 0, 1}, true, "10.0.0.0/8 private"},
		{[4]byte{10, 255, 255, 255}, true, "10.0.0.0/8 boundary"},
		{[4]byte{127, 0, 0, 1}, true, "127.0.0.0/8 loopback"},
		{[4]byte{127, 255, 255, 255}, true, "127.0.0.0/8 boundary"},
		{[4]byte{169, 254, 0, 1}, true, "169.254.0.0/16 link-local"},
		{[4]byte{169, 254, 255, 255}, true, "169.254.0.0/16 boundary"},
		{[4]byte{172, 16, 0, 1}, true, "172.16.0.0/12 private start"},
		{[4]byte{172, 31, 255, 255}, true, "172.16.0.0/12 private end"},
		{[4]byte{172, 20, 0, 1}, true, "172.16.0.0/12 private middle"},
		{[4]byte{192, 168, 0, 1}, true, "192.168.0.0/16 private"},
		{[4]byte{192, 168, 255, 255}, true, "192.168.0.0/16 boundary"},
		{[4]byte{100, 64, 0, 1}, true, "100.64.0.0/10 CGNAT start"},
		{[4]byte{100, 127, 255, 255}, true, "100.64.0.0/10 CGNAT end"},
		{[4]byte{100, 100, 0, 1}, true, "100.64.0.0/10 CGNAT middle"},

		// Public IPs
		{[4]byte{8, 8, 8, 8}, false, "Google DNS"},
		{[4]byte{1, 1, 1, 1}, false, "Cloudflare DNS"},
		{[4]byte{208, 67, 222, 222}, false, "OpenDNS"},
		{[4]byte{172, 15, 0, 1}, false, "just before 172.16/12"},
		{[4]byte{172, 32, 0, 1}, false, "just after 172.31/12"},
		{[4]byte{169, 253, 0, 1}, false, "just before 169.254/16"},
		{[4]byte{169, 255, 0, 1}, false, "just after 169.254/16"},
		{[4]byte{100, 63, 0, 1}, false, "just before 100.64/10"},
		{[4]byte{100, 128, 0, 1}, false, "just after 100.127/10"},
		{[4]byte{192, 167, 0, 1}, false, "just before 192.168/16"},
		{[4]byte{192, 169, 0, 1}, false, "just after 192.168/16"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsPrivateIPv4(tc.input)
			if result != tc.expected {
				t.Errorf("IsPrivateIPv4(%v) = %v, expected %v (%s)", tc.input, result, tc.expected, tc.name)
			}
		})
	}
}

func TestIsPrivateIPAddress(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
		name     string
	}{
		// IPv4 private addresses
		{"127.0.0.1", true, "loopback"},
		{"10.0.0.1", true, "10.x private"},
		{"192.168.1.1", true, "192.168.x private"},
		{"172.16.0.1", true, "172.16.x private"},
		{"172.31.255.255", true, "172.31.x private boundary"},
		{"169.254.1.1", true, "link-local"},
		{"0.0.0.0", true, "zero address"},
		{"100.64.0.1", true, "CGNAT start"},
		{"100.127.255.255", true, "CGNAT end"},

		// IPv4 public addresses
		{"8.8.8.8", false, "Google DNS"},
		{"1.1.1.1", false, "Cloudflare DNS"},
		{"208.67.222.222", false, "OpenDNS"},
		{"172.15.0.1", false, "just before 172.16/12"},
		{"172.32.0.1", false, "just after 172.31/12"},

		// IPv6 loopback
		{"::1", true, "IPv6 loopback"},
		{"::", true, "IPv6 unspecified"},
		{"[::1]", true, "IPv6 loopback bracketed"},
		{"[::]", true, "IPv6 unspecified bracketed"},

		// IPv6 private prefixes
		{"fe80::1", true, "fe80 link-local"},
		{"fe80:0:0:0:0:0:0:1", true, "fe80 link-local full"},
		{"fec0::1", true, "fec0 site-local"},
		{"fc00::1", true, "fc unique local"},
		{"fd00::1", true, "fd unique local"},
		{"fd12:3456:789a::1", true, "fd unique local full"},

		// IPv6 public addresses
		{"2001:4860:4860::8888", false, "Google DNS IPv6"},
		{"2606:4700:4700::1111", false, "Cloudflare DNS IPv6"},

		// IPv4-mapped IPv6
		{"::ffff:192.168.1.1", true, "IPv4-mapped private"},
		{"::ffff:10.0.0.1", true, "IPv4-mapped 10.x"},
		{"::ffff:127.0.0.1", true, "IPv4-mapped loopback"},
		{"::ffff:8.8.8.8", false, "IPv4-mapped public"},
		{"::ffff:1.1.1.1", false, "IPv4-mapped Cloudflare"},
		{"::ffff:c0a8:0101", true, "IPv4-mapped private hex"},

		// Edge cases
		{"", false, "empty string"},
		{"  192.168.1.1  ", true, "whitespace IPv4"},
		{"  ::1  ", true, "whitespace IPv6"},
		{"invalid", false, "invalid address"},
		{"[fe80::1]", true, "bracketed IPv6"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsPrivateIPAddress(tc.input)
			if result != tc.expected {
				t.Errorf("IsPrivateIPAddress(%q) = %v, expected %v (%s)", tc.input, result, tc.expected, tc.name)
			}
		})
	}
}

func TestIsBlockedHostname(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
		name     string
	}{
		// Explicitly blocked hostnames
		{"localhost", true, "localhost"},
		{"LOCALHOST", true, "localhost uppercase"},
		{"  localhost  ", true, "localhost with spaces"},
		{"localhost.", true, "localhost with trailing dot"},
		{"metadata.google.internal", true, "GCE metadata"},
		{"METADATA.GOOGLE.INTERNAL", true, "GCE metadata uppercase"},

		// Dangerous suffixes
		{"foo.localhost", true, ".localhost suffix"},
		{"bar.local", true, ".local suffix"},
		{"baz.internal", true, ".internal suffix"},
		{"sub.domain.localhost", true, "nested .localhost"},
		{"sub.domain.local", true, "nested .local"},
		{"sub.domain.internal", true, "nested .internal"},

		// Safe hostnames
		{"example.com", false, "example.com"},
		{"google.com", false, "google.com"},
		{"api.example.com", false, "subdomain"},
		{"localhostnot.com", false, "contains localhost but not suffix"},
		{"mylocal.com", false, "ends with local but not .local"},
		{"notinternal.com", false, "ends with internal but not .internal"},

		// Edge cases
		{"", false, "empty string"},
		{"   ", false, "whitespace only"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsBlockedHostname(tc.input)
			if result != tc.expected {
				t.Errorf("IsBlockedHostname(%q) = %v, expected %v (%s)", tc.input, result, tc.expected, tc.name)
			}
		})
	}
}

func TestValidatePublicHostname(t *testing.T) {
	tests := []struct {
		input        string
		expectError  bool
		expectSSRF   bool
		name         string
	}{
		// Blocked hostnames
		{"localhost", true, true, "localhost blocked"},
		{"metadata.google.internal", true, true, "GCE metadata blocked"},
		{"foo.localhost", true, true, ".localhost suffix blocked"},
		{"bar.local", true, true, ".local suffix blocked"},
		{"baz.internal", true, true, ".internal suffix blocked"},

		// Private IPs directly
		{"127.0.0.1", true, true, "loopback IP blocked"},
		{"192.168.1.1", true, true, "private IP blocked"},
		{"10.0.0.1", true, true, "10.x IP blocked"},
		{"[::1]", true, true, "IPv6 loopback blocked"},
		{"[fe80::1]", true, true, "IPv6 link-local blocked"},

		// Empty/invalid
		{"", true, false, "empty hostname"},
		{"   ", true, false, "whitespace only"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePublicHostname(tc.input)
			if tc.expectError {
				if err == nil {
					t.Errorf("ValidatePublicHostname(%q) expected error, got nil", tc.input)
					return
				}
				if tc.expectSSRF {
					var ssrfErr *SSRFBlockedError
					if !errors.As(err, &ssrfErr) {
						t.Errorf("ValidatePublicHostname(%q) expected SSRFBlockedError, got %T: %v", tc.input, err, err)
					}
				}
			} else {
				if err != nil {
					t.Errorf("ValidatePublicHostname(%q) unexpected error: %v", tc.input, err)
				}
			}
		})
	}
}

// TestValidatePublicHostnameWithRealDNS tests validation against real DNS lookups.
// These tests may fail in isolated network environments.
func TestValidatePublicHostnameWithRealDNS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping DNS lookup tests in short mode")
	}

	// Test that public hostnames pass validation
	publicHostnames := []string{
		"google.com",
		"cloudflare.com",
		"github.com",
	}

	for _, hostname := range publicHostnames {
		t.Run(hostname, func(t *testing.T) {
			err := ValidatePublicHostname(hostname)
			if err != nil {
				// DNS lookups can fail in CI environments, so we just log a warning
				t.Logf("Warning: ValidatePublicHostname(%q) returned error: %v (may be expected in isolated environments)", hostname, err)
			}
		})
	}
}
