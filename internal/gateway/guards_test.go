package gateway

import (
	"strings"
	"testing"
)

func TestMaxToolResultSize(t *testing.T) {
	if MaxToolResultSize != 64*1024 {
		t.Errorf("MaxToolResultSize = %d, want %d", MaxToolResultSize, 64*1024)
	}
}

func TestSanitizeToolResult_Truncation(t *testing.T) {
	// Create content larger than limit
	largeContent := strings.Repeat("a", MaxToolResultSize+100)

	result := SanitizeToolResult(largeContent)

	if len(result) > MaxToolResultSize+50 { // Allow for suffix
		t.Errorf("Result not truncated properly, len = %d", len(result))
	}

	if !strings.Contains(result, "[truncated]") {
		t.Error("Truncated result should contain [truncated] suffix")
	}
}

func TestSanitizeToolResult_APIKeyRedaction(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantRed bool
	}{
		{
			name:    "api_key with equals",
			input:   `api_key=sk-12345678901234567890`,
			wantRed: true,
		},
		{
			name:    "apikey with colon",
			input:   `apikey: "abcdefghij1234567890abcd"`,
			wantRed: true,
		},
		{
			name:    "API-KEY uppercase",
			input:   `API-KEY = "test_key_123456789012345"`,
			wantRed: true,
		},
		{
			name:    "short value not redacted",
			input:   `api_key=short`,
			wantRed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeToolResult(tt.input)
			hasRedacted := strings.Contains(result, "[REDACTED]")
			if hasRedacted != tt.wantRed {
				t.Errorf("SanitizeToolResult(%q) redacted = %v, want %v; result = %q",
					tt.input, hasRedacted, tt.wantRed, result)
			}
		})
	}
}

func TestSanitizeToolResult_BearerToken(t *testing.T) {
	input := `Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.abc123`

	result := SanitizeToolResult(input)

	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("Bearer token not redacted: %s", result)
	}
}

func TestSanitizeToolResult_AWSKeys(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "aws access key",
			input: `aws_access_key=AKIAIOSFODNN7EXAMPLE`,
		},
		{
			name:  "aws secret key",
			input: `aws_secret_access_key="wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"`,
		},
		{
			name:  "amazon token",
			input: `amazon_session_token: IQoJb3JpZ2luX2VjEA...`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeToolResult(tt.input)
			if !strings.Contains(result, "[REDACTED]") {
				t.Errorf("AWS key not redacted: %s", result)
			}
		})
	}
}

func TestSanitizeToolResult_GenericSecrets(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "password",
			input: `password=MySecretPass123!`,
		},
		{
			name:  "passwd",
			input: `passwd: "supersecret123"`,
		},
		{
			name:  "secret",
			input: `secret=verylongsecretvalue`,
		},
		{
			name:  "token",
			input: `token: ghp_abc123456789012345`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeToolResult(tt.input)
			if !strings.Contains(result, "[REDACTED]") {
				t.Errorf("Generic secret not redacted: %s", result)
			}
		})
	}
}

func TestSanitizeToolResult_PrivateKey(t *testing.T) {
	input := `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEA2Z3qX2BTLS4e
-----END RSA PRIVATE KEY-----`

	result := SanitizeToolResult(input)

	if !strings.Contains(result, "[REDACTED]") {
		t.Errorf("Private key header not redacted: %s", result)
	}
}

func TestDetectSecrets(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "no secrets",
			content: "This is normal content",
			want:    nil,
		},
		{
			name:    "api key",
			content: `api_key=sk-12345678901234567890`,
			want:    []string{"api_key"},
		},
		{
			name:    "bearer token",
			content: `Bearer eyJhbGciOiJIUzI1NiJ9`,
			want:    []string{"bearer_token"},
		},
		{
			name:    "multiple secrets",
			content: `api_key=test12345678901234567890 and password=mysecretpass`,
			want:    []string{"api_key", "generic_secret"},
		},
		{
			name:    "private key",
			content: `-----BEGIN PRIVATE KEY-----`,
			want:    []string{"private_key"},
		},
		{
			name:    "empty content",
			content: "",
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectSecrets(tt.content)
			if len(got) != len(tt.want) {
				t.Errorf("DetectSecrets() = %v, want %v", got, tt.want)
				return
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("DetectSecrets()[%d] = %q, want %q", i, v, tt.want[i])
				}
			}
		})
	}
}

func TestContainsPrivateKey(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name:    "RSA private key",
			content: "-----BEGIN RSA PRIVATE KEY-----\nfoo\n-----END RSA PRIVATE KEY-----",
			want:    true,
		},
		{
			name:    "EC private key",
			content: "-----BEGIN EC PRIVATE KEY-----",
			want:    true,
		},
		{
			name:    "public key not matched",
			content: "-----BEGIN PUBLIC KEY-----",
			want:    false,
		},
		{
			name:    "no key",
			content: "just some text",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ContainsPrivateKey(tt.content); got != tt.want {
				t.Errorf("ContainsPrivateKey() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTruncateWithSuffix(t *testing.T) {
	tests := []struct {
		name    string
		content string
		maxLen  int
		suffix  string
		want    string
	}{
		{
			name:    "no truncation needed",
			content: "short",
			maxLen:  100,
			suffix:  "...",
			want:    "short",
		},
		{
			name:    "truncation with suffix",
			content: "this is a long string",
			maxLen:  10,
			suffix:  "...",
			want:    "this is...",
		},
		{
			name:    "exact length",
			content: "exact",
			maxLen:  5,
			suffix:  "...",
			want:    "exact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateWithSuffix(tt.content, tt.maxLen, tt.suffix)
			if got != tt.want {
				t.Errorf("TruncateWithSuffix() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRedactSecrets(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		replacement string
		wantContain string
	}{
		{
			name:        "default replacement",
			content:     "api_key=sk-12345678901234567890",
			replacement: "",
			wantContain: "[REDACTED]",
		},
		{
			name:        "custom replacement",
			content:     "password=supersecret123",
			replacement: "[HIDDEN]",
			wantContain: "[HIDDEN]",
		},
		{
			name:        "empty content",
			content:     "",
			replacement: "",
			wantContain: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactSecrets(tt.content, tt.replacement)
			if tt.wantContain != "" && !strings.Contains(got, tt.wantContain) {
				t.Errorf("RedactSecrets() = %q, want to contain %q", got, tt.wantContain)
			}
		})
	}
}
