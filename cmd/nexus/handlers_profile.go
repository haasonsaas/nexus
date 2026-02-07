package main

import (
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/onboard"
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/spf13/cobra"
)

// =============================================================================
// Profile Command Handlers
// =============================================================================

// runProfileList handles the profile list command.
func runProfileList(cmd *cobra.Command) error {
	profiles, err := profile.ListProfiles()
	if err != nil {
		return err
	}
	active, err := profile.ReadActiveProfile()
	if err != nil {
		active = ""
	}
	out := cmd.OutOrStdout()
	if len(profiles) == 0 {
		fmt.Fprintln(out, "No profiles found.")
		return nil
	}
	fmt.Fprintln(out, "Profiles:")
	for _, name := range profiles {
		marker := ""
		if name == active {
			marker = " (active)"
		}
		fmt.Fprintf(out, "  - %s%s\n", name, marker)
	}
	return nil
}

// runProfileUse handles the profile use command.
func runProfileUse(cmd *cobra.Command, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("profile name is required")
	}
	if err := profile.WriteActiveProfile(name); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Active profile set: %s\n", name)
	return nil
}

// runProfilePath handles the profile path command.
func runProfilePath(cmd *cobra.Command, name string) error {
	path := profile.ProfileConfigPath(name)
	fmt.Fprintln(cmd.OutOrStdout(), path)
	return nil
}

// runProfileInit handles the profile init command.
func runProfileInit(cmd *cobra.Command, name, provider string, setActive bool) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("profile name is required")
	}
	path := profile.ProfileConfigPath(name)
	opts := onboard.Options{Provider: provider}
	raw := onboard.BuildConfig(opts)
	if err := onboard.WriteConfig(path, raw); err != nil {
		return err
	}
	if setActive {
		if err := profile.WriteActiveProfile(name); err != nil {
			return err
		}
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Profile config written: %s\n", path)
	return nil
}
