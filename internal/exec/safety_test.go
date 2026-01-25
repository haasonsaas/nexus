package exec

import (
	"errors"
	"testing"
)

func TestIsLikelyPath(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		// Valid paths
		{"absolute unix path", "/usr/bin/ls", true},
		{"relative path with dot", "./script.sh", true},
		{"home directory path", "~/bin/tool", true},
		{"path with subdirectories", "/home/user/bin/app", true},
		{"Windows absolute path", "C:\\Windows\\System32\\cmd.exe", true},
		{"Windows path with forward slash", "C:/Program Files/app.exe", true},
		{"path with backslash", "dir\\subdir\\file", true},
		{"path starting with double dot", "../parent/script", true},

		// Not paths (bare names)
		{"bare name", "ls", false},
		{"bare name with extension", "node.exe", false},
		{"bare name with dash", "my-tool", false},
		{"bare name with underscore", "my_tool", false},
		{"bare name with plus", "g++", false},
		{"empty string", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsLikelyPath(tc.value)
			if result != tc.expected {
				t.Errorf("IsLikelyPath(%q) = %v, want %v", tc.value, result, tc.expected)
			}
		})
	}
}

func TestIsSafeExecutableValue(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected bool
	}{
		// Valid bare names
		{"simple command", "ls", true},
		{"git command", "git", true},
		{"node with extension", "node.exe", true},
		{"gcc compiler", "gcc", true},
		{"g++ compiler", "g++", true},
		{"python3", "python3", true},
		{"npm", "npm", true},
		{"command with dash", "my-tool", true},
		{"command with underscore", "my_tool", true},
		{"command with dot", "tool.sh", true},

		// Valid paths
		{"absolute unix path", "/usr/bin/ls", true},
		{"relative script", "./script.sh", true},
		{"home bin path", "~/bin/tool", true},
		{"Windows cmd", "C:\\Windows\\System32\\cmd.exe", true},
		{"Windows forward slash", "C:/Windows/cmd.exe", true},
		{"deep path", "/opt/app/v2/bin/run", true},
		{"path with spaces avoided", "/usr/local/bin/my-app", true},

		// Rejected: shell metacharacters
		{"semicolon injection", "ls;rm", false},
		{"pipe injection", "echo|cat", false},
		{"ampersand injection", "cmd&rm", false},
		{"backtick injection", "ls`whoami`", false},
		{"dollar injection", "ls$PATH", false},
		{"less than injection", "cmd<file", false},
		{"greater than injection", "cmd>file", false},

		// Rejected: control characters
		{"newline injection", "ls\nrm", false},
		{"carriage return injection", "cmd\rmalicious", false},
		{"mixed control chars", "first\r\nsecond", false},

		// Rejected: quote injection
		{"double quote injection", "ls\"test", false},
		{"single quote injection", "ls'test", false},
		{"quotes in middle", "test\"name", false},

		// Rejected: option injection (for bare names)
		{"dash prefix option", "-rf", false},
		{"double dash option", "--help", false},
		{"option injection", "-e malicious", false},

		// Rejected: null bytes
		{"null byte injection", "ls\x00rm", false},
		{"null byte at end", "cmd\x00", false},

		// Rejected: empty or whitespace
		{"empty string", "", false},
		{"whitespace only", "   ", false},
		{"tabs only", "\t\t", false},

		// Edge cases
		{"path starting with dash", "./-rf", true},  // This is a path, not option injection
		{"complex valid name", "x86_64-linux-gnu-gcc-11", true},
		{"just a dot", ".", true}, // Could be a valid path
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsSafeExecutableValue(tc.value)
			if result != tc.expected {
				t.Errorf("IsSafeExecutableValue(%q) = %v, want %v", tc.value, result, tc.expected)
			}
		})
	}
}

func TestSanitizeExecutableValue(t *testing.T) {
	tests := []struct {
		name        string
		value       string
		expected    string
		expectedErr error
	}{
		// Valid cases (should return trimmed value)
		{"simple command", "ls", "ls", nil},
		{"command with spaces", "  git  ", "git", nil},
		{"path with spaces around", "  /usr/bin/ls  ", "/usr/bin/ls", nil},
		{"valid path", "/usr/bin/bash", "/usr/bin/bash", nil},

		// Error cases
		{"empty string", "", "", ErrEmptyValue},
		{"whitespace only", "   ", "", ErrEmptyValue},
		{"null byte", "ls\x00rm", "", ErrNullByte},
		{"newline", "ls\nrm", "", ErrControlChar},
		{"carriage return", "cmd\rtest", "", ErrControlChar},
		{"shell metachar semicolon", "ls;rm", "", ErrShellMetachar},
		{"shell metachar pipe", "a|b", "", ErrShellMetachar},
		{"shell metachar backtick", "a`b`", "", ErrShellMetachar},
		{"shell metachar dollar", "a$b", "", ErrShellMetachar},
		{"quote double", "a\"b", "", ErrQuoteChar},
		{"quote single", "a'b", "", ErrQuoteChar},
		{"option injection", "-rf", "", ErrOptionInjection},
		{"invalid chars for bare", "foo bar", "", ErrInvalidBareNameChars},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := SanitizeExecutableValue(tc.value)
			if tc.expectedErr != nil {
				if !errors.Is(err, tc.expectedErr) {
					t.Errorf("SanitizeExecutableValue(%q) error = %v, want %v", tc.value, err, tc.expectedErr)
				}
			} else {
				if err != nil {
					t.Errorf("SanitizeExecutableValue(%q) unexpected error = %v", tc.value, err)
				}
				if result != tc.expected {
					t.Errorf("SanitizeExecutableValue(%q) = %q, want %q", tc.value, result, tc.expected)
				}
			}
		})
	}
}

func TestIsSafeArgument(t *testing.T) {
	tests := []struct {
		name     string
		arg      string
		expected bool
	}{
		// Valid arguments
		{"simple arg", "file.txt", true},
		{"path arg", "/path/to/file", true},
		{"flag with value", "--output=result.txt", true},
		{"short flag", "-v", true},
		{"double dash flag", "--verbose", true},
		{"argument with equals", "key=value", true},
		{"argument with colon", "host:port", true},
		{"argument with at", "user@host", true},
		{"argument with percent", "100%", true},
		{"URL argument", "https://example.com/path", true},
		{"quoted content", "file with spaces.txt", true}, // Note: quotes allowed in args
		{"single quoted content", "'quoted'", true},
		{"double quoted content", "\"quoted\"", true},

		// Rejected: shell metacharacters
		{"semicolon in arg", "file;rm", false},
		{"pipe in arg", "file|cat", false},
		{"ampersand in arg", "cmd&next", false},
		{"backtick in arg", "file`cmd`", false},
		{"dollar expansion", "$HOME/file", false},
		{"redirect less", "file<input", false},
		{"redirect greater", "file>output", false},

		// Rejected: control characters
		{"newline in arg", "line1\nline2", false},
		{"carriage return in arg", "line1\rline2", false},

		// Rejected: null bytes
		{"null byte in arg", "file\x00name", false},

		// Rejected: empty
		{"empty arg", "", false},

		// Edge cases
		{"whitespace arg", "   ", true}, // Whitespace is valid for arguments
		{"tab arg", "\t", true},         // Tab is valid (not a control char we check)
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsSafeArgument(tc.arg)
			if result != tc.expected {
				t.Errorf("IsSafeArgument(%q) = %v, want %v", tc.arg, result, tc.expected)
			}
		})
	}
}

func TestSanitizeArgument(t *testing.T) {
	tests := []struct {
		name        string
		arg         string
		expected    string
		expectedErr error
	}{
		// Valid cases
		{"simple arg", "file.txt", "file.txt", nil},
		{"flag arg", "--verbose", "--verbose", nil},
		{"path arg", "/path/to/file", "/path/to/file", nil},

		// Error cases
		{"empty", "", "", ErrEmptyArgument},
		{"null byte", "file\x00name", "", ErrArgumentNullByte},
		{"newline", "line1\nline2", "", ErrArgumentControlChar},
		{"shell metachar", "file;rm", "", ErrArgumentShellMetachar},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := SanitizeArgument(tc.arg)
			if tc.expectedErr != nil {
				if !errors.Is(err, tc.expectedErr) {
					t.Errorf("SanitizeArgument(%q) error = %v, want %v", tc.arg, err, tc.expectedErr)
				}
			} else {
				if err != nil {
					t.Errorf("SanitizeArgument(%q) unexpected error = %v", tc.arg, err)
				}
				if result != tc.expected {
					t.Errorf("SanitizeArgument(%q) = %q, want %q", tc.arg, result, tc.expected)
				}
			}
		})
	}
}

func TestSanitizeArguments(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expected    []string
		expectError bool
		errorIndex  int
	}{
		// Valid cases
		{"nil args", nil, nil, false, -1},
		{"empty slice", []string{}, []string{}, false, -1},
		{"single valid arg", []string{"file.txt"}, []string{"file.txt"}, false, -1},
		{"multiple valid args", []string{"-v", "--output", "file.txt"}, []string{"-v", "--output", "file.txt"}, false, -1},

		// Error cases
		{"first arg invalid", []string{"file;rm", "good"}, nil, true, 0},
		{"second arg invalid", []string{"good", "file\nname"}, nil, true, 1},
		{"third arg invalid", []string{"a", "b", "c|d"}, nil, true, 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := SanitizeArguments(tc.args)
			if tc.expectError {
				if err == nil {
					t.Errorf("SanitizeArguments(%v) expected error, got nil", tc.args)
					return
				}
				var argErr *ArgumentError
				if errors.As(err, &argErr) {
					if argErr.Index != tc.errorIndex {
						t.Errorf("SanitizeArguments(%v) error index = %d, want %d", tc.args, argErr.Index, tc.errorIndex)
					}
				} else {
					t.Errorf("SanitizeArguments(%v) error type = %T, want *ArgumentError", tc.args, err)
				}
			} else {
				if err != nil {
					t.Errorf("SanitizeArguments(%v) unexpected error = %v", tc.args, err)
					return
				}
				if len(result) != len(tc.expected) {
					t.Errorf("SanitizeArguments(%v) len = %d, want %d", tc.args, len(result), len(tc.expected))
					return
				}
				for i, v := range result {
					if v != tc.expected[i] {
						t.Errorf("SanitizeArguments(%v)[%d] = %q, want %q", tc.args, i, v, tc.expected[i])
					}
				}
			}
		})
	}
}

func TestArgumentError(t *testing.T) {
	err := &ArgumentError{
		Index: 2,
		Arg:   "bad;arg",
		Err:   ErrArgumentShellMetachar,
	}

	// Test Error() method
	errStr := err.Error()
	if errStr == "" {
		t.Error("ArgumentError.Error() should not be empty")
	}

	// Test Unwrap() method
	unwrapped := err.Unwrap()
	if !errors.Is(unwrapped, ErrArgumentShellMetachar) {
		t.Errorf("ArgumentError.Unwrap() = %v, want %v", unwrapped, ErrArgumentShellMetachar)
	}
}

func TestRegexPatterns(t *testing.T) {
	// Test that regex patterns are properly compiled
	tests := []struct {
		name    string
		pattern interface{}
	}{
		{"ShellMetachars", ShellMetachars},
		{"ControlChars", ControlChars},
		{"QuoteChars", QuoteChars},
		{"BareNamePattern", BareNamePattern},
		{"WindowsDriveLetter", WindowsDriveLetter},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.pattern == nil {
				t.Errorf("%s pattern is nil", tc.name)
			}
		})
	}
}

// Benchmark tests
func BenchmarkIsSafeExecutableValue(b *testing.B) {
	testCases := []string{
		"ls",
		"/usr/bin/ls",
		"./script.sh",
		"C:\\Windows\\cmd.exe",
		"unsafe;command",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, tc := range testCases {
			IsSafeExecutableValue(tc)
		}
	}
}

func BenchmarkIsSafeArgument(b *testing.B) {
	testCases := []string{
		"file.txt",
		"--verbose",
		"/path/to/file",
		"unsafe;arg",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, tc := range testCases {
			IsSafeArgument(tc)
		}
	}
}
