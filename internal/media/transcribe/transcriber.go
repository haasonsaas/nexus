// Package transcribe provides audio transcription capabilities using various providers.
package transcribe

import (
	"fmt"
	"io"
	"log/slog"

	"github.com/haasonsaas/nexus/internal/media"
)

// Config holds configuration for transcription providers.
type Config struct {
	// Provider is the transcription provider to use (e.g., "openai")
	Provider string `yaml:"provider"`

	// APIKey is the API key for the provider
	APIKey string `yaml:"api_key"`

	// BaseURL is an optional custom base URL for the API
	BaseURL string `yaml:"base_url"`

	// Model is the transcription model to use (e.g., "whisper-1")
	Model string `yaml:"model"`

	// Language is the default language for transcription (ISO 639-1)
	// If empty, the provider will auto-detect the language
	Language string `yaml:"language"`

	// Logger is an optional structured logger
	Logger *slog.Logger `yaml:"-"`
}

// applyDefaults sets default values for unset configuration fields.
func (c *Config) applyDefaults() {
	if c.Provider == "" {
		c.Provider = "openai"
	}
	if c.Model == "" {
		c.Model = "whisper-1"
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

// Transcriber wraps the media.Transcriber interface with additional metadata.
type Transcriber struct {
	provider media.Transcriber
	name     string
	logger   *slog.Logger
}

// Transcribe converts audio to text.
// It delegates to the underlying provider implementation.
func (t *Transcriber) Transcribe(audio io.Reader, mimeType string, language string) (string, error) {
	t.logger.Debug("transcribing audio",
		"provider", t.name,
		"mime_type", mimeType,
		"language", language)

	text, err := t.provider.Transcribe(audio, mimeType, language)
	if err != nil {
		t.logger.Error("transcription failed",
			"provider", t.name,
			"error", err)
		return "", err
	}

	t.logger.Debug("transcription complete",
		"provider", t.name,
		"text_length", len(text))

	return text, nil
}

// Name returns the provider name.
func (t *Transcriber) Name() string {
	return t.name
}

// New creates a new Transcriber with the given configuration.
// It returns an error if the provider is not supported or if required
// configuration is missing.
func New(cfg Config) (*Transcriber, error) {
	cfg.applyDefaults()

	var provider media.Transcriber
	var err error

	switch cfg.Provider {
	case "openai":
		provider, err = NewOpenAITranscriber(OpenAIConfig{
			APIKey:   cfg.APIKey,
			BaseURL:  cfg.BaseURL,
			Model:    cfg.Model,
			Language: cfg.Language,
			Logger:   cfg.Logger,
		})
	default:
		return nil, fmt.Errorf("unsupported transcription provider: %s", cfg.Provider)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create %s transcriber: %w", cfg.Provider, err)
	}

	return &Transcriber{
		provider: provider,
		name:     cfg.Provider,
		logger:   cfg.Logger.With("component", "transcriber"),
	}, nil
}

// NewWithProvider creates a Transcriber with a custom provider implementation.
// This is useful for testing or for using custom transcription providers.
func NewWithProvider(name string, provider media.Transcriber, logger *slog.Logger) *Transcriber {
	if logger == nil {
		logger = slog.Default()
	}
	return &Transcriber{
		provider: provider,
		name:     name,
		logger:   logger.With("component", "transcriber"),
	}
}
