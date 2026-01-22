package onboard

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
	"gopkg.in/yaml.v3"
)

// Options captures onboarding inputs.
type Options struct {
	ConfigPath     string
	DatabaseURL    string
	JWTSecret      string
	Provider       string
	ProviderKey    string
	EnableTelegram bool
	TelegramToken  string
	EnableDiscord  bool
	DiscordToken   string
	DiscordAppID   string
	EnableSlack    bool
	SlackBotToken  string
	SlackAppToken  string
	SlackSecret    string
	WorkspacePath  string
}

// BuildConfig builds a config map from options.
func BuildConfig(opts Options) map[string]any {
	provider := normalizeProvider(opts.Provider)
	if provider == "" {
		provider = "anthropic"
	}

	jwtSecret := opts.JWTSecret
	if strings.TrimSpace(jwtSecret) == "" {
		jwtSecret = GenerateJWTSecret()
	}

	databaseURL := opts.DatabaseURL
	if strings.TrimSpace(databaseURL) == "" {
		databaseURL = "postgres://root@localhost:26257/nexus?sslmode=disable"
	}

	cfg := map[string]any{
		"version": config.CurrentVersion,
		"server": map[string]any{
			"host":         "0.0.0.0",
			"grpc_port":    50051,
			"http_port":    8080,
			"metrics_port": 9090,
		},
		"database": map[string]any{
			"url": databaseURL,
		},
		"auth": map[string]any{
			"jwt_secret": jwtSecret,
		},
		"session": map[string]any{
			"default_agent_id": "main",
			"slack_scope":      "thread",
			"discord_scope":    "thread",
		},
		"llm": map[string]any{
			"default_provider": provider,
			"providers": map[string]any{
				provider: map[string]any{
					"api_key": opts.ProviderKey,
				},
			},
		},
		"channels": map[string]any{
			"telegram": map[string]any{
				"enabled":   opts.EnableTelegram,
				"bot_token": opts.TelegramToken,
			},
			"discord": map[string]any{
				"enabled":   opts.EnableDiscord,
				"bot_token": opts.DiscordToken,
				"app_id":    opts.DiscordAppID,
			},
			"slack": map[string]any{
				"enabled":        opts.EnableSlack,
				"bot_token":      opts.SlackBotToken,
				"app_token":      opts.SlackAppToken,
				"signing_secret": opts.SlackSecret,
			},
		},
	}

	if strings.TrimSpace(opts.WorkspacePath) != "" {
		cfg["workspace"] = map[string]any{
			"enabled": true,
			"path":    opts.WorkspacePath,
		}
	}

	return cfg
}

// GenerateJWTSecret returns a base64-encoded random secret.
func GenerateJWTSecret() string {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// ApplyAuthConfig sets provider credentials in a raw config map.
func ApplyAuthConfig(raw map[string]any, provider string, apiKey string, setDefault bool) {
	if raw == nil {
		return
	}
	provider = normalizeProvider(provider)
	if provider == "" {
		return
	}

	llm := ensureMap(raw, "llm")
	providers := ensureMap(llm, "providers")
	entry := ensureMap(providers, provider)
	entry["api_key"] = apiKey
	if setDefault {
		llm["default_provider"] = provider
	}
}

// WriteConfig writes the config map to disk.
func WriteConfig(path string, raw map[string]any) error {
	if raw == nil {
		return fmt.Errorf("config is nil")
	}
	data, err := yaml.Marshal(raw)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func normalizeProvider(provider string) string {
	provider = strings.TrimSpace(strings.ToLower(provider))
	switch provider {
	case "anthropic", "openai", "google", "openrouter":
		return provider
	default:
		return provider
	}
}

func ensureMap(root map[string]any, key string) map[string]any {
	if root == nil {
		return map[string]any{}
	}
	value, ok := root[key]
	if !ok {
		m := map[string]any{}
		root[key] = m
		return m
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	m := map[string]any{}
	root[key] = m
	return m
}
