package sandbox

import (
	"testing"
	"time"
)

func TestResolveDaytonaConfig_EnvOverrides(t *testing.T) {
	t.Setenv("DAYTONA_API_KEY", "test-key")
	t.Setenv("DAYTONA_SNAPSHOT", "snap-1")
	t.Setenv("DAYTONA_IMAGE", "daytona/image:latest")
	t.Setenv("DAYTONA_SANDBOX_CLASS", "medium")
	t.Setenv("DAYTONA_WORKSPACE_DIR", "workspace")
	t.Setenv("DAYTONA_NETWORK_ALLOW_LIST", "github.com,api.openai.com")
	t.Setenv("DAYTONA_AUTO_STOP_INTERVAL", "15m")
	t.Setenv("DAYTONA_AUTO_ARCHIVE_INTERVAL", "2h")
	t.Setenv("DAYTONA_AUTO_DELETE_INTERVAL", "24h")

	cfg, err := resolveDaytonaConfig(nil)
	if err != nil {
		t.Fatalf("resolveDaytonaConfig: %v", err)
	}

	if cfg.Snapshot != "snap-1" {
		t.Errorf("Snapshot = %q, want %q", cfg.Snapshot, "snap-1")
	}
	if cfg.Image != "daytona/image:latest" {
		t.Errorf("Image = %q, want %q", cfg.Image, "daytona/image:latest")
	}
	if cfg.SandboxClass != "medium" {
		t.Errorf("SandboxClass = %q, want %q", cfg.SandboxClass, "medium")
	}
	if cfg.WorkspaceDir != "workspace" {
		t.Errorf("WorkspaceDir = %q, want %q", cfg.WorkspaceDir, "workspace")
	}
	if cfg.NetworkAllow != "github.com,api.openai.com" {
		t.Errorf("NetworkAllow = %q, want %q", cfg.NetworkAllow, "github.com,api.openai.com")
	}
	if cfg.AutoStop == nil || *cfg.AutoStop != 15*time.Minute {
		t.Errorf("AutoStop = %v, want %v", cfg.AutoStop, 15*time.Minute)
	}
	if cfg.AutoArchive == nil || *cfg.AutoArchive != 2*time.Hour {
		t.Errorf("AutoArchive = %v, want %v", cfg.AutoArchive, 2*time.Hour)
	}
	if cfg.AutoDelete == nil || *cfg.AutoDelete != 24*time.Hour {
		t.Errorf("AutoDelete = %v, want %v", cfg.AutoDelete, 24*time.Hour)
	}
}

func TestResolveDaytonaConfig_ConfigWinsOverEnv(t *testing.T) {
	t.Setenv("DAYTONA_API_KEY", "env-key")
	t.Setenv("DAYTONA_SNAPSHOT", "env-snap")
	t.Setenv("DAYTONA_IMAGE", "env-image")
	t.Setenv("DAYTONA_SANDBOX_CLASS", "env-class")
	t.Setenv("DAYTONA_WORKSPACE_DIR", "env-workspace")
	t.Setenv("DAYTONA_NETWORK_ALLOW_LIST", "env-allow")
	t.Setenv("DAYTONA_AUTO_STOP_INTERVAL", "10m")
	t.Setenv("DAYTONA_AUTO_ARCHIVE_INTERVAL", "1h")
	t.Setenv("DAYTONA_AUTO_DELETE_INTERVAL", "12h")

	stop := 30 * time.Minute
	archive := 3 * time.Hour
	del := 48 * time.Hour
	cfg, err := resolveDaytonaConfig(&DaytonaConfig{
		APIKey:       "cfg-key",
		Snapshot:     "cfg-snap",
		Image:        "cfg-image",
		SandboxClass: "cfg-class",
		WorkspaceDir: "cfg-workspace",
		NetworkAllow: "cfg-allow",
		AutoStop:     &stop,
		AutoArchive:  &archive,
		AutoDelete:   &del,
	})
	if err != nil {
		t.Fatalf("resolveDaytonaConfig: %v", err)
	}

	if cfg.Snapshot != "cfg-snap" {
		t.Errorf("Snapshot = %q, want %q", cfg.Snapshot, "cfg-snap")
	}
	if cfg.Image != "cfg-image" {
		t.Errorf("Image = %q, want %q", cfg.Image, "cfg-image")
	}
	if cfg.SandboxClass != "cfg-class" {
		t.Errorf("SandboxClass = %q, want %q", cfg.SandboxClass, "cfg-class")
	}
	if cfg.WorkspaceDir != "cfg-workspace" {
		t.Errorf("WorkspaceDir = %q, want %q", cfg.WorkspaceDir, "cfg-workspace")
	}
	if cfg.NetworkAllow != "cfg-allow" {
		t.Errorf("NetworkAllow = %q, want %q", cfg.NetworkAllow, "cfg-allow")
	}
	if cfg.AutoStop == nil || *cfg.AutoStop != stop {
		t.Errorf("AutoStop = %v, want %v", cfg.AutoStop, stop)
	}
	if cfg.AutoArchive == nil || *cfg.AutoArchive != archive {
		t.Errorf("AutoArchive = %v, want %v", cfg.AutoArchive, archive)
	}
	if cfg.AutoDelete == nil || *cfg.AutoDelete != del {
		t.Errorf("AutoDelete = %v, want %v", cfg.AutoDelete, del)
	}
}
