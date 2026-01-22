package security

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewAuditor(t *testing.T) {
	opts := DefaultAuditOptions()
	auditor := NewAuditor(opts)

	if auditor == nil {
		t.Fatal("NewAuditor returned nil")
	}
}

func TestAuditFilesystemPermissions(t *testing.T) {
	// Create a temp directory for testing
	tmpDir := t.TempDir()

	// Create a test config file with loose permissions
	configPath := filepath.Join(tmpDir, "nexus.yaml")
	if err := os.WriteFile(configPath, []byte("test: true\n"), 0644); err != nil {
		t.Fatal(err)
	}

	opts := AuditOptions{
		ConfigPath:        configPath,
		StateDir:          tmpDir,
		IncludeFilesystem: true,
		IncludeGateway:    false, // Skip gateway checks as config is minimal
	}

	auditor := NewAuditor(opts)
	report, err := auditor.Audit(context.Background())

	if err != nil {
		t.Fatalf("Audit failed: %v", err)
	}

	// Should find the world-readable config file
	found := false
	for _, f := range report.Findings {
		if f.CheckID == "fs.config.perms_world_readable" {
			found = true
			if f.Severity != SeverityCritical {
				t.Errorf("Expected critical severity, got %s", f.Severity)
			}
		}
	}

	if !found {
		t.Error("Expected to find world-readable config finding")
	}
}

func TestAuditWorldWritableDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a world-writable subdirectory
	credsDir := filepath.Join(tmpDir, "credentials")
	if err := os.Mkdir(credsDir, 0777); err != nil {
		t.Fatal(err)
	}
	// Explicitly set permissions to override umask
	if err := os.Chmod(credsDir, 0777); err != nil {
		t.Fatal(err)
	}

	opts := AuditOptions{
		StateDir:          tmpDir,
		IncludeFilesystem: true,
		IncludeGateway:    false,
	}

	auditor := NewAuditor(opts)
	report, err := auditor.Audit(context.Background())

	if err != nil {
		t.Fatalf("Audit failed: %v", err)
	}

	// Should find the world-writable credentials directory
	found := false
	for _, f := range report.Findings {
		if f.CheckID == "fs.credentials_dir.perms_world_writable" {
			found = true
			if f.Severity != SeverityCritical {
				t.Errorf("Expected critical severity, got %s", f.Severity)
			}
		}
	}

	if !found {
		t.Error("Expected to find world-writable credentials_dir finding")
	}
}

func TestCountBySeverity(t *testing.T) {
	findings := []Finding{
		{CheckID: "test1", Severity: SeverityCritical},
		{CheckID: "test2", Severity: SeverityCritical},
		{CheckID: "test3", Severity: SeverityWarn},
		{CheckID: "test4", Severity: SeverityInfo},
		{CheckID: "test5", Severity: SeverityInfo},
		{CheckID: "test6", Severity: SeverityInfo},
	}

	summary := countBySeverity(findings)

	if summary.Critical != 2 {
		t.Errorf("Expected 2 critical, got %d", summary.Critical)
	}
	if summary.Warn != 1 {
		t.Errorf("Expected 1 warn, got %d", summary.Warn)
	}
	if summary.Info != 3 {
		t.Errorf("Expected 3 info, got %d", summary.Info)
	}
}

func TestFormatReport(t *testing.T) {
	report := &Report{
		Timestamp: 1705900000,
		Summary: Summary{
			Critical: 1,
			Warn:     2,
			Info:     0,
		},
		Findings: []Finding{
			{
				CheckID:     "test.critical",
				Severity:    SeverityCritical,
				Title:       "Critical Issue",
				Detail:      "Something is very wrong.",
				Remediation: "Fix it now.",
			},
			{
				CheckID:  "test.warn1",
				Severity: SeverityWarn,
				Title:    "Warning Issue",
				Detail:   "Something might be wrong.",
			},
		},
	}

	output := FormatReport(report)

	// Check that key elements are present
	if output == "" {
		t.Error("FormatReport returned empty string")
	}

	if !contains(output, "Critical: 1") {
		t.Error("Report should contain critical count")
	}
	if !contains(output, "Warnings: 2") {
		t.Error("Report should contain warning count")
	}
	if !contains(output, "[CRITICAL]") {
		t.Error("Report should contain CRITICAL label")
	}
	if !contains(output, "Fix it now.") {
		t.Error("Report should contain remediation")
	}
}

func TestTitleCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"state_dir", "State Dir"},
		{"config", "Config"},
		{"credentials_dir", "Credentials Dir"},
		{"sensitive_file", "Sensitive File"},
	}

	for _, tc := range tests {
		result := titleCase(tc.input)
		if result != tc.expected {
			t.Errorf("titleCase(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr, 0))
}

func containsAt(s, substr string, start int) bool {
	for i := start; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
