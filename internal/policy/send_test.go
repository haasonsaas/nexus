package policy

import (
	"testing"
)

func TestNormalizeSendPolicyOverride(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  *SendPolicyOverride
	}{
		// Valid allow values
		{name: "allow", input: "allow", want: ptrSendPolicy(SendPolicyAllow)},
		{name: "on", input: "on", want: ptrSendPolicy(SendPolicyAllow)},
		{name: "ALLOW uppercase", input: "ALLOW", want: ptrSendPolicy(SendPolicyAllow)},
		{name: "On mixed case", input: "On", want: ptrSendPolicy(SendPolicyAllow)},
		{name: "allow with spaces", input: "  allow  ", want: ptrSendPolicy(SendPolicyAllow)},

		// Valid deny values
		{name: "deny", input: "deny", want: ptrSendPolicy(SendPolicyDeny)},
		{name: "off", input: "off", want: ptrSendPolicy(SendPolicyDeny)},
		{name: "DENY uppercase", input: "DENY", want: ptrSendPolicy(SendPolicyDeny)},
		{name: "OFF uppercase", input: "OFF", want: ptrSendPolicy(SendPolicyDeny)},
		{name: "deny with spaces", input: "  deny  ", want: ptrSendPolicy(SendPolicyDeny)},

		// Invalid values
		{name: "empty string", input: "", want: nil},
		{name: "whitespace only", input: "   ", want: nil},
		{name: "unknown value", input: "unknown", want: nil},
		{name: "yes", input: "yes", want: nil},
		{name: "no", input: "no", want: nil},
		{name: "true", input: "true", want: nil},
		{name: "false", input: "false", want: nil},
		{name: "inherit", input: "inherit", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeSendPolicyOverride(tt.input)
			if tt.want == nil {
				if got != nil {
					t.Errorf("NormalizeSendPolicyOverride(%q) = %v, want nil", tt.input, *got)
				}
			} else {
				if got == nil {
					t.Errorf("NormalizeSendPolicyOverride(%q) = nil, want %v", tt.input, *tt.want)
				} else if *got != *tt.want {
					t.Errorf("NormalizeSendPolicyOverride(%q) = %v, want %v", tt.input, *got, *tt.want)
				}
			}
		})
	}
}

func TestParseSendPolicyCommand(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantHasCommand bool
		wantMode       string
	}{
		// Valid commands with modes
		{name: "send allow", input: "/send allow", wantHasCommand: true, wantMode: "allow"},
		{name: "send deny", input: "/send deny", wantHasCommand: true, wantMode: "deny"},
		{name: "send on", input: "/send on", wantHasCommand: true, wantMode: "allow"},
		{name: "send off", input: "/send off", wantHasCommand: true, wantMode: "deny"},

		// Inherit/default/reset modes
		{name: "send inherit", input: "/send inherit", wantHasCommand: true, wantMode: "inherit"},
		{name: "send default", input: "/send default", wantHasCommand: true, wantMode: "inherit"},
		{name: "send reset", input: "/send reset", wantHasCommand: true, wantMode: "inherit"},

		// Case insensitivity
		{name: "SEND ALLOW", input: "/SEND ALLOW", wantHasCommand: true, wantMode: "allow"},
		{name: "Send Deny", input: "/Send Deny", wantHasCommand: true, wantMode: "deny"},
		{name: "send INHERIT", input: "/send INHERIT", wantHasCommand: true, wantMode: "inherit"},

		// Command without argument
		{name: "send only", input: "/send", wantHasCommand: true, wantMode: ""},

		// Colon syntax
		{name: "send: allow", input: "/send: allow", wantHasCommand: true, wantMode: "allow"},
		{name: "send:allow no space", input: "/send:allow", wantHasCommand: true, wantMode: "allow"},
		{name: "send : allow spaces", input: "/send : allow", wantHasCommand: true, wantMode: "allow"},
		{name: "send: deny", input: "/send: deny", wantHasCommand: true, wantMode: "deny"},

		// Whitespace handling
		{name: "leading whitespace", input: "  /send allow", wantHasCommand: true, wantMode: "allow"},
		{name: "trailing whitespace", input: "/send allow  ", wantHasCommand: true, wantMode: "allow"},
		{name: "extra spaces between", input: "/send   allow", wantHasCommand: true, wantMode: "allow"},

		// Unknown mode (command valid but mode not recognized)
		{name: "send unknown", input: "/send unknown", wantHasCommand: true, wantMode: ""},

		// Not a command
		{name: "empty string", input: "", wantHasCommand: false, wantMode: ""},
		{name: "whitespace only", input: "   ", wantHasCommand: false, wantMode: ""},
		{name: "no slash", input: "send allow", wantHasCommand: false, wantMode: ""},
		{name: "different command", input: "/help", wantHasCommand: false, wantMode: ""},
		{name: "send with extra text", input: "/send allow extra", wantHasCommand: false, wantMode: ""},
		{name: "inline text before", input: "hey /send allow", wantHasCommand: false, wantMode: ""},
		{name: "sentence with send", input: "please send allow", wantHasCommand: false, wantMode: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSendPolicyCommand(tt.input)
			if got.HasCommand != tt.wantHasCommand {
				t.Errorf("ParseSendPolicyCommand(%q).HasCommand = %v, want %v", tt.input, got.HasCommand, tt.wantHasCommand)
			}
			if got.Mode != tt.wantMode {
				t.Errorf("ParseSendPolicyCommand(%q).Mode = %q, want %q", tt.input, got.Mode, tt.wantMode)
			}
		})
	}
}

func TestNormalizeCommandBody(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "simple command", input: "/send", want: "/send"},
		{name: "command with arg", input: "/send allow", want: "/send allow"},
		{name: "colon syntax", input: "/send: allow", want: "/send allow"},
		{name: "colon no space", input: "/send:allow", want: "/send allow"},
		{name: "colon with spaces", input: "/send : allow", want: "/send allow"},
		{name: "colon only", input: "/send:", want: "/send"},
		{name: "not a command", input: "hello world", want: "hello world"},
		{name: "with newline", input: "/send allow\nmore text", want: "/send allow"},
		{name: "leading spaces", input: "  /send allow", want: "/send allow"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeCommandBody(tt.input)
			if got != tt.want {
				t.Errorf("normalizeCommandBody(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func ptrSendPolicy(p SendPolicyOverride) *SendPolicyOverride {
	return &p
}
