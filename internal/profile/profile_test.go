package profile

import "testing"

func TestProfileConfigPath(t *testing.T) {
	path := ProfileConfigPath("test")
	if path == DefaultConfigName {
		t.Fatalf("expected profile path, got default")
	}
}
