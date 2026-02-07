package main

import (
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/spf13/cobra"
)

// =============================================================================
// RAG Commands
// =============================================================================

// buildRagCmd creates the "rag" command group.
func buildRagCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rag",
		Short: "Evaluate and inspect RAG retrieval quality",
	}
	cmd.AddCommand(buildRagEvalCmd(), buildRagPackCmd())
	return cmd
}

func buildRagEvalCmd() *cobra.Command {
	var (
		configPath  string
		testSet     string
		output      string
		limit       int
		threshold   float32
		judge       bool
		judgeModel  string
		judgeProv   string
		judgeTokens int
	)
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run RAG evaluation against a test set",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRagEval(cmd, configPath, testSet, output, limit, threshold, judge, judgeModel, judgeProv, judgeTokens)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&testSet, "test-set", "", "Path to RAG evaluation test set (YAML)")
	cmd.Flags().StringVar(&output, "output", "", "Write JSON report to file (optional)")
	cmd.Flags().IntVar(&limit, "limit", 10, "Maximum number of retrieval results per case")
	cmd.Flags().Float32Var(&threshold, "threshold", 0.7, "Minimum similarity threshold (0-1)")
	cmd.Flags().BoolVar(&judge, "judge", false, "Enable LLM-as-judge scoring")
	cmd.Flags().StringVar(&judgeProv, "judge-provider", "", "Provider ID for LLM judge (defaults to llm.default_provider)")
	cmd.Flags().StringVar(&judgeModel, "judge-model", "", "Model ID for LLM judge (defaults to provider default)")
	cmd.Flags().IntVar(&judgeTokens, "judge-max-tokens", 1024, "Max tokens for answer generation when judging")
	cobra.CheckErr(cmd.MarkFlagRequired("test-set"))
	return cmd
}

func buildRagPackCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pack",
		Short: "Manage RAG knowledge packs",
	}
	cmd.AddCommand(
		buildRagPackInstallCmd(),
		buildRagPackListCmd(),
		buildRagPackSearchCmd(),
	)
	return cmd
}

func buildRagPackInstallCmd() *cobra.Command {
	var (
		configPath string
		packDir    string
	)
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install a knowledge pack into RAG storage",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRagPackInstall(cmd, configPath, packDir)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&packDir, "path", "", "Path to knowledge pack directory")
	cobra.CheckErr(cmd.MarkFlagRequired("path"))
	return cmd
}

func buildRagPackListCmd() *cobra.Command {
	var (
		configPath string
		root       string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available knowledge packs",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRagPackList(cmd, configPath, root)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&root, "root", "", "Root directory containing packs (defaults to workspace/packs and ~/.nexus/packs)")
	return cmd
}

func buildRagPackSearchCmd() *cobra.Command {
	var (
		configPath string
		root       string
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search available knowledge packs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRagPackSearch(cmd, configPath, root, args[0])
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVar(&root, "root", "", "Root directory containing packs (defaults to workspace/packs and ~/.nexus/packs)")
	return cmd
}
