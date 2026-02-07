package main

import (
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/spf13/cobra"
)

// =============================================================================
// Agent Commands
// =============================================================================

// buildAgentsCmd creates the "agents" command group for managing AI agents.
func buildAgentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage AI agents",
		Long: `Configure and manage AI agent instances.

Agents define the behavior, LLM provider, and tools available for conversations.
Each agent can have different system prompts, model configurations, and tool access.`,
	}

	cmd.AddCommand(buildAgentsListCmd())
	cmd.AddCommand(buildAgentsCreateCmd())
	cmd.AddCommand(buildAgentsShowCmd())

	return cmd
}

func buildAgentsListCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured agents",
		Long:  "Display all AI agents defined in the system.",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			return printAgentsList(cmd.OutOrStdout(), configPath)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	return cmd
}

func buildAgentsCreateCmd() *cobra.Command {
	var (
		configPath string
		name       string
		provider   string
		model      string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new agent",
		Long: `Create a new AI agent with specified configuration.

The agent definition will be appended to AGENTS.md and loaded by the server.`,
		Example: `  # Create agent with Claude
  nexus agents create --name "coder" --provider anthropic --model claude-sonnet-4-20250514

  # Create agent with GPT-4
  nexus agents create --name "researcher" --provider openai --model gpt-4o`,
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			return printAgentCreate(cmd.OutOrStdout(), configPath, name, provider, model)
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	cmd.Flags().StringVarP(&name, "name", "n", "", "Agent name (required)")
	cmd.Flags().StringVarP(&provider, "provider", "p", "anthropic", "LLM provider")
	cmd.Flags().StringVarP(&model, "model", "m", "", "Model identifier")
	cobra.CheckErr(cmd.MarkFlagRequired("name"))

	return cmd
}

func buildAgentsShowCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "show [agent-id]",
		Short: "Show agent details",
		Long:  "Display detailed configuration for a specific agent.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath = resolveConfigPath(configPath)
			return printAgentShow(cmd.OutOrStdout(), configPath, args[0])
		},
	}

	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to config file")
	return cmd
}
