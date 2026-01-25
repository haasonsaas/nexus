package policy

import (
	"testing"
)

func TestNormalizeGroupActivation(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  *GroupActivationMode
	}{
		// Valid mention values
		{name: "mention", input: "mention", want: ptrActivationMode(ActivationMention)},
		{name: "MENTION uppercase", input: "MENTION", want: ptrActivationMode(ActivationMention)},
		{name: "Mention mixed case", input: "Mention", want: ptrActivationMode(ActivationMention)},
		{name: "mention with spaces", input: "  mention  ", want: ptrActivationMode(ActivationMention)},

		// Valid always values
		{name: "always", input: "always", want: ptrActivationMode(ActivationAlways)},
		{name: "ALWAYS uppercase", input: "ALWAYS", want: ptrActivationMode(ActivationAlways)},
		{name: "Always mixed case", input: "Always", want: ptrActivationMode(ActivationAlways)},
		{name: "always with spaces", input: "  always  ", want: ptrActivationMode(ActivationAlways)},

		// Invalid values
		{name: "empty string", input: "", want: nil},
		{name: "whitespace only", input: "   ", want: nil},
		{name: "unknown value", input: "unknown", want: nil},
		{name: "on", input: "on", want: nil},
		{name: "off", input: "off", want: nil},
		{name: "enabled", input: "enabled", want: nil},
		{name: "disabled", input: "disabled", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeGroupActivation(tt.input)
			if tt.want == nil {
				if got != nil {
					t.Errorf("NormalizeGroupActivation(%q) = %v, want nil", tt.input, *got)
				}
			} else {
				if got == nil {
					t.Errorf("NormalizeGroupActivation(%q) = nil, want %v", tt.input, *tt.want)
				} else if *got != *tt.want {
					t.Errorf("NormalizeGroupActivation(%q) = %v, want %v", tt.input, *got, *tt.want)
				}
			}
		})
	}
}

func TestParseActivationCommand(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantHasCommand bool
		wantMode       *GroupActivationMode
	}{
		// Valid commands with modes
		{name: "activation mention", input: "/activation mention", wantHasCommand: true, wantMode: ptrActivationMode(ActivationMention)},
		{name: "activation always", input: "/activation always", wantHasCommand: true, wantMode: ptrActivationMode(ActivationAlways)},

		// Case insensitivity
		{name: "ACTIVATION MENTION", input: "/ACTIVATION MENTION", wantHasCommand: true, wantMode: ptrActivationMode(ActivationMention)},
		{name: "Activation Always", input: "/Activation Always", wantHasCommand: true, wantMode: ptrActivationMode(ActivationAlways)},

		// Command without argument
		{name: "activation only", input: "/activation", wantHasCommand: true, wantMode: nil},

		// Colon syntax
		{name: "activation: mention", input: "/activation: mention", wantHasCommand: true, wantMode: ptrActivationMode(ActivationMention)},
		{name: "activation:mention no space", input: "/activation:mention", wantHasCommand: true, wantMode: ptrActivationMode(ActivationMention)},
		{name: "activation : always", input: "/activation : always", wantHasCommand: true, wantMode: ptrActivationMode(ActivationAlways)},

		// Whitespace handling
		{name: "leading whitespace", input: "  /activation mention", wantHasCommand: true, wantMode: ptrActivationMode(ActivationMention)},
		{name: "trailing whitespace", input: "/activation mention  ", wantHasCommand: true, wantMode: ptrActivationMode(ActivationMention)},
		{name: "extra spaces between", input: "/activation   always", wantHasCommand: true, wantMode: ptrActivationMode(ActivationAlways)},

		// Unknown mode (command valid but mode not recognized)
		{name: "activation unknown", input: "/activation unknown", wantHasCommand: true, wantMode: nil},
		{name: "activation on", input: "/activation on", wantHasCommand: true, wantMode: nil},

		// Not a command
		{name: "empty string", input: "", wantHasCommand: false, wantMode: nil},
		{name: "whitespace only", input: "   ", wantHasCommand: false, wantMode: nil},
		{name: "no slash", input: "activation mention", wantHasCommand: false, wantMode: nil},
		{name: "different command", input: "/help", wantHasCommand: false, wantMode: nil},
		{name: "activation with extra text", input: "/activation mention extra", wantHasCommand: false, wantMode: nil},
		{name: "inline text before", input: "hey /activation mention", wantHasCommand: false, wantMode: nil},
		{name: "sentence with activation", input: "please set activation mention", wantHasCommand: false, wantMode: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseActivationCommand(tt.input)
			if got.HasCommand != tt.wantHasCommand {
				t.Errorf("ParseActivationCommand(%q).HasCommand = %v, want %v", tt.input, got.HasCommand, tt.wantHasCommand)
			}
			if tt.wantMode == nil {
				if got.Mode != nil {
					t.Errorf("ParseActivationCommand(%q).Mode = %v, want nil", tt.input, *got.Mode)
				}
			} else {
				if got.Mode == nil {
					t.Errorf("ParseActivationCommand(%q).Mode = nil, want %v", tt.input, *tt.wantMode)
				} else if *got.Mode != *tt.wantMode {
					t.Errorf("ParseActivationCommand(%q).Mode = %v, want %v", tt.input, *got.Mode, *tt.wantMode)
				}
			}
		})
	}
}

func ptrActivationMode(m GroupActivationMode) *GroupActivationMode {
	return &m
}
