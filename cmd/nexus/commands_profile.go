package main

import "github.com/spf13/cobra"

// =============================================================================
// Profile Commands
// =============================================================================

// buildProfileCmd creates the "profile" command group.
func buildProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Manage configuration profiles",
	}
	cmd.AddCommand(buildProfileListCmd(), buildProfileUseCmd(), buildProfilePathCmd(), buildProfileInitCmd())
	return cmd
}

func buildProfileListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List available profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProfileList(cmd)
		},
	}
}

func buildProfileUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use [name]",
		Short: "Set the active profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProfileUse(cmd, args[0])
		},
	}
}

func buildProfilePathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path [name]",
		Short: "Print the config path for a profile",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) > 0 {
				name = args[0]
			}
			return runProfilePath(cmd, name)
		},
	}
}

func buildProfileInitCmd() *cobra.Command {
	var provider string
	var setActive bool
	cmd := &cobra.Command{
		Use:   "init [name]",
		Short: "Initialize a new profile config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProfileInit(cmd, args[0], provider, setActive)
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "anthropic", "Default LLM provider")
	cmd.Flags().BoolVar(&setActive, "use", false, "Set as active profile after creation")
	return cmd
}
