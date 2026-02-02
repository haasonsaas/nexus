package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/tools/sandbox"
	"github.com/haasonsaas/nexus/pkg/pluginsdk"
)

const (
	daytonaRunnerDefaultName  = "nexus-plugin-runner"
	daytonaWorkspacePluginDir = "plugin"
	daytonaConfigFilename     = "plugin-config.json"
	daytonaParamsFilename     = "tool-params.json"
)

type daytonaRuntimePluginLoader struct {
	cfg       config.PluginIsolationConfig
	runnerMu  sync.Mutex
	runner    *daytonaPluginRunner
	runnerErr error
}

func newDaytonaRuntimePluginLoader(cfg config.PluginIsolationConfig) runtimePluginLoader {
	return &daytonaRuntimePluginLoader{cfg: cfg}
}

func (l *daytonaRuntimePluginLoader) Load(pluginID string, path string) (pluginsdk.RuntimePlugin, error) {
	info, err := LoadManifestForPath(path)
	if err != nil {
		return nil, err
	}
	manifest := info.Manifest
	if manifest == nil {
		return nil, fmt.Errorf("plugin manifest not found at %s", path)
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	if pluginID != "" && strings.TrimSpace(manifest.ID) != "" && manifest.ID != pluginID {
		return nil, fmt.Errorf("runtime plugin id mismatch: expected %q got %q", pluginID, manifest.ID)
	}
	if hasUnsupportedIsolationCapabilities(manifest) {
		return nil, fmt.Errorf("%w: plugin %q declares non-tool capabilities", ErrIsolationUnsupported, manifest.ID)
	}

	runner, err := l.ensureRunner()
	if err != nil {
		return nil, err
	}

	return &daytonaRuntimePlugin{
		id:         manifest.ID,
		manifest:   manifest,
		pluginPath: path,
		runner:     runner,
	}, nil
}

func (l *daytonaRuntimePluginLoader) ensureRunner() (*daytonaPluginRunner, error) {
	l.runnerMu.Lock()
	defer l.runnerMu.Unlock()
	if l.runner != nil || l.runnerErr != nil {
		return l.runner, l.runnerErr
	}
	runner, err := newDaytonaPluginRunner(l.cfg)
	if err != nil {
		l.runnerErr = err
		return nil, err
	}
	l.runner = runner
	return runner, nil
}

type daytonaRuntimePlugin struct {
	id         string
	manifest   *pluginsdk.Manifest
	pluginPath string
	runner     *daytonaPluginRunner
	toolsOnce  sync.Once
	tools      []pluginsdk.ToolDefinition
	toolsErr   error
}

func (p *daytonaRuntimePlugin) Manifest() *pluginsdk.Manifest {
	return p.manifest
}

func (p *daytonaRuntimePlugin) RegisterChannels(registry pluginsdk.ChannelRegistry, cfg map[string]any) error {
	return nil
}

func (p *daytonaRuntimePlugin) RegisterTools(registry pluginsdk.ToolRegistry, cfg map[string]any) error {
	if registry == nil {
		return nil
	}
	p.toolsOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), p.runner.defaultTimeout)
		defer cancel()
		p.tools, p.toolsErr = p.runner.ListTools(ctx, p.pluginPath, cfg, p.id)
	})
	if p.toolsErr != nil {
		return p.toolsErr
	}
	handlers := make(map[string]pluginsdk.ToolHandler, len(p.tools))
	for _, tool := range p.tools {
		toolName := tool.Name
		if toolName == "" {
			return fmt.Errorf("runtime plugin tool name is required")
		}
		if _, exists := handlers[toolName]; exists {
			return fmt.Errorf("runtime plugin tool %q already registered", toolName)
		}
		handler := func(ctx context.Context, params json.RawMessage) (*pluginsdk.ToolResult, error) {
			return p.runner.ExecTool(ctx, p.pluginPath, cfg, p.id, toolName, params)
		}
		if err := registry.RegisterTool(tool, handler); err != nil {
			return err
		}
		handlers[toolName] = handler
	}
	return nil
}

type daytonaPluginRunner struct {
	runner         *sandbox.DaytonaRunner
	runnerPath     string
	defaultTimeout time.Duration
}

func newDaytonaPluginRunner(cfg config.PluginIsolationConfig) (*daytonaPluginRunner, error) {
	runnerPath := strings.TrimSpace(cfg.RunnerPath)
	if runnerPath == "" {
		path, err := exec.LookPath(daytonaRunnerDefaultName)
		if err != nil {
			return nil, fmt.Errorf("%w: %s not found in PATH (set plugins.isolation.runner_path)", ErrIsolationUnavailable, daytonaRunnerDefaultName)
		}
		runnerPath = path
	}

	memMB, err := parseMemoryMB(cfg.Limits.MaxMemory)
	if err != nil {
		return nil, fmt.Errorf("invalid plugins.isolation.limits.max_memory: %w", err)
	}
	defaultTimeout := cfg.Timeout
	if defaultTimeout <= 0 {
		defaultTimeout = 30 * time.Second
	}
	options := sandbox.DaytonaRunnerOptions{
		DefaultCPU:      cfg.Limits.MaxCPU,
		DefaultMemoryMB: memMB,
		DefaultTimeout:  defaultTimeout,
		NetworkEnabled:  cfg.NetworkEnabled,
		WorkspaceAccess: sandbox.WorkspaceReadOnly,
	}

	daytonaCfg := sandbox.DaytonaConfig{
		APIKey:         cfg.Daytona.APIKey,
		JWTToken:       cfg.Daytona.JWTToken,
		OrganizationID: cfg.Daytona.OrganizationID,
		APIURL:         cfg.Daytona.APIURL,
		Target:         cfg.Daytona.Target,
		Snapshot:       cfg.Daytona.Snapshot,
		Image:          cfg.Daytona.Image,
		SandboxClass:   cfg.Daytona.SandboxClass,
		WorkspaceDir:   cfg.Daytona.WorkspaceDir,
		NetworkAllow:   cfg.Daytona.NetworkAllow,
		ReuseSandbox:   cfg.Daytona.ReuseSandbox,
		AutoStop:       cfg.Daytona.AutoStop,
		AutoArchive:    cfg.Daytona.AutoArchive,
		AutoDelete:     cfg.Daytona.AutoDelete,
	}

	runner, err := sandbox.NewDaytonaRunner(daytonaCfg, options)
	if err != nil {
		return nil, err
	}

	return &daytonaPluginRunner{
		runner:         runner,
		runnerPath:     runnerPath,
		defaultTimeout: defaultTimeout,
	}, nil
}

func (r *daytonaPluginRunner) ListTools(ctx context.Context, pluginPath string, cfg map[string]any, pluginID string) ([]pluginsdk.ToolDefinition, error) {
	workspace, pluginRel, runnerRel, cleanup, err := preparePluginWorkspace(pluginPath, pluginID, r.runnerPath)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	configRel, err := writeIsolationConfigFile(workspace, cfg)
	if err != nil {
		return nil, err
	}

	command := fmt.Sprintf("./%s list-tools --plugin %s", runnerRel, pluginRel)
	if configRel != "" {
		command += fmt.Sprintf(" --config-file %s", configRel)
	}

	payload, runErr := r.runCommand(ctx, workspace, command, nil)

	var resp toolListResponse
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &resp); err != nil {
			if runErr != nil {
				return nil, runErr
			}
			return nil, fmt.Errorf("parse tool list response: %w", err)
		}
		if resp.Error != "" {
			return nil, errors.New(resp.Error)
		}
	}
	if runErr != nil {
		return nil, runErr
	}
	return resp.Tools, nil
}

func (r *daytonaPluginRunner) ExecTool(ctx context.Context, pluginPath string, cfg map[string]any, pluginID string, toolName string, params json.RawMessage) (*pluginsdk.ToolResult, error) {
	if toolName == "" {
		return nil, fmt.Errorf("tool name is required")
	}
	workspace, pluginRel, runnerRel, cleanup, err := preparePluginWorkspace(pluginPath, pluginID, r.runnerPath)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	configRel, err := writeIsolationConfigFile(workspace, cfg)
	if err != nil {
		return nil, err
	}

	paramsRel, err := writeIsolationParamsFile(workspace, params)
	if err != nil {
		return nil, err
	}

	command := fmt.Sprintf("./%s exec-tool --plugin %s --tool %s", runnerRel, pluginRel, toolName)
	if configRel != "" {
		command += fmt.Sprintf(" --config-file %s", configRel)
	}
	if paramsRel != "" {
		command += fmt.Sprintf(" --params-file %s", paramsRel)
	}

	payload, runErr := r.runCommand(ctx, workspace, command, nil)

	var resp toolExecResponse
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &resp); err != nil {
			if runErr != nil {
				return nil, runErr
			}
			return nil, fmt.Errorf("parse tool response: %w", err)
		}
		if resp.Error != "" {
			return nil, errors.New(resp.Error)
		}
	}
	if runErr != nil {
		return nil, runErr
	}
	if resp.Result == nil {
		return nil, fmt.Errorf("tool result missing")
	}
	return resp.Result, nil
}

func (r *daytonaPluginRunner) runCommand(ctx context.Context, workspace string, command string, params *sandbox.ExecuteParams) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := r.runner.RunCommand(ctx, workspace, command, params)
	if err != nil {
		return nil, err
	}
	output := strings.TrimSpace(result.Stdout)
	if result.ExitCode != 0 {
		if output == "" {
			return nil, fmt.Errorf("plugin runner failed (exit %d)", result.ExitCode)
		}
		return []byte(output), fmt.Errorf("plugin runner failed (exit %d)", result.ExitCode)
	}
	return []byte(output), nil
}

type toolListResponse struct {
	Tools []pluginsdk.ToolDefinition `json:"tools"`
	Error string                     `json:"error,omitempty"`
}

type toolExecResponse struct {
	Result *pluginsdk.ToolResult `json:"result,omitempty"`
	Error  string                `json:"error,omitempty"`
}

func hasUnsupportedIsolationCapabilities(manifest *pluginsdk.Manifest) bool {
	if manifest == nil {
		return false
	}
	return len(manifest.Channels) > 0 ||
		len(manifest.Commands) > 0 ||
		len(manifest.Services) > 0 ||
		len(manifest.Hooks) > 0
}

func preparePluginWorkspace(pluginPath string, pluginID string, runnerPath string) (string, string, string, func(), error) {
	pluginBinary := resolvePluginBinary(pluginPath, pluginID)
	if pluginBinary == "" {
		return "", "", "", func() {}, fmt.Errorf("plugin binary not found at %s", pluginPath)
	}
	pluginDir := filepath.Dir(pluginBinary)
	workspace, err := os.MkdirTemp("", "nexus-plugin-daytona-*")
	if err != nil {
		return "", "", "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(workspace) }

	destPluginDir := filepath.Join(workspace, daytonaWorkspacePluginDir)
	if err := copyDir(pluginDir, destPluginDir); err != nil {
		cleanup()
		return "", "", "", func() {}, err
	}
	relBinary, err := filepath.Rel(pluginDir, pluginBinary)
	if err != nil {
		cleanup()
		return "", "", "", func() {}, err
	}
	pluginRel := filepath.ToSlash(filepath.Join(daytonaWorkspacePluginDir, relBinary))

	runnerName := daytonaRunnerDefaultName
	runnerDest := filepath.Join(workspace, runnerName)
	if err := copyFile(runnerPath, runnerDest, 0o755); err != nil {
		cleanup()
		return "", "", "", func() {}, err
	}

	return workspace, pluginRel, runnerName, cleanup, nil
}

func writeIsolationConfigFile(workspace string, cfg map[string]any) (string, error) {
	if cfg == nil || len(cfg) == 0 {
		return "", nil
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", err
	}
	path := filepath.Join(workspace, daytonaConfigFilename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return filepath.ToSlash(daytonaConfigFilename), nil
}

func writeIsolationParamsFile(workspace string, params json.RawMessage) (string, error) {
	raw := strings.TrimSpace(string(params))
	if raw == "" {
		raw = "{}"
	}
	if !json.Valid([]byte(raw)) {
		return "", fmt.Errorf("tool params must be valid JSON")
	}
	path := filepath.Join(workspace, daytonaParamsFilename)
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		return "", err
	}
	return filepath.ToSlash(daytonaParamsFilename), nil
}

func copyDir(src, dest string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("copyDir source is not a directory: %s", src)
	}
	if err := os.MkdirAll(dest, info.Mode().Perm()); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dest, rel)
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinks are not supported in plugin directories (%s)", path)
		}
		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, info.Mode().Perm())
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFileWithMode(path, target, info.Mode().Perm())
	})
}

func copyFile(src, dest string, mode os.FileMode) error {
	return copyFileWithMode(src, dest, mode)
}

func copyFileWithMode(src, dest string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

func parseMemoryMB(value string) (int, error) {
	trimmed := strings.ToUpper(strings.TrimSpace(value))
	if trimmed == "" {
		return 0, nil
	}
	multiplier := 1
	switch {
	case strings.HasSuffix(trimmed, "GB"):
		multiplier = 1024
		trimmed = strings.TrimSuffix(trimmed, "GB")
	case strings.HasSuffix(trimmed, "MB"):
		trimmed = strings.TrimSuffix(trimmed, "MB")
	case strings.HasSuffix(trimmed, "G"):
		multiplier = 1024
		trimmed = strings.TrimSuffix(trimmed, "G")
	case strings.HasSuffix(trimmed, "M"):
		trimmed = strings.TrimSuffix(trimmed, "M")
	}
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" {
		return 0, nil
	}
	valueInt, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, err
	}
	return valueInt * multiplier, nil
}
