package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/artifacts"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/gateway"
	"github.com/spf13/cobra"
)

// =============================================================================
// Artifacts Handlers
// =============================================================================

func runArtifactsList(cmd *cobra.Command, configPath string, limit int, artifactType, sessionID, edgeID string) error {
	configPath = resolveConfigPath(configPath)

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	repo, cleanup, err := createArtifactRepository(cfg)
	if err != nil {
		return fmt.Errorf("create artifact repository: %w", err)
	}
	if cleanup != nil {
		defer cleanup()
	}
	if repo == nil {
		return fmt.Errorf("artifacts not configured (set artifacts.backend in config)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	filter := artifacts.Filter{
		SessionID: sessionID,
		EdgeID:    edgeID,
		Type:      artifactType,
		Limit:     limit,
	}

	list, err := repo.ListArtifacts(ctx, filter)
	if err != nil {
		return fmt.Errorf("list artifacts: %w", err)
	}

	out := cmd.OutOrStdout()
	if len(list) == 0 {
		fmt.Fprintln(out, "No artifacts found")
		return nil
	}

	fmt.Fprintf(out, "Found %d artifacts:\n\n", len(list))
	for _, art := range list {
		fmt.Fprintf(out, "ID:       %s\n", art.Id)
		fmt.Fprintf(out, "Type:     %s\n", art.Type)
		fmt.Fprintf(out, "MIME:     %s\n", art.MimeType)
		if art.Filename != "" {
			fmt.Fprintf(out, "Filename: %s\n", art.Filename)
		}
		fmt.Fprintf(out, "Size:     %d bytes\n", art.Size)
		fmt.Fprintf(out, "Ref:      %s\n", art.Reference)
		fmt.Fprintln(out, "---")
	}

	return nil
}

func runArtifactsGet(cmd *cobra.Command, configPath, artifactID, outputPath string) error {
	configPath = resolveConfigPath(configPath)

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	repo, cleanup, err := createArtifactRepository(cfg)
	if err != nil {
		return fmt.Errorf("create artifact repository: %w", err)
	}
	if cleanup != nil {
		defer cleanup()
	}
	if repo == nil {
		return fmt.Errorf("artifacts not configured (set artifacts.backend in config)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	art, data, err := repo.GetArtifact(ctx, artifactID)
	if err != nil {
		return fmt.Errorf("get artifact: %w", err)
	}
	defer data.Close()

	out := cmd.OutOrStdout()

	// Display artifact info
	fmt.Fprintf(out, "ID:       %s\n", art.Id)
	fmt.Fprintf(out, "Type:     %s\n", art.Type)
	fmt.Fprintf(out, "MIME:     %s\n", art.MimeType)
	if art.Filename != "" {
		fmt.Fprintf(out, "Filename: %s\n", art.Filename)
	}
	fmt.Fprintf(out, "Size:     %d bytes\n", art.Size)
	fmt.Fprintf(out, "Ref:      %s\n", art.Reference)

	// Save to file if output path specified
	if outputPath != "" {
		f, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return fmt.Errorf("open output file: %w", err)
		}
		defer f.Close()

		if _, err := io.Copy(f, data); err != nil {
			return fmt.Errorf("write artifact data: %w", err)
		}
		fmt.Fprintf(out, "\nSaved to: %s\n", outputPath)
	} else {
		// Suggest download path
		filename := art.Filename
		if filename == "" {
			filename = art.Id + extensionForMimeCLI(art.MimeType)
		}
		fmt.Fprintf(out, "\nUse -o to save data to file (suggested: %s)\n", filename)
	}

	return nil
}

func runArtifactsDelete(cmd *cobra.Command, configPath, artifactID string, force bool) error {
	if !force {
		reader := bufio.NewReader(os.Stdin)
		fmt.Printf("Delete artifact %s? [y/N]: ", artifactID)
		response, err := reader.ReadString('\n')
		if err != nil {
			fmt.Println("Cancelled")
			return nil
		}
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Cancelled")
			return nil
		}
	}

	configPath = resolveConfigPath(configPath)

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	repo, cleanup, err := createArtifactRepository(cfg)
	if err != nil {
		return fmt.Errorf("create artifact repository: %w", err)
	}
	if cleanup != nil {
		defer cleanup()
	}
	if repo == nil {
		return fmt.Errorf("artifacts not configured (set artifacts.backend in config)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := repo.DeleteArtifact(ctx, artifactID); err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}

	fmt.Printf("Deleted artifact: %s\n", artifactID)
	return nil
}

// createArtifactRepository creates an artifact repository from config.
// Returns nil if artifacts are not configured.
func createArtifactRepository(cfg *config.Config) (artifacts.Repository, func(), error) {
	if cfg == nil {
		return nil, nil, nil
	}
	repo, err := gateway.BuildArtifactRepository(context.Background(), cfg, slog.Default())
	if err != nil {
		return nil, nil, err
	}
	return repo, nil, nil
}

// extensionForMimeCLI returns a file extension for a MIME type.
func extensionForMimeCLI(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	case "application/json":
		return ".json"
	default:
		return ".dat"
	}
}
