package main

import (
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/spf13/cobra"
)

// =============================================================================
// Sessions Commands
// =============================================================================

// buildSessionsCmd creates the "sessions" command group for session tooling.
func buildSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Manage sessions and branches",
	}
	cmd.AddCommand(buildSessionsBranchesCmd())
	return cmd
}

func buildSessionsBranchesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "branches",
		Short: "Manage session branches",
	}
	cmd.AddCommand(
		buildSessionsBranchesListCmd(),
		buildSessionsBranchesForkCmd(),
		buildSessionsBranchesMergeCmd(),
		buildSessionsBranchesCompareCmd(),
		buildSessionsBranchesHistoryCmd(),
		buildSessionsBranchesTreeCmd(),
	)
	return cmd
}

func buildSessionsBranchesListCmd() *cobra.Command {
	var (
		configPath      string
		sessionID       string
		includeArchived bool
		limit           int
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List branches for a session",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsBranchesList(cmd, configPath, sessionID, includeArchived, limit)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Session ID to list branches for")
	cmd.Flags().BoolVar(&includeArchived, "include-archived", false, "Include archived branches")
	cmd.Flags().IntVar(&limit, "limit", 50, "Max number of branches to return")
	return cmd
}

func buildSessionsBranchesForkCmd() *cobra.Command {
	var (
		configPath     string
		parentBranchID string
		name           string
		branchPoint    int64
	)
	cmd := &cobra.Command{
		Use:   "fork",
		Short: "Fork a branch at a sequence point",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsBranchesFork(cmd, configPath, parentBranchID, name, branchPoint)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&parentBranchID, "parent", "", "Parent branch ID to fork from")
	cmd.Flags().StringVar(&name, "name", "", "Name for the new branch")
	cmd.Flags().Int64Var(&branchPoint, "point", -1, "Branch point sequence number in the parent branch")
	return cmd
}

func buildSessionsBranchesTreeCmd() *cobra.Command {
	var (
		configPath string
		sessionID  string
	)
	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Show branch tree for a session",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsBranchesTree(cmd, configPath, sessionID)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&sessionID, "session-id", "", "Session ID to show branch tree for")
	return cmd
}

func buildSessionsBranchesMergeCmd() *cobra.Command {
	var (
		configPath string
		sourceID   string
		targetID   string
		strategy   string
	)
	cmd := &cobra.Command{
		Use:   "merge",
		Short: "Merge a source branch into a target branch",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsBranchesMerge(cmd, configPath, sourceID, targetID, strategy)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&sourceID, "source", "", "Source branch ID")
	cmd.Flags().StringVar(&targetID, "target", "", "Target branch ID")
	cmd.Flags().StringVar(&strategy, "strategy", "continue", "Merge strategy (replace, continue, interleave)")
	return cmd
}

func buildSessionsBranchesCompareCmd() *cobra.Command {
	var (
		configPath string
		sourceID   string
		targetID   string
	)
	cmd := &cobra.Command{
		Use:   "compare",
		Short: "Compare two branches",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsBranchesCompare(cmd, configPath, sourceID, targetID)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&sourceID, "source", "", "Source branch ID")
	cmd.Flags().StringVar(&targetID, "target", "", "Target branch ID")
	return cmd
}

func buildSessionsBranchesHistoryCmd() *cobra.Command {
	var (
		configPath string
		branchID   string
		limit      int
		fromSeq    int64
	)
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Show branch message history",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSessionsBranchesHistory(cmd, configPath, branchID, limit, fromSeq)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&branchID, "branch-id", "", "Branch ID to fetch history for")
	cmd.Flags().IntVar(&limit, "limit", 50, "Max number of messages to return")
	cmd.Flags().Int64Var(&fromSeq, "from-sequence", -1, "Start from sequence number (inclusive)")
	return cmd
}
