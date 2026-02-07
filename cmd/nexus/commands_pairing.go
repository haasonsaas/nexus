package main

import "github.com/spf13/cobra"

// =============================================================================
// Pairing Commands
// =============================================================================

// buildPairingCmd creates the "pairing" command group.
func buildPairingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pairing",
		Short: "Manage pairing requests for messaging channels",
	}
	cmd.AddCommand(buildPairingListCmd(), buildPairingApproveCmd(), buildPairingDenyCmd())
	return cmd
}

func buildPairingListCmd() *cobra.Command {
	var provider string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pending pairing requests",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPairingList(cmd, provider)
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "Filter by provider (telegram, discord, slack, whatsapp, signal, imessage, matrix, teams, mattermost, nextcloud-talk, zalo, bluebubbles)")
	return cmd
}

func buildPairingApproveCmd() *cobra.Command {
	var provider string
	cmd := &cobra.Command{
		Use:   "approve [code]",
		Short: "Approve a pairing code",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPairingApprove(cmd, args[0], provider)
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "Provider for the pairing code")
	return cmd
}

func buildPairingDenyCmd() *cobra.Command {
	var provider string
	cmd := &cobra.Command{
		Use:   "deny [code]",
		Short: "Deny a pairing code",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPairingDeny(cmd, args[0], provider)
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "Provider for the pairing code")
	return cmd
}
