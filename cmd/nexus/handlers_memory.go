package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/memory"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/spf13/cobra"
)

// =============================================================================
// Memory Command Handlers
// =============================================================================

// runMemorySearch handles the memory search command.
func runMemorySearch(cmd *cobra.Command, configPath, query, scope, scopeID string, limit int, threshold float32) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mgr, err := memory.NewManager(&cfg.VectorMemory)
	if err != nil {
		return fmt.Errorf("failed to create memory manager: %w", err)
	}
	defer mgr.Close()

	memScope := models.MemoryScope(scope)
	resp, err := mgr.Search(cmd.Context(), &models.SearchRequest{
		Query:     query,
		Scope:     memScope,
		ScopeID:   scopeID,
		Limit:     limit,
		Threshold: threshold,
	})
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	out := cmd.OutOrStdout()
	if len(resp.Results) == 0 {
		fmt.Fprintln(out, "No results found.")
		return nil
	}

	fmt.Fprintf(out, "Found %d results (query time: %v):\n\n", len(resp.Results), resp.QueryTime)
	for i, result := range resp.Results {
		content := result.Entry.Content
		if len(content) > 200 {
			content = content[:197] + "..."
		}
		fmt.Fprintf(out, "%d. [Score: %.3f] %s\n", i+1, result.Score, content)
		fmt.Fprintf(out, "   Source: %s | Created: %s\n\n",
			result.Entry.Metadata.Source, result.Entry.CreatedAt.Format(time.RFC3339))
	}
	return nil
}

// runMemoryIndex handles the memory index command.
func runMemoryIndex(cmd *cobra.Command, configPath, path, scope, scopeID, source string) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mgr, err := memory.NewManager(&cfg.VectorMemory)
	if err != nil {
		return fmt.Errorf("failed to create memory manager: %w", err)
	}
	defer mgr.Close()

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat path: %w", err)
	}

	var entries []*models.MemoryEntry
	if info.IsDir() {
		err = filepath.Walk(path, func(p string, fi os.FileInfo, err error) error {
			if err != nil || fi.IsDir() {
				return err
			}
			entry, err := fileToEntry(p, scope, scopeID, source)
			if err != nil {
				slog.Warn("skipping file", "path", p, "error", err)
				return nil
			}
			entries = append(entries, entry)
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to walk directory: %w", err)
		}
	} else {
		entry, err := fileToEntry(path, scope, scopeID, source)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No files to index.")
		return nil
	}

	if err := mgr.Index(cmd.Context(), entries); err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Indexed %d entries.\n", len(entries))
	return nil
}

// runMemoryStats handles the memory stats command.
func runMemoryStats(cmd *cobra.Command, configPath string) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mgr, err := memory.NewManager(&cfg.VectorMemory)
	if err != nil {
		return fmt.Errorf("failed to create memory manager: %w", err)
	}
	defer mgr.Close()

	stats, err := mgr.Stats(cmd.Context())
	if err != nil {
		return fmt.Errorf("failed to get stats: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Memory Statistics")
	fmt.Fprintln(out, "=================")
	fmt.Fprintf(out, "Total Entries:      %d\n", stats.TotalEntries)
	fmt.Fprintf(out, "Backend:            %s\n", stats.Backend)
	fmt.Fprintf(out, "Embedding Provider: %s\n", stats.EmbeddingProvider)
	fmt.Fprintf(out, "Embedding Model:    %s\n", stats.EmbeddingModel)
	fmt.Fprintf(out, "Dimension:          %d\n", stats.Dimension)
	return nil
}

// runMemoryCompact handles the memory compact command.
func runMemoryCompact(cmd *cobra.Command, configPath string) error {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mgr, err := memory.NewManager(&cfg.VectorMemory)
	if err != nil {
		return fmt.Errorf("failed to create memory manager: %w", err)
	}
	defer mgr.Close()

	if err := mgr.Compact(cmd.Context()); err != nil {
		return fmt.Errorf("compact failed: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Memory compacted successfully.")
	return nil
}
