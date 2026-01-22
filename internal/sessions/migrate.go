package sessions

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migration represents an embedded migration.
type Migration struct {
	ID      string
	UpSQL   string
	DownSQL string
}

// AppliedMigration represents a migration applied to the database.
type AppliedMigration struct {
	ID        string
	AppliedAt time.Time
}

// Migrator applies database migrations.
type Migrator struct {
	db         *sql.DB
	migrations []Migration
}

// NewMigrator creates a migrator backed by the given db.
func NewMigrator(db *sql.DB) (*Migrator, error) {
	if db == nil {
		return nil, fmt.Errorf("db is required")
	}
	migrations, err := loadMigrations()
	if err != nil {
		return nil, err
	}
	return &Migrator{db: db, migrations: migrations}, nil
}

// EnsureSchema ensures the schema_migrations table exists.
func (m *Migrator) EnsureSchema(ctx context.Context) error {
	_, err := m.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			id STRING PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	return nil
}

// Up applies pending migrations. If steps <= 0, apply all.
func (m *Migrator) Up(ctx context.Context, steps int) ([]string, error) {
	if err := m.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	applied, err := m.appliedMigrationIDs(ctx)
	if err != nil {
		return nil, err
	}
	pending := []Migration{}
	for _, migration := range m.migrations {
		if applied[migration.ID] {
			continue
		}
		pending = append(pending, migration)
	}
	if steps > 0 && steps < len(pending) {
		pending = pending[:steps]
	}

	appliedIDs := []string{}
	for _, migration := range pending {
		if strings.TrimSpace(migration.UpSQL) == "" {
			return appliedIDs, fmt.Errorf("missing up migration for %s", migration.ID)
		}
		tx, err := m.db.BeginTx(ctx, nil)
		if err != nil {
			return appliedIDs, fmt.Errorf("begin migration %s: %w", migration.ID, err)
		}
		if _, err := tx.ExecContext(ctx, migration.UpSQL); err != nil {
			_ = tx.Rollback()
			return appliedIDs, fmt.Errorf("apply migration %s: %w", migration.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (id) VALUES ($1)`, migration.ID); err != nil {
			_ = tx.Rollback()
			return appliedIDs, fmt.Errorf("record migration %s: %w", migration.ID, err)
		}
		if err := tx.Commit(); err != nil {
			return appliedIDs, fmt.Errorf("commit migration %s: %w", migration.ID, err)
		}
		appliedIDs = append(appliedIDs, migration.ID)
	}
	return appliedIDs, nil
}

// Down rolls back the last N applied migrations.
func (m *Migrator) Down(ctx context.Context, steps int) ([]string, error) {
	if steps <= 0 {
		steps = 1
	}
	if err := m.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	applied, err := m.appliedMigrationList(ctx)
	if err != nil {
		return nil, err
	}
	if len(applied) == 0 {
		return nil, nil
	}
	if steps > len(applied) {
		steps = len(applied)
	}
	toRollback := applied[len(applied)-steps:]
	rolled := []string{}
	for i := len(toRollback) - 1; i >= 0; i-- {
		entry := toRollback[i]
		migration, ok := m.migrationByID(entry.ID)
		if !ok {
			return rolled, fmt.Errorf("migration %s not found", entry.ID)
		}
		if strings.TrimSpace(migration.DownSQL) == "" {
			return rolled, fmt.Errorf("missing down migration for %s", migration.ID)
		}
		tx, err := m.db.BeginTx(ctx, nil)
		if err != nil {
			return rolled, fmt.Errorf("begin rollback %s: %w", migration.ID, err)
		}
		if _, err := tx.ExecContext(ctx, migration.DownSQL); err != nil {
			_ = tx.Rollback()
			return rolled, fmt.Errorf("rollback migration %s: %w", migration.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM schema_migrations WHERE id = $1`, migration.ID); err != nil {
			_ = tx.Rollback()
			return rolled, fmt.Errorf("delete migration %s: %w", migration.ID, err)
		}
		if err := tx.Commit(); err != nil {
			return rolled, fmt.Errorf("commit rollback %s: %w", migration.ID, err)
		}
		rolled = append(rolled, migration.ID)
	}
	return rolled, nil
}

// Status returns applied and pending migrations.
func (m *Migrator) Status(ctx context.Context) ([]AppliedMigration, []Migration, error) {
	if err := m.EnsureSchema(ctx); err != nil {
		return nil, nil, err
	}
	applied, err := m.appliedMigrationList(ctx)
	if err != nil {
		return nil, nil, err
	}
	appliedIDs := make(map[string]bool, len(applied))
	for _, entry := range applied {
		appliedIDs[entry.ID] = true
	}
	pending := []Migration{}
	for _, migration := range m.migrations {
		if !appliedIDs[migration.ID] {
			pending = append(pending, migration)
		}
	}
	return applied, pending, nil
}

func (m *Migrator) appliedMigrationIDs(ctx context.Context) (map[string]bool, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("schema_migrations: %w", err)
	}
	return applied, nil
}

func (m *Migrator) appliedMigrationList(ctx context.Context) ([]AppliedMigration, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id, applied_at FROM schema_migrations ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := []AppliedMigration{}
	for rows.Next() {
		var entry AppliedMigration
		if err := rows.Scan(&entry.ID, &entry.AppliedAt); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied = append(applied, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("schema_migrations: %w", err)
	}
	return applied, nil
}

func (m *Migrator) migrationByID(id string) (Migration, bool) {
	for _, migration := range m.migrations {
		if migration.ID == id {
			return migration, true
		}
	}
	return Migration{}, false
}

func loadMigrations() ([]Migration, error) {
	paths, err := fs.Glob(migrationsFS, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}

	entries := map[string]*Migration{}
	for _, path := range paths {
		base := strings.TrimPrefix(path, "migrations/")
		suffix := ""
		switch {
		case strings.HasSuffix(base, ".up.sql"):
			suffix = ".up.sql"
		case strings.HasSuffix(base, ".down.sql"):
			suffix = ".down.sql"
		default:
			continue
		}
		id := strings.TrimSuffix(base, suffix)
		entry := entries[id]
		if entry == nil {
			entry = &Migration{ID: id}
			entries[id] = entry
		}
		data, err := migrationsFS.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", path, err)
		}
		if suffix == ".up.sql" {
			entry.UpSQL = string(data)
		} else {
			entry.DownSQL = string(data)
		}
	}

	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	migrations := make([]Migration, 0, len(ids))
	for _, id := range ids {
		migrations = append(migrations, *entries[id])
	}
	return migrations, nil
}
