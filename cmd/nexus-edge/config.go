package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	defaultEdgeConfigDir  = ".nexus-edge"
	defaultEdgeConfigName = "config.yaml"
)

var errConfigNotFound = errors.New("edge config not found")

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return defaultEdgeConfigName
	}
	return filepath.Join(home, defaultEdgeConfigDir, defaultEdgeConfigName)
}

func resolveConfigPath(explicit string) (string, bool) {
	if strings.TrimSpace(explicit) != "" {
		return expandUserPath(explicit), true
	}
	if env := strings.TrimSpace(os.Getenv("NEXUS_EDGE_CONFIG")); env != "" {
		return expandUserPath(env), true
	}
	defaultPath := defaultConfigPath()
	if _, err := os.Stat(defaultPath); err == nil {
		return defaultPath, true
	}
	return defaultPath, false
}

func expandUserPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil && strings.TrimSpace(home) != "" {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, errConfigNotFound
		}
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

func mergeConfig(base, override Config) Config {
	if strings.TrimSpace(override.CoreURL) != "" {
		base.CoreURL = override.CoreURL
	}
	if strings.TrimSpace(override.EdgeID) != "" {
		base.EdgeID = override.EdgeID
	}
	if strings.TrimSpace(override.Name) != "" {
		base.Name = override.Name
	}
	if strings.TrimSpace(override.AuthToken) != "" {
		base.AuthToken = override.AuthToken
	}
	if strings.TrimSpace(override.PairingToken) != "" {
		base.PairingToken = override.PairingToken
	}
	if override.ReconnectDelay > 0 {
		base.ReconnectDelay = override.ReconnectDelay
	}
	if override.HeartbeatInterval > 0 {
		base.HeartbeatInterval = override.HeartbeatInterval
	}
	if strings.TrimSpace(override.LogLevel) != "" {
		base.LogLevel = override.LogLevel
	}
	if len(override.ChannelTypes) > 0 {
		base.ChannelTypes = override.ChannelTypes
	}
	if override.NodePolicy.Shell != nil {
		if base.NodePolicy.Shell == nil {
			base.NodePolicy.Shell = &ShellPolicy{}
		}
		if len(override.NodePolicy.Shell.Allowlist) > 0 {
			base.NodePolicy.Shell.Allowlist = override.NodePolicy.Shell.Allowlist
		}
		if len(override.NodePolicy.Shell.Denylist) > 0 {
			base.NodePolicy.Shell.Denylist = override.NodePolicy.Shell.Denylist
		}
	}
	return base
}

func normalizeConfig(cfg Config) Config {
	if strings.TrimSpace(cfg.EdgeID) == "" {
		hostname, _ := os.Hostname() //nolint:errcheck // best effort
		cfg.EdgeID = hostname
	}
	if strings.TrimSpace(cfg.Name) == "" {
		cfg.Name = cfg.EdgeID
	}
	if strings.TrimSpace(cfg.AuthToken) == "" && strings.TrimSpace(cfg.PairingToken) != "" {
		cfg.AuthToken = cfg.PairingToken
	}
	cfg.CoreURL = normalizeCoreURL(cfg.CoreURL)
	return cfg
}

func normalizeCoreURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return trimmed
	}
	if strings.Contains(trimmed, "://") {
		if parsed, err := url.Parse(trimmed); err == nil && parsed.Host != "" {
			return parsed.Host
		}
	}
	return trimmed
}

func writeConfig(path string, cfg Config) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("config path is required")
	}
	path = expandUserPath(path)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	toWrite := normalizeConfig(cfg)
	if strings.TrimSpace(toWrite.PairingToken) == "" && strings.TrimSpace(toWrite.AuthToken) != "" {
		toWrite.PairingToken = toWrite.AuthToken
	}
	if strings.TrimSpace(toWrite.PairingToken) != "" {
		toWrite.AuthToken = ""
	}
	data, err := yaml.Marshal(&toWrite)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}
