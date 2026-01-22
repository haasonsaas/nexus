package security

import (
	"testing"
)

func TestAnalyzeCommand(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		wantSafe bool
		wantRisk string
	}{
		{
			name:     "simple command",
			command:  "echo hello",
			wantSafe: true,
		},
		{
			name:     "command with semicolon",
			command:  "echo hello; rm -rf /",
			wantSafe: false,
			wantRisk: "command_chain",
		},
		{
			name:     "command with &&",
			command:  "test -f foo && cat foo",
			wantSafe: false,
			wantRisk: "command_chain",
		},
		{
			name:     "command with ||",
			command:  "test -f foo || echo missing",
			wantSafe: false,
			wantRisk: "command_chain",
		},
		{
			name:     "command with pipe",
			command:  "cat file | grep pattern",
			wantSafe: false,
			wantRisk: "pipe",
		},
		{
			name:     "command with redirect out",
			command:  "echo data > file",
			wantSafe: false,
			wantRisk: "redirect",
		},
		{
			name:     "command with redirect append",
			command:  "echo data >> file",
			wantSafe: false,
			wantRisk: "redirect",
		},
		{
			name:     "command with redirect in",
			command:  "cat < file",
			wantSafe: false,
			wantRisk: "redirect",
		},
		{
			name:     "command with backtick subshell",
			command:  "echo `whoami`",
			wantSafe: false,
			wantRisk: "subshell",
		},
		{
			name:     "command with $() subshell",
			command:  "echo $(whoami)",
			wantSafe: false,
			wantRisk: "subshell",
		},
		{
			name:     "command with background",
			command:  "sleep 100 &",
			wantSafe: false,
			wantRisk: "background",
		},
		{
			name:     "empty command",
			command:  "",
			wantSafe: true,
		},
		{
			name:     "command with arguments",
			command:  "python3 main.py --verbose --input data.txt",
			wantSafe: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AnalyzeCommand(tt.command)

			if result.IsSafe != tt.wantSafe {
				t.Errorf("AnalyzeCommand(%q).IsSafe = %v, want %v", tt.command, result.IsSafe, tt.wantSafe)
			}

			if !tt.wantSafe && tt.wantRisk != "" {
				found := false
				for _, token := range result.DangerousTokens {
					if token.Risk == tt.wantRisk {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("AnalyzeCommand(%q) did not find risk %q, got tokens: %v", tt.command, tt.wantRisk, result.DangerousTokens)
				}
			}
		})
	}
}

func TestAnalyzeCommandQuoteAware(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		wantSafe bool
	}{
		{
			name:     "semicolon inside single quotes",
			command:  "echo 'hello; world'",
			wantSafe: true,
		},
		{
			name:     "semicolon inside double quotes",
			command:  `echo "hello; world"`,
			wantSafe: true,
		},
		{
			name:     "semicolon outside quotes",
			command:  "echo 'hello'; echo 'world'",
			wantSafe: false,
		},
		{
			name:     "pipe inside quotes",
			command:  "echo 'cat | grep'",
			wantSafe: true,
		},
		{
			name:     "pipe outside quotes",
			command:  "echo hello | grep h",
			wantSafe: false,
		},
		{
			name:     "redirect inside quotes",
			command:  `echo "data > file"`,
			wantSafe: true,
		},
		{
			name:     "redirect outside quotes",
			command:  `echo "data" > file`,
			wantSafe: false,
		},
		{
			name:     "subshell inside quotes",
			command:  "echo '$(whoami)'",
			wantSafe: true,
		},
		{
			name:     "subshell outside quotes",
			command:  "echo $(whoami)",
			wantSafe: false,
		},
		{
			name:     "backtick inside single quotes",
			command:  "echo '`whoami`'",
			wantSafe: true,
		},
		{
			name:     "backtick outside quotes",
			command:  "echo `whoami`",
			wantSafe: false,
		},
		{
			name:     "escaped quote",
			command:  `echo "hello\"world"`,
			wantSafe: true,
		},
		{
			name:     "mixed quotes safe",
			command:  `echo "hello 'world'" 'foo "bar"'`,
			wantSafe: true,
		},
		{
			name:     "mixed quotes with external semicolon",
			command:  `echo "hello"; echo 'world'`,
			wantSafe: false,
		},
		{
			name:     "background inside quotes",
			command:  "echo 'sleep &'",
			wantSafe: true,
		},
		{
			name:     "background outside quotes",
			command:  "sleep 10 &",
			wantSafe: false,
		},
		{
			name:     "complex safe command",
			command:  `python3 -c "print('hello; world')" --arg="value|with|pipes"`,
			wantSafe: true,
		},
		{
			name:     "empty string",
			command:  "",
			wantSafe: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AnalyzeCommandQuoteAware(tt.command)

			if result.IsSafe != tt.wantSafe {
				t.Errorf("AnalyzeCommandQuoteAware(%q).IsSafe = %v, want %v\nTokens: %v\nReason: %s",
					tt.command, result.IsSafe, tt.wantSafe, result.DangerousTokens, result.Reason)
			}
		})
	}
}

func TestIsSafeCommand(t *testing.T) {
	tests := []struct {
		command  string
		wantSafe bool
	}{
		{"echo hello", true},
		{"echo hello; rm -rf /", false},
		{"echo 'hello; world'", true},
		{"cat file | grep foo", false},
		{"echo 'cat | grep'", true},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			if got := IsSafeCommand(tt.command); got != tt.wantSafe {
				t.Errorf("IsSafeCommand(%q) = %v, want %v", tt.command, got, tt.wantSafe)
			}
		})
	}
}

func TestExtractUnsafeReason(t *testing.T) {
	tests := []struct {
		command    string
		wantReason string
	}{
		{"echo hello", ""},
		{"echo hello; rm -rf /", "command chaining allows execution of multiple commands"},
	}

	for _, tt := range tests {
		t.Run(tt.command, func(t *testing.T) {
			reason := ExtractUnsafeReason(tt.command)
			if tt.wantReason == "" && reason != "" {
				t.Errorf("ExtractUnsafeReason(%q) = %q, want empty", tt.command, reason)
			}
			if tt.wantReason != "" && reason == "" {
				t.Errorf("ExtractUnsafeReason(%q) = empty, want non-empty", tt.command)
			}
		})
	}
}

func TestContainsShellMetacharacters(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"hello", false},
		{"hello world", false},
		{"hello;world", true},
		{"hello|world", true},
		{"hello>world", true},
		{"hello<world", true},
		{"hello&world", true},
		{"hello`world", true},
		{"hello$world", true},
		{"hello(world", true},
		{"hello)world", true},
		{"hello{world", true},
		{"hello}world", true},
		{"hello[world", true},
		{"hello]world", true},
		{"hello*world", true},
		{"hello?world", true},
		{"hello!world", true},
		{"hello#world", true},
		{"hello~world", true},
		{"hello=world", true},
		{"hello%world", true},
		{"hello^world", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ContainsShellMetacharacters(tt.input); got != tt.want {
				t.Errorf("ContainsShellMetacharacters(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsValidFilename(t *testing.T) {
	tests := []struct {
		name  string
		valid bool
	}{
		{"main.py", true},
		{"test_file.txt", true},
		{"data-2024.csv", true},
		{"", false},
		{".", false},
		{"..", false},
		{".hidden", false},
		{"path/to/file", false},
		{"path\\to\\file", false},
		{"file;name", false},
		{"file|name", false},
		{"file>name", false},
		{"file<name", false},
		{"file&name", false},
		{"file`name", false},
		{"file$name", false},
		{"file(name", false},
		{"file*name", false},
		{"file?name", false},
		{"file\x00name", false}, // null byte
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValidFilename(tt.name); got != tt.valid {
				t.Errorf("IsValidFilename(%q) = %v, want %v", tt.name, got, tt.valid)
			}
		})
	}
}

func TestSanitizeCommand(t *testing.T) {
	tests := []struct {
		input string
		safe  bool // whether the sanitized output should be safe
	}{
		{"echo hello", true},
		{"echo hello; rm -rf /", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			sanitized := SanitizeCommand(tt.input)
			// After sanitization, if input was unsafe, result should be safe
			// (or at least wrapped in quotes)
			if tt.input != "" && !IsSafeCommand(tt.input) {
				if !IsSafeCommand(sanitized) && sanitized[0] != '\'' {
					t.Errorf("SanitizeCommand(%q) = %q, expected to be safe or quoted", tt.input, sanitized)
				}
			}
		})
	}
}

func BenchmarkAnalyzeCommand(b *testing.B) {
	cmd := "python3 main.py --verbose --input data.txt"
	for i := 0; i < b.N; i++ {
		AnalyzeCommand(cmd)
	}
}

func BenchmarkAnalyzeCommandQuoteAware(b *testing.B) {
	cmd := `python3 -c "print('hello; world')" --arg="value|with|pipes"`
	for i := 0; i < b.N; i++ {
		AnalyzeCommandQuoteAware(cmd)
	}
}
