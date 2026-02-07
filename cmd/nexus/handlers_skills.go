package main

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/doctor"
	"github.com/haasonsaas/nexus/internal/extensions"
	"github.com/haasonsaas/nexus/internal/skills"
	"github.com/spf13/cobra"
)

// =============================================================================
// Skills Command Handlers
// =============================================================================

// runSkillsList handles the skills list command.
func runSkillsList(cmd *cobra.Command, configPath string, all bool) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	mgr, err := skills.NewManager(&cfg.Skills, cfg.Workspace.Path, nil)
	if err != nil {
		return fmt.Errorf("failed to create skill manager: %w", err)
	}

	if err := mgr.Discover(cmd.Context()); err != nil {
		return fmt.Errorf("skill discovery failed: %w", err)
	}

	out := cmd.OutOrStdout()
	var skillsList []*skills.SkillEntry
	if all {
		skillsList = mgr.ListAll()
	} else {
		skillsList = mgr.ListEligible()
	}

	if len(skillsList) == 0 {
		fmt.Fprintln(out, "No skills found.")
		return nil
	}

	fmt.Fprintln(out, "Skills:")
	for _, skill := range skillsList {
		emoji := ""
		if skill.Metadata != nil && skill.Metadata.Emoji != "" {
			emoji = skill.Metadata.Emoji + " "
		}

		status := "eligible"
		if all {
			result, err := mgr.CheckEligibility(skill.Name)
			if err != nil {
				status = "unknown"
			} else if result != nil && !result.Eligible {
				status = "ineligible"
			}
		}

		fmt.Fprintf(out, "  %s%s (%s, %s)\n", emoji, skill.Name, skill.Source, status)
		if skill.Description != "" {
			desc := skill.Description
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			fmt.Fprintf(out, "    %s\n", desc)
		}
	}

	if all {
		reasons := mgr.GetIneligibleReasons()
		if len(reasons) > 0 {
			fmt.Fprintln(out, "\nIneligible reasons:")
			for name, reason := range reasons {
				fmt.Fprintf(out, "  %s: %s\n", name, reason)
			}
		}
	}

	return nil
}

// runSkillsShow handles the skills show command.
func runSkillsShow(cmd *cobra.Command, configPath, skillName string, showContent bool) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	mgr, err := skills.NewManager(&cfg.Skills, cfg.Workspace.Path, nil)
	if err != nil {
		return fmt.Errorf("failed to create skill manager: %w", err)
	}

	if err := mgr.Discover(cmd.Context()); err != nil {
		return fmt.Errorf("skill discovery failed: %w", err)
	}

	skill, ok := mgr.GetSkill(skillName)
	if !ok {
		return fmt.Errorf("skill not found: %s", skillName)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Skill: %s\n", skill.Name)
	fmt.Fprintln(out, strings.Repeat("=", len(skill.Name)+7))
	fmt.Fprintln(out)

	if skill.Description != "" {
		fmt.Fprintf(out, "Description: %s\n", skill.Description)
	}
	if skill.Homepage != "" {
		fmt.Fprintf(out, "Homepage: %s\n", skill.Homepage)
	}
	fmt.Fprintf(out, "Path: %s\n", skill.Path)
	fmt.Fprintf(out, "Source: %s\n", skill.Source)

	// Metadata
	if skill.Metadata != nil {
		fmt.Fprintln(out, "\nMetadata:")
		if skill.Metadata.Emoji != "" {
			fmt.Fprintf(out, "  Emoji: %s\n", skill.Metadata.Emoji)
		}
		if skill.Metadata.Always {
			fmt.Fprintln(out, "  Always: true")
		}
		if len(skill.Metadata.OS) > 0 {
			fmt.Fprintf(out, "  OS: %v\n", skill.Metadata.OS)
		}
		if skill.Metadata.PrimaryEnv != "" {
			fmt.Fprintf(out, "  Primary Env: %s\n", skill.Metadata.PrimaryEnv)
		}

		// Requirements
		if skill.Metadata.Requires != nil {
			req := skill.Metadata.Requires
			fmt.Fprintln(out, "\nRequirements:")
			if len(req.Bins) > 0 {
				fmt.Fprintf(out, "  Binaries: %v\n", req.Bins)
			}
			if len(req.AnyBins) > 0 {
				fmt.Fprintf(out, "  Any Binary: %v\n", req.AnyBins)
			}
			if len(req.Env) > 0 {
				fmt.Fprintf(out, "  Env Vars: %v\n", req.Env)
			}
			if len(req.Config) > 0 {
				fmt.Fprintf(out, "  Config: %v\n", req.Config)
			}
		}

		// Install specs
		if len(skill.Metadata.Install) > 0 {
			fmt.Fprintln(out, "\nInstall Options:")
			for _, spec := range skill.Metadata.Install {
				label := spec.Label
				if label == "" {
					label = spec.ID
				}
				fmt.Fprintf(out, "  - %s (%s)\n", label, spec.Kind)
			}
		}
	}

	// Eligibility
	result, err := mgr.CheckEligibility(skill.Name)
	if err == nil && result != nil {
		fmt.Fprintln(out)
		if result.Eligible {
			fmt.Fprintln(out, "Status: Eligible")
		} else {
			fmt.Fprintf(out, "Status: Ineligible (%s)\n", result.Reason)
		}
	} else if err != nil {
		fmt.Fprintf(out, "Eligibility check failed: %v\n", err)
	}

	// Content
	if showContent {
		content, err := mgr.LoadContent(skill.Name)
		if err != nil {
			return fmt.Errorf("failed to load content: %w", err)
		}
		fmt.Fprintln(out, "\nContent:")
		fmt.Fprintln(out, strings.Repeat("-", 40))
		fmt.Fprintln(out, content)
	}

	return nil
}

// runSkillsCheck handles the skills check command.
func runSkillsCheck(cmd *cobra.Command, configPath, skillName string) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	mgr, err := skills.NewManager(&cfg.Skills, cfg.Workspace.Path, nil)
	if err != nil {
		return fmt.Errorf("failed to create skill manager: %w", err)
	}

	if err := mgr.Discover(cmd.Context()); err != nil {
		return fmt.Errorf("skill discovery failed: %w", err)
	}

	result, err := mgr.CheckEligibility(skillName)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	if result.Eligible {
		fmt.Fprintf(out, "Skill '%s' is eligible\n", skillName)
		if result.Reason != "" {
			fmt.Fprintf(out, "  Reason: %s\n", result.Reason)
		}
	} else {
		fmt.Fprintf(out, "Skill '%s' is NOT eligible\n", skillName)
		fmt.Fprintf(out, "  Reason: %s\n", result.Reason)
	}

	return nil
}

// runSkillsEnable handles the skills enable command.
func runSkillsEnable(cmd *cobra.Command, configPath, skillName string) error {
	configPath = resolveConfigPath(configPath)
	raw, err := doctor.LoadRawConfig(configPath)
	if err != nil {
		return err
	}
	setSkillEnabled(raw, skillName, true)
	if err := doctor.WriteRawConfig(configPath, raw); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Enabled skill: %s\n", skillName)
	return nil
}

// runSkillsDisable handles the skills disable command.
func runSkillsDisable(cmd *cobra.Command, configPath, skillName string) error {
	configPath = resolveConfigPath(configPath)
	raw, err := doctor.LoadRawConfig(configPath)
	if err != nil {
		return err
	}
	setSkillEnabled(raw, skillName, false)
	if err := doctor.WriteRawConfig(configPath, raw); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Disabled skill: %s\n", skillName)
	return nil
}

// =============================================================================
// Extensions Command Handlers
// =============================================================================

func runExtensionsList(cmd *cobra.Command, configPath string) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	var skillsMgr *skills.Manager
	manager, err := skills.NewManager(&cfg.Skills, cfg.Workspace.Path, nil)
	if err != nil {
		return fmt.Errorf("init skills manager: %w", err)
	}
	if err := manager.Discover(cmd.Context()); err != nil {
		return fmt.Errorf("discover skills: %w", err)
	}
	skillsMgr = manager

	exts := extensions.List(cfg, skillsMgr)
	if len(exts) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No extensions configured.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "KIND\tID\tSTATUS\tSOURCE")
	for _, ext := range exts {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", ext.Kind, ext.ID, ext.Status, ext.Source)
	}
	return w.Flush()
}
