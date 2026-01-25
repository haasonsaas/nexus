package hooks

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

// Gmail hook defaults
const (
	DefaultGmailLabel        = "INBOX"
	DefaultGmailTopic        = "nexus-gmail-watch"
	DefaultGmailSubscription = "nexus-gmail-watch-push"
	DefaultGmailServeBind    = "127.0.0.1"
	DefaultGmailServePort    = 8788
	DefaultGmailServePath    = "/gmail-pubsub"
	DefaultGmailMaxBytes     = 20_000
	DefaultGmailRenewMinutes = 12 * 60 // 12 hours
	DefaultHooksPath         = "/hooks"
	DefaultGatewayPort       = 8080
)

// TailscaleMode for Gmail hook
type TailscaleMode string

const (
	TailscaleModeOff    TailscaleMode = "off"
	TailscaleModeFunnel TailscaleMode = "funnel"
	TailscaleModeServe  TailscaleMode = "serve"
)

// GmailHookOverrides for configuration overrides
type GmailHookOverrides struct {
	Account           string
	Label             string
	Topic             string
	Subscription      string
	PushToken         string
	HookToken         string
	HookURL           string
	IncludeBody       bool
	MaxBytes          int
	RenewEveryMinutes int
	ServeBind         string
	ServePort         int
	ServePath         string
	TailscaleMode     TailscaleMode
	TailscalePath     string
	TailscaleTarget   string
}

// GmailHookRuntimeConfig is the resolved configuration
type GmailHookRuntimeConfig struct {
	Account           string
	Label             string
	Topic             string
	Subscription      string
	PushToken         string
	HookToken         string
	HookURL           string
	IncludeBody       bool
	MaxBytes          int
	RenewEveryMinutes int
	Serve             struct {
		Bind string
		Port int
		Path string
	}
	Tailscale struct {
		Mode   TailscaleMode
		Path   string
		Target string
	}
}

// GenerateHookToken generates a random hex token
func GenerateHookToken(bytes int) string {
	if bytes <= 0 {
		bytes = 24
	}
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// MergeHookPresets merges existing presets with a new one
func MergeHookPresets(existing []string, preset string) []string {
	next := make(map[string]struct{})
	for _, item := range existing {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			next[trimmed] = struct{}{}
		}
	}
	if trimmed := strings.TrimSpace(preset); trimmed != "" {
		next[trimmed] = struct{}{}
	}
	result := make([]string, 0, len(next))
	for k := range next {
		result = append(result, k)
	}
	return result
}

// NormalizeHooksPath normalizes the hooks path
func NormalizeHooksPath(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		base = DefaultHooksPath
	}
	if base == "/" {
		return DefaultHooksPath
	}
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	return strings.TrimRight(base, "/")
}

// NormalizeServePath normalizes the serve path
func NormalizeServePath(raw string) string {
	base := strings.TrimSpace(raw)
	if base == "" {
		base = DefaultGmailServePath
	}
	// Tailscale funnel/serve strips the set-path prefix before proxying.
	// To accept requests at /<path> externally, we must listen on "/".
	if base == "/" {
		return "/"
	}
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	return strings.TrimRight(base, "/")
}

// BuildDefaultHookURL builds the default hook URL
func BuildDefaultHookURL(hooksPath string, port int) string {
	if port <= 0 {
		port = DefaultGatewayPort
	}
	basePath := NormalizeHooksPath(hooksPath)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	return joinURL(baseURL, basePath+"/gmail")
}

// GmailHookConfigSource provides configuration values for Gmail hook.
// This interface avoids import cycles with the config package.
type GmailHookConfigSource interface {
	// GetHTTPPort returns the HTTP server port.
	GetHTTPPort() int
	// GetHooksToken returns the hooks authentication token.
	GetHooksToken() string
	// GetHooksPath returns the hooks base path.
	GetHooksPath() string
}

// ResolveGatewayPort resolves the gateway port from a config source
func ResolveGatewayPort(cfg GmailHookConfigSource) int {
	if cfg == nil {
		return DefaultGatewayPort
	}
	if port := cfg.GetHTTPPort(); port > 0 {
		return port
	}
	return DefaultGatewayPort
}

// ResolveGmailHookRuntimeConfig resolves the full Gmail hook configuration.
// The cfg parameter is optional and provides defaults from the main config.
func ResolveGmailHookRuntimeConfig(cfg GmailHookConfigSource, overrides GmailHookOverrides) (*GmailHookRuntimeConfig, error) {
	// Get hook token from overrides or config
	hookToken := overrides.HookToken
	if hookToken == "" && cfg != nil {
		hookToken = cfg.GetHooksToken()
	}
	if hookToken == "" {
		return nil, fmt.Errorf("hooks.token missing (needed for gmail hook)")
	}

	account := overrides.Account
	if account == "" {
		return nil, fmt.Errorf("gmail account required")
	}

	topic := overrides.Topic
	if topic == "" {
		return nil, fmt.Errorf("gmail topic required")
	}

	subscription := overrides.Subscription
	if subscription == "" {
		subscription = DefaultGmailSubscription
	}

	pushToken := overrides.PushToken
	if pushToken == "" {
		return nil, fmt.Errorf("gmail push token required")
	}

	hooksPath := DefaultHooksPath
	if cfg != nil && cfg.GetHooksPath() != "" {
		hooksPath = cfg.GetHooksPath()
	}

	hookURL := overrides.HookURL
	if hookURL == "" {
		hookURL = BuildDefaultHookURL(hooksPath, ResolveGatewayPort(cfg))
	}

	includeBody := overrides.IncludeBody

	maxBytes := overrides.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultGmailMaxBytes
	}

	renewEveryMinutes := overrides.RenewEveryMinutes
	if renewEveryMinutes <= 0 {
		renewEveryMinutes = DefaultGmailRenewMinutes
	}

	serveBind := overrides.ServeBind
	if serveBind == "" {
		serveBind = DefaultGmailServeBind
	}

	servePort := overrides.ServePort
	if servePort <= 0 {
		servePort = DefaultGmailServePort
	}

	tailscaleMode := overrides.TailscaleMode
	if tailscaleMode == "" {
		tailscaleMode = TailscaleModeOff
	}

	tailscaleTarget := ""
	if tailscaleMode != TailscaleModeOff && strings.TrimSpace(overrides.TailscaleTarget) != "" {
		tailscaleTarget = strings.TrimSpace(overrides.TailscaleTarget)
	}

	// Determine serve path based on tailscale mode
	servePathRaw := overrides.ServePath
	if servePathRaw == "" {
		servePathRaw = DefaultGmailServePath
	}
	normalizedServePathRaw := NormalizeServePath(servePathRaw)

	// Tailscale strips the public path before proxying, so listen on "/" when on.
	servePath := normalizedServePathRaw
	if tailscaleMode != TailscaleModeOff && tailscaleTarget == "" {
		servePath = "/"
	}

	tailscalePath := NormalizeServePath(normalizedServePathRaw)
	if tailscaleMode != TailscaleModeOff && overrides.TailscalePath != "" {
		tailscalePath = NormalizeServePath(overrides.TailscalePath)
	}

	result := &GmailHookRuntimeConfig{
		Account:           account,
		Label:             overrides.Label,
		Topic:             topic,
		Subscription:      subscription,
		PushToken:         pushToken,
		HookToken:         hookToken,
		HookURL:           hookURL,
		IncludeBody:       includeBody,
		MaxBytes:          maxBytes,
		RenewEveryMinutes: renewEveryMinutes,
	}

	if result.Label == "" {
		result.Label = DefaultGmailLabel
	}

	result.Serve.Bind = serveBind
	result.Serve.Port = servePort
	result.Serve.Path = servePath

	result.Tailscale.Mode = tailscaleMode
	result.Tailscale.Path = tailscalePath
	result.Tailscale.Target = tailscaleTarget

	return result, nil
}

// BuildWatchStartArgs builds command args for gmail watch start
func BuildWatchStartArgs(cfg *GmailHookRuntimeConfig) []string {
	return []string{
		"gmail",
		"watch",
		"start",
		"--account",
		cfg.Account,
		"--label",
		cfg.Label,
		"--topic",
		cfg.Topic,
	}
}

// BuildWatchServeArgs builds command args for gmail watch serve
func BuildWatchServeArgs(cfg *GmailHookRuntimeConfig) []string {
	args := []string{
		"gmail",
		"watch",
		"serve",
		"--account",
		cfg.Account,
		"--bind",
		cfg.Serve.Bind,
		"--port",
		fmt.Sprintf("%d", cfg.Serve.Port),
		"--path",
		cfg.Serve.Path,
		"--token",
		cfg.PushToken,
		"--hook-url",
		cfg.HookURL,
		"--hook-token",
		cfg.HookToken,
	}
	if cfg.IncludeBody {
		args = append(args, "--include-body")
	}
	if cfg.MaxBytes > 0 {
		args = append(args, "--max-bytes", fmt.Sprintf("%d", cfg.MaxBytes))
	}
	return args
}

// BuildTopicPath builds a GCP topic path
func BuildTopicPath(projectID, topicName string) string {
	return fmt.Sprintf("projects/%s/topics/%s", projectID, topicName)
}

// ParseTopicPath parses a GCP topic path
func ParseTopicPath(topic string) (projectID, topicName string, ok bool) {
	topic = strings.TrimSpace(topic)
	parts := strings.Split(topic, "/")
	if len(parts) != 4 {
		return "", "", false
	}
	if !strings.EqualFold(parts[0], "projects") || !strings.EqualFold(parts[2], "topics") {
		return "", "", false
	}
	// Validate that project ID and topic name are non-empty
	if strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[3]) == "" {
		return "", "", false
	}
	return parts[1], parts[3], true
}

// joinURL joins a base URL with a path
func joinURL(base string, path string) string {
	u, err := url.Parse(base)
	if err != nil {
		return base + path
	}
	basePath := strings.TrimRight(u.Path, "/")
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u.Path = basePath + path
	return u.String()
}
