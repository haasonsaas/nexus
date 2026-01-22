package provisioning

import (
	"fmt"
	"log/slog"
	"os"

	"gopkg.in/yaml.v3"
)

// ConfigVersion represents a config file version.
type ConfigVersion struct {
	Major int
	Minor int
}

// String returns version as "X.Y" format.
func (v ConfigVersion) String() string {
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}

// CurrentVersion is the latest config version.
var CurrentVersion = ConfigVersion{Major: 1, Minor: 0}

// Migration describes a config migration from one version to another.
type Migration struct {
	From        ConfigVersion
	To          ConfigVersion
	Description string
	Migrate     func(map[string]any) error
}

// MigrationRegistry holds all registered migrations.
type MigrationRegistry struct {
	migrations []Migration
	logger     *slog.Logger
}

// NewMigrationRegistry creates a registry with built-in migrations.
func NewMigrationRegistry(logger *slog.Logger) *MigrationRegistry {
	if logger == nil {
		logger = slog.Default()
	}
	r := &MigrationRegistry{logger: logger}
	r.registerBuiltinMigrations()
	return r
}

func (r *MigrationRegistry) registerBuiltinMigrations() {
	// Example migration: plugins.* -> tools.*
	// This is a no-op since it's already handled elsewhere
}

// Register adds a migration to the registry.
func (r *MigrationRegistry) Register(m Migration) {
	r.migrations = append(r.migrations, m)
}

// GetMigrationChain returns ordered migrations from current to target version.
func (r *MigrationRegistry) GetMigrationChain(from, to ConfigVersion) []Migration {
	var chain []Migration
	current := from

	for current != to {
		found := false
		for _, m := range r.migrations {
			if m.From == current {
				chain = append(chain, m)
				current = m.To
				found = true
				break
			}
		}
		if !found {
			break
		}
	}

	return chain
}

// MigrationReport describes the result of applying migrations.
type MigrationReport struct {
	FromVersion ConfigVersion
	ToVersion   ConfigVersion
	Applied     []string
	Errors      []error
}

// ConfigMigrator handles config file version migrations.
type ConfigMigrator struct {
	configPath string
	registry   *MigrationRegistry
	logger     *slog.Logger
}

// NewConfigMigrator creates a config migrator.
func NewConfigMigrator(configPath string, logger *slog.Logger) *ConfigMigrator {
	if logger == nil {
		logger = slog.Default()
	}
	return &ConfigMigrator{
		configPath: configPath,
		registry:   NewMigrationRegistry(logger),
		logger:     logger,
	}
}

// DetectVersion reads the config and returns its version.
func (m *ConfigMigrator) DetectVersion() (ConfigVersion, error) {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return ConfigVersion{}, fmt.Errorf("read config: %w", err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return ConfigVersion{}, fmt.Errorf("parse config: %w", err)
	}

	// Check for version field
	if v, ok := raw["version"]; ok {
		switch ver := v.(type) {
		case int:
			return ConfigVersion{Major: ver, Minor: 0}, nil
		case string:
			var major, minor int
			if _, err := fmt.Sscanf(ver, "%d.%d", &major, &minor); err != nil {
				return ConfigVersion{}, fmt.Errorf("invalid version format: %s", ver)
			}
			return ConfigVersion{Major: major, Minor: minor}, nil
		}
	}

	// Default to 1.0 for legacy configs
	return ConfigVersion{Major: 1, Minor: 0}, nil
}

// NeedsMigration checks if the config needs to be migrated.
func (m *ConfigMigrator) NeedsMigration() (bool, ConfigVersion, error) {
	current, err := m.DetectVersion()
	if err != nil {
		return false, ConfigVersion{}, err
	}
	return current != CurrentVersion, current, nil
}

// Migrate applies all necessary migrations to bring config to current version.
func (m *ConfigMigrator) Migrate() (*MigrationReport, error) {
	current, err := m.DetectVersion()
	if err != nil {
		return nil, err
	}

	report := &MigrationReport{
		FromVersion: current,
		ToVersion:   current,
	}

	if current == CurrentVersion {
		m.logger.Info("config already at current version", "version", current)
		return report, nil
	}

	// Load raw config
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Apply migrations
	chain := m.registry.GetMigrationChain(current, CurrentVersion)
	for _, migration := range chain {
		m.logger.Info("applying migration",
			"from", migration.From,
			"to", migration.To,
			"description", migration.Description)

		if err := migration.Migrate(raw); err != nil {
			report.Errors = append(report.Errors, err)
			m.logger.Error("migration failed", "error", err)
			return report, err
		}

		report.Applied = append(report.Applied, migration.Description)
		report.ToVersion = migration.To
	}

	// Update version field
	raw["version"] = CurrentVersion.Major

	// Write back
	output, err := yaml.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	if err := os.WriteFile(m.configPath, output, 0644); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}

	m.logger.Info("config migrated",
		"from", report.FromVersion,
		"to", report.ToVersion,
		"applied", len(report.Applied))

	return report, nil
}
