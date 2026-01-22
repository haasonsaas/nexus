package doctor

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
)

func TestAuditSecurityFlagsInsecurePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not reliable on windows")
	}

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nexus.yaml")
	if err := os.WriteFile(cfgPath, []byte("server:\n  host: 127.0.0.1\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.Chmod(cfgPath, 0o666); err != nil {
		t.Fatalf("chmod config: %v", err)
	}

	workspace := filepath.Join(dir, "workspace")
	if err := os.MkdirAll(workspace, 0o777); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := os.Chmod(workspace, 0o777); err != nil {
		t.Fatalf("chmod workspace: %v", err)
	}

	cfg := &config.Config{
		Server: config.ServerConfig{Host: "127.0.0.1"},
		Workspace: config.WorkspaceConfig{
			Path: workspace,
		},
	}

	audit := AuditSecurity(cfg, cfgPath)
	if len(audit.Findings) == 0 {
		t.Fatal("expected security findings")
	}

	if !hasSeverity(audit.Findings, SeverityCritical, "writable") {
		t.Fatalf("expected critical finding for writable perms: %#v", audit.Findings)
	}
	if !hasSeverity(audit.Findings, SeverityWarning, "readable") {
		t.Fatalf("expected warning finding for readable perms: %#v", audit.Findings)
	}
}

func TestAuditSecurityFlagsPublicBindWithoutAuth(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{Host: "0.0.0.0"},
	}

	audit := AuditSecurity(cfg, "")
	if !hasSeverity(audit.Findings, SeverityCritical, "publicly reachable") {
		t.Fatalf("expected critical finding for public bind: %#v", audit.Findings)
	}
}

func hasSeverity(findings []SecurityFinding, severity SecuritySeverity, contains string) bool {
	for _, finding := range findings {
		if finding.Severity != severity {
			continue
		}
		if contains == "" || strings.Contains(finding.Message, contains) {
			return true
		}
	}
	return false
}
