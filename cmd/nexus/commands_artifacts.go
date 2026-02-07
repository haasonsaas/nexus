package main

import (
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/spf13/cobra"
)

// =============================================================================
// Artifacts Commands
// =============================================================================

func buildArtifactsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "artifacts",
		Short: "Manage artifacts produced by tool execution",
		Long:  `List, view, and manage artifacts (screenshots, recordings, files) produced by edge tools.`,
	}
	cmd.AddCommand(
		buildArtifactsListCmd(),
		buildArtifactsGetCmd(),
		buildArtifactsDeleteCmd(),
	)
	return cmd
}

func buildArtifactsListCmd() *cobra.Command {
	var configPath string
	var limit int
	var artifactType string
	var sessionID string
	var edgeID string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List artifacts",
		Long:  `List artifacts, optionally filtered by type, session, or edge.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runArtifactsList(cmd, configPath, limit, artifactType, sessionID, edgeID)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().IntVarP(&limit, "limit", "n", 50, "Maximum number of artifacts to show")
	cmd.Flags().StringVarP(&artifactType, "type", "t", "", "Filter by artifact type (screenshot, recording, file)")
	cmd.Flags().StringVarP(&sessionID, "session", "s", "", "Filter by session ID")
	cmd.Flags().StringVarP(&edgeID, "edge", "e", "", "Filter by edge ID")
	return cmd
}

func buildArtifactsGetCmd() *cobra.Command {
	var configPath string
	var outputPath string
	cmd := &cobra.Command{
		Use:   "get <artifact-id>",
		Short: "Get an artifact",
		Long:  `Download an artifact by its ID.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runArtifactsGet(cmd, configPath, args[0], outputPath)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().StringVarP(&outputPath, "output", "o", "", "Output file path (default: stdout for text, or ./<filename>)")
	return cmd
}

func buildArtifactsDeleteCmd() *cobra.Command {
	var configPath string
	var force bool
	cmd := &cobra.Command{
		Use:   "delete <artifact-id>",
		Short: "Delete an artifact",
		Long:  `Delete an artifact by its ID.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runArtifactsDelete(cmd, configPath, args[0], force)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", profile.DefaultConfigPath(), "Path to YAML configuration file")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")
	return cmd
}
