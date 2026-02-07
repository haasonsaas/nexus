package config

import (
	"time"

	"github.com/haasonsaas/nexus/internal/audit"
	"github.com/haasonsaas/nexus/internal/ratelimit"
)

type ServerConfig struct {
	Host        string `yaml:"host"`
	GRPCPort    int    `yaml:"grpc_port"`
	HTTPPort    int    `yaml:"http_port"`
	MetricsPort int    `yaml:"metrics_port"`
}

type DatabaseConfig struct {
	URL             string        `yaml:"url"`
	MaxConnections  int           `yaml:"max_connections"`
	ConnMaxLifetime time.Duration `yaml:"conn_max_lifetime"`
}

// ClusterConfig controls multi-gateway behavior.
type ClusterConfig struct {
	// Enabled turns on cluster-aware behavior.
	Enabled bool `yaml:"enabled"`

	// NodeID uniquely identifies this gateway instance.
	NodeID string `yaml:"node_id"`

	// AllowMultipleGateways bypasses the singleton gateway lock.
	AllowMultipleGateways bool `yaml:"allow_multiple_gateways"`

	// SessionLocks controls distributed session locking.
	SessionLocks SessionLockConfig `yaml:"session_locks"`
}

// SessionLockConfig configures distributed session locks.
type SessionLockConfig struct {
	// Enabled uses DB-backed session locks.
	Enabled bool `yaml:"enabled"`

	// TTL is the lock lease duration.
	TTL time.Duration `yaml:"ttl"`

	// RefreshInterval is how often leases are renewed.
	RefreshInterval time.Duration `yaml:"refresh_interval"`

	// AcquireTimeout is how long to wait for a lock.
	AcquireTimeout time.Duration `yaml:"acquire_timeout"`

	// PollInterval controls backoff when lock is held by another owner.
	PollInterval time.Duration `yaml:"poll_interval"`
}

// CanvasHostConfig configures the dedicated canvas host.
type CanvasHostConfig struct {
	Enabled      *bool  `yaml:"enabled"`
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	Root         string `yaml:"root"`
	Namespace    string `yaml:"namespace"`
	LiveReload   *bool  `yaml:"live_reload"`
	InjectClient *bool  `yaml:"inject_client"`
	AutoIndex    *bool  `yaml:"auto_index"`
	A2UIRoot     string `yaml:"a2ui_root"`
}

// CanvasConfig configures canvas persistence and retention.
type CanvasConfig struct {
	Retention CanvasRetentionConfig `yaml:"retention"`
	Tokens    CanvasTokenConfig     `yaml:"tokens"`
	Actions   CanvasActionConfig    `yaml:"actions"`
	Audit     audit.Config          `yaml:"audit"`
}

// CanvasRetentionConfig controls how long canvas state and events are retained.
type CanvasRetentionConfig struct {
	StateMaxAge   time.Duration `yaml:"state_max_age"`
	EventMaxAge   time.Duration `yaml:"event_max_age"`
	StateMaxBytes int64         `yaml:"state_max_bytes"`
	EventMaxBytes int64         `yaml:"event_max_bytes"`
}

// CanvasTokenConfig controls signed canvas access tokens.
type CanvasTokenConfig struct {
	Secret string        `yaml:"secret"`
	TTL    time.Duration `yaml:"ttl"`
}

// CanvasActionConfig configures canvas UI action handling.
type CanvasActionConfig struct {
	RateLimit   ratelimit.Config `yaml:"rate_limit"`
	DefaultRole string           `yaml:"default_role"`
}
