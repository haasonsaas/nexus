package sessions

import "testing"

func TestLoadMigrations(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations() error = %v", err)
	}
	if len(migrations) < 2 {
		t.Fatalf("expected at least 2 migrations, got %d", len(migrations))
	}
	if migrations[0].ID != "001_create_sessions" {
		t.Fatalf("expected first migration to be 001_create_sessions, got %q", migrations[0].ID)
	}
}
