package main

import (
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/spf13/cobra"
)

// =============================================================================
// Skills Commands
// =============================================================================

// buildSkillsCmd creates the "skills" command group.
func buildSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage skills (SKILL.md-based)",
		Long: `Manage skills that extend agent capabilities.

Skills are discovered from:
  - <workspace>/skills/ (highest priority)
  - ~/.nexus/skills/ (user skills)
  - Bundled skills (shipped with binary)
  - Extra directories (skills.load.extraDirs)

Each skill is a directory containing a SKILL.md file with YAML frontmatter.`,
	}
	cmd.AddCommand(
		buildSkillsListCmd(),
		buildSkillsShowCmd(),
		buildSkillsCheckCmd(),
		buildSkillsEnableCmd(),
		buildSkillsDisableCmd(),
	)
	return cmd
}

func buildSkillsListCmd() *cobra.Command {
	var configPath string
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List discovered skills",
		Long: `List all discovered skills and their eligibility status.

By default, only eligible skills are shown. Use --all to include ineligible skills.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsList(cmd, configPath, all)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Show all skills including ineligible ones")
	return cmd
}

func buildSkillsShowCmd() *cobra.Command {
	var configPath string
	var showContent bool
	cmd := &cobra.Command{
		Use:   "show [name]",
		Short: "Show skill details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsShow(cmd, configPath, args[0], showContent)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().BoolVar(&showContent, "content", false, "Show full skill content")
	return cmd
}

func buildSkillsCheckCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "check [name]",
		Short: "Check skill eligibility",
		Long:  "Check if a skill is eligible to be loaded and show the reason if not.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsCheck(cmd, configPath, args[0])
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

func buildSkillsEnableCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "enable [name]",
		Short: "Enable a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsEnable(cmd, configPath, args[0])
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

func buildSkillsDisableCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "disable [name]",
		Short: "Disable a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsDisable(cmd, configPath, args[0])
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

// =============================================================================
// Extensions Commands
// =============================================================================

func buildExtensionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "extensions",
		Short: "List configured extensions (skills, plugins, MCP)",
	}
	cmd.AddCommand(buildExtensionsListCmd())
	return cmd
}

func buildExtensionsListCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List configured extensions",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runExtensionsList(cmd, configPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}
