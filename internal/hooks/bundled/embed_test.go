package bundled

import (
	"io/fs"
	"testing"
)

func TestBundledFS(t *testing.T) {
	t.Run("returns valid filesystem", func(t *testing.T) {
		fsys := BundledFS()
		if fsys == nil {
			t.Fatal("BundledFS returned nil")
		}
	})

	t.Run("can read directory", func(t *testing.T) {
		fsys := BundledFS()
		entries, err := fs.ReadDir(fsys, ".")
		if err != nil {
			t.Fatalf("ReadDir error: %v", err)
		}
		// Should have at least one entry (the example-hook directory)
		if len(entries) == 0 {
			t.Error("expected at least one entry in bundled hooks")
		}
	})

	t.Run("can access hook files", func(t *testing.T) {
		fsys := BundledFS()
		// Try to find any HOOK.md file
		found := false
		err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Name() == "HOOK.md" {
				found = true
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WalkDir error: %v", err)
		}
		if !found {
			t.Error("expected to find at least one HOOK.md file")
		}
	})

	t.Run("can read HOOK.md content", func(t *testing.T) {
		fsys := BundledFS()
		var hookPath string
		err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Name() == "HOOK.md" {
				hookPath = path
			}
			return nil
		})
		if err != nil {
			t.Fatalf("WalkDir error: %v", err)
		}
		if hookPath == "" {
			t.Skip("no HOOK.md found to read")
		}

		data, err := fs.ReadFile(fsys, hookPath)
		if err != nil {
			t.Fatalf("ReadFile error: %v", err)
		}
		if len(data) == 0 {
			t.Error("HOOK.md content is empty")
		}
	})
}

func TestBundledFS_SubdirectoryAccess(t *testing.T) {
	fsys := BundledFS()

	// List top-level directories
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		t.Fatalf("ReadDir error: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Try to access the subdirectory
			subEntries, err := fs.ReadDir(fsys, entry.Name())
			if err != nil {
				t.Errorf("failed to read subdirectory %q: %v", entry.Name(), err)
				continue
			}
			// Should have at least HOOK.md
			hasHookMd := false
			for _, se := range subEntries {
				if se.Name() == "HOOK.md" {
					hasHookMd = true
					break
				}
			}
			if !hasHookMd {
				t.Errorf("subdirectory %q missing HOOK.md", entry.Name())
			}
		}
	}
}
