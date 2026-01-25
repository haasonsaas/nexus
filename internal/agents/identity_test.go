package agents

import (
	"testing"
)

func TestDefaultAckReaction(t *testing.T) {
	// Verify the constant is the eyes emoji
	if DefaultAckReaction != "\U0001F440" {
		t.Errorf("DefaultAckReaction = %q, want eyes emoji", DefaultAckReaction)
	}
}

func TestResolveAgentIdentity(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		agentID string
		want    *IdentityConfig
	}{
		{
			name:    "nil config",
			cfg:     nil,
			agentID: "test-agent",
			want:    nil,
		},
		{
			name:    "nil agents config",
			cfg:     &Config{},
			agentID: "test-agent",
			want:    nil,
		},
		{
			name: "agent not found",
			cfg: &Config{
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{},
				},
			},
			agentID: "test-agent",
			want:    nil,
		},
		{
			name: "agent found with nil identity",
			cfg: &Config{
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{
						"test-agent": {},
					},
				},
			},
			agentID: "test-agent",
			want:    nil,
		},
		{
			name: "agent found with identity",
			cfg: &Config{
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{
						"test-agent": {
							Identity: &IdentityConfig{
								Name:        "Test Agent",
								Emoji:       "🤖",
								Description: "A test agent",
							},
						},
					},
				},
			},
			agentID: "test-agent",
			want: &IdentityConfig{
				Name:        "Test Agent",
				Emoji:       "🤖",
				Description: "A test agent",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveAgentIdentity(tt.cfg, tt.agentID)

			if tt.want == nil {
				if got != nil {
					t.Errorf("ResolveAgentIdentity() = %v, want nil", got)
				}
				return
			}

			if got == nil {
				t.Errorf("ResolveAgentIdentity() = nil, want %v", tt.want)
				return
			}

			if got.Name != tt.want.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.want.Name)
			}
			if got.Emoji != tt.want.Emoji {
				t.Errorf("Emoji = %q, want %q", got.Emoji, tt.want.Emoji)
			}
			if got.Description != tt.want.Description {
				t.Errorf("Description = %q, want %q", got.Description, tt.want.Description)
			}
		})
	}
}

func TestResolveAckReaction(t *testing.T) {
	customReaction := "👍"
	emptyReaction := ""

	tests := []struct {
		name    string
		cfg     *Config
		agentID string
		want    string
	}{
		{
			name:    "nil config returns default",
			cfg:     nil,
			agentID: "test-agent",
			want:    DefaultAckReaction,
		},
		{
			name:    "empty config returns default",
			cfg:     &Config{},
			agentID: "test-agent",
			want:    DefaultAckReaction,
		},
		{
			name: "configured ack_reaction takes priority",
			cfg: &Config{
				Messages: &MessagesConfig{
					AckReaction: &customReaction,
				},
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{
						"test-agent": {
							Identity: &IdentityConfig{
								Emoji: "🤖",
							},
						},
					},
				},
			},
			agentID: "test-agent",
			want:    "👍",
		},
		{
			name: "empty configured ack_reaction returns empty",
			cfg: &Config{
				Messages: &MessagesConfig{
					AckReaction: &emptyReaction,
				},
			},
			agentID: "test-agent",
			want:    "",
		},
		{
			name: "identity emoji used when no configured reaction",
			cfg: &Config{
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{
						"test-agent": {
							Identity: &IdentityConfig{
								Emoji: "🤖",
							},
						},
					},
				},
			},
			agentID: "test-agent",
			want:    "🤖",
		},
		{
			name: "identity emoji with whitespace is trimmed",
			cfg: &Config{
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{
						"test-agent": {
							Identity: &IdentityConfig{
								Emoji: "  🤖  ",
							},
						},
					},
				},
			},
			agentID: "test-agent",
			want:    "🤖",
		},
		{
			name: "empty identity emoji returns default",
			cfg: &Config{
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{
						"test-agent": {
							Identity: &IdentityConfig{
								Emoji: "",
							},
						},
					},
				},
			},
			agentID: "test-agent",
			want:    DefaultAckReaction,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveAckReaction(tt.cfg, tt.agentID)
			if got != tt.want {
				t.Errorf("ResolveAckReaction() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveIdentityNamePrefix(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		agentID string
		want    string
	}{
		{
			name:    "nil config returns empty",
			cfg:     nil,
			agentID: "test-agent",
			want:    "",
		},
		{
			name: "no identity returns empty",
			cfg: &Config{
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{
						"test-agent": {},
					},
				},
			},
			agentID: "test-agent",
			want:    "",
		},
		{
			name: "empty name returns empty",
			cfg: &Config{
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{
						"test-agent": {
							Identity: &IdentityConfig{
								Name: "",
							},
						},
					},
				},
			},
			agentID: "test-agent",
			want:    "",
		},
		{
			name: "whitespace name returns empty",
			cfg: &Config{
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{
						"test-agent": {
							Identity: &IdentityConfig{
								Name: "   ",
							},
						},
					},
				},
			},
			agentID: "test-agent",
			want:    "",
		},
		{
			name: "valid name returns bracketed format",
			cfg: &Config{
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{
						"test-agent": {
							Identity: &IdentityConfig{
								Name: "MyBot",
							},
						},
					},
				},
			},
			agentID: "test-agent",
			want:    "[MyBot]",
		},
		{
			name: "name with spaces is trimmed",
			cfg: &Config{
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{
						"test-agent": {
							Identity: &IdentityConfig{
								Name: "  My Bot  ",
							},
						},
					},
				},
			},
			agentID: "test-agent",
			want:    "[My Bot]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveIdentityNamePrefix(tt.cfg, tt.agentID)
			if got != tt.want {
				t.Errorf("ResolveIdentityNamePrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveIdentityName(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		agentID string
		want    string
	}{
		{
			name:    "nil config returns empty",
			cfg:     nil,
			agentID: "test-agent",
			want:    "",
		},
		{
			name: "valid name returns just the name",
			cfg: &Config{
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{
						"test-agent": {
							Identity: &IdentityConfig{
								Name: "MyBot",
							},
						},
					},
				},
			},
			agentID: "test-agent",
			want:    "MyBot",
		},
		{
			name: "name with spaces is trimmed",
			cfg: &Config{
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{
						"test-agent": {
							Identity: &IdentityConfig{
								Name: "  My Bot  ",
							},
						},
					},
				},
			},
			agentID: "test-agent",
			want:    "My Bot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveIdentityName(tt.cfg, tt.agentID)
			if got != tt.want {
				t.Errorf("ResolveIdentityName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveMessagePrefix(t *testing.T) {
	configuredPrefix := "custom-prefix"

	tests := []struct {
		name    string
		cfg     *Config
		agentID string
		opts    *MessagePrefixOptions
		want    string
	}{
		{
			name:    "nil config and nil opts returns default fallback",
			cfg:     nil,
			agentID: "test-agent",
			opts:    nil,
			want:    "[clawdbot]",
		},
		{
			name:    "explicitly configured prefix from options takes priority",
			cfg:     nil,
			agentID: "test-agent",
			opts: &MessagePrefixOptions{
				Configured: &configuredPrefix,
			},
			want: "custom-prefix",
		},
		{
			name: "message prefix from config used when no explicit option",
			cfg: &Config{
				Messages: &MessagesConfig{
					MessagePrefix: "config-prefix",
				},
			},
			agentID: "test-agent",
			opts:    nil,
			want:    "config-prefix",
		},
		{
			name:    "hasAllowFrom returns empty string",
			cfg:     nil,
			agentID: "test-agent",
			opts: &MessagePrefixOptions{
				HasAllowFrom: true,
			},
			want: "",
		},
		{
			name: "identity name prefix used when available",
			cfg: &Config{
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{
						"test-agent": {
							Identity: &IdentityConfig{
								Name: "MyBot",
							},
						},
					},
				},
			},
			agentID: "test-agent",
			opts:    nil,
			want:    "[MyBot]",
		},
		{
			name:    "custom fallback used when no identity",
			cfg:     nil,
			agentID: "test-agent",
			opts: &MessagePrefixOptions{
				Fallback: "[custom-fallback]",
			},
			want: "[custom-fallback]",
		},
		{
			name: "explicit prefix overrides hasAllowFrom",
			cfg: &Config{
				Messages: &MessagesConfig{
					MessagePrefix: "explicit",
				},
			},
			agentID: "test-agent",
			opts: &MessagePrefixOptions{
				HasAllowFrom: true,
			},
			want: "explicit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveMessagePrefix(tt.cfg, tt.agentID, tt.opts)
			if got != tt.want {
				t.Errorf("ResolveMessagePrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveResponsePrefix(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		agentID string
		want    string
	}{
		{
			name:    "nil config returns empty",
			cfg:     nil,
			agentID: "test-agent",
			want:    "",
		},
		{
			name: "nil messages returns empty",
			cfg: &Config{
				Messages: nil,
			},
			agentID: "test-agent",
			want:    "",
		},
		{
			name: "empty response prefix returns empty",
			cfg: &Config{
				Messages: &MessagesConfig{
					ResponsePrefix: "",
				},
			},
			agentID: "test-agent",
			want:    "",
		},
		{
			name: "auto mode returns identity name prefix",
			cfg: &Config{
				Messages: &MessagesConfig{
					ResponsePrefix: "auto",
				},
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{
						"test-agent": {
							Identity: &IdentityConfig{
								Name: "MyBot",
							},
						},
					},
				},
			},
			agentID: "test-agent",
			want:    "[MyBot]",
		},
		{
			name: "auto mode with no identity returns empty",
			cfg: &Config{
				Messages: &MessagesConfig{
					ResponsePrefix: "auto",
				},
			},
			agentID: "test-agent",
			want:    "",
		},
		{
			name: "explicit value returned as-is",
			cfg: &Config{
				Messages: &MessagesConfig{
					ResponsePrefix: "[CustomPrefix]",
				},
			},
			agentID: "test-agent",
			want:    "[CustomPrefix]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveResponsePrefix(tt.cfg, tt.agentID)
			if got != tt.want {
				t.Errorf("ResolveResponsePrefix() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveEffectiveMessagesConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		agentID string
		opts    *EffectiveMessagesConfigOptions
		want    EffectiveMessagesConfig
	}{
		{
			name:    "nil config returns defaults",
			cfg:     nil,
			agentID: "test-agent",
			opts:    nil,
			want: EffectiveMessagesConfig{
				MessagePrefix:  "[clawdbot]",
				ResponsePrefix: "",
			},
		},
		{
			name: "combines message and response prefix",
			cfg: &Config{
				Messages: &MessagesConfig{
					ResponsePrefix: "auto",
				},
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{
						"test-agent": {
							Identity: &IdentityConfig{
								Name: "MyBot",
							},
						},
					},
				},
			},
			agentID: "test-agent",
			opts:    nil,
			want: EffectiveMessagesConfig{
				MessagePrefix:  "[MyBot]",
				ResponsePrefix: "[MyBot]",
			},
		},
		{
			name: "hasAllowFrom affects message prefix only",
			cfg: &Config{
				Messages: &MessagesConfig{
					ResponsePrefix: "[response]",
				},
			},
			agentID: "test-agent",
			opts: &EffectiveMessagesConfigOptions{
				HasAllowFrom: true,
			},
			want: EffectiveMessagesConfig{
				MessagePrefix:  "",
				ResponsePrefix: "[response]",
			},
		},
		{
			name:    "fallback message prefix used",
			cfg:     nil,
			agentID: "test-agent",
			opts: &EffectiveMessagesConfigOptions{
				FallbackMessagePrefix: "[fallback]",
			},
			want: EffectiveMessagesConfig{
				MessagePrefix:  "[fallback]",
				ResponsePrefix: "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveEffectiveMessagesConfig(tt.cfg, tt.agentID, tt.opts)
			if got.MessagePrefix != tt.want.MessagePrefix {
				t.Errorf("MessagePrefix = %q, want %q", got.MessagePrefix, tt.want.MessagePrefix)
			}
			if got.ResponsePrefix != tt.want.ResponsePrefix {
				t.Errorf("ResponsePrefix = %q, want %q", got.ResponsePrefix, tt.want.ResponsePrefix)
			}
		})
	}
}

func TestResolveHumanDelayConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		agentID string
		want    *HumanDelayConfig
	}{
		{
			name:    "nil config returns nil",
			cfg:     nil,
			agentID: "test-agent",
			want:    nil,
		},
		{
			name: "no human delay config returns nil",
			cfg: &Config{
				Agents: &AgentsConfig{
					Defaults: &AgentConfig{},
					Agents:   map[string]*AgentConfig{},
				},
			},
			agentID: "test-agent",
			want:    nil,
		},
		{
			name: "only defaults returns defaults",
			cfg: &Config{
				Agents: &AgentsConfig{
					Defaults: &AgentConfig{
						HumanDelay: &HumanDelayConfig{
							Mode:  "random",
							MinMs: 100,
							MaxMs: 500,
						},
					},
				},
			},
			agentID: "test-agent",
			want: &HumanDelayConfig{
				Mode:  "random",
				MinMs: 100,
				MaxMs: 500,
			},
		},
		{
			name: "only overrides returns overrides",
			cfg: &Config{
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{
						"test-agent": {
							HumanDelay: &HumanDelayConfig{
								Mode:  "fixed",
								MinMs: 200,
								MaxMs: 200,
							},
						},
					},
				},
			},
			agentID: "test-agent",
			want: &HumanDelayConfig{
				Mode:  "fixed",
				MinMs: 200,
				MaxMs: 200,
			},
		},
		{
			name: "overrides take precedence over defaults",
			cfg: &Config{
				Agents: &AgentsConfig{
					Defaults: &AgentConfig{
						HumanDelay: &HumanDelayConfig{
							Mode:  "random",
							MinMs: 100,
							MaxMs: 500,
						},
					},
					Agents: map[string]*AgentConfig{
						"test-agent": {
							HumanDelay: &HumanDelayConfig{
								MinMs: 200,
								// Mode and MaxMs not set, should come from defaults
							},
						},
					},
				},
			},
			agentID: "test-agent",
			want: &HumanDelayConfig{
				Mode:  "random",
				MinMs: 200,
				MaxMs: 500,
			},
		},
		{
			name: "override mode takes precedence",
			cfg: &Config{
				Agents: &AgentsConfig{
					Defaults: &AgentConfig{
						HumanDelay: &HumanDelayConfig{
							Mode:  "random",
							MinMs: 100,
							MaxMs: 500,
						},
					},
					Agents: map[string]*AgentConfig{
						"test-agent": {
							HumanDelay: &HumanDelayConfig{
								Mode: "off",
							},
						},
					},
				},
			},
			agentID: "test-agent",
			want: &HumanDelayConfig{
				Mode:  "off",
				MinMs: 100,
				MaxMs: 500,
			},
		},
		{
			name: "zero values in override do not override defaults",
			cfg: &Config{
				Agents: &AgentsConfig{
					Defaults: &AgentConfig{
						HumanDelay: &HumanDelayConfig{
							Mode:  "random",
							MinMs: 100,
							MaxMs: 500,
						},
					},
					Agents: map[string]*AgentConfig{
						"test-agent": {
							HumanDelay: &HumanDelayConfig{
								MinMs: 0, // zero should not override
								MaxMs: 0, // zero should not override
							},
						},
					},
				},
			},
			agentID: "test-agent",
			want: &HumanDelayConfig{
				Mode:  "random",
				MinMs: 100,
				MaxMs: 500,
			},
		},
		{
			name: "different agent not affected",
			cfg: &Config{
				Agents: &AgentsConfig{
					Defaults: &AgentConfig{
						HumanDelay: &HumanDelayConfig{
							Mode:  "random",
							MinMs: 100,
							MaxMs: 500,
						},
					},
					Agents: map[string]*AgentConfig{
						"other-agent": {
							HumanDelay: &HumanDelayConfig{
								Mode: "off",
							},
						},
					},
				},
			},
			agentID: "test-agent",
			want: &HumanDelayConfig{
				Mode:  "random",
				MinMs: 100,
				MaxMs: 500,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveHumanDelayConfig(tt.cfg, tt.agentID)

			if tt.want == nil {
				if got != nil {
					t.Errorf("ResolveHumanDelayConfig() = %v, want nil", got)
				}
				return
			}

			if got == nil {
				t.Errorf("ResolveHumanDelayConfig() = nil, want %v", tt.want)
				return
			}

			if got.Mode != tt.want.Mode {
				t.Errorf("Mode = %q, want %q", got.Mode, tt.want.Mode)
			}
			if got.MinMs != tt.want.MinMs {
				t.Errorf("MinMs = %d, want %d", got.MinMs, tt.want.MinMs)
			}
			if got.MaxMs != tt.want.MaxMs {
				t.Errorf("MaxMs = %d, want %d", got.MaxMs, tt.want.MaxMs)
			}
		})
	}
}

func TestResolveAgentConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		agentID string
		wantNil bool
	}{
		{
			name:    "nil config",
			cfg:     nil,
			agentID: "test-agent",
			wantNil: true,
		},
		{
			name:    "nil agents",
			cfg:     &Config{},
			agentID: "test-agent",
			wantNil: true,
		},
		{
			name: "nil agents map",
			cfg: &Config{
				Agents: &AgentsConfig{},
			},
			agentID: "test-agent",
			wantNil: true,
		},
		{
			name: "agent not found",
			cfg: &Config{
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{},
				},
			},
			agentID: "test-agent",
			wantNil: true,
		},
		{
			name: "agent found",
			cfg: &Config{
				Agents: &AgentsConfig{
					Agents: map[string]*AgentConfig{
						"test-agent": {
							Identity: &IdentityConfig{
								Name: "Test",
							},
						},
					},
				},
			},
			agentID: "test-agent",
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveAgentConfig(tt.cfg, tt.agentID)
			if tt.wantNil && got != nil {
				t.Errorf("ResolveAgentConfig() = %v, want nil", got)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("ResolveAgentConfig() = nil, want non-nil")
			}
		})
	}
}

// TestEdgeCases tests various edge cases and boundary conditions.
func TestEdgeCases(t *testing.T) {
	t.Run("empty agent ID", func(t *testing.T) {
		cfg := &Config{
			Agents: &AgentsConfig{
				Agents: map[string]*AgentConfig{
					"": {
						Identity: &IdentityConfig{
							Name: "Empty ID Agent",
						},
					},
				},
			},
		}

		got := ResolveIdentityName(cfg, "")
		if got != "Empty ID Agent" {
			t.Errorf("ResolveIdentityName() = %q, want %q", got, "Empty ID Agent")
		}
	})

	t.Run("unicode in names", func(t *testing.T) {
		cfg := &Config{
			Agents: &AgentsConfig{
				Agents: map[string]*AgentConfig{
					"unicode-agent": {
						Identity: &IdentityConfig{
							Name:  "日本語エージェント",
							Emoji: "🇯🇵",
						},
					},
				},
			},
		}

		gotName := ResolveIdentityName(cfg, "unicode-agent")
		if gotName != "日本語エージェント" {
			t.Errorf("ResolveIdentityName() = %q, want %q", gotName, "日本語エージェント")
		}

		gotPrefix := ResolveIdentityNamePrefix(cfg, "unicode-agent")
		if gotPrefix != "[日本語エージェント]" {
			t.Errorf("ResolveIdentityNamePrefix() = %q, want %q", gotPrefix, "[日本語エージェント]")
		}

		gotReaction := ResolveAckReaction(cfg, "unicode-agent")
		if gotReaction != "🇯🇵" {
			t.Errorf("ResolveAckReaction() = %q, want %q", gotReaction, "🇯🇵")
		}
	})

	t.Run("very long name", func(t *testing.T) {
		longName := "This is a very long agent name that might be used in production"
		cfg := &Config{
			Agents: &AgentsConfig{
				Agents: map[string]*AgentConfig{
					"long-name-agent": {
						Identity: &IdentityConfig{
							Name: longName,
						},
					},
				},
			},
		}

		got := ResolveIdentityNamePrefix(cfg, "long-name-agent")
		want := "[" + longName + "]"
		if got != want {
			t.Errorf("ResolveIdentityNamePrefix() = %q, want %q", got, want)
		}
	})

	t.Run("special characters in name", func(t *testing.T) {
		cfg := &Config{
			Agents: &AgentsConfig{
				Agents: map[string]*AgentConfig{
					"special-agent": {
						Identity: &IdentityConfig{
							Name: "Agent [v1.0] (beta)",
						},
					},
				},
			},
		}

		got := ResolveIdentityNamePrefix(cfg, "special-agent")
		if got != "[Agent [v1.0] (beta)]" {
			t.Errorf("ResolveIdentityNamePrefix() = %q, want %q", got, "[Agent [v1.0] (beta)]")
		}
	})
}
