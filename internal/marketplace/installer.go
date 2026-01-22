package marketplace

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

// Installer handles plugin installation, updates, and uninstallation.
type Installer struct {
	store    *Store
	registry *RegistryClient
	verifier *Verifier
	logger   *slog.Logger
}

// InstallerOption configures an Installer.
type InstallerOption func(*Installer)

// WithInstallerLogger sets the logger.
func WithInstallerLogger(logger *slog.Logger) InstallerOption {
	return func(i *Installer) {
		i.logger = logger
	}
}

// NewInstaller creates a new plugin installer.
func NewInstaller(store *Store, registry *RegistryClient, verifier *Verifier, opts ...InstallerOption) *Installer {
	i := &Installer{
		store:    store,
		registry: registry,
		verifier: verifier,
		logger:   slog.Default().With("component", "marketplace.installer"),
	}

	for _, opt := range opts {
		opt(i)
	}

	return i
}

// InstallResult contains the result of an installation.
type InstallResult struct {
	// Plugin is the installed plugin info.
	Plugin *pluginsdk.InstalledPlugin

	// Installed indicates a new installation.
	Installed bool

	// Updated indicates an update.
	Updated bool

	// PreviousVersion is the previous version if updated.
	PreviousVersion string
}

// Install installs a plugin from the marketplace.
func (i *Installer) Install(ctx context.Context, id string, opts pluginsdk.InstallOptions) (*InstallResult, error) {
	i.logger.Info("installing plugin", "id", id, "version", opts.Version)

	// Check if already installed
	if existing, ok := i.store.Get(id); ok && !opts.Force {
		return nil, fmt.Errorf("plugin already installed: %s (version %s). Use --force to reinstall", id, existing.Version)
	}

	// Get plugin manifest from registry
	manifest, registryURL, err := i.registry.GetPlugin(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find plugin: %w", err)
	}

	// Check version
	if opts.Version != "" && manifest.Version != opts.Version {
		return nil, fmt.Errorf("requested version %s not found (available: %s)", opts.Version, manifest.Version)
	}

	// Get artifact for current platform
	artifact := GetArtifactForPlatform(manifest)
	if artifact == nil {
		return nil, fmt.Errorf("no compatible artifact for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// Download artifact
	data, err := i.registry.DownloadArtifact(ctx, artifact)
	if err != nil {
		return nil, fmt.Errorf("download artifact: %w", err)
	}

	// Verify artifact
	if !opts.SkipVerify {
		result := i.verifier.VerifyArtifact(data, artifact)
		if !result.Valid {
			return nil, fmt.Errorf("artifact verification failed: %w", result.Error)
		}
		i.logger.Info("artifact verified",
			"checksum", result.ComputedChecksum,
			"signedBy", result.SignedBy)
	}

	// Extract and stage install
	var previousVersion string
	if existing, ok := i.store.Get(id); ok {
		previousVersion = existing.Version
	}

	stageDir, err := os.MkdirTemp(i.store.BasePath(), ".install-")
	if err != nil {
		return nil, fmt.Errorf("create staging dir: %w", err)
	}
	defer os.RemoveAll(stageDir)

	binaryPath, err := i.extractArtifactToDir(stageDir, data, artifact)
	if err != nil {
		return nil, fmt.Errorf("extract artifact: %w", err)
	}

	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}
	manifestPath := filepath.Join(stageDir, pluginsdk.ManifestFilename)
	if err := os.WriteFile(manifestPath, manifestData, 0o644); err != nil {
		return nil, fmt.Errorf("save manifest: %w", err)
	}
	if _, err := os.Stat(binaryPath); err != nil {
		return nil, fmt.Errorf("plugin binary missing: %w", err)
	}
	if _, err := os.Stat(manifestPath); err != nil {
		return nil, fmt.Errorf("plugin manifest missing: %w", err)
	}

	installPath := i.store.PluginPath(id)
	relBinary, err := filepath.Rel(stageDir, binaryPath)
	if err != nil {
		return nil, fmt.Errorf("resolve binary path: %w", err)
	}
	backupPath, hadExisting, err := stageInstall(stageDir, installPath, os.Rename)
	if err != nil {
		return nil, err
	}
	binaryPath = filepath.Join(installPath, relBinary)

	// Create installed plugin entry
	installed := &pluginsdk.InstalledPlugin{
		ID:           id,
		Version:      manifest.Version,
		Path:         installPath,
		BinaryPath:   binaryPath,
		ManifestPath: filepath.Join(installPath, pluginsdk.ManifestFilename),
		Checksum:     ComputeChecksum(data),
		Verified:     !opts.SkipVerify,
		InstalledAt:  time.Now(),
		UpdatedAt:    time.Now(),
		Source:       registryURL,
		AutoUpdate:   opts.AutoUpdate,
		Enabled:      true,
		Config:       opts.Config,
		Manifest:     manifest,
	}

	// Add to store
	if err := i.store.Add(installed); err != nil {
		if rollbackErr := rollbackInstall(installPath, backupPath, hadExisting); rollbackErr != nil {
			i.logger.Warn("failed to rollback install after store error", "error", rollbackErr)
		}
		return nil, fmt.Errorf("save to store: %w", err)
	}
	if backupPath != "" {
		if err := os.RemoveAll(backupPath); err != nil {
			i.logger.Warn("failed to remove backup after install", "path", backupPath, "error", err)
		}
	}

	i.logger.Info("plugin installed",
		"id", id,
		"version", manifest.Version,
		"path", installPath)

	result := &InstallResult{
		Plugin:    installed,
		Installed: previousVersion == "",
		Updated:   previousVersion != "",
	}
	if previousVersion != "" {
		result.PreviousVersion = previousVersion
	}

	return result, nil
}

// Update updates a plugin to the latest version.
func (i *Installer) Update(ctx context.Context, id string, opts pluginsdk.UpdateOptions) (*InstallResult, error) {
	i.logger.Info("updating plugin", "id", id)

	// Check if installed
	existing, ok := i.store.Get(id)
	if !ok {
		return nil, fmt.Errorf("plugin not installed: %s", id)
	}

	// Get latest manifest
	manifest, _, err := i.registry.GetPlugin(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find plugin: %w", err)
	}

	// Check if update is needed
	if manifest.Version == existing.Version && !opts.Force {
		return nil, fmt.Errorf("already at latest version: %s", manifest.Version)
	}

	// Install new version
	installOpts := pluginsdk.InstallOptions{
		Version:    opts.Version,
		Force:      true,
		SkipVerify: opts.SkipVerify,
		AutoUpdate: existing.AutoUpdate,
		Config:     existing.Config,
	}

	return i.Install(ctx, id, installOpts)
}

// Uninstall removes a plugin.
func (i *Installer) Uninstall(ctx context.Context, id string) error {
	i.logger.Info("uninstalling plugin", "id", id)

	// Check if installed
	existing, ok := i.store.Get(id)
	if !ok {
		return fmt.Errorf("plugin not installed: %s", id)
	}

	// Remove plugin directory
	if err := i.store.RemovePluginDir(id); err != nil {
		i.logger.Warn("failed to remove plugin directory",
			"id", id,
			"path", existing.Path,
			"error", err)
	}

	// Remove from store
	if err := i.store.Remove(id); err != nil {
		return fmt.Errorf("remove from store: %w", err)
	}

	i.logger.Info("plugin uninstalled",
		"id", id,
		"version", existing.Version)

	return nil
}

// VerifyInstalled verifies an installed plugin's integrity.
func (i *Installer) VerifyInstalled(ctx context.Context, id string) (*VerificationResult, error) {
	i.logger.Info("verifying installed plugin", "id", id)

	// Check if installed
	installed, ok := i.store.Get(id)
	if !ok {
		return nil, fmt.Errorf("plugin not installed: %s", id)
	}

	// Read binary
	data, err := os.ReadFile(installed.BinaryPath)
	if err != nil {
		return nil, fmt.Errorf("read binary: %w", err)
	}

	// Verify checksum
	result := i.verifier.VerifyChecksum(data, installed.Checksum)
	if !result.Valid {
		i.logger.Warn("plugin verification failed",
			"id", id,
			"error", result.Error)
	} else {
		i.logger.Info("plugin verification passed",
			"id", id,
			"checksum", result.ComputedChecksum)
	}

	return result, nil
}

// UpdateAll updates all plugins with auto-update enabled.
func (i *Installer) UpdateAll(ctx context.Context) ([]*InstallResult, error) {
	plugins := i.store.GetPluginsNeedingUpdate()
	if len(plugins) == 0 {
		return nil, nil
	}

	var results []*InstallResult
	var errors []error

	for _, plugin := range plugins {
		result, err := i.Update(ctx, plugin.ID, pluginsdk.UpdateOptions{})
		if err != nil {
			errors = append(errors, fmt.Errorf("%s: %w", plugin.ID, err))
			continue
		}
		if result.Updated {
			results = append(results, result)
		}
	}

	if len(errors) > 0 {
		i.logger.Warn("some updates failed", "errors", len(errors))
	}

	return results, nil
}

// extractArtifact extracts a downloaded artifact.
func (i *Installer) extractArtifactToDir(destDir string, data []byte, artifact *pluginsdk.PluginArtifact) (string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", fmt.Errorf("create plugin directory: %w", err)
	}

	var binaryPath string
	var err error
	format := artifact.Format
	if format == "" {
		format = detectFormat(artifact.URL)
	}

	switch format {
	case "so", "":
		// Raw .so file
		binaryPath = filepath.Join(destDir, "plugin.so")
		if err := os.WriteFile(binaryPath, data, 0o755); err != nil {
			return "", fmt.Errorf("write binary: %w", err)
		}

	case "tar.gz", "tgz":
		binaryPath, err = i.extractTarGz(destDir, data)
		if err != nil {
			return "", err
		}

	case "zip":
		binaryPath, err = i.extractZip(destDir, data)
		if err != nil {
			return "", err
		}

	default:
		return "", fmt.Errorf("unsupported artifact format: %s", format)
	}

	return binaryPath, nil
}

func stageInstall(tempDir, liveDir string, renameFn func(string, string) error) (string, bool, error) {
	info, err := os.Stat(liveDir)
	hasLive := false
	if err == nil {
		if !info.IsDir() {
			return "", true, fmt.Errorf("live path is not a directory: %s", liveDir)
		}
		hasLive = true
	} else if !os.IsNotExist(err) {
		return "", false, fmt.Errorf("stat live path: %w", err)
	}

	var backupPath string
	if hasLive {
		backupPath = fmt.Sprintf("%s.bak-%s", liveDir, time.Now().Format("20060102-150405"))
		if err := renameFn(liveDir, backupPath); err != nil {
			return "", true, fmt.Errorf("backup existing plugin: %w", err)
		}
	}

	if err := renameFn(tempDir, liveDir); err != nil {
		if hasLive && backupPath != "" {
			if rbErr := renameFn(backupPath, liveDir); rbErr != nil {
				return backupPath, hasLive, fmt.Errorf("activate plugin failed: %w; rollback failed: %v", err, rbErr)
			}
		}
		return backupPath, hasLive, fmt.Errorf("activate plugin failed: %w", err)
	}

	return backupPath, hasLive, nil
}

func rollbackInstall(liveDir, backupPath string, hadExisting bool) error {
	if hadExisting && backupPath != "" {
		failedPath := fmt.Sprintf("%s.failed-%s", liveDir, time.Now().Format("20060102-150405"))
		if err := os.Rename(liveDir, failedPath); err != nil {
			return fmt.Errorf("move failed install: %w", err)
		}
		if err := os.Rename(backupPath, liveDir); err != nil {
			return fmt.Errorf("restore backup: %w", err)
		}
		if err := os.RemoveAll(failedPath); err != nil {
			return fmt.Errorf("cleanup failed install: %w", err)
		}
		return nil
	}
	return os.RemoveAll(liveDir)
}

// extractTarGz extracts a .tar.gz archive.
func (i *Installer) extractTarGz(destDir string, data []byte) (string, error) {
	gzr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("open gzip: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	var binaryPath string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("read tar: %w", err)
		}

		// Sanitize path
		target := filepath.Join(destDir, filepath.Clean(header.Name))
		if !strings.HasPrefix(target, destDir) {
			continue // Skip paths outside destDir
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", fmt.Errorf("create directory: %w", err)
			}

		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return "", fmt.Errorf("create parent directory: %w", err)
			}

			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return "", fmt.Errorf("create file: %w", err)
			}

			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return "", fmt.Errorf("extract file: %w", err)
			}
			f.Close()

			// Check for binary
			if strings.HasSuffix(header.Name, ".so") {
				binaryPath = target
			}
		}
	}

	if binaryPath == "" {
		// Look for plugin.so
		binaryPath = filepath.Join(destDir, "plugin.so")
		if _, err := os.Stat(binaryPath); err != nil {
			return "", fmt.Errorf("no plugin binary found in archive")
		}
	}

	return binaryPath, nil
}

// extractZip extracts a .zip archive.
func (i *Installer) extractZip(destDir string, data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("open zip: %w", err)
	}

	var binaryPath string

	for _, f := range zr.File {
		// Sanitize path
		target := filepath.Join(destDir, filepath.Clean(f.Name))
		if !strings.HasPrefix(target, destDir) {
			continue
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return "", fmt.Errorf("create directory: %w", err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return "", fmt.Errorf("create parent directory: %w", err)
		}

		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("open file in zip: %w", err)
		}

		outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return "", fmt.Errorf("create file: %w", err)
		}

		if _, err := io.Copy(outFile, rc); err != nil {
			rc.Close()
			outFile.Close()
			return "", fmt.Errorf("extract file: %w", err)
		}

		rc.Close()
		outFile.Close()

		// Check for binary
		if strings.HasSuffix(f.Name, ".so") {
			binaryPath = target
		}
	}

	if binaryPath == "" {
		binaryPath = filepath.Join(destDir, "plugin.so")
		if _, err := os.Stat(binaryPath); err != nil {
			return "", fmt.Errorf("no plugin binary found in archive")
		}
	}

	return binaryPath, nil
}

// detectFormat detects the artifact format from URL.
func detectFormat(url string) string {
	url = strings.ToLower(url)
	if strings.HasSuffix(url, ".tar.gz") || strings.HasSuffix(url, ".tgz") {
		return "tar.gz"
	}
	if strings.HasSuffix(url, ".zip") {
		return "zip"
	}
	if strings.HasSuffix(url, ".so") {
		return "so"
	}
	return ""
}

// CheckUpdates checks for available updates.
func (i *Installer) CheckUpdates(ctx context.Context) (map[string]string, error) {
	updates := make(map[string]string)

	for _, installed := range i.store.List() {
		manifest, _, err := i.registry.GetPlugin(ctx, installed.ID)
		if err != nil {
			i.logger.Debug("failed to check update",
				"id", installed.ID,
				"error", err)
			continue
		}

		if manifest.Version != installed.Version {
			updates[installed.ID] = manifest.Version
		}
	}

	return updates, nil
}
