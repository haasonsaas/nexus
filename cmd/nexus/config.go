// Package main provides the CLI entry point for the Nexus multi-channel AI gateway.
//
// config.go contains configuration loading utilities, profile resolution,
// and database connection helpers used by CLI commands.
package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/marketplace"
	"github.com/haasonsaas/nexus/internal/mcp"
	"github.com/haasonsaas/nexus/internal/profile"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

// resolveConfigPath determines the configuration file path based on:
// 1. Active profile (from flag or NEXUS_PROFILE env var)
// 2. Explicit path provided by user
// 3. Default config path
func resolveConfigPath(path string) string {
	activeProfile := strings.TrimSpace(profileName)
	if activeProfile == "" {
		activeProfile = strings.TrimSpace(os.Getenv("NEXUS_PROFILE"))
	}
	if activeProfile != "" {
		return profile.ProfileConfigPath(activeProfile)
	}
	if strings.TrimSpace(path) == "" || path == profile.DefaultConfigName {
		return profile.DefaultConfigPath()
	}
	return path
}

// openMigrationDB opens a database connection for running migrations.
// It applies connection pool settings from the config.
func openMigrationDB(cfg *config.Config) (*sql.DB, error) {
	if cfg == nil || strings.TrimSpace(cfg.Database.URL) == "" {
		return nil, fmt.Errorf("database url is required")
	}
	db, err := sql.Open("postgres", cfg.Database.URL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	pool := sessions.DefaultCockroachConfig()
	if cfg.Database.MaxConnections > 0 {
		pool.MaxOpenConns = cfg.Database.MaxConnections
	}
	if cfg.Database.ConnMaxLifetime > 0 {
		pool.ConnMaxLifetime = cfg.Database.ConnMaxLifetime
	}
	db.SetMaxOpenConns(pool.MaxOpenConns)
	db.SetMaxIdleConns(pool.MaxIdleConns)
	db.SetConnMaxLifetime(pool.ConnMaxLifetime)
	db.SetConnMaxIdleTime(pool.ConnMaxIdleTime)

	ctx, cancel := context.WithTimeout(context.Background(), pool.ConnectTimeout)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

// loadMCPManager creates an MCP manager from the configuration.
func loadMCPManager(configPath string) (*config.Config, *mcp.Manager, error) {
	configPath = resolveConfigPath(configPath)
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, err
	}
	if !cfg.MCP.Enabled {
		return cfg, mcp.NewManager(&cfg.MCP, slog.Default()), nil
	}
	return cfg, mcp.NewManager(&cfg.MCP, slog.Default()), nil
}

// createMarketplaceManager creates a marketplace manager for plugin operations.
func createMarketplaceManager(cfg *config.Config) (*marketplace.Manager, error) {
	managerCfg := &marketplace.ManagerConfig{
		Registries:  cfg.Marketplace.Registries,
		TrustedKeys: cfg.Marketplace.TrustedKeys,
	}
	return marketplace.NewManager(managerCfg)
}

// fileToEntry converts a file path to a memory entry for indexing.
func fileToEntry(path, scope, scopeID, source string) (*models.MemoryEntry, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	entry := &models.MemoryEntry{
		Content: string(content),
		Metadata: models.MemoryMetadata{
			Source: source,
			Extra:  map[string]any{"path": path},
		},
		CreatedAt: time.Now(),
	}
	switch models.MemoryScope(scope) {
	case models.ScopeSession:
		entry.SessionID = scopeID
	case models.ScopeChannel:
		entry.ChannelID = scopeID
	case models.ScopeAgent:
		entry.AgentID = scopeID
	}
	return entry, nil
}

// setSkillEnabled modifies the raw config to enable or disable a skill.
func setSkillEnabled(raw map[string]any, name string, enabled bool) {
	if raw == nil {
		return
	}
	skillsSection, ok := raw["skills"].(map[string]any)
	if !ok {
		skillsSection = map[string]any{}
		raw["skills"] = skillsSection
	}
	entries, ok := skillsSection["entries"].(map[string]any)
	if !ok {
		entries = map[string]any{}
		skillsSection["entries"] = entries
	}
	entry, ok := entries[name].(map[string]any)
	if !ok {
		entry = map[string]any{}
		entries[name] = entry
	}
	entry["enabled"] = enabled
}

// promptString prompts the user for a string input with an optional default value.
func promptString(reader *bufio.Reader, label string, defaultValue string) string {
	if defaultValue != "" {
		fmt.Printf("%s [%s]: ", label, defaultValue)
	} else {
		fmt.Printf("%s: ", label)
	}
	text, _ := reader.ReadString('\n')
	text = strings.TrimSpace(text)
	if text == "" {
		return defaultValue
	}
	return text
}

// promptBool prompts the user for a yes/no input.
func promptBool(reader *bufio.Reader, label string, defaultValue bool) bool {
	defaultLabel := "n"
	if defaultValue {
		defaultLabel = "y"
	}
	answer := promptString(reader, label+" (y/n)", defaultLabel)
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" {
		return defaultValue
	}
	return answer == "y" || answer == "yes"
}

// parseMCPQualifiedName parses a server.name qualified identifier.
func parseMCPQualifiedName(value string) (string, string, error) {
	parts := strings.SplitN(value, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expected format <server>.<name>")
	}
	return parts[0], parts[1], nil
}

// parseAnyArgs parses key=value arguments into a map with type inference.
func parseAnyArgs(items []string) (map[string]any, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make(map[string]any)
	for _, item := range items {
		key, value, err := parseKeyValue(item)
		if err != nil {
			return nil, err
		}
		var parsed any
		if err := json.Unmarshal([]byte(value), &parsed); err == nil {
			out[key] = parsed
		} else {
			out[key] = value
		}
	}
	return out, nil
}

// parseStringArgs parses key=value arguments into a string map.
func parseStringArgs(items []string) (map[string]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	out := make(map[string]string)
	for _, item := range items {
		key, value, err := parseKeyValue(item)
		if err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, nil
}

// parseKeyValue parses a single key=value string.
func parseKeyValue(item string) (string, string, error) {
	parts := strings.SplitN(item, "=", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
		return "", "", fmt.Errorf("invalid arg %q, expected key=value", item)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

// workspacePathFromProfile returns a workspace path based on profile name.
func workspacePathFromProfile(profileName string) string {
	home, _ := os.UserHomeDir()
	if strings.TrimSpace(home) == "" {
		home = "."
	}
	return filepath.Join(home, "nexus-"+profileName)
}
