package skills

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestManagerStartWatchingTracksSkillDirs(t *testing.T) {
	workspace := t.TempDir()
	skillsDir := filepath.Join(workspace, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}

	skillPath := filepath.Join(skillsDir, "alpha")
	if err := os.MkdirAll(skillPath, 0o755); err != nil {
		t.Fatalf("mkdir skill: %v", err)
	}
	skillFile := filepath.Join(skillPath, SkillFilename)
	if err := os.WriteFile(skillFile, []byte("---\nname: alpha\ndescription: test skill\n---\n# Alpha\n"), 0o644); err != nil {
		t.Fatalf("write skill file: %v", err)
	}

	cfg := &SkillsConfig{
		Load: &LoadConfig{
			Watch:           true,
			WatchDebounceMs: 10,
		},
	}
	manager, err := NewManager(cfg, workspace, nil)
	if err != nil {
		t.Fatalf("NewManager error: %v", err)
	}
	defer func() { _ = manager.Close() }()

	if err := manager.Discover(context.Background()); err != nil {
		t.Fatalf("Discover error: %v", err)
	}
	if err := manager.StartWatching(context.Background()); err != nil {
		t.Fatalf("StartWatching error: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		manager.watchMu.Lock()
		_, ok := manager.watchPaths[skillPath]
		manager.watchMu.Unlock()
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected watcher to include %s", skillPath)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
