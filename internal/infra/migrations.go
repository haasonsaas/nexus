// Package infra provides state migrations for schema versioning and upgrades.
package infra

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// MigrationVersion represents a version number.
type MigrationVersion int

// Migration represents a single migration step.
type Migration struct {
	Version     MigrationVersion
	Name        string
	Description string
	Up          func(ctx *MigrationContext) error
	Down        func(ctx *MigrationContext) error
}

// MigrationContext provides context for migration execution.
type MigrationContext struct {
	StateDir   string
	ConfigPath string
	Logger     MigrationLogger
	DryRun     bool
	Data       map[string]any
}

// MigrationLogger logs migration progress.
type MigrationLogger interface {
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// MigrationState tracks applied migrations.
type MigrationState struct {
	Version           MigrationVersion   `json:"version"`
	AppliedMigrations []AppliedMigration `json:"applied_migrations"`
	LastMigratedAt    int64              `json:"last_migrated_at,omitempty"`
}

// AppliedMigration records a completed migration.
type AppliedMigration struct {
	Version    MigrationVersion `json:"version"`
	Name       string           `json:"name"`
	AppliedAt  int64            `json:"applied_at"`
	DurationMs int64            `json:"duration_ms,omitempty"`
}

// MigrationManager manages state migrations.
type MigrationManager struct {
	mu          sync.Mutex
	migrations  []*Migration
	stateDir    string
	statePath   string
	logger      MigrationLogger
	autoMigrate bool
}

// MigrationManagerConfig configures the migration manager.
type MigrationManagerConfig struct {
	StateDir    string
	StatePath   string
	Logger      MigrationLogger
	AutoMigrate bool
}

// DefaultMigrationStatePath returns the default migrations state path.
func DefaultMigrationStatePath(stateDir string) string {
	return filepath.Join(stateDir, "migrations.json")
}

// NewMigrationManager creates a new migration manager.
func NewMigrationManager(config *MigrationManagerConfig) *MigrationManager {
	if config == nil {
		config = &MigrationManagerConfig{}
	}

	stateDir := config.StateDir
	if stateDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		stateDir = filepath.Join(home, ".nexus")
	}

	statePath := config.StatePath
	if statePath == "" {
		statePath = DefaultMigrationStatePath(stateDir)
	}

	logger := config.Logger
	if logger == nil {
		logger = &noopLogger{}
	}

	return &MigrationManager{
		migrations:  make([]*Migration, 0),
		stateDir:    stateDir,
		statePath:   statePath,
		logger:      logger,
		autoMigrate: config.AutoMigrate,
	}
}

// Register adds a migration to the manager.
func (m *MigrationManager) Register(migration *Migration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.migrations = append(m.migrations, migration)
	// Keep sorted by version
	sort.Slice(m.migrations, func(i, j int) bool {
		return m.migrations[i].Version < m.migrations[j].Version
	})
}

// LoadState loads the current migration state.
func (m *MigrationManager) LoadState() (*MigrationState, error) {
	data, err := os.ReadFile(m.statePath)
	if errors.Is(err, fs.ErrNotExist) {
		return &MigrationState{
			Version:           0,
			AppliedMigrations: make([]AppliedMigration, 0),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read migration state: %w", err)
	}

	var state MigrationState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parse migration state: %w", err)
	}

	return &state, nil
}

// SaveState saves the migration state.
func (m *MigrationManager) SaveState(state *MigrationState) error {
	dir := filepath.Dir(m.statePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create migrations dir: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal migration state: %w", err)
	}

	if err := os.WriteFile(m.statePath, data, 0o644); err != nil {
		return fmt.Errorf("write migration state: %w", err)
	}

	return nil
}

// CurrentVersion returns the current schema version.
func (m *MigrationManager) CurrentVersion() (MigrationVersion, error) {
	state, err := m.LoadState()
	if err != nil {
		return 0, err
	}
	return state.Version, nil
}

// LatestVersion returns the latest available migration version.
func (m *MigrationManager) LatestVersion() MigrationVersion {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.migrations) == 0 {
		return 0
	}
	return m.migrations[len(m.migrations)-1].Version
}

// PendingMigrations returns migrations that haven't been applied.
func (m *MigrationManager) PendingMigrations() ([]*Migration, error) {
	state, err := m.LoadState()
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	pending := make([]*Migration, 0)
	for _, migration := range m.migrations {
		if migration.Version > state.Version {
			pending = append(pending, migration)
		}
	}
	return pending, nil
}

// NeedsMigration returns true if there are pending migrations.
func (m *MigrationManager) NeedsMigration() (bool, error) {
	pending, err := m.PendingMigrations()
	if err != nil {
		return false, err
	}
	return len(pending) > 0, nil
}

// MigrateUp applies all pending migrations.
func (m *MigrationManager) MigrateUp(ctx *MigrationContext) (*MigrationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ctx == nil {
		ctx = &MigrationContext{}
	}
	if ctx.StateDir == "" {
		ctx.StateDir = m.stateDir
	}
	if ctx.Logger == nil {
		ctx.Logger = m.logger
	}
	if ctx.Data == nil {
		ctx.Data = make(map[string]any)
	}

	state, err := m.LoadState()
	if err != nil {
		return nil, err
	}

	result := &MigrationResult{
		StartVersion: state.Version,
		Applied:      make([]AppliedMigration, 0),
	}

	for _, migration := range m.migrations {
		if migration.Version <= state.Version {
			continue
		}

		if migration.Up == nil {
			return result, fmt.Errorf("migration %d has no Up function", migration.Version)
		}

		m.logger.Info("Applying migration %d: %s", migration.Version, migration.Name)

		startedAt := time.Now()
		if err := migration.Up(ctx); err != nil {
			result.Error = fmt.Errorf("migration %d failed: %w", migration.Version, err)
			return result, result.Error
		}
		durationMs := time.Since(startedAt).Milliseconds()

		applied := AppliedMigration{
			Version:    migration.Version,
			Name:       migration.Name,
			AppliedAt:  time.Now().UnixMilli(),
			DurationMs: durationMs,
		}

		state.Version = migration.Version
		state.AppliedMigrations = append(state.AppliedMigrations, applied)
		state.LastMigratedAt = time.Now().UnixMilli()

		if !ctx.DryRun {
			if err := m.SaveState(state); err != nil {
				return result, fmt.Errorf("save state after migration %d: %w", migration.Version, err)
			}
		}

		result.Applied = append(result.Applied, applied)
		m.logger.Info("Migration %d completed in %dms", migration.Version, durationMs)
	}

	result.EndVersion = state.Version
	return result, nil
}

// MigrateDown rolls back the last migration.
func (m *MigrationManager) MigrateDown(ctx *MigrationContext) (*MigrationResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if ctx == nil {
		ctx = &MigrationContext{}
	}
	if ctx.StateDir == "" {
		ctx.StateDir = m.stateDir
	}
	if ctx.Logger == nil {
		ctx.Logger = m.logger
	}

	state, err := m.LoadState()
	if err != nil {
		return nil, err
	}

	if state.Version == 0 {
		return &MigrationResult{
			StartVersion: 0,
			EndVersion:   0,
		}, nil
	}

	// Find the migration to roll back
	var migration *Migration
	for _, mig := range m.migrations {
		if mig.Version == state.Version {
			migration = mig
			break
		}
	}

	if migration == nil {
		return nil, fmt.Errorf("migration %d not found", state.Version)
	}

	if migration.Down == nil {
		return nil, fmt.Errorf("migration %d has no Down function", state.Version)
	}

	result := &MigrationResult{
		StartVersion: state.Version,
		Applied:      make([]AppliedMigration, 0),
	}

	m.logger.Info("Rolling back migration %d: %s", migration.Version, migration.Name)

	startedAt := time.Now()
	if err := migration.Down(ctx); err != nil {
		result.Error = fmt.Errorf("rollback %d failed: %w", migration.Version, err)
		return result, result.Error
	}
	durationMs := time.Since(startedAt).Milliseconds()

	// Find previous version
	prevVersion := MigrationVersion(0)
	for i := len(m.migrations) - 1; i >= 0; i-- {
		if m.migrations[i].Version < migration.Version {
			prevVersion = m.migrations[i].Version
			break
		}
	}

	state.Version = prevVersion
	state.LastMigratedAt = time.Now().UnixMilli()

	// Remove the applied migration record
	newApplied := make([]AppliedMigration, 0)
	for _, app := range state.AppliedMigrations {
		if app.Version != migration.Version {
			newApplied = append(newApplied, app)
		}
	}
	state.AppliedMigrations = newApplied

	if !ctx.DryRun {
		if err := m.SaveState(state); err != nil {
			return result, fmt.Errorf("save state after rollback: %w", err)
		}
	}

	result.EndVersion = state.Version
	result.Applied = append(result.Applied, AppliedMigration{
		Version:    migration.Version,
		Name:       migration.Name + " (rollback)",
		AppliedAt:  time.Now().UnixMilli(),
		DurationMs: durationMs,
	})

	m.logger.Info("Rollback %d completed in %dms", migration.Version, durationMs)
	return result, nil
}

// MigrateTo migrates to a specific version.
func (m *MigrationManager) MigrateTo(ctx *MigrationContext, targetVersion MigrationVersion) (*MigrationResult, error) {
	current, err := m.CurrentVersion()
	if err != nil {
		return nil, err
	}

	if targetVersion == current {
		return &MigrationResult{
			StartVersion: current,
			EndVersion:   current,
		}, nil
	}

	if targetVersion > current {
		// Migrate up, but only to target version
		result := &MigrationResult{
			StartVersion: current,
			Applied:      make([]AppliedMigration, 0),
		}

		pending, err := m.PendingMigrations()
		if err != nil {
			return nil, err
		}

		for _, migration := range pending {
			if migration.Version > targetVersion {
				break
			}
			// Apply this migration
			subResult, err := m.MigrateUp(ctx)
			if err != nil {
				result.Error = err
				return result, err
			}
			result.Applied = append(result.Applied, subResult.Applied...)
			result.EndVersion = subResult.EndVersion
		}

		return result, nil
	}

	// Migrate down
	result := &MigrationResult{
		StartVersion: current,
		Applied:      make([]AppliedMigration, 0),
	}

	for current > targetVersion {
		subResult, err := m.MigrateDown(ctx)
		if err != nil {
			result.Error = err
			return result, err
		}
		result.Applied = append(result.Applied, subResult.Applied...)
		current = subResult.EndVersion
	}

	result.EndVersion = current
	return result, nil
}

// MigrationResult contains the result of a migration operation.
type MigrationResult struct {
	StartVersion MigrationVersion   `json:"start_version"`
	EndVersion   MigrationVersion   `json:"end_version"`
	Applied      []AppliedMigration `json:"applied"`
	Error        error              `json:"-"`
}

// AutoMigrateOnStartup runs migrations if auto-migrate is enabled.
func (m *MigrationManager) AutoMigrateOnStartup() error {
	if !m.autoMigrate {
		return nil
	}

	needsMigration, err := m.NeedsMigration()
	if err != nil {
		return err
	}

	if !needsMigration {
		return nil
	}

	m.logger.Info("Running automatic migrations...")
	result, err := m.MigrateUp(nil)
	if err != nil {
		return err
	}

	m.logger.Info("Migrated from version %d to %d (%d migrations applied)",
		result.StartVersion, result.EndVersion, len(result.Applied))
	return nil
}

// noopLogger is a no-op logger implementation.
type noopLogger struct{}

func (l *noopLogger) Info(msg string, args ...any)  {}
func (l *noopLogger) Warn(msg string, args ...any)  {}
func (l *noopLogger) Error(msg string, args ...any) {}

// stdLogger logs to stdout.
type stdLogger struct{}

func (l *stdLogger) Info(msg string, args ...any) {
	fmt.Printf("[migrations] "+msg+"\n", args...)
}
func (l *stdLogger) Warn(msg string, args ...any) {
	fmt.Printf("[migrations] WARN: "+msg+"\n", args...)
}
func (l *stdLogger) Error(msg string, args ...any) {
	fmt.Printf("[migrations] ERROR: "+msg+"\n", args...)
}

// NewStdLogger creates a logger that writes to stdout.
func NewStdLogger() MigrationLogger {
	return &stdLogger{}
}

// SessionKeyMigration creates a migration for session key format changes.
func SessionKeyMigration() *Migration {
	return &Migration{
		Version:     1,
		Name:        "session_key_format",
		Description: "Update session keys to new agent-scoped format",
		Up: func(ctx *MigrationContext) error {
			// Implementation would update session store keys
			ctx.Logger.Info("Migrating session keys to agent-scoped format")
			return nil
		},
		Down: func(ctx *MigrationContext) error {
			ctx.Logger.Info("Rolling back session key format changes")
			return nil
		},
	}
}
