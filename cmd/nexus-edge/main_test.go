package main

import "testing"

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.CoreURL == "" {
		t.Fatalf("expected CoreURL to be set")
	}
	if cfg.EdgeID == "" {
		t.Fatalf("expected EdgeID to be set")
	}
	if cfg.Name == "" {
		t.Fatalf("expected Name to be set")
	}
	if cfg.ReconnectDelay == 0 {
		t.Fatalf("expected ReconnectDelay to be set")
	}
	if cfg.HeartbeatInterval == 0 {
		t.Fatalf("expected HeartbeatInterval to be set")
	}
}
