package diagnostics

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// CacheTraceStage represents different stages of cache tracing
type CacheTraceStage string

const (
	StageSessionLoaded    CacheTraceStage = "session:loaded"
	StageSessionSanitized CacheTraceStage = "session:sanitized"
	StageSessionLimited   CacheTraceStage = "session:limited"
	StagePromptBefore     CacheTraceStage = "prompt:before"
	StagePromptImages     CacheTraceStage = "prompt:images"
	StageStreamContext    CacheTraceStage = "stream:context"
	StageSessionAfter     CacheTraceStage = "session:after"
)

// CacheTraceEvent represents a single trace event
type CacheTraceEvent struct {
	Timestamp           string                 `json:"ts"`
	Sequence            int                    `json:"seq"`
	Stage               CacheTraceStage        `json:"stage"`
	RunID               string                 `json:"runId,omitempty"`
	SessionID           string                 `json:"sessionId,omitempty"`
	SessionKey          string                 `json:"sessionKey,omitempty"`
	Provider            string                 `json:"provider,omitempty"`
	ModelID             string                 `json:"modelId,omitempty"`
	ModelAPI            string                 `json:"modelApi,omitempty"`
	WorkspaceDir        string                 `json:"workspaceDir,omitempty"`
	Prompt              string                 `json:"prompt,omitempty"`
	System              interface{}            `json:"system,omitempty"`
	Options             map[string]interface{} `json:"options,omitempty"`
	Model               map[string]interface{} `json:"model,omitempty"`
	Messages            []interface{}          `json:"messages,omitempty"`
	MessageCount        int                    `json:"messageCount,omitempty"`
	MessageRoles        []string               `json:"messageRoles,omitempty"`
	MessageFingerprints []string               `json:"messageFingerprints,omitempty"`
	MessagesDigest      string                 `json:"messagesDigest,omitempty"`
	SystemDigest        string                 `json:"systemDigest,omitempty"`
	Note                string                 `json:"note,omitempty"`
	Error               string                 `json:"error,omitempty"`
}

// CacheTraceConfig configures the trace behavior
type CacheTraceConfig struct {
	Enabled         bool
	FilePath        string
	IncludeMessages bool
	IncludePrompt   bool
	IncludeSystem   bool
}

// CacheTrace records diagnostic trace events to a JSONL file
type CacheTrace struct {
	config   CacheTraceConfig
	baseInfo CacheTraceEvent
	sequence int
	mu       sync.Mutex
	writer   CacheTraceWriter
}

// CacheTraceWriter abstracts the writing mechanism for testing
type CacheTraceWriter interface {
	Write(line string) error
	FilePath() string
}

// fileWriter implements CacheTraceWriter for file-based writing
type fileWriter struct {
	filePath string
	mu       sync.Mutex
	ready    chan struct{}
	file     *os.File
}

// newFileWriter creates a new file writer with async initialization
func newFileWriter(filePath string) *fileWriter {
	w := &fileWriter{
		filePath: filePath,
		ready:    make(chan struct{}),
	}
	go w.init()
	return w
}

func (w *fileWriter) init() {
	defer close(w.ready)
	dir := filepath.Dir(w.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return
	}
	f, err := os.OpenFile(w.filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	w.mu.Lock()
	w.file = f
	w.mu.Unlock()
}

func (w *fileWriter) Write(line string) error {
	<-w.ready
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return fmt.Errorf("file not initialized (path=%s)", w.filePath)
	}
	_, err := w.file.WriteString(line)
	return err
}

func (w *fileWriter) FilePath() string {
	return w.filePath
}

func (w *fileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// writerRegistry manages shared writers across traces
var (
	writerRegistry   = make(map[string]*fileWriter)
	writerRegistryMu sync.Mutex
)

func getWriter(filePath string) *fileWriter {
	writerRegistryMu.Lock()
	defer writerRegistryMu.Unlock()
	if w, ok := writerRegistry[filePath]; ok {
		return w
	}
	w := newFileWriter(filePath)
	writerRegistry[filePath] = w
	return w
}

// CacheTraceParams for creating a new trace
type CacheTraceParams struct {
	RunID        string
	SessionID    string
	SessionKey   string
	Provider     string
	ModelID      string
	ModelAPI     string
	WorkspaceDir string
}

// CacheTraceEventPayload for additional event data
type CacheTraceEventPayload struct {
	Prompt   string
	System   interface{}
	Options  map[string]interface{}
	Model    map[string]interface{}
	Messages []interface{}
	Note     string
	Error    string
}

// NewCacheTrace creates a new cache trace instance
// Returns nil if tracing is disabled
func NewCacheTrace(config CacheTraceConfig, params CacheTraceParams) *CacheTrace {
	if !config.Enabled {
		return nil
	}

	var writer CacheTraceWriter
	if config.FilePath != "" {
		writer = getWriter(config.FilePath)
	}

	return &CacheTrace{
		config: config,
		baseInfo: CacheTraceEvent{
			RunID:        params.RunID,
			SessionID:    params.SessionID,
			SessionKey:   params.SessionKey,
			Provider:     params.Provider,
			ModelID:      params.ModelID,
			ModelAPI:     params.ModelAPI,
			WorkspaceDir: params.WorkspaceDir,
		},
		writer: writer,
	}
}

// NewCacheTraceWithWriter creates a new cache trace with a custom writer (for testing)
func NewCacheTraceWithWriter(config CacheTraceConfig, params CacheTraceParams, writer CacheTraceWriter) *CacheTrace {
	if !config.Enabled {
		return nil
	}

	return &CacheTrace{
		config: config,
		baseInfo: CacheTraceEvent{
			RunID:        params.RunID,
			SessionID:    params.SessionID,
			SessionKey:   params.SessionKey,
			Provider:     params.Provider,
			ModelID:      params.ModelID,
			ModelAPI:     params.ModelAPI,
			WorkspaceDir: params.WorkspaceDir,
		},
		writer: writer,
	}
}

// RecordStage records a trace event for a specific stage
func (ct *CacheTrace) RecordStage(stage CacheTraceStage, payload *CacheTraceEventPayload) {
	if ct == nil || ct.writer == nil {
		return
	}

	ct.mu.Lock()
	ct.sequence++
	seq := ct.sequence
	ct.mu.Unlock()

	event := CacheTraceEvent{
		Timestamp:    time.Now().UTC().Format(time.RFC3339Nano),
		Sequence:     seq,
		Stage:        stage,
		RunID:        ct.baseInfo.RunID,
		SessionID:    ct.baseInfo.SessionID,
		SessionKey:   ct.baseInfo.SessionKey,
		Provider:     ct.baseInfo.Provider,
		ModelID:      ct.baseInfo.ModelID,
		ModelAPI:     ct.baseInfo.ModelAPI,
		WorkspaceDir: ct.baseInfo.WorkspaceDir,
	}

	if payload != nil {
		if ct.config.IncludePrompt && payload.Prompt != "" {
			event.Prompt = payload.Prompt
		}
		if ct.config.IncludeSystem && payload.System != nil {
			event.System = payload.System
			event.SystemDigest = Digest(payload.System)
		}
		if payload.Options != nil {
			event.Options = payload.Options
		}
		if payload.Model != nil {
			event.Model = payload.Model
		}

		if payload.Messages != nil {
			count, roles, fingerprints, digest := SummarizeMessages(payload.Messages)
			event.MessageCount = count
			event.MessageRoles = roles
			event.MessageFingerprints = fingerprints
			event.MessagesDigest = digest
			if ct.config.IncludeMessages {
				event.Messages = payload.Messages
			}
		}

		if payload.Note != "" {
			event.Note = payload.Note
		}
		if payload.Error != "" {
			event.Error = payload.Error
		}
	}

	line := safeJSONStringify(event)
	if line == "" {
		return
	}
	if err := ct.writer.Write(line + "\n"); err != nil {
		_ = err
	}
}

// FilePath returns the file path being written to
func (ct *CacheTrace) FilePath() string {
	if ct == nil || ct.writer == nil {
		return ""
	}
	return ct.writer.FilePath()
}

// Enabled returns true if tracing is enabled
func (ct *CacheTrace) Enabled() bool {
	return ct != nil && ct.config.Enabled
}

// Close closes the trace file if the writer supports closing
func (ct *CacheTrace) Close() error {
	if ct == nil || ct.writer == nil {
		return nil
	}
	if closer, ok := ct.writer.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// Digest computes SHA256 digest of a value using stable serialization
func Digest(value interface{}) string {
	serialized := stableStringify(value)
	hash := sha256.Sum256([]byte(serialized))
	return hex.EncodeToString(hash[:])
}

// stableStringify creates a stable string representation for hashing
// Keys are sorted to ensure consistent output
func stableStringify(value interface{}) string {
	if value == nil {
		return "null"
	}

	switch v := value.(type) {
	case string:
		return jsonEscape(v)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v)
	case float32, float64:
		return fmt.Sprintf("%v", v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case []byte:
		return fmt.Sprintf(`{"type":"bytes","data":"%s"}`, hex.EncodeToString(v))
	case error:
		return stableStringify(map[string]interface{}{
			"error": v.Error(),
		})
	case []interface{}:
		parts := make([]string, len(v))
		for i, elem := range v {
			parts[i] = stableStringify(elem)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = fmt.Sprintf("%s:%s", jsonEscape(k), stableStringify(v[k]))
		}
		return "{" + strings.Join(parts, ",") + "}"
	default:
		// Fall back to JSON marshaling for complex types
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		// Re-parse and stable stringify to ensure key ordering
		var parsed interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			return string(data)
		}
		return stableStringify(parsed)
	}
}

// jsonEscape escapes a string for JSON
func jsonEscape(s string) string {
	data, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(data)
}

// SummarizeMessages creates message summaries without full content
func SummarizeMessages(messages []interface{}) (count int, roles []string, fingerprints []string, digest string) {
	count = len(messages)
	roles = make([]string, count)
	fingerprints = make([]string, count)

	for i, msg := range messages {
		fingerprints[i] = Digest(msg)
		// Extract role if present
		if m, ok := msg.(map[string]interface{}); ok {
			if role, ok := m["role"].(string); ok {
				roles[i] = role
			}
		}
	}

	// Compute overall digest from fingerprints
	digest = Digest(strings.Join(fingerprints, "|"))
	return
}

// safeJSONStringify safely marshals a value to JSON, handling special cases
func safeJSONStringify(value interface{}) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

// ResolveCacheTraceConfig resolves configuration from environment/config
func ResolveCacheTraceConfig(env map[string]string, configEnabled bool, configPath string) CacheTraceConfig {
	cfg := CacheTraceConfig{
		Enabled:         configEnabled,
		FilePath:        configPath,
		IncludeMessages: true,
		IncludePrompt:   true,
		IncludeSystem:   true,
	}

	// Environment variable overrides
	if envVal, ok := env["NEXUS_CACHE_TRACE"]; ok {
		cfg.Enabled = parseBooleanValue(envVal)
	}
	if envVal, ok := env["NEXUS_CACHE_TRACE_FILE"]; ok && strings.TrimSpace(envVal) != "" {
		cfg.FilePath = resolveUserPath(envVal)
	}
	if envVal, ok := env["NEXUS_CACHE_TRACE_MESSAGES"]; ok {
		cfg.IncludeMessages = parseBooleanValue(envVal)
	}
	if envVal, ok := env["NEXUS_CACHE_TRACE_PROMPT"]; ok {
		cfg.IncludePrompt = parseBooleanValue(envVal)
	}
	if envVal, ok := env["NEXUS_CACHE_TRACE_SYSTEM"]; ok {
		cfg.IncludeSystem = parseBooleanValue(envVal)
	}

	// Set default file path if enabled but no path specified
	if cfg.Enabled && cfg.FilePath == "" {
		cfg.FilePath = defaultCacheTracePath()
	}

	return cfg
}

// parseBooleanValue parses a string as a boolean value
func parseBooleanValue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		return false
	}
}

// resolveUserPath expands ~ to user home directory
func resolveUserPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// defaultCacheTracePath returns the default path for cache trace files
func defaultCacheTracePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "nexus", "logs", "cache-trace.jsonl")
	}
	return filepath.Join(home, ".nexus", "logs", "cache-trace.jsonl")
}
