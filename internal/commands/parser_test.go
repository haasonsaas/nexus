package commands

import (
	"context"
	"testing"
)

func TestParser_Parse(t *testing.T) {
	registry := NewRegistry(nil)
	registry.Register(&Command{
		Name:        "help",
		AcceptsArgs: true,
		Handler: func(ctx context.Context, inv *Invocation) (*Result, error) {
			return &Result{Text: "help"}, nil
		},
	})
	registry.Register(&Command{
		Name:        "status",
		AcceptsArgs: false,
		Handler: func(ctx context.Context, inv *Invocation) (*Result, error) {
			return &Result{Text: "status"}, nil
		},
	})

	parser := NewParser(registry)

	tests := []struct {
		name             string
		input            string
		wantHasCommand   bool
		wantControlCmd   bool
		wantPrimaryName  string
		wantPrimaryArgs  string
		wantCommandCount int
	}{
		{
			name:             "empty string",
			input:            "",
			wantHasCommand:   false,
			wantCommandCount: 0,
		},
		{
			name:             "no command",
			input:            "hello world",
			wantHasCommand:   false,
			wantCommandCount: 0,
		},
		{
			name:             "simple command",
			input:            "/help",
			wantHasCommand:   true,
			wantControlCmd:   true,
			wantPrimaryName:  "help",
			wantPrimaryArgs:  "",
			wantCommandCount: 1,
		},
		{
			name:             "command with args",
			input:            "/help status",
			wantHasCommand:   true,
			wantControlCmd:   true,
			wantPrimaryName:  "help",
			wantPrimaryArgs:  "status",
			wantCommandCount: 1,
		},
		{
			name:             "bang prefix",
			input:            "!status",
			wantHasCommand:   true,
			wantControlCmd:   true,
			wantPrimaryName:  "status",
			wantPrimaryArgs:  "",
			wantCommandCount: 1,
		},
		{
			name:             "unregistered command",
			input:            "/unknown",
			wantHasCommand:   true,
			wantControlCmd:   false, // not a registered command
			wantPrimaryName:  "unknown",
			wantPrimaryArgs:  "",
			wantCommandCount: 1,
		},
		{
			name:             "inline command",
			input:            "hey /help please",
			wantHasCommand:   true,
			wantControlCmd:   false,
			wantPrimaryName:  "",
			wantCommandCount: 1,
		},
		{
			name:             "multiple inline commands",
			input:            "check /help and /status",
			wantHasCommand:   true,
			wantControlCmd:   false,
			wantPrimaryName:  "",
			wantCommandCount: 2,
		},
		{
			name:             "not a command - no letter after prefix",
			input:            "/123",
			wantHasCommand:   false,
			wantCommandCount: 0,
		},
		{
			name:             "url is not command",
			input:            "check out https://example.com/help",
			wantHasCommand:   false,
			wantCommandCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detection := parser.Parse(tt.input)

			if detection.HasCommand != tt.wantHasCommand {
				t.Errorf("HasCommand = %v, want %v", detection.HasCommand, tt.wantHasCommand)
			}

			if detection.IsControlCommand != tt.wantControlCmd {
				t.Errorf("IsControlCommand = %v, want %v", detection.IsControlCommand, tt.wantControlCmd)
			}

			if len(detection.Commands) != tt.wantCommandCount {
				t.Errorf("command count = %d, want %d", len(detection.Commands), tt.wantCommandCount)
			}

			if tt.wantPrimaryName != "" {
				if detection.Primary == nil {
					t.Error("expected Primary to be set")
				} else {
					if detection.Primary.Name != tt.wantPrimaryName {
						t.Errorf("Primary.Name = %s, want %s", detection.Primary.Name, tt.wantPrimaryName)
					}
					if detection.Primary.Args != tt.wantPrimaryArgs {
						t.Errorf("Primary.Args = %s, want %s", detection.Primary.Args, tt.wantPrimaryArgs)
					}
				}
			}
		})
	}
}

func TestParser_ParseCommand(t *testing.T) {
	parser := NewParser(nil)

	tests := []struct {
		name     string
		input    string
		wantName string
		wantArgs string
		wantNil  bool
	}{
		{
			name:    "empty",
			input:   "",
			wantNil: true,
		},
		{
			name:    "not a command",
			input:   "hello",
			wantNil: true,
		},
		{
			name:     "simple command",
			input:    "/help",
			wantName: "help",
			wantArgs: "",
		},
		{
			name:     "command with args",
			input:    "/search foo bar baz",
			wantName: "search",
			wantArgs: "foo bar baz",
		},
		{
			name:     "uppercase command",
			input:    "/HELP",
			wantName: "help",
			wantArgs: "",
		},
		{
			name:     "command with hyphen",
			input:    "/my-command arg",
			wantName: "my-command",
			wantArgs: "arg",
		},
		{
			name:     "bang prefix",
			input:    "!help",
			wantName: "help",
			wantArgs: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := parser.ParseCommand(tt.input)

			if tt.wantNil {
				if cmd != nil {
					t.Errorf("expected nil, got %+v", cmd)
				}
				return
			}

			if cmd == nil {
				t.Fatal("expected command, got nil")
			}

			if cmd.Name != tt.wantName {
				t.Errorf("Name = %s, want %s", cmd.Name, tt.wantName)
			}

			if cmd.Args != tt.wantArgs {
				t.Errorf("Args = %s, want %s", cmd.Args, tt.wantArgs)
			}
		})
	}
}

func TestRegistry_Register(t *testing.T) {
	r := NewRegistry(nil)

	cmd := &Command{
		Name:        "test",
		Aliases:     []string{"t", "tst"},
		Description: "Test command",
		Handler: func(ctx context.Context, inv *Invocation) (*Result, error) {
			return &Result{Text: "test"}, nil
		},
	}

	if err := r.Register(cmd); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Should find by name
	found, ok := r.Get("test")
	if !ok {
		t.Error("command not found by name")
	}
	if found.Name != "test" {
		t.Errorf("wrong command returned")
	}

	// Should find by alias
	found, ok = r.Get("t")
	if !ok {
		t.Error("command not found by alias 't'")
	}
	if found.Name != "test" {
		t.Error("alias returned wrong command")
	}

	// Duplicate registration should fail
	if err := r.Register(cmd); err == nil {
		t.Error("expected error for duplicate registration")
	}
}

func TestRegistry_Execute(t *testing.T) {
	r := NewRegistry(nil)

	called := false
	r.Register(&Command{
		Name:        "test",
		AcceptsArgs: true,
		Handler: func(ctx context.Context, inv *Invocation) (*Result, error) {
			called = true
			return &Result{
				Text: "executed: " + inv.Args,
			}, nil
		},
	})

	inv := &Invocation{
		Name: "test",
		Args: "foo bar",
	}

	result, err := r.Execute(context.Background(), inv)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !called {
		t.Error("handler was not called")
	}

	if result.Text != "executed: foo bar" {
		t.Errorf("unexpected result: %s", result.Text)
	}
}

func TestRegistry_AdminOnly(t *testing.T) {
	r := NewRegistry(nil)

	r.Register(&Command{
		Name:      "admin",
		AdminOnly: true,
		Handler: func(ctx context.Context, inv *Invocation) (*Result, error) {
			return &Result{Text: "admin action"}, nil
		},
	})

	// Non-admin should be rejected
	inv := &Invocation{
		Name:    "admin",
		IsAdmin: false,
	}

	result, err := r.Execute(context.Background(), inv)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Error == "" {
		t.Error("expected error for non-admin user")
	}

	// Admin should succeed
	inv.IsAdmin = true
	result, err = r.Execute(context.Background(), inv)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Error != "" {
		t.Errorf("unexpected error for admin: %s", result.Error)
	}
}

func TestNormalizeCommandText(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"/help", "help"},
		{"!help foo", "help foo"},
		{"help", "help"},
		{"  /help  ", "help"},
	}

	for _, tt := range tests {
		got := NormalizeCommandText(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeCommandText(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSplitCommandArgs(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantArgs string
	}{
		{"", "", ""},
		{"help", "help", ""},
		{"help foo", "help", "foo"},
		{"SEARCH bar baz", "search", "bar baz"},
		{"  cmd  arg  ", "cmd", "arg"},
	}

	for _, tt := range tests {
		name, args := SplitCommandArgs(tt.input)
		if name != tt.wantName || args != tt.wantArgs {
			t.Errorf("SplitCommandArgs(%q) = (%q, %q), want (%q, %q)",
				tt.input, name, args, tt.wantName, tt.wantArgs)
		}
	}
}
