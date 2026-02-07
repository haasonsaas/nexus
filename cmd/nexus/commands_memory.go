package main

import (
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/spf13/cobra"
)

// =============================================================================
// Memory Commands
// =============================================================================

// buildMemoryCmd creates the "memory" command group for vector memory.
func buildMemoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Manage vector memory for semantic search",
		Long: `Manage the vector memory system for semantic search.

Vector memory allows semantic search over conversation history
and indexed documents using embedding models (OpenAI, Ollama).

Storage backends: sqlite-vec (default), LanceDB, pgvector`,
	}
	cmd.AddCommand(
		buildMemorySearchCmd(),
		buildMemoryIndexCmd(),
		buildMemoryStatsCmd(),
		buildMemoryCompactCmd(),
	)
	return cmd
}

func buildMemorySearchCmd() *cobra.Command {
	var (
		configPath string
		scope      string
		scopeID    string
		limit      int
		threshold  float32
	)
	cmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search memory using semantic similarity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMemorySearch(cmd, configPath, args[0], scope, scopeID, limit, threshold)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&scope, "scope", "all", "Search scope (session, channel, agent, global, all)")
	cmd.Flags().StringVar(&scopeID, "scope-id", "", "Scope ID for scoped searches")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum number of results")
	cmd.Flags().Float32Var(&threshold, "threshold", 0.7, "Minimum similarity threshold (0-1)")
	return cmd
}

func buildMemoryIndexCmd() *cobra.Command {
	var (
		configPath string
		scope      string
		scopeID    string
		source     string
	)
	cmd := &cobra.Command{
		Use:   "index [file-or-directory]",
		Short: "Index files into memory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMemoryIndex(cmd, configPath, args[0], scope, scopeID, source)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&scope, "scope", "global", "Memory scope (session, channel, agent, global)")
	cmd.Flags().StringVar(&scopeID, "scope-id", "", "Scope ID")
	cmd.Flags().StringVar(&source, "source", "document", "Source label for indexed content")
	return cmd
}

func buildMemoryStatsCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show memory statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMemoryStats(cmd, configPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}

func buildMemoryCompactCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "compact",
		Short: "Compact and optimize memory storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMemoryCompact(cmd, configPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	return cmd
}
