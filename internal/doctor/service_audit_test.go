package doctor

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestFindServiceFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "nexus.service"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.service"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	matches := findServiceFiles(dir, ".service", "nexus")
	if len(matches) != 1 {
		t.Fatalf("expected 1 match, got %d", len(matches))
	}
}

func TestCheckPortDetectsInUse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer listener.Close()

	addr := listener.Addr().(*net.TCPAddr)
	status := CheckPort("127.0.0.1", addr.Port)
	if !status.InUse {
		t.Fatalf("expected port to be in use")
	}
}
