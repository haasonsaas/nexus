package main

import "testing"

func TestBuildRootCmdIncludesSubcommands(t *testing.T) {
	cmd := buildRootCmd()
	names := map[string]bool{}
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}

	required := []string{"serve", "migrate", "mcp", "rag"}
	for _, name := range required {
		if !names[name] {
			t.Fatalf("expected subcommand %q to be registered", name)
		}
	}
}
