package canvas

import (
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
)

func TestNewHost(t *testing.T) {
	t.Run("creates host with valid config", func(t *testing.T) {
		tmpDir := t.TempDir()
		liveReload := true
		cfg := config.CanvasHostConfig{
			Port:       18793,
			Root:       tmpDir,
			Namespace:  "/__nexus__",
			LiveReload: &liveReload,
		}
		host, err := NewHost(cfg, nil)
		if err != nil {
			t.Errorf("NewHost error: %v", err)
		}
		if host == nil {
			t.Error("expected non-nil host")
		}
	})

	t.Run("returns error for empty root", func(t *testing.T) {
		cfg := config.CanvasHostConfig{
			Port:      18793,
			Root:      "",
			Namespace: "/__nexus__",
		}
		_, err := NewHost(cfg, nil)
		if err == nil {
			t.Error("expected error for empty root")
		}
	})

	t.Run("returns error for whitespace root", func(t *testing.T) {
		cfg := config.CanvasHostConfig{
			Port:      18793,
			Root:      "   ",
			Namespace: "/__nexus__",
		}
		_, err := NewHost(cfg, nil)
		if err == nil {
			t.Error("expected error for whitespace root")
		}
	})

	t.Run("returns error for zero port", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := config.CanvasHostConfig{
			Port:      0,
			Root:      tmpDir,
			Namespace: "/__nexus__",
		}
		_, err := NewHost(cfg, nil)
		if err == nil {
			t.Error("expected error for zero port")
		}
	})

	t.Run("returns error for negative port", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := config.CanvasHostConfig{
			Port:      -1,
			Root:      tmpDir,
			Namespace: "/__nexus__",
		}
		_, err := NewHost(cfg, nil)
		if err == nil {
			t.Error("expected error for negative port")
		}
	})
}

func TestHost_CanvasURL(t *testing.T) {
	t.Run("returns empty for nil host", func(t *testing.T) {
		var h *Host
		if h.CanvasURL("") != "" {
			t.Error("expected empty URL for nil host")
		}
	})

	t.Run("uses request host when bound to 0.0.0.0", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := config.CanvasHostConfig{
			Host:      "0.0.0.0",
			Port:      18793,
			Root:      tmpDir,
			Namespace: "/__nexus__",
		}
		host, err := NewHost(cfg, nil)
		if err != nil {
			t.Fatalf("NewHost error: %v", err)
		}
		url := host.CanvasURL("nexus.example.com")
		if url != "http://nexus.example.com:18793/__nexus__/canvas/" {
			t.Errorf("CanvasURL() = %q, want %q", url, "http://nexus.example.com:18793/__nexus__/canvas/")
		}
	})

	t.Run("uses localhost when host is empty and no request host", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := config.CanvasHostConfig{
			Host:      "",
			Port:      18793,
			Root:      tmpDir,
			Namespace: "/__nexus__",
		}
		host, err := NewHost(cfg, nil)
		if err != nil {
			t.Fatalf("NewHost error: %v", err)
		}
		url := host.CanvasURL("")
		if url != "http://localhost:18793/__nexus__/canvas/" {
			t.Errorf("CanvasURL() = %q, want %q", url, "http://localhost:18793/__nexus__/canvas/")
		}
	})

	t.Run("returns specified host", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := config.CanvasHostConfig{
			Host:      "192.168.1.100",
			Port:      8080,
			Root:      tmpDir,
			Namespace: "/myapp",
		}
		host, err := NewHost(cfg, nil)
		if err != nil {
			t.Fatalf("NewHost error: %v", err)
		}
		url := host.CanvasURL("")
		if url != "http://192.168.1.100:8080/myapp/canvas/" {
			t.Errorf("CanvasURL() = %q, want %q", url, "http://192.168.1.100:8080/myapp/canvas/")
		}
	})

	t.Run("formats IPv6 host correctly", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := config.CanvasHostConfig{
			Host:      "::1",
			Port:      18793,
			Root:      tmpDir,
			Namespace: "/__nexus__",
		}
		host, err := NewHost(cfg, nil)
		if err != nil {
			t.Fatalf("NewHost error: %v", err)
		}
		url := host.CanvasURL("")
		if url != "http://[::1]:18793/__nexus__/canvas/" {
			t.Errorf("CanvasURL() = %q, want %q", url, "http://[::1]:18793/__nexus__/canvas/")
		}
	})
}

func TestNormalizeNamespace(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		expected  string
	}{
		{"empty becomes default", "", "/__nexus__"},
		{"adds leading slash", "canvas", "/canvas"},
		{"keeps leading slash", "/canvas", "/canvas"},
		{"removes trailing slash", "/canvas/", "/canvas"},
		{"handles whitespace", "  /canvas  ", "/canvas"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeNamespace(tt.namespace)
			if result != tt.expected {
				t.Errorf("normalizeNamespace(%q) = %q, want %q", tt.namespace, result, tt.expected)
			}
		})
	}
}

func TestInjectLiveReload(t *testing.T) {
	tmpDir := t.TempDir()
	liveReload := true
	injectClient := true
	cfg := config.CanvasHostConfig{
		Port:         18793,
		Root:         tmpDir,
		Namespace:    "/__nexus__",
		LiveReload:   &liveReload,
		InjectClient: &injectClient,
	}
	host, _ := NewHost(cfg, nil)

	t.Run("injects before closing body", func(t *testing.T) {
		html := `<html><body><h1>Test</h1></body></html>`
		result := host.injectLiveReload(html)
		if !contains(result, "<script src=") || !contains(result, "</body>") {
			t.Errorf("expected script injection before </body>, got: %s", result)
		}
	})

	t.Run("injects before closing head if no body", func(t *testing.T) {
		html := `<html><head><title>Test</title></head></html>`
		result := host.injectLiveReload(html)
		if !contains(result, "<script src=") {
			t.Errorf("expected script injection, got: %s", result)
		}
	})

	t.Run("appends if no body or head", func(t *testing.T) {
		html := `<html><h1>Test</h1></html>`
		result := host.injectLiveReload(html)
		if !contains(result, "<script src=") {
			t.Errorf("expected script appended, got: %s", result)
		}
	})

	t.Run("skips if already injected", func(t *testing.T) {
		scriptPath := host.liveReloadScriptPath()
		html := `<html><body><script src="` + scriptPath + `"></script></body></html>`
		result := host.injectLiveReload(html)
		// Count script tags
		count := 0
		for i := 0; i < len(result)-7; i++ {
			if result[i:i+7] == "<script" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("expected 1 script tag, got %d in: %s", count, result)
		}
	})
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
