package config

import "time"

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// ObservabilityConfig configures tracing and other observability features.
type ObservabilityConfig struct {
	Tracing TracingConfig `yaml:"tracing"`
}

// TracingConfig controls OpenTelemetry tracing.
type TracingConfig struct {
	Enabled        bool              `yaml:"enabled"`
	Endpoint       string            `yaml:"endpoint"`
	ServiceName    string            `yaml:"service_name"`
	ServiceVersion string            `yaml:"service_version"`
	Environment    string            `yaml:"environment"`
	SamplingRate   float64           `yaml:"sampling_rate"`
	Insecure       bool              `yaml:"insecure"`
	Attributes     map[string]string `yaml:"attributes"`
}

// SecurityConfig configures security features.
type SecurityConfig struct {
	Posture SecurityPostureConfig `yaml:"posture"`
}

// SecurityPostureConfig controls continuous security posture auditing.
type SecurityPostureConfig struct {
	Enabled            bool                   `yaml:"enabled"`
	Interval           time.Duration          `yaml:"interval"`
	IncludeFilesystem  *bool                  `yaml:"include_filesystem"`
	IncludeGateway     *bool                  `yaml:"include_gateway"`
	IncludeConfig      *bool                  `yaml:"include_config"`
	CheckSymlinks      *bool                  `yaml:"check_symlinks"`
	AllowGroupReadable bool                   `yaml:"allow_group_readable"`
	EmitEvents         *bool                  `yaml:"emit_events"`
	AutoRemediation    SecurityRemediationCfg `yaml:"auto_remediation"`
}

// SecurityRemediationCfg configures posture remediation behavior.
type SecurityRemediationCfg struct {
	Enabled bool   `yaml:"enabled"`
	Mode    string `yaml:"mode"` // lockdown | warn_only
}

// ArtifactConfig configures artifact storage and retention.
type ArtifactConfig struct {
	// Backend specifies storage backend: "local", "s3", or "minio".
	Backend string `yaml:"backend"`

	// LocalPath is the directory for local storage.
	LocalPath string `yaml:"local_path"`

	// MetadataPath is the file path for artifact metadata persistence.
	MetadataPath string `yaml:"metadata_path"`

	// MetadataBackend selects where artifact metadata is stored: "file" or "database".
	MetadataBackend string `yaml:"metadata_backend"`

	// S3Bucket is the bucket name for S3/MinIO storage.
	S3Bucket string `yaml:"s3_bucket"`

	// S3Endpoint is the endpoint URL for MinIO or S3-compatible storage.
	S3Endpoint string `yaml:"s3_endpoint"`

	// S3Region is the AWS region for S3.
	S3Region string `yaml:"s3_region"`

	// S3Prefix is an optional path prefix for all S3 objects.
	S3Prefix string `yaml:"s3_prefix"`

	// S3AccessKeyID is the AWS access key ID for S3 authentication.
	S3AccessKeyID string `yaml:"s3_access_key_id"`

	// S3SecretAccessKey is the AWS secret access key for S3 authentication.
	S3SecretAccessKey string `yaml:"s3_secret_access_key"`

	// TTLs configures retention period by artifact type.
	TTLs map[string]time.Duration `yaml:"ttls"`

	// PruneInterval is how often to cleanup expired artifacts.
	PruneInterval time.Duration `yaml:"prune_interval"`

	// MaxStorageSize is the total quota in bytes (0 = unlimited).
	MaxStorageSize int64 `yaml:"max_storage_size"`

	// Redaction configures rules for sensitive artifacts.
	Redaction ArtifactRedactionConfig `yaml:"redaction"`
}

// ArtifactRedactionConfig controls artifact redaction behavior.
type ArtifactRedactionConfig struct {
	// Enabled toggles redaction.
	Enabled bool `yaml:"enabled"`

	// Types lists artifact types to redact (case-insensitive).
	Types []string `yaml:"types"`

	// MimeTypes lists MIME types to redact (supports wildcards like "image/*").
	MimeTypes []string `yaml:"mime_types"`

	// FilenamePatterns are regex patterns to match against filenames.
	FilenamePatterns []string `yaml:"filename_patterns"`
}

// TranscriptionConfig configures audio transcription.
type TranscriptionConfig struct {
	// Enabled enables/disables transcription globally
	Enabled bool `yaml:"enabled"`

	// Provider is the transcription provider (e.g., "openai")
	Provider string `yaml:"provider"`

	// APIKey is the API key for the transcription provider
	APIKey string `yaml:"api_key"`

	// BaseURL is an optional custom base URL for the API
	BaseURL string `yaml:"base_url"`

	// Model is the transcription model to use (e.g., "whisper-1")
	Model string `yaml:"model"`

	// Language is the default language for transcription (ISO 639-1)
	// If empty, the provider will auto-detect the language
	Language string `yaml:"language"`
}

// CronConfig configures scheduled jobs.
type CronConfig struct {
	Enabled bool            `yaml:"enabled"`
	Jobs    []CronJobConfig `yaml:"jobs"`
}

// CronJobConfig defines a scheduled job.
type CronJobConfig struct {
	ID       string             `yaml:"id"`
	Name     string             `yaml:"name"`
	Type     string             `yaml:"type"`
	Enabled  bool               `yaml:"enabled"`
	Schedule CronScheduleConfig `yaml:"schedule"`
	Message  *CronMessageConfig `yaml:"message,omitempty"`
	Webhook  *CronWebhookConfig `yaml:"webhook,omitempty"`
	Custom   *CronCustomConfig  `yaml:"custom,omitempty"`
	Retry    CronRetryConfig    `yaml:"retry"`
}

// CronScheduleConfig defines when a job runs.
type CronScheduleConfig struct {
	Cron     string        `yaml:"cron"`
	Every    time.Duration `yaml:"every"`
	At       string        `yaml:"at"`
	Timezone string        `yaml:"timezone"`
}

// CronMessageConfig defines a message job payload.
type CronMessageConfig struct {
	Channel   string         `yaml:"channel"`
	ChannelID string         `yaml:"channel_id"`
	Content   string         `yaml:"content"`
	Template  string         `yaml:"template"`
	Data      map[string]any `yaml:"data"`
	Tools     []string       `yaml:"tools,omitempty"`
}

// CronWebhookConfig defines a webhook job payload.
type CronWebhookConfig struct {
	URL     string            `yaml:"url"`
	Method  string            `yaml:"method"`
	Headers map[string]string `yaml:"headers"`
	Body    string            `yaml:"body"`
	Timeout time.Duration     `yaml:"timeout"`
	Auth    *CronWebhookAuth  `yaml:"auth,omitempty"`
}

// CronWebhookAuth defines authentication for webhook jobs.
type CronWebhookAuth struct {
	Type   string `yaml:"type"`
	Token  string `yaml:"token,omitempty"`
	User   string `yaml:"user,omitempty"`
	Pass   string `yaml:"pass,omitempty"`
	Header string `yaml:"header,omitempty"`
}

// CronCustomConfig defines a custom cron job payload.
type CronCustomConfig struct {
	Handler string         `yaml:"handler"`
	Args    map[string]any `yaml:"args"`
}

// CronRetryConfig controls retry behavior for cron jobs.
type CronRetryConfig struct {
	MaxRetries int           `yaml:"max_retries"`
	Backoff    time.Duration `yaml:"backoff"`
	MaxBackoff time.Duration `yaml:"max_backoff"`
}

// TasksConfig configures the scheduled tasks system.
type TasksConfig struct {
	// Enabled enables the scheduled tasks scheduler.
	Enabled bool `yaml:"enabled"`

	// WorkerID uniquely identifies this scheduler instance for distributed locking.
	// Defaults to a generated UUID if empty.
	WorkerID string `yaml:"worker_id"`

	// PollInterval is how often the scheduler checks for due tasks.
	// Defaults to 10 seconds.
	PollInterval time.Duration `yaml:"poll_interval"`

	// AcquireInterval is how often the scheduler tries to acquire pending executions.
	// Defaults to 1 second.
	AcquireInterval time.Duration `yaml:"acquire_interval"`

	// LockDuration is how long an execution lock is held.
	// Should be longer than the maximum expected execution time.
	// Defaults to 10 minutes.
	LockDuration time.Duration `yaml:"lock_duration"`

	// MaxConcurrency is the maximum number of concurrent task executions.
	// Defaults to 5.
	MaxConcurrency int `yaml:"max_concurrency"`

	// CleanupInterval is how often stale executions are cleaned up.
	// Defaults to 1 minute.
	CleanupInterval time.Duration `yaml:"cleanup_interval"`

	// StaleTimeout is how long an execution can run before being marked stale.
	// Defaults to 30 minutes.
	StaleTimeout time.Duration `yaml:"stale_timeout"`

	// DefaultTimeout is the default timeout for task execution if not specified on the task.
	// Defaults to 5 minutes.
	DefaultTimeout time.Duration `yaml:"default_timeout"`
}

// RAGConfig configures the Retrieval-Augmented Generation pipeline.
type RAGConfig struct {
	// Enabled enables the RAG system.
	Enabled bool `yaml:"enabled"`

	// Store configures the document store backend.
	Store RAGStoreConfig `yaml:"store"`

	// Chunking configures document chunking.
	Chunking RAGChunkingConfig `yaml:"chunking"`

	// Embeddings configures the embedding provider.
	Embeddings RAGEmbeddingsConfig `yaml:"embeddings"`

	// Search configures default search behavior.
	Search RAGSearchConfig `yaml:"search"`

	// ContextInjection configures automatic context injection.
	ContextInjection RAGContextInjectionConfig `yaml:"context_injection"`
}

// RAGStoreConfig configures the RAG document store.
type RAGStoreConfig struct {
	// Backend is the storage backend: "pgvector"
	Backend string `yaml:"backend"`

	// DSN is the PostgreSQL connection string (for pgvector).
	// If empty and UseDatabaseURL is true, uses the main database.url.
	DSN string `yaml:"dsn"`

	// UseDatabaseURL uses the main database.url for pgvector storage.
	UseDatabaseURL bool `yaml:"use_database_url"`

	// Dimension is the embedding vector dimension.
	// Default: 1536 (OpenAI text-embedding-3-small)
	Dimension int `yaml:"dimension"`

	// RunMigrations controls whether to run migrations on startup.
	RunMigrations *bool `yaml:"run_migrations"`
}

// RAGChunkingConfig configures document chunking.
type RAGChunkingConfig struct {
	// ChunkSize is the target chunk size in characters.
	// Default: 1000
	ChunkSize int `yaml:"chunk_size"`

	// ChunkOverlap is the overlap between chunks in characters.
	// Default: 200
	ChunkOverlap int `yaml:"chunk_overlap"`

	// MinChunkSize is the minimum chunk size to keep.
	// Default: 100
	MinChunkSize int `yaml:"min_chunk_size"`
}

// RAGEmbeddingsConfig configures the embedding provider for RAG.
type RAGEmbeddingsConfig struct {
	// Provider is the embedding provider: "openai", "ollama"
	Provider string `yaml:"provider"`

	// APIKey is the API key for the provider.
	APIKey string `yaml:"api_key"`

	// BaseURL is the API base URL (optional).
	BaseURL string `yaml:"base_url"`

	// Model is the embedding model to use.
	// Default: "text-embedding-3-small" for OpenAI
	Model string `yaml:"model"`

	// BatchSize is the maximum texts per embedding batch.
	// Default: 100
	BatchSize int `yaml:"batch_size"`
}

// RAGSearchConfig configures default search behavior.
type RAGSearchConfig struct {
	// DefaultLimit is the default number of results.
	// Default: 5
	DefaultLimit int `yaml:"default_limit"`

	// DefaultThreshold is the default similarity threshold (0-1).
	// Default: 0.7
	DefaultThreshold float32 `yaml:"default_threshold"`

	// MaxResults is the maximum results allowed.
	// Default: 20
	MaxResults int `yaml:"max_results"`
}

// RAGContextInjectionConfig configures automatic context injection.
type RAGContextInjectionConfig struct {
	// Enabled enables automatic RAG context injection.
	Enabled bool `yaml:"enabled"`

	// MaxChunks is the maximum chunks to inject.
	// Default: 5
	MaxChunks int `yaml:"max_chunks"`

	// MaxTokens is the maximum tokens to inject.
	// Default: 2000
	MaxTokens int `yaml:"max_tokens"`

	// MinScore is the minimum similarity score for inclusion.
	// Default: 0.7
	MinScore float32 `yaml:"min_score"`

	// Scope limits retrieval: "global", "agent", "session", "channel"
	// Default: "global"
	Scope string `yaml:"scope"`
}

// EdgeConfig configures the edge protocol for remote tool execution.
type EdgeConfig struct {
	// Enabled enables the edge service for remote edge daemons.
	Enabled bool `yaml:"enabled"`

	// AuthMode controls how edges authenticate: "token", "tofu", or "dev".
	// token: Pre-shared tokens (production)
	// tofu: Trust-On-First-Use with manual approval
	// dev: Accept all connections (development only)
	AuthMode string `yaml:"auth_mode"`

	// Tokens maps edge IDs to pre-shared authentication tokens.
	// Only used when AuthMode is "token".
	Tokens map[string]string `yaml:"tokens"`

	// HeartbeatInterval is how often edges should send heartbeats.
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`

	// HeartbeatTimeout is how long before an edge is considered disconnected.
	HeartbeatTimeout time.Duration `yaml:"heartbeat_timeout"`

	// DefaultToolTimeout is the default timeout for tool execution.
	DefaultToolTimeout time.Duration `yaml:"default_tool_timeout"`

	// MaxConcurrentTools limits concurrent tool executions per edge.
	MaxConcurrentTools int `yaml:"max_concurrent_tools"`

	// EventBufferSize is the buffer size for edge events.
	EventBufferSize int `yaml:"event_buffer_size"`
}
