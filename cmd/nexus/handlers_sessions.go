package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"text/tabwriter"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/spf13/cobra"
)

// =============================================================================
// Sessions Command Handlers
// =============================================================================

func runSessionsBranchesList(cmd *cobra.Command, configPath, sessionID string, includeArchived bool, limit int) error {
	configPath = resolveConfigPath(configPath)
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session-id is required")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	store, closeFn, err := openBranchStore(cfg)
	if err != nil {
		return err
	}
	if closeFn != nil {
		defer closeFn()
	}

	opts := sessions.DefaultBranchListOptions()
	opts.IncludeArchived = includeArchived
	if limit > 0 {
		opts.Limit = limit
	}

	branches, err := store.ListBranches(cmd.Context(), sessionID, opts)
	if err != nil {
		return fmt.Errorf("list branches: %w", err)
	}
	if len(branches) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No branches found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tSTATUS\tPARENT\tPOINT\tPRIMARY\tUPDATED")
	for _, branch := range branches {
		parent := "-"
		if branch.ParentBranchID != nil {
			parent = *branch.ParentBranchID
		}
		updated := branch.UpdatedAt.Format(time.RFC3339)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%t\t%s\n",
			branch.ID, branch.Name, branch.Status, parent, branch.BranchPoint, branch.IsPrimary, updated)
	}
	return w.Flush()
}

func runSessionsBranchesFork(cmd *cobra.Command, configPath, parentBranchID, name string, branchPoint int64) error {
	configPath = resolveConfigPath(configPath)
	if strings.TrimSpace(parentBranchID) == "" {
		return fmt.Errorf("parent is required")
	}
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("name is required")
	}
	if branchPoint < 0 {
		return fmt.Errorf("point is required")
	}

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	store, closeFn, err := openBranchStore(cfg)
	if err != nil {
		return err
	}
	if closeFn != nil {
		defer closeFn()
	}

	branch, err := store.ForkBranch(cmd.Context(), parentBranchID, branchPoint, name)
	if err != nil {
		return fmt.Errorf("fork branch: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Created branch %s (session %s)\n", branch.ID, branch.SessionID)
	return nil
}

func runSessionsBranchesTree(cmd *cobra.Command, configPath, sessionID string) error {
	configPath = resolveConfigPath(configPath)
	if strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("session-id is required")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	store, closeFn, err := openBranchStore(cfg)
	if err != nil {
		return err
	}
	if closeFn != nil {
		defer closeFn()
	}

	tree, err := store.GetBranchTree(cmd.Context(), sessionID)
	if err != nil {
		if errors.Is(err, sessions.ErrBranchNotFound) {
			fmt.Fprintln(cmd.OutOrStdout(), "No branches found.")
			return nil
		}
		return fmt.Errorf("get branch tree: %w", err)
	}

	printBranchTree(cmd.OutOrStdout(), tree, 0)
	return nil
}

func runSessionsBranchesMerge(cmd *cobra.Command, configPath, sourceID, targetID, strategy string) error {
	configPath = resolveConfigPath(configPath)
	if strings.TrimSpace(sourceID) == "" || strings.TrimSpace(targetID) == "" {
		return fmt.Errorf("source and target are required")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	store, closeFn, err := openBranchStore(cfg)
	if err != nil {
		return err
	}
	if closeFn != nil {
		defer closeFn()
	}

	strategy = strings.ToLower(strings.TrimSpace(strategy))
	if strategy == "" {
		strategy = string(models.MergeStrategyContinue)
	}
	var mergeStrategy models.MergeStrategy
	switch strategy {
	case string(models.MergeStrategyReplace):
		mergeStrategy = models.MergeStrategyReplace
	case string(models.MergeStrategyInterleave):
		mergeStrategy = models.MergeStrategyInterleave
	default:
		mergeStrategy = models.MergeStrategyContinue
	}

	merge, err := store.MergeBranch(cmd.Context(), sourceID, targetID, mergeStrategy)
	if err != nil {
		return fmt.Errorf("merge branch: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Merged %s into %s (strategy=%s, messages=%d)\n",
		merge.SourceBranchID, merge.TargetBranchID, merge.Strategy, merge.MessageCount)
	return nil
}

func runSessionsBranchesCompare(cmd *cobra.Command, configPath, sourceID, targetID string) error {
	configPath = resolveConfigPath(configPath)
	if strings.TrimSpace(sourceID) == "" || strings.TrimSpace(targetID) == "" {
		return fmt.Errorf("source and target are required")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	store, closeFn, err := openBranchStore(cfg)
	if err != nil {
		return err
	}
	if closeFn != nil {
		defer closeFn()
	}

	compare, err := store.CompareBranches(cmd.Context(), sourceID, targetID)
	if err != nil {
		return fmt.Errorf("compare branches: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Source: %s (%s)\n", compare.SourceBranch.ID, compare.SourceBranch.Name)
	fmt.Fprintf(out, "Target: %s (%s)\n", compare.TargetBranch.ID, compare.TargetBranch.Name)
	if compare.CommonAncestor != nil {
		fmt.Fprintf(out, "Common ancestor: %s (%s)\n", compare.CommonAncestor.ID, compare.CommonAncestor.Name)
	}
	fmt.Fprintf(out, "Divergence point: %d\n", compare.DivergencePoint)
	fmt.Fprintf(out, "Source ahead: %d\n", compare.SourceAhead)
	fmt.Fprintf(out, "Target ahead: %d\n", compare.TargetAhead)
	return nil
}

func runSessionsBranchesHistory(cmd *cobra.Command, configPath, branchID string, limit int, fromSeq int64) error {
	configPath = resolveConfigPath(configPath)
	if strings.TrimSpace(branchID) == "" {
		return fmt.Errorf("branch-id is required")
	}
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	store, closeFn, err := openBranchStore(cfg)
	if err != nil {
		return err
	}
	if closeFn != nil {
		defer closeFn()
	}

	if limit <= 0 {
		limit = 50
	}

	var msgs []*models.Message
	if fromSeq >= 0 {
		msgs, err = store.GetBranchHistoryFromSequence(cmd.Context(), branchID, fromSeq, limit)
	} else {
		msgs, err = store.GetBranchHistory(cmd.Context(), branchID, limit)
	}
	if err != nil {
		return fmt.Errorf("get branch history: %w", err)
	}
	if len(msgs) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No messages found.")
		return nil
	}

	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "SEQ\tROLE\tCONTENT")
	for _, msg := range msgs {
		content := strings.TrimSpace(msg.Content)
		if len(content) > 120 {
			content = content[:117] + "..."
		}
		fmt.Fprintf(w, "%d\t%s\t%s\n", msg.SequenceNum, msg.Role, content)
	}
	return w.Flush()
}

func openBranchStore(cfg *config.Config) (*sessions.CockroachBranchStore, func(), error) {
	if cfg == nil {
		return nil, nil, fmt.Errorf("config is required")
	}
	if strings.TrimSpace(cfg.Database.URL) == "" {
		return nil, nil, fmt.Errorf("database.url is required")
	}

	store, err := sessions.NewCockroachStoreFromDSN(cfg.Database.URL, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open session store: %w", err)
	}
	branchStore := sessions.NewCockroachBranchStore(store.DB())
	return branchStore, func() {
		_ = store.Close()
	}, nil
}

func printBranchTree(w io.Writer, node *models.BranchTree, indent int) {
	if node == nil || node.Branch == nil {
		return
	}
	prefix := strings.Repeat("  ", indent)
	primary := ""
	if node.Branch.IsPrimary {
		primary = " primary"
	}
	fmt.Fprintf(w, "%s- %s%s (%s) id=%s point=%d\n",
		prefix,
		node.Branch.Name,
		primary,
		node.Branch.Status,
		node.Branch.ID,
		node.Branch.BranchPoint,
	)
	for _, child := range node.Children {
		printBranchTree(w, child, indent+1)
	}
}
