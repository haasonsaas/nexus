package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPermissionChecks(t *testing.T) {
	tests := []struct {
		name      string
		mode      os.FileMode
		checkFunc func(os.FileMode) bool
		want      bool
	}{
		{"world writable 777", 0777, isWorldWritable, true},
		{"world writable 666", 0666, isWorldWritable, true},
		{"not world writable 755", 0755, isWorldWritable, false},
		{"not world writable 644", 0644, isWorldWritable, false},
		{"not world writable 600", 0600, isWorldWritable, false},

		{"group writable 775", 0775, isGroupWritable, true},
		{"group writable 664", 0664, isGroupWritable, true},
		{"not group writable 755", 0755, isGroupWritable, false},
		{"not group writable 644", 0644, isGroupWritable, false},

		{"world readable 644", 0644, isWorldReadable, true},
		{"world readable 755", 0755, isWorldReadable, true},
		{"not world readable 640", 0640, isWorldReadable, false},
		{"not world readable 600", 0600, isWorldReadable, false},

		{"group readable 640", 0640, isGroupReadable, true},
		{"group readable 644", 0644, isGroupReadable, true},
		{"not group readable 600", 0600, isGroupReadable, false},
		{"not group readable 700", 0700, isGroupReadable, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.checkFunc(tt.mode)
			if got != tt.want {
				t.Errorf("check(%o) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}

func TestIsSensitiveFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/path/to/api_key.txt", true},
		{"/path/to/secret.json", true},
		{"/path/to/token.txt", true},
		{"/path/to/credentials.yaml", true},
		{"/path/to/password.txt", true},
		{"/path/to/private.key", true},
		{"/path/to/cert.pem", true},
		{"/path/to/id_rsa", true},
		{"/path/to/id_ed25519", true},
		{"/path/to/.env", true},
		{"/path/to/.env.local", true},
		{"/path/to/.env.production", true},

		{"/path/to/readme.md", false},
		{"/path/to/main.go", false},
		{"/path/to/config.yaml", false},
		{"/path/to/data.json", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := isSensitiveFile(tt.path)
			if got != tt.want {
				t.Errorf("isSensitiveFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestAuditor_WorldWritableDir(t *testing.T) {
	dir := t.TempDir()
	testDir := filepath.Join(dir, "test")

	if err := os.Mkdir(testDir, 0700); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}
	// Chmod to bypass umask
	if err := os.Chmod(testDir, 0777); err != nil {
		t.Fatalf("failed to chmod test dir: %v", err)
	}

	auditor := NewAuditor(AuditConfig{
		StateDir: testDir,
	})

	result, err := auditor.Run()
	if err != nil {
		t.Fatalf("audit failed: %v", err)
	}

	if !result.HasCritical() {
		t.Error("expected critical finding for world-writable directory")
	}

	found := false
	for _, f := range result.Findings {
		if f.CheckID == "FS-002" {
			found = true
			if f.Severity != SeverityCritical {
				t.Errorf("FS-002 severity = %v, want critical", f.Severity)
			}
		}
	}
	if !found {
		t.Error("expected FS-002 finding for world-writable directory")
	}
}

func TestAuditor_GroupWritableDir(t *testing.T) {
	dir := t.TempDir()
	testDir := filepath.Join(dir, "test")

	if err := os.Mkdir(testDir, 0700); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}
	// Chmod to bypass umask
	if err := os.Chmod(testDir, 0775); err != nil {
		t.Fatalf("failed to chmod test dir: %v", err)
	}

	auditor := NewAuditor(AuditConfig{
		StateDir: testDir,
	})

	result, err := auditor.Run()
	if err != nil {
		t.Fatalf("audit failed: %v", err)
	}

	if !result.HasHighOrAbove() {
		t.Error("expected high severity finding for group-writable directory")
	}

	found := false
	for _, f := range result.Findings {
		if f.CheckID == "FS-003" {
			found = true
		}
	}
	if !found {
		t.Error("expected FS-003 finding for group-writable directory")
	}
}

func TestAuditor_SecureDir(t *testing.T) {
	dir := t.TempDir()
	testDir := filepath.Join(dir, "test")

	if err := os.Mkdir(testDir, 0700); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}

	auditor := NewAuditor(AuditConfig{
		StateDir: testDir,
	})

	result, err := auditor.Run()
	if err != nil {
		t.Fatalf("audit failed: %v", err)
	}

	if len(result.Findings) != 0 {
		t.Errorf("expected no findings for secure directory, got %d", len(result.Findings))
		for _, f := range result.Findings {
			t.Logf("  - %s: %s", f.CheckID, f.Title)
		}
	}
}

func TestAuditor_WorldWritableConfigFile(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(configFile, []byte("test: true"), 0600); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}
	// Chmod to bypass umask
	if err := os.Chmod(configFile, 0666); err != nil {
		t.Fatalf("failed to chmod config file: %v", err)
	}

	auditor := NewAuditor(AuditConfig{
		ConfigFile: configFile,
	})

	result, err := auditor.Run()
	if err != nil {
		t.Fatalf("audit failed: %v", err)
	}

	if !result.HasCritical() {
		t.Error("expected critical finding for world-writable config file")
	}

	found := false
	for _, f := range result.Findings {
		if f.CheckID == "FS-011" {
			found = true
		}
	}
	if !found {
		t.Error("expected FS-011 finding for world-writable config file")
	}
}

func TestAuditor_WorldReadableConfigFile(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(configFile, []byte("test: true"), 0644); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}

	auditor := NewAuditor(AuditConfig{
		ConfigFile: configFile,
	})

	result, err := auditor.Run()
	if err != nil {
		t.Fatalf("audit failed: %v", err)
	}

	if !result.HasHighOrAbove() {
		t.Error("expected high severity finding for world-readable config file")
	}

	found := false
	for _, f := range result.Findings {
		if f.CheckID == "FS-013" {
			found = true
		}
	}
	if !found {
		t.Error("expected FS-013 finding for world-readable config file")
	}
}

func TestAuditor_SecureConfigFile(t *testing.T) {
	dir := t.TempDir()
	configFile := filepath.Join(dir, "config.yaml")

	if err := os.WriteFile(configFile, []byte("test: true"), 0600); err != nil {
		t.Fatalf("failed to create config file: %v", err)
	}

	auditor := NewAuditor(AuditConfig{
		ConfigFile: configFile,
	})

	result, err := auditor.Run()
	if err != nil {
		t.Fatalf("audit failed: %v", err)
	}

	if len(result.Findings) != 0 {
		t.Errorf("expected no findings for secure config file, got %d", len(result.Findings))
		for _, f := range result.Findings {
			t.Logf("  - %s: %s", f.CheckID, f.Title)
		}
	}
}

func TestAuditor_SymlinkDetection(t *testing.T) {
	dir := t.TempDir()
	realDir := filepath.Join(dir, "real")
	linkDir := filepath.Join(dir, "link")

	if err := os.Mkdir(realDir, 0700); err != nil {
		t.Fatalf("failed to create real dir: %v", err)
	}

	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	auditor := NewAuditor(AuditConfig{
		StateDir:      linkDir,
		CheckSymlinks: true,
	})

	result, err := auditor.Run()
	if err != nil {
		t.Fatalf("audit failed: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if f.CheckID == "FS-001" {
			found = true
		}
	}
	if !found {
		t.Error("expected FS-001 finding for symlink directory")
	}
}

func TestAuditor_SensitiveFileInDir(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, "state")

	if err := os.Mkdir(stateDir, 0700); err != nil {
		t.Fatalf("failed to create state dir: %v", err)
	}

	// Create a sensitive file with bad permissions
	secretFile := filepath.Join(stateDir, "api_key.txt")
	if err := os.WriteFile(secretFile, []byte("secret123"), 0644); err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}

	auditor := NewAuditor(AuditConfig{
		StateDir: stateDir,
	})

	result, err := auditor.Run()
	if err != nil {
		t.Fatalf("audit failed: %v", err)
	}

	found := false
	for _, f := range result.Findings {
		if f.CheckID == "FS-005" {
			found = true
		}
	}
	if !found {
		t.Error("expected FS-005 finding for world-readable sensitive file")
	}
}

func TestAuditor_NonexistentPaths(t *testing.T) {
	auditor := NewAuditor(AuditConfig{
		StateDir:   "/nonexistent/path/to/dir",
		ConfigFile: "/nonexistent/path/to/config.yaml",
	})

	result, err := auditor.Run()
	if err != nil {
		t.Fatalf("audit should not fail for nonexistent paths: %v", err)
	}

	if len(result.Findings) != 0 {
		t.Errorf("expected no findings for nonexistent paths, got %d", len(result.Findings))
	}
}

func TestAuditResult_CountBySeverity(t *testing.T) {
	result := &AuditResult{
		Findings: []Finding{
			{CheckID: "1", Severity: SeverityCritical},
			{CheckID: "2", Severity: SeverityHigh},
			{CheckID: "3", Severity: SeverityHigh},
			{CheckID: "4", Severity: SeverityMedium},
			{CheckID: "5", Severity: SeverityLow},
			{CheckID: "6", Severity: SeverityLow},
			{CheckID: "7", Severity: SeverityLow},
		},
	}

	counts := result.CountBySeverity()

	if counts[SeverityCritical] != 1 {
		t.Errorf("critical count = %d, want 1", counts[SeverityCritical])
	}
	if counts[SeverityHigh] != 2 {
		t.Errorf("high count = %d, want 2", counts[SeverityHigh])
	}
	if counts[SeverityMedium] != 1 {
		t.Errorf("medium count = %d, want 1", counts[SeverityMedium])
	}
	if counts[SeverityLow] != 3 {
		t.Errorf("low count = %d, want 3", counts[SeverityLow])
	}
}

func TestValidatePermissions(t *testing.T) {
	dir := t.TempDir()

	// Test secure file
	secureFile := filepath.Join(dir, "secure.txt")
	if err := os.WriteFile(secureFile, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := ValidatePermissions(secureFile, 0600); err != nil {
		t.Errorf("secure file should pass validation: %v", err)
	}

	// Test insecure file
	insecureFile := filepath.Join(dir, "insecure.txt")
	if err := os.WriteFile(insecureFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ValidatePermissions(insecureFile, 0600); err == nil {
		t.Error("insecure file should fail validation")
	}
}

func TestCheckPath(t *testing.T) {
	dir := t.TempDir()

	// Create insecure directory
	insecureDir := filepath.Join(dir, "insecure")
	if err := os.Mkdir(insecureDir, 0777); err != nil {
		t.Fatal(err)
	}

	findings, err := CheckPath(insecureDir)
	if err != nil {
		t.Fatalf("CheckPath failed: %v", err)
	}

	if len(findings) == 0 {
		t.Error("expected findings for insecure directory")
	}

	// Create secure file
	secureFile := filepath.Join(dir, "secure.txt")
	if err := os.WriteFile(secureFile, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}

	findings, err = CheckPath(secureFile)
	if err != nil {
		t.Fatalf("CheckPath failed: %v", err)
	}

	if len(findings) != 0 {
		t.Errorf("expected no findings for secure file, got %d", len(findings))
	}
}
