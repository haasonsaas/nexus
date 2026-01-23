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

func TestShouldIgnorePath(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"", false},
		{"file.txt", false},
		{"/home/user/file.txt", false},
		{".git", true},
		{".hidden", true},
		{"/home/.config", true},
		{"node_modules", true},
		{"/project/node_modules/pkg", true},
		{"./file.txt", false},
		{"../parent", false},
		{"/home/user/.gitignore", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := shouldIgnorePath(tt.path); got != tt.expected {
				t.Errorf("shouldIgnorePath(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestHost_Close_NilHost(t *testing.T) {
	var h *Host
	err := h.Close()
	if err != nil {
		t.Errorf("Close() error = %v, want nil", err)
	}
}

func TestHost_Start_NilHost(t *testing.T) {
	var h *Host
	err := h.Start(nil)
	if err != nil {
		t.Errorf("Start() error = %v, want nil", err)
	}
}

func TestHost_NamespacedPath(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.CanvasHostConfig{
		Port:      18793,
		Root:      tmpDir,
		Namespace: "/myns",
	}
	host, err := NewHost(cfg, nil)
	if err != nil {
		t.Fatalf("NewHost error: %v", err)
	}

	tests := []struct {
		suffix   string
		expected string
	}{
		{"canvas", "/myns/canvas"},
		{"/canvas", "/myns/canvas"},
		{"ws", "/myns/ws"},
	}

	for _, tt := range tests {
		t.Run(tt.suffix, func(t *testing.T) {
			if got := host.namespacedPath(tt.suffix); got != tt.expected {
				t.Errorf("namespacedPath(%q) = %q, want %q", tt.suffix, got, tt.expected)
			}
		})
	}
}

func TestHost_NamespacedPath_RootNamespace(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.CanvasHostConfig{
		Port:      18793,
		Root:      tmpDir,
		Namespace: "/",
	}
	host, err := NewHost(cfg, nil)
	if err != nil {
		t.Fatalf("NewHost error: %v", err)
	}

	if got := host.namespacedPath("canvas"); got != "/canvas" {
		t.Errorf("namespacedPath(\"canvas\") = %q, want %q", got, "/canvas")
	}
}

func TestHost_Prefixes(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.CanvasHostConfig{
		Port:      18793,
		Root:      tmpDir,
		Namespace: "/__nexus__",
	}
	host, err := NewHost(cfg, nil)
	if err != nil {
		t.Fatalf("NewHost error: %v", err)
	}

	if got := host.canvasPrefix(); got != "/__nexus__/canvas" {
		t.Errorf("canvasPrefix() = %q, want %q", got, "/__nexus__/canvas")
	}
	if got := host.a2uiPrefix(); got != "/__nexus__/a2ui" {
		t.Errorf("a2uiPrefix() = %q, want %q", got, "/__nexus__/a2ui")
	}
	if got := host.liveReloadWSPath(); got != "/__nexus__/ws" {
		t.Errorf("liveReloadWSPath() = %q, want %q", got, "/__nexus__/ws")
	}
	if got := host.liveReloadScriptPath(); got != "/__nexus__/live.js" {
		t.Errorf("liveReloadScriptPath() = %q, want %q", got, "/__nexus__/live.js")
	}
}

func TestHost_LiveReloadScript(t *testing.T) {
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
		t.Fatalf("NewHost error: %v", err)
	}

	script := host.liveReloadScript()

	// Check for expected content
	if !contains(script, "WebSocket") {
		t.Error("script should contain WebSocket")
	}
	if !contains(script, "reload") {
		t.Error("script should contain reload handler")
	}
	if !contains(script, host.liveReloadWSPath()) {
		t.Error("script should contain WS path")
	}
}

func TestHost_EnsureRoot(t *testing.T) {
	t.Run("creates missing directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		newRoot := tmpDir + "/newcanvas"
		cfg := config.CanvasHostConfig{
			Port:      18793,
			Root:      newRoot,
			Namespace: "/__nexus__",
		}
		host, err := NewHost(cfg, nil)
		if err != nil {
			t.Fatalf("NewHost error: %v", err)
		}

		if err := host.ensureRoot(); err != nil {
			t.Errorf("ensureRoot() error = %v", err)
		}
	})

	t.Run("accepts existing directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := config.CanvasHostConfig{
			Port:      18793,
			Root:      tmpDir,
			Namespace: "/__nexus__",
		}
		host, err := NewHost(cfg, nil)
		if err != nil {
			t.Fatalf("NewHost error: %v", err)
		}

		if err := host.ensureRoot(); err != nil {
			t.Errorf("ensureRoot() error = %v", err)
		}
	})
}

func TestDefaultIndexHTML(t *testing.T) {
	if defaultIndexHTML == "" {
		t.Error("defaultIndexHTML should not be empty")
	}
	if !contains(defaultIndexHTML, "<!doctype html>") {
		t.Error("defaultIndexHTML should contain doctype")
	}
	if !contains(defaultIndexHTML, "Nexus Canvas") {
		t.Error("defaultIndexHTML should contain Nexus Canvas")
	}
}
