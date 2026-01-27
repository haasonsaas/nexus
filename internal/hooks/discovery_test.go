package hooks

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseHook(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
		check   func(t *testing.T, entry *HookEntry)
	}{
		{
			name: "valid hook",
			content: `---
name: test-hook
description: A test hook
events:
  - gateway.startup
  - command.new
---
# Test Hook

This is the hook content.
`,
			wantErr: false,
			check: func(t *testing.T, entry *HookEntry) {
				if entry.Config.Name != "test-hook" {
					t.Errorf("expected name 'test-hook', got %q", entry.Config.Name)
				}
				if entry.Config.Description != "A test hook" {
					t.Errorf("expected description 'A test hook', got %q", entry.Config.Description)
				}
				if len(entry.Config.Events) != 2 {
					t.Errorf("expected 2 events, got %d", len(entry.Config.Events))
				}
				if entry.Content != "# Test Hook\n\nThis is the hook content." {
					t.Errorf("unexpected content: %q", entry.Content)
				}
			},
		},
		{
			name: "hook with requirements",
			content: `---
name: git-hook
description: A hook requiring git
events:
  - command.new
requires:
  bins:
    - git
  env:
    - GITHUB_TOKEN
  os:
    - darwin
    - linux
---
Content here.
`,
			wantErr: false,
			check: func(t *testing.T, entry *HookEntry) {
				if entry.Config.Requires == nil {
					t.Fatal("expected requires to be set")
				}
				if len(entry.Config.Requires.Bins) != 1 || entry.Config.Requires.Bins[0] != "git" {
					t.Errorf("expected bins [git], got %v", entry.Config.Requires.Bins)
				}
				if len(entry.Config.Requires.Env) != 1 || entry.Config.Requires.Env[0] != "GITHUB_TOKEN" {
					t.Errorf("expected env [GITHUB_TOKEN], got %v", entry.Config.Requires.Env)
				}
				if len(entry.Config.Requires.OS) != 2 {
					t.Errorf("expected 2 OS values, got %d", len(entry.Config.Requires.OS))
				}
			},
		},
		{
			name: "hook with priority",
			content: `---
name: priority-hook
description: Hook with custom priority
events:
  - gateway.startup
priority: 25
---
`,
			wantErr: false,
			check: func(t *testing.T, entry *HookEntry) {
				if entry.Config.Priority != PriorityHigh {
					t.Errorf("expected priority %d, got %d", PriorityHigh, entry.Config.Priority)
				}
			},
		},
		{
			name: "hook with always flag",
			content: `---
name: always-hook
description: Hook that always runs
events:
  - gateway.startup
always: true
---
`,
			wantErr: false,
			check: func(t *testing.T, entry *HookEntry) {
				if !entry.Config.Always {
					t.Error("expected always to be true")
				}
			},
		},
		{
			name:    "missing frontmatter delimiter",
			content: `name: bad-hook`,
			wantErr: true,
		},
		{
			name: "missing closing delimiter",
			content: `---
name: bad-hook
events:
  - gateway.startup
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, err := ParseHook([]byte(tt.content), "/test/path")
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseHook() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil && tt.check != nil {
				tt.check(t, entry)
			}
		})
	}
}

func TestValidateHook(t *testing.T) {
	tests := []struct {
		name    string
		entry   *HookEntry
		wantErr bool
	}{
		{
			name: "valid hook",
			entry: &HookEntry{
				Config: HookConfig{
					Name:   "valid-hook",
					Events: []string{"gateway.startup"},
				},
			},
			wantErr: false,
		},
		{
			name: "missing name",
			entry: &HookEntry{
				Config: HookConfig{
					Events: []string{"gateway.startup"},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid name with uppercase",
			entry: &HookEntry{
				Config: HookConfig{
					Name:   "Invalid-Hook",
					Events: []string{"gateway.startup"},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid name with spaces",
			entry: &HookEntry{
				Config: HookConfig{
					Name:   "invalid hook",
					Events: []string{"gateway.startup"},
				},
			},
			wantErr: true,
		},
		{
			name: "missing events",
			entry: &HookEntry{
				Config: HookConfig{
					Name:   "no-events",
					Events: []string{},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateHook(tt.entry)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateHook() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLocalSourceDiscover(t *testing.T) {
	// Create a test directory structure
	tmpDir := t.TempDir()
	hooksDir := filepath.Join(tmpDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("failed to create hooks dir: %v", err)
	}

	// Create a valid hook
	hook1Dir := filepath.Join(hooksDir, "hook1")
	if err := os.MkdirAll(hook1Dir, 0o755); err != nil {
		t.Fatalf("failed to create hook1 dir: %v", err)
	}
	hook1Content := `---
name: hook1
description: First hook
events:
  - gateway.startup
---
Hook 1 content.
`
	if err := os.WriteFile(filepath.Join(hook1Dir, "HOOK.md"), []byte(hook1Content), 0o644); err != nil {
		t.Fatalf("failed to write hook1: %v", err)
	}

	// Create another valid hook
	hook2Dir := filepath.Join(hooksDir, "hook2")
	if err := os.MkdirAll(hook2Dir, 0o755); err != nil {
		t.Fatalf("failed to create hook2 dir: %v", err)
	}
	hook2Content := `---
name: hook2
description: Second hook
events:
  - command.new
  - command.reset
---
Hook 2 content.
`
	if err := os.WriteFile(filepath.Join(hook2Dir, "HOOK.md"), []byte(hook2Content), 0o644); err != nil {
		t.Fatalf("failed to write hook2: %v", err)
	}

	// Create directory without HOOK.md
	if err := os.MkdirAll(filepath.Join(hooksDir, "not-a-hook"), 0o755); err != nil {
		t.Fatalf("failed to create not-a-hook dir: %v", err)
	}

	// Test discovery
	source := NewLocalSource(hooksDir, SourceWorkspace, PriorityWorkspace)
	hooks, err := source.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	if len(hooks) != 2 {
		t.Errorf("expected 2 hooks, got %d", len(hooks))
	}

	hookNames := make(map[string]bool)
	for _, h := range hooks {
		hookNames[h.Config.Name] = true
		if h.Source != SourceWorkspace {
			t.Errorf("expected source %s, got %s", SourceWorkspace, h.Source)
		}
		if h.SourcePriority != PriorityWorkspace {
			t.Errorf("expected priority %d, got %d", PriorityWorkspace, h.SourcePriority)
		}
	}

	if !hookNames["hook1"] || !hookNames["hook2"] {
		t.Errorf("expected hooks [hook1, hook2], got %v", hookNames)
	}
}

func TestLocalSourceDiscoverNonExistentDir(t *testing.T) {
	source := NewLocalSource("/nonexistent/path", SourceLocal, PriorityLocal)
	hooks, err := source.Discover(context.Background())
	if err != nil {
		t.Errorf("Discover() should not error on non-existent dir, got %v", err)
	}
	if hooks != nil {
		t.Errorf("expected nil hooks, got %v", hooks)
	}
}

func TestCheckEligibility(t *testing.T) {
	tests := []struct {
		name     string
		entry    *HookEntry
		ctx      *GatingContext
		eligible bool
		reason   string
	}{
		{
			name: "no requirements - eligible",
			entry: &HookEntry{
				Config: HookConfig{
					Name:   "simple",
					Events: []string{"gateway.startup"},
				},
			},
			ctx:      NewGatingContext(nil),
			eligible: true,
		},
		{
			name: "disabled in config",
			entry: &HookEntry{
				Config: HookConfig{
					Name:    "disabled",
					Events:  []string{"gateway.startup"},
					Enabled: boolPtr(false),
				},
			},
			ctx:      NewGatingContext(nil),
			eligible: false,
			reason:   "disabled in config",
		},
		{
			name: "always flag bypasses checks",
			entry: &HookEntry{
				Config: HookConfig{
					Name:   "always",
					Events: []string{"gateway.startup"},
					Always: true,
					Requires: &HookRequirements{
						Bins: []string{"nonexistent-binary-xyz"},
					},
				},
			},
			ctx:      NewGatingContext(nil),
			eligible: true,
			reason:   "always enabled",
		},
		{
			name: "wrong OS",
			entry: &HookEntry{
				Config: HookConfig{
					Name:   "os-specific",
					Events: []string{"gateway.startup"},
					Requires: &HookRequirements{
						OS: []string{"nonexistent-os"},
					},
				},
			},
			ctx:      NewGatingContext(nil),
			eligible: false,
		},
		{
			name: "missing binary",
			entry: &HookEntry{
				Config: HookConfig{
					Name:   "needs-binary",
					Events: []string{"gateway.startup"},
					Requires: &HookRequirements{
						Bins: []string{"nonexistent-binary-xyz-123"},
					},
				},
			},
			ctx:      NewGatingContext(nil),
			eligible: false,
		},
		{
			name: "missing env var",
			entry: &HookEntry{
				Config: HookConfig{
					Name:   "needs-env",
					Events: []string{"gateway.startup"},
					Requires: &HookRequirements{
						Env: []string{"NONEXISTENT_ENV_VAR_XYZ_123"},
					},
				},
			},
			ctx:      NewGatingContext(nil),
			eligible: false,
		},
		{
			name: "any bins satisfied",
			entry: &HookEntry{
				Config: HookConfig{
					Name:   "any-bins",
					Events: []string{"gateway.startup"},
					Requires: &HookRequirements{
						AnyBins: []string{"nonexistent1", "ls", "nonexistent2"},
					},
				},
			},
			ctx:      NewGatingContext(nil),
			eligible: true,
		},
		{
			name: "any bins not satisfied",
			entry: &HookEntry{
				Config: HookConfig{
					Name:   "any-bins-missing",
					Events: []string{"gateway.startup"},
					Requires: &HookRequirements{
						AnyBins: []string{"nonexistent1", "nonexistent2"},
					},
				},
			},
			ctx:      NewGatingContext(nil),
			eligible: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.entry.CheckEligibility(tt.ctx)
			if result.Eligible != tt.eligible {
				t.Errorf("CheckEligibility() eligible = %v, want %v (reason: %s)",
					result.Eligible, tt.eligible, result.Reason)
			}
			if tt.reason != "" && result.Reason != tt.reason {
				t.Errorf("CheckEligibility() reason = %q, want %q", result.Reason, tt.reason)
			}
		})
	}
}

func TestGatingContextCheckEnv(t *testing.T) {
	ctx := NewGatingContext(nil)

	// Set a test env var
	t.Setenv("TEST_HOOKS_VAR", "value")

	if !ctx.CheckEnv("TEST_HOOKS_VAR") {
		t.Error("expected TEST_HOOKS_VAR to be found")
	}

	if ctx.CheckEnv("NONEXISTENT_VAR_XYZ") {
		t.Error("expected NONEXISTENT_VAR_XYZ to not be found")
	}

	// Check caching
	if !ctx.EnvVars["TEST_HOOKS_VAR"] {
		t.Error("expected env var to be cached")
	}
}

func TestGatingContextCheckConfig(t *testing.T) {
	configValues := map[string]any{
		"tools": map[string]any{
			"browser": map[string]any{
				"enabled": true,
			},
			"sandbox": map[string]any{
				"enabled": false,
			},
		},
	}

	ctx := NewGatingContext(configValues)

	if !ctx.CheckConfig("tools.browser.enabled") {
		t.Error("expected tools.browser.enabled to be truthy")
	}

	if ctx.CheckConfig("tools.sandbox.enabled") {
		t.Error("expected tools.sandbox.enabled to be falsy")
	}

	if ctx.CheckConfig("nonexistent.path") {
		t.Error("expected nonexistent.path to be falsy")
	}
}

func TestDiscoverAll(t *testing.T) {
	tmpDir := t.TempDir()

	// Create workspace hooks dir
	wsHooksDir := filepath.Join(tmpDir, "workspace", "hooks")
	if err := os.MkdirAll(filepath.Join(wsHooksDir, "shared-hook"), 0o755); err != nil {
		t.Fatalf("failed to create ws hook dir: %v", err)
	}
	wsHookContent := `---
name: shared-hook
description: Workspace version
events:
  - gateway.startup
---
Workspace content.
`
	if err := os.WriteFile(filepath.Join(wsHooksDir, "shared-hook", "HOOK.md"), []byte(wsHookContent), 0o644); err != nil {
		t.Fatalf("failed to write ws hook: %v", err)
	}

	// Create local hooks dir with same hook name
	localHooksDir := filepath.Join(tmpDir, "local", "hooks")
	if err := os.MkdirAll(filepath.Join(localHooksDir, "shared-hook"), 0o755); err != nil {
		t.Fatalf("failed to create local hook dir: %v", err)
	}
	localHookContent := `---
name: shared-hook
description: Local version
events:
  - gateway.startup
---
Local content.
`
	if err := os.WriteFile(filepath.Join(localHooksDir, "shared-hook", "HOOK.md"), []byte(localHookContent), 0o644); err != nil {
		t.Fatalf("failed to write local hook: %v", err)
	}

	// Discover with priority (workspace > local)
	sources := []DiscoverySource{
		NewLocalSource(localHooksDir, SourceLocal, PriorityLocal),
		NewLocalSource(wsHooksDir, SourceWorkspace, PriorityWorkspace),
	}

	hooks, err := DiscoverAll(context.Background(), sources)
	if err != nil {
		t.Fatalf("DiscoverAll() error = %v", err)
	}

	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook after dedup, got %d", len(hooks))
	}

	// Workspace should win due to higher priority
	if hooks[0].Source != SourceWorkspace {
		t.Errorf("expected workspace source to win, got %s", hooks[0].Source)
	}
	if hooks[0].Config.Description != "Workspace version" {
		t.Errorf("expected workspace description, got %q", hooks[0].Config.Description)
	}
}

func TestFilterEligible(t *testing.T) {
	hooks := []*HookEntry{
		{
			Config: HookConfig{
				Name:   "eligible",
				Events: []string{"gateway.startup"},
			},
		},
		{
			Config: HookConfig{
				Name:    "disabled",
				Events:  []string{"gateway.startup"},
				Enabled: boolPtr(false),
			},
		},
		{
			Config: HookConfig{
				Name:   "missing-binary",
				Events: []string{"gateway.startup"},
				Requires: &HookRequirements{
					Bins: []string{"nonexistent-binary-xyz"},
				},
			},
		},
	}

	ctx := NewGatingContext(nil)
	eligible := FilterEligible(hooks, ctx)

	if len(eligible) != 1 {
		t.Errorf("expected 1 eligible hook, got %d", len(eligible))
	}

	if eligible[0].Config.Name != "eligible" {
		t.Errorf("expected 'eligible' hook, got %q", eligible[0].Config.Name)
	}
}

func TestBuildDefaultSources(t *testing.T) {
	sources := BuildDefaultSources("/workspace", "/home/.nexus/hooks", []string{"/extra1", "/extra2"})

	// BuildDefaultSources no longer includes bundled (those are added via EmbeddedSource)
	if len(sources) != 4 {
		t.Fatalf("expected 4 sources, got %d", len(sources))
	}

	// Verify order and types
	expectedTypes := []SourceType{SourceExtra, SourceExtra, SourceLocal, SourceWorkspace}
	for i, source := range sources {
		if source.Type() != expectedTypes[i] {
			t.Errorf("source %d: expected type %s, got %s", i, expectedTypes[i], source.Type())
		}
	}

	// Verify priorities are ascending
	for i := 1; i < len(sources); i++ {
		if sources[i].Priority() < sources[i-1].Priority() {
			t.Errorf("sources not in priority order at index %d", i)
		}
	}
}

func TestDefaultLocalPath(t *testing.T) {
	path := DefaultLocalPath()
	if path == "" {
		t.Error("expected non-empty path")
	}
	if !filepath.IsAbs(path) {
		t.Errorf("expected absolute path, got %q", path)
	}
}

func boolPtr(b bool) *bool {
	return &b
}
