package ssrf

import (
	"strconv"
	"strings"
)

// privateIPv6Prefixes contains prefixes that identify private/link-local IPv6 addresses.
var privateIPv6Prefixes = []string{"fe80:", "fec0:", "fc", "fd"}

// normalizeHostname normalizes a hostname by trimming whitespace, converting to lowercase,
// removing trailing dots, and unwrapping IPv6 brackets.
func normalizeHostname(hostname string) string {
	normalized := strings.TrimSpace(hostname)
	normalized = strings.ToLower(normalized)
	normalized = strings.TrimSuffix(normalized, ".")

	if strings.HasPrefix(normalized, "[") && strings.HasSuffix(normalized, "]") {
		normalized = normalized[1 : len(normalized)-1]
	}

	return normalized
}

// parseIPv4 parses an IPv4 address string and returns its four octets.
// Returns an error if the address is invalid.
func parseIPv4(address string) ([4]byte, error) {
	var result [4]byte

	parts := strings.Split(address, ".")
	if len(parts) != 4 {
		return result, NewSSRFBlockedError("invalid IPv4 address: must have 4 octets")
	}

	for i, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil {
			return result, NewSSRFBlockedError("invalid IPv4 address: invalid octet")
		}
		if value < 0 || value > 255 {
			return result, NewSSRFBlockedError("invalid IPv4 address: octet out of range")
		}
		result[i] = byte(value)
	}

	return result, nil
}

// parseIPv4FromMappedIPv6 extracts and parses the IPv4 address from an IPv4-mapped IPv6 suffix.
// It handles both dotted-decimal (e.g., "192.168.1.1") and hex notation (e.g., "c0a8:0101").
func parseIPv4FromMappedIPv6(mapped string) ([4]byte, error) {
	var result [4]byte

	// Handle dotted decimal notation (e.g., "192.168.1.1")
	if strings.Contains(mapped, ".") {
		return parseIPv4(mapped)
	}

	// Handle hex notation (e.g., "c0a8:0101" or "c0a80101")
	parts := strings.Split(mapped, ":")
	var cleanParts []string
	for _, p := range parts {
		if p != "" {
			cleanParts = append(cleanParts, p)
		}
	}

	if len(cleanParts) == 1 {
		// Single hex value representing all 4 bytes (e.g., "c0a80101")
		value, err := strconv.ParseUint(cleanParts[0], 16, 32)
		if err != nil {
			return result, NewSSRFBlockedError("invalid IPv4-mapped IPv6: invalid hex value")
		}
		if value > 0xffffffff {
			return result, NewSSRFBlockedError("invalid IPv4-mapped IPv6: value out of range")
		}
		result[0] = byte((value >> 24) & 0xff)
		result[1] = byte((value >> 16) & 0xff)
		result[2] = byte((value >> 8) & 0xff)
		result[3] = byte(value & 0xff)
		return result, nil
	}

	if len(cleanParts) != 2 {
		return result, NewSSRFBlockedError("invalid IPv4-mapped IPv6: expected 2 hex groups")
	}

	// Two hex values (e.g., "c0a8:0101")
	high, err := strconv.ParseUint(cleanParts[0], 16, 16)
	if err != nil {
		return result, NewSSRFBlockedError("invalid IPv4-mapped IPv6: invalid high hex value")
	}
	low, err := strconv.ParseUint(cleanParts[1], 16, 16)
	if err != nil {
		return result, NewSSRFBlockedError("invalid IPv4-mapped IPv6: invalid low hex value")
	}

	value := (high << 16) + low
	result[0] = byte((value >> 24) & 0xff)
	result[1] = byte((value >> 16) & 0xff)
	result[2] = byte((value >> 8) & 0xff)
	result[3] = byte(value & 0xff)

	return result, nil
}

// IsPrivateIPv4 checks if an IPv4 address (represented as 4 bytes) is a private/reserved address.
// This includes:
// - 0.0.0.0/8 (current network)
// - 10.0.0.0/8 (private)
// - 127.0.0.0/8 (loopback)
// - 169.254.0.0/16 (link-local)
// - 172.16.0.0/12 (private)
// - 192.168.0.0/16 (private)
// - 100.64.0.0/10 (carrier-grade NAT)
func IsPrivateIPv4(parts [4]byte) bool {
	octet1 := parts[0]
	octet2 := parts[1]

	// 0.0.0.0/8 - current network
	if octet1 == 0 {
		return true
	}
	// 10.0.0.0/8 - private
	if octet1 == 10 {
		return true
	}
	// 127.0.0.0/8 - loopback
	if octet1 == 127 {
		return true
	}
	// 169.254.0.0/16 - link-local
	if octet1 == 169 && octet2 == 254 {
		return true
	}
	// 172.16.0.0/12 - private (172.16.x.x - 172.31.x.x)
	if octet1 == 172 && octet2 >= 16 && octet2 <= 31 {
		return true
	}
	// 192.168.0.0/16 - private
	if octet1 == 192 && octet2 == 168 {
		return true
	}
	// 100.64.0.0/10 - carrier-grade NAT (100.64.x.x - 100.127.x.x)
	if octet1 == 100 && octet2 >= 64 && octet2 <= 127 {
		return true
	}

	return false
}

// IsPrivateIPAddress checks if an IP address string (IPv4 or IPv6) is a private/reserved address.
func IsPrivateIPAddress(address string) bool {
	normalized := strings.TrimSpace(address)
	normalized = strings.ToLower(normalized)

	// Unwrap IPv6 brackets
	if strings.HasPrefix(normalized, "[") && strings.HasSuffix(normalized, "]") {
		normalized = normalized[1 : len(normalized)-1]
	}

	if normalized == "" {
		return false
	}

	// Handle IPv4-mapped IPv6 addresses (::ffff:x.x.x.x or ::ffff:xxxx:xxxx)
	if strings.HasPrefix(normalized, "::ffff:") {
		mapped := normalized[len("::ffff:"):]
		ipv4, err := parseIPv4FromMappedIPv6(mapped)
		if err == nil {
			return IsPrivateIPv4(ipv4)
		}
	}

	// Handle IPv6 addresses
	if strings.Contains(normalized, ":") {
		// Loopback addresses
		if normalized == "::" || normalized == "::1" {
			return true
		}
		// Check private IPv6 prefixes
		for _, prefix := range privateIPv6Prefixes {
			if strings.HasPrefix(normalized, prefix) {
				return true
			}
		}
		return false
	}

	// Handle IPv4 addresses
	ipv4, err := parseIPv4(normalized)
	if err != nil {
		return false
	}
	return IsPrivateIPv4(ipv4)
}
