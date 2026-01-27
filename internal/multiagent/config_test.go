package multiagent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseConfigYAML(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		wantErr  bool
		validate func(t *testing.T, config *MultiAgentConfig)
	}{
		{
			name: "minimal config",
			yaml: `
default_agent_id: "agent-1"
agents:
  - id: "agent-1"
    name: "Agent 1"
`,
			wantErr: false,
			validate: func(t *testing.T, config *MultiAgentConfig) {
				if config.DefaultAgentID != "agent-1" {
					t.Errorf("expected DefaultAgentID 'agent-1', got %s", config.DefaultAgentID)
				}
				if len(config.Agents) != 1 {
					t.Errorf("expected 1 agent, got %d", len(config.Agents))
				}
			},
		},
		{
			name: "full config",
			yaml: `
default_agent_id: "coordinator"
supervisor_agent_id: "coordinator"
enable_peer_handoffs: true
max_handoff_depth: 5
handoff_timeout: 10m
default_context_mode: "summary"
agents:
  - id: "coordinator"
    name: "Coordinator"
    description: "Main coordinator"
    system_prompt: "You are a coordinator"
    model: "claude-3-opus"
    tools:
      - handoff
      - list_agents
    can_receive_handoffs: true
  - id: "code-expert"
    name: "Code Expert"
    description: "Handles code"
    tools:
      - exec
      - write
    can_receive_handoffs: true
    max_iterations: 10
global_handoff_rules:
  - target_agent_id: "coordinator"
    priority: 100
    triggers:
      - type: "error"
`,
			wantErr: false,
			validate: func(t *testing.T, config *MultiAgentConfig) {
				if config.SupervisorAgentID != "coordinator" {
					t.Error("expected SupervisorAgentID to be set")
				}
				if config.MaxHandoffDepth != 5 {
					t.Errorf("expected MaxHandoffDepth=5, got %d", config.MaxHandoffDepth)
				}
				if config.DefaultContextMode != ContextSummary {
					t.Errorf("expected context mode 'summary', got %s", config.DefaultContextMode)
				}
				if len(config.Agents) != 2 {
					t.Errorf("expected 2 agents, got %d", len(config.Agents))
				}
				if len(config.GlobalHandoffRules) != 1 {
					t.Errorf("expected 1 global rule, got %d", len(config.GlobalHandoffRules))
				}
			},
		},
		{
			name: "applies defaults",
			yaml: `
agents:
  - id: "agent-1"
`,
			wantErr: false,
			validate: func(t *testing.T, config *MultiAgentConfig) {
				if config.MaxHandoffDepth != 10 {
					t.Errorf("expected default MaxHandoffDepth=10, got %d", config.MaxHandoffDepth)
				}
				if config.HandoffTimeout != 5*time.Minute {
					t.Errorf("expected default HandoffTimeout=5m, got %v", config.HandoffTimeout)
				}
				if config.DefaultContextMode != ContextFull {
					t.Errorf("expected default context mode 'full', got %s", config.DefaultContextMode)
				}
				// Agent name should default to ID
				if config.Agents[0].Name != "agent-1" {
					t.Errorf("expected agent name to default to ID, got %s", config.Agents[0].Name)
				}
			},
		},
		{
			name: "agent without ID errors",
			yaml: `
agents:
  - name: "No ID Agent"
`,
			wantErr: true,
		},
		{
			name:    "invalid YAML",
			yaml:    `not: valid: yaml: [`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := ParseConfigYAML([]byte(tt.yaml))

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.validate != nil {
				tt.validate(t, config)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	// Create a test directory
	tmpDir := t.TempDir()

	t.Run("load valid config", func(t *testing.T) {
		configPath := filepath.Join(tmpDir, "config.yaml")
		content := `
default_agent_id: "test-agent"
agents:
  - id: "test-agent"
    name: "Test Agent"
`
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write config: %v", err)
		}

		config, err := LoadConfig(configPath)
		if err != nil {
			t.Fatalf("failed to load config: %v", err)
		}

		if config.DefaultAgentID != "test-agent" {
			t.Errorf("expected DefaultAgentID 'test-agent', got %s", config.DefaultAgentID)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := LoadConfig(filepath.Join(tmpDir, "nonexistent.yaml"))
		if err == nil {
			t.Error("expected error for nonexistent file")
		}
	})
}

func TestSaveConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "output.yaml")

	config := &MultiAgentConfig{
		DefaultAgentID: "test-agent",
		Agents: []AgentDefinition{
			{
				ID:   "test-agent",
				Name: "Test Agent",
			},
		},
	}

	if err := SaveConfig(config, configPath); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	// Verify file was created and can be loaded
	loaded, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load saved config: %v", err)
	}

	if loaded.DefaultAgentID != "test-agent" {
		t.Errorf("expected DefaultAgentID 'test-agent', got %s", loaded.DefaultAgentID)
	}
}

func TestParseAgentsMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		markdown string
		validate func(t *testing.T, manifest *AgentManifest)
	}{
		{
			name: "single agent",
			markdown: `# Agent: test-agent
Name: Test Agent
Description: A test agent
Model: claude-3-opus
can_receive_handoffs: true

## System Prompt
You are a helpful test agent.

## Tools
- exec
- read
`,
			validate: func(t *testing.T, manifest *AgentManifest) {
				if len(manifest.Agents) != 1 {
					t.Fatalf("expected 1 agent, got %d", len(manifest.Agents))
				}
				agent := manifest.Agents[0]
				if agent.ID != "test-agent" {
					t.Errorf("expected ID 'test-agent', got %s", agent.ID)
				}
				if agent.Name != "Test Agent" {
					t.Errorf("expected Name 'Test Agent', got %s", agent.Name)
				}
				if agent.Model != "claude-3-opus" {
					t.Errorf("expected Model 'claude-3-opus', got %s", agent.Model)
				}
				if len(agent.Tools) != 2 {
					t.Errorf("expected 2 tools, got %d", len(agent.Tools))
				}
				if !containsSubstring(agent.SystemPrompt, "helpful test agent") {
					t.Error("expected system prompt to be set")
				}
			},
		},
		{
			name: "multiple agents",
			markdown: `# Agent: agent-1
Name: Agent One
Description: First agent

---

# Agent: agent-2
Name: Agent Two
Description: Second agent
`,
			validate: func(t *testing.T, manifest *AgentManifest) {
				if len(manifest.Agents) != 2 {
					t.Fatalf("expected 2 agents, got %d", len(manifest.Agents))
				}
			},
		},
		{
			name: "agent with handoffs",
			markdown: `# Agent: router
Name: Router
Description: Routes requests

## Handoffs
- To: code-agent, Triggers: keyword:code, Context: summary, Priority: 10
- To: research-agent, Triggers: pattern:research.*, Return: true
`,
			validate: func(t *testing.T, manifest *AgentManifest) {
				if len(manifest.Agents) != 1 {
					t.Fatalf("expected 1 agent, got %d", len(manifest.Agents))
				}
				agent := manifest.Agents[0]
				if len(agent.HandoffRules) != 2 {
					t.Errorf("expected 2 handoff rules, got %d", len(agent.HandoffRules))
				}
				if agent.HandoffRules[0].TargetAgentID != "code-agent" {
					t.Errorf("expected target 'code-agent', got %s", agent.HandoffRules[0].TargetAgentID)
				}
				if agent.HandoffRules[0].Priority != 10 {
					t.Errorf("expected priority 10, got %d", agent.HandoffRules[0].Priority)
				}
				if !agent.HandoffRules[1].ReturnToSender {
					t.Error("expected ReturnToSender to be true")
				}
			},
		},
		{
			name: "agent with max_iterations",
			markdown: `# Agent: iterative
max_iterations: 20
`,
			validate: func(t *testing.T, manifest *AgentManifest) {
				if manifest.Agents[0].MaxIterations != 20 {
					t.Errorf("expected MaxIterations=20, got %d", manifest.Agents[0].MaxIterations)
				}
			},
		},
		{
			name: "can_receive_handoffs variations",
			markdown: `# Agent: yes-agent
can_receive_handoffs: yes

---

# Agent: true-agent
can_receive_handoffs: true

---

# Agent: false-agent
can_receive_handoffs: false
`,
			validate: func(t *testing.T, manifest *AgentManifest) {
				if !manifest.Agents[0].CanReceiveHandoffs {
					t.Error("expected 'yes' to enable handoffs")
				}
				if !manifest.Agents[1].CanReceiveHandoffs {
					t.Error("expected 'true' to enable handoffs")
				}
				if manifest.Agents[2].CanReceiveHandoffs {
					t.Error("expected 'false' to disable handoffs")
				}
			},
		},
		{
			name:     "empty markdown",
			markdown: "",
			validate: func(t *testing.T, manifest *AgentManifest) {
				if len(manifest.Agents) != 0 {
					t.Errorf("expected 0 agents, got %d", len(manifest.Agents))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest, err := ParseAgentsMarkdown(tt.markdown, "test.md")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if manifest.Source != "test.md" {
				t.Errorf("expected source 'test.md', got %s", manifest.Source)
			}

			if tt.validate != nil {
				tt.validate(t, manifest)
			}
		})
	}
}

func TestParseTriggers(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		want    int
		wantErr bool
	}{
		{
			name: "single keyword",
			spec: "keyword:help",
			want: 1,
		},
		{
			name: "multiple triggers comma separated",
			spec: "keyword:code,pattern:test.*",
			want: 2,
		},
		{
			name: "multiple triggers space separated",
			spec: "keyword:one pattern:two intent:three",
			want: 3,
		},
		{
			name: "trigger types",
			spec: "kw:test regex:pattern intent:classify tool:exec explicit:agent fallback: always: complete: error:",
			want: 9,
		},
		{
			name: "no type defaults to keyword",
			spec: "justakeyword",
			want: 1,
		},
		{
			name: "empty spec",
			spec: "",
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			triggers := parseTriggers(tt.spec)
			if len(triggers) != tt.want {
				t.Errorf("expected %d triggers, got %d", tt.want, len(triggers))
			}
		})
	}
}

func TestParseTriggers_Types(t *testing.T) {
	triggers := parseTriggers("keyword:k pattern:p intent:i tool:t explicit:e fallback: always: task_complete: error:")

	expectedTypes := []TriggerType{
		TriggerKeyword,
		TriggerPattern,
		TriggerIntent,
		TriggerToolUse,
		TriggerExplicit,
		TriggerFallback,
		TriggerAlways,
		TriggerTaskComplete,
		TriggerError,
	}

	for i, expected := range expectedTypes {
		if triggers[i].Type != expected {
			t.Errorf("trigger %d: expected type %s, got %s", i, expected, triggers[i].Type)
		}
	}
}

func TestLoadAgentsManifest(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("load valid manifest", func(t *testing.T) {
		manifestPath := filepath.Join(tmpDir, "AGENTS.md")
		content := `# Agent: test-agent
Name: Test Agent
Description: Test description
`
		if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write manifest: %v", err)
		}

		manifest, err := LoadAgentsManifest(manifestPath)
		if err != nil {
			t.Fatalf("failed to load manifest: %v", err)
		}

		if len(manifest.Agents) != 1 {
			t.Errorf("expected 1 agent, got %d", len(manifest.Agents))
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := LoadAgentsManifest(filepath.Join(tmpDir, "nonexistent.md"))
		if err == nil {
			t.Error("expected error for nonexistent file")
		}
	})
}

func TestDiscoverAgentsFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	agentsFiles := []string{
		"AGENTS.md",
		"custom.agents.md",
		"subdir/AGENTS.md",
	}

	nonAgentsFiles := []string{
		"README.md",
		"other.md",
	}

	for _, f := range agentsFiles {
		path := filepath.Join(tmpDir, f)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("failed to create dir: %v", err)
		}
		if err := os.WriteFile(path, []byte("# Agent: test\n"), 0644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}
	}

	for _, f := range nonAgentsFiles {
		path := filepath.Join(tmpDir, f)
		if err := os.WriteFile(path, []byte("# Not an agents file\n"), 0644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}
	}

	discovered, err := DiscoverAgentsFiles(tmpDir)
	if err != nil {
		t.Fatalf("failed to discover files: %v", err)
	}

	if len(discovered) != len(agentsFiles) {
		t.Errorf("expected %d files, got %d", len(agentsFiles), len(discovered))
	}
}

func TestLoadAllAgentsFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create two manifest files
	file1 := filepath.Join(tmpDir, "agents1.md")
	file2 := filepath.Join(tmpDir, "agents2.md")

	if err := os.WriteFile(file1, []byte("# Agent: agent-1\nName: Agent 1\n"), 0644); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}
	if err := os.WriteFile(file2, []byte("# Agent: agent-2\nName: Agent 2\n"), 0644); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	manifest, err := LoadAllAgentsFiles([]string{file1, file2})
	if err != nil {
		t.Fatalf("failed to load files: %v", err)
	}

	if len(manifest.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(manifest.Agents))
	}
}

func TestLoadAllAgentsFiles_Error(t *testing.T) {
	_, err := LoadAllAgentsFiles([]string{"/nonexistent/file.md"})
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestConfigFromManifest(t *testing.T) {
	manifest := &AgentManifest{
		Agents: []AgentDefinition{
			{ID: "first-agent", Name: "First"},
			{ID: "second-agent", Name: "Second"},
		},
	}

	config := ConfigFromManifest(manifest)

	if config == nil {
		t.Fatal("expected config to be created")
	}

	if config.DefaultAgentID != "first-agent" {
		t.Errorf("expected first agent as default, got %s", config.DefaultAgentID)
	}

	if len(config.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(config.Agents))
	}

	if config.MaxHandoffDepth != 10 {
		t.Errorf("expected MaxHandoffDepth=10, got %d", config.MaxHandoffDepth)
	}

	if !config.EnablePeerHandoffs {
		t.Error("expected EnablePeerHandoffs to be true")
	}
}

func TestConfigFromManifest_Empty(t *testing.T) {
	manifest := &AgentManifest{}
	config := ConfigFromManifest(manifest)

	if config.DefaultAgentID != "" {
		t.Error("expected empty default agent ID for empty manifest")
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name       string
		config     *MultiAgentConfig
		wantErrors int
	}{
		{
			name:       "nil config",
			config:     nil,
			wantErrors: 1,
		},
		{
			name: "valid config",
			config: &MultiAgentConfig{
				DefaultAgentID: "agent-1",
				Agents: []AgentDefinition{
					{ID: "agent-1", Name: "Agent 1"},
				},
			},
			wantErrors: 0,
		},
		{
			name: "empty agent ID",
			config: &MultiAgentConfig{
				Agents: []AgentDefinition{
					{ID: "", Name: "No ID"},
				},
			},
			wantErrors: 1,
		},
		{
			name: "duplicate agent IDs",
			config: &MultiAgentConfig{
				Agents: []AgentDefinition{
					{ID: "agent-1", Name: "Agent 1"},
					{ID: "agent-1", Name: "Duplicate"},
				},
			},
			wantErrors: 1,
		},
		{
			name: "default agent not found",
			config: &MultiAgentConfig{
				DefaultAgentID: "nonexistent",
				Agents: []AgentDefinition{
					{ID: "agent-1", Name: "Agent 1"},
				},
			},
			wantErrors: 1,
		},
		{
			name: "supervisor agent not found",
			config: &MultiAgentConfig{
				SupervisorAgentID: "nonexistent",
				Agents: []AgentDefinition{
					{ID: "agent-1", Name: "Agent 1"},
				},
			},
			wantErrors: 1,
		},
		{
			name: "handoff target not found",
			config: &MultiAgentConfig{
				Agents: []AgentDefinition{
					{
						ID:   "agent-1",
						Name: "Agent 1",
						HandoffRules: []HandoffRule{
							{TargetAgentID: "nonexistent"},
						},
					},
				},
			},
			wantErrors: 1,
		},
		{
			name: "global rule target not found",
			config: &MultiAgentConfig{
				Agents: []AgentDefinition{
					{ID: "agent-1", Name: "Agent 1"},
				},
				GlobalHandoffRules: []HandoffRule{
					{TargetAgentID: "nonexistent"},
				},
			},
			wantErrors: 1,
		},
		{
			name: "multiple errors",
			config: &MultiAgentConfig{
				DefaultAgentID:    "nonexistent1",
				SupervisorAgentID: "nonexistent2",
				Agents: []AgentDefinition{
					{ID: "", Name: "No ID"},
					{ID: "agent-1", Name: "Agent 1"},
					{ID: "agent-1", Name: "Duplicate"},
				},
			},
			wantErrors: 4, // empty ID, duplicate, default not found, supervisor not found
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := ValidateConfig(tt.config)
			if len(errors) != tt.wantErrors {
				t.Errorf("expected %d errors, got %d: %v", tt.wantErrors, len(errors), errors)
			}
		})
	}
}

func TestValidateManifest(t *testing.T) {
	tests := []struct {
		name       string
		manifest   *AgentManifest
		wantErrors int
	}{
		{
			name:       "nil manifest",
			manifest:   nil,
			wantErrors: 1,
		},
		{
			name: "valid manifest",
			manifest: &AgentManifest{
				Agents: []AgentDefinition{
					{ID: "agent-1", Name: "Agent 1"},
					{ID: "agent-2", Name: "Agent 2"},
				},
			},
			wantErrors: 0,
		},
		{
			name: "empty agent ID",
			manifest: &AgentManifest{
				Agents: []AgentDefinition{
					{ID: "", Name: "No ID"},
				},
			},
			wantErrors: 1,
		},
		{
			name: "duplicate agent IDs",
			manifest: &AgentManifest{
				Agents: []AgentDefinition{
					{ID: "agent-1", Name: "Agent 1"},
					{ID: "agent-1", Name: "Duplicate"},
				},
			},
			wantErrors: 1,
		},
		{
			name: "duplicate agent directories",
			manifest: &AgentManifest{
				Agents: []AgentDefinition{
					{ID: "agent-1", Name: "Agent 1", AgentDir: "/shared/dir"},
					{ID: "agent-2", Name: "Agent 2", AgentDir: "/shared/dir"},
				},
			},
			wantErrors: 1,
		},
		{
			name: "duplicate agent directories with different paths to same location",
			manifest: &AgentManifest{
				Agents: []AgentDefinition{
					{ID: "agent-1", Name: "Agent 1", AgentDir: "/shared/dir"},
					{ID: "agent-2", Name: "Agent 2", AgentDir: "/shared/./dir"},
				},
			},
			wantErrors: 1,
		},
		{
			name: "empty agent_dir is allowed (uses default)",
			manifest: &AgentManifest{
				Agents: []AgentDefinition{
					{ID: "agent-1", Name: "Agent 1", AgentDir: ""},
					{ID: "agent-2", Name: "Agent 2", AgentDir: ""},
				},
			},
			wantErrors: 0,
		},
		{
			name: "unique agent directories",
			manifest: &AgentManifest{
				Agents: []AgentDefinition{
					{ID: "agent-1", Name: "Agent 1", AgentDir: "/dir1"},
					{ID: "agent-2", Name: "Agent 2", AgentDir: "/dir2"},
				},
			},
			wantErrors: 0,
		},
		{
			name: "handoff target not found",
			manifest: &AgentManifest{
				Agents: []AgentDefinition{
					{
						ID:   "agent-1",
						Name: "Agent 1",
						HandoffRules: []HandoffRule{
							{TargetAgentID: "nonexistent"},
						},
					},
				},
			},
			wantErrors: 1,
		},
		{
			name: "multiple errors",
			manifest: &AgentManifest{
				Agents: []AgentDefinition{
					{ID: "", Name: "No ID"},
					{ID: "agent-1", Name: "Agent 1", AgentDir: "/shared"},
					{ID: "agent-1", Name: "Duplicate", AgentDir: "/shared"},
				},
			},
			wantErrors: 3, // empty ID, duplicate ID, duplicate dir
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errors := ValidateManifest(tt.manifest)
			if len(errors) != tt.wantErrors {
				t.Errorf("expected %d errors, got %d: %v", tt.wantErrors, len(errors), errors)
			}
		})
	}
}

func TestLoadAgentsManifest_ValidationErrors(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("duplicate agent IDs fails", func(t *testing.T) {
		manifestPath := filepath.Join(tmpDir, "dup_ids.md")
		content := `# Agent: test-agent
Name: Test Agent

---

# Agent: test-agent
Name: Duplicate Agent
`
		if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write manifest: %v", err)
		}

		_, err := LoadAgentsManifest(manifestPath)
		if err == nil {
			t.Error("expected error for duplicate agent IDs")
		}
		if !containsSubstring(err.Error(), "duplicate agent ID") {
			t.Errorf("expected 'duplicate agent ID' error, got: %v", err)
		}
	})

	t.Run("duplicate agent directories fails", func(t *testing.T) {
		manifestPath := filepath.Join(tmpDir, "dup_dirs.md")
		content := `# Agent: agent-1
Name: Agent 1
AgentDir: /shared/state

---

# Agent: agent-2
Name: Agent 2
AgentDir: /shared/state
`
		if err := os.WriteFile(manifestPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write manifest: %v", err)
		}

		_, err := LoadAgentsManifest(manifestPath)
		if err == nil {
			t.Error("expected error for duplicate agent directories")
		}
		if !containsSubstring(err.Error(), "duplicate agent_dir") {
			t.Errorf("expected 'duplicate agent_dir' error, got: %v", err)
		}
	})
}

func TestExampleAgentsMD(t *testing.T) {
	example := ExampleAgentsMD()

	if example == "" {
		t.Error("expected non-empty example")
	}

	// Verify it can be parsed
	manifest, err := ParseAgentsMarkdown(example, "example.md")
	if err != nil {
		t.Fatalf("failed to parse example: %v", err)
	}

	if len(manifest.Agents) < 2 {
		t.Error("expected multiple agents in example")
	}

	// Check for expected content
	expectedContent := []string{
		"coordinator",
		"code-expert",
		"research-expert",
		"handoff",
		"Triggers",
	}

	for _, content := range expectedContent {
		if !containsSubstring(example, content) {
			t.Errorf("expected example to contain %q", content)
		}
	}
}

func TestParseHandoffLine(t *testing.T) {
	// parseHandoffLine is tested through ParseAgentsMarkdown since it's an internal function
	// Test parsing through the public API
	markdown := `# Agent: router
Name: Router

## Handoffs
- To: target-agent, Triggers: keyword:help, Context: summary, Priority: 10, Return: true
`
	manifest, err := ParseAgentsMarkdown(markdown, "test.md")
	if err != nil {
		t.Fatalf("failed to parse: %v", err)
	}

	if len(manifest.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(manifest.Agents))
	}

	if len(manifest.Agents[0].HandoffRules) != 1 {
		t.Fatalf("expected 1 handoff rule, got %d", len(manifest.Agents[0].HandoffRules))
	}

	rule := manifest.Agents[0].HandoffRules[0]

	if rule.TargetAgentID != "target-agent" {
		t.Errorf("expected target 'target-agent', got %s", rule.TargetAgentID)
	}

	if len(rule.Triggers) != 1 {
		t.Errorf("expected 1 trigger, got %d", len(rule.Triggers))
	}

	if rule.ContextMode != ContextSummary {
		t.Errorf("expected context mode 'summary', got %s", rule.ContextMode)
	}

	if rule.Priority != 10 {
		t.Errorf("expected priority 10, got %d", rule.Priority)
	}

	if !rule.ReturnToSender {
		t.Error("expected ReturnToSender to be true")
	}
}

func TestValidateConfigDetectsAgentDirCollisions(t *testing.T) {
	cfg := &MultiAgentConfig{
		Agents: []AgentDefinition{
			{ID: "agent-a", AgentDir: "./state"},
			{ID: "agent-b", AgentDir: "state"},
		},
	}

	errs := ValidateConfig(cfg)
	if len(errs) == 0 {
		t.Fatal("expected validation errors")
	}

	found := false
	for _, err := range errs {
		if strings.Contains(err.Error(), "duplicate agent_dir") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected duplicate agent_dir error, got: %v", errs)
	}
}

func TestValidateConfigAllowsEmptyAgentDir(t *testing.T) {
	cfg := &MultiAgentConfig{
		Agents: []AgentDefinition{
			{ID: "agent-a"},
			{ID: "agent-b"},
		},
	}

	errs := ValidateConfig(cfg)
	if len(errs) != 0 {
		t.Fatalf("expected no validation errors, got: %v", errs)
	}
}
