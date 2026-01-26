// Package tts provides text-to-speech functionality with multiple provider support.
// It supports Edge TTS (free Microsoft service), OpenAI TTS, and ElevenLabs,
// with automatic fallback between providers.
package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Provider identifies a TTS provider.
type Provider string

const (
	// ProviderEdge uses Microsoft's Edge TTS service (free).
	ProviderEdge Provider = "edge"

	// ProviderOpenAI uses OpenAI's TTS API.
	ProviderOpenAI Provider = "openai"

	// ProviderElevenLabs uses ElevenLabs' TTS API.
	ProviderElevenLabs Provider = "elevenlabs"
)

// Config holds TTS configuration.
type Config struct {
	// Enabled toggles TTS functionality.
	Enabled bool `yaml:"enabled"`

	// Provider is the primary TTS provider to use.
	Provider Provider `yaml:"provider"`

	// FallbackChain specifies providers to try if the primary fails.
	// Providers are tried in order.
	FallbackChain []Provider `yaml:"fallback_chain"`

	// MaxTextLength is the maximum text length to process.
	// Text longer than this is truncated.
	// Default: 4096
	MaxTextLength int `yaml:"max_text_length"`

	// TimeoutSeconds is the timeout for TTS generation.
	// Default: 30
	TimeoutSeconds int `yaml:"timeout_seconds"`

	// OutputDir is the directory for generated audio files.
	// Default: system temp directory
	OutputDir string `yaml:"output_dir"`

	// Edge configures the Edge TTS provider.
	Edge EdgeConfig `yaml:"edge"`

	// OpenAI configures the OpenAI TTS provider.
	OpenAI OpenAIConfig `yaml:"openai"`

	// ElevenLabs configures the ElevenLabs TTS provider.
	ElevenLabs ElevenLabsConfig `yaml:"elevenlabs"`
}

// EdgeConfig configures Edge TTS.
type EdgeConfig struct {
	// Voice is the Edge TTS voice to use.
	// Example: "en-US-MichelleNeural", "en-US-GuyNeural"
	// Default: "en-US-AriaNeural"
	Voice string `yaml:"voice"`

	// OutputFormat is the audio output format.
	// Example: "audio-24khz-48kbitrate-mono-mp3", "audio-24khz-96kbitrate-mono-mp3"
	// Default: "audio-24khz-48kbitrate-mono-mp3"
	OutputFormat string `yaml:"output_format"`
}

// OpenAIConfig configures OpenAI TTS.
type OpenAIConfig struct {
	// APIKey is the OpenAI API key.
	APIKey string `yaml:"api_key"`

	// Model is the TTS model to use.
	// Options: "tts-1", "tts-1-hd", "gpt-4o-mini-tts"
	// Default: "tts-1"
	Model string `yaml:"model"`

	// Voice is the voice to use.
	// Options: "alloy", "echo", "fable", "onyx", "nova", "shimmer"
	// Default: "alloy"
	Voice string `yaml:"voice"`

	// ResponseFormat is the audio format.
	// Options: "mp3", "opus", "aac", "flac", "wav", "pcm"
	// Default: "mp3"
	ResponseFormat string `yaml:"response_format"`

	// Speed is the speech speed (0.25 to 4.0).
	// Default: 1.0
	Speed float64 `yaml:"speed"`

	// BaseURL is the API base URL (optional).
	BaseURL string `yaml:"base_url"`
}

// ElevenLabsConfig configures ElevenLabs TTS.
type ElevenLabsConfig struct {
	// APIKey is the ElevenLabs API key.
	APIKey string `yaml:"api_key"`

	// VoiceID is the voice ID to use.
	// Default: "21m00Tcm4TlvDq8ikWAM" (Rachel)
	VoiceID string `yaml:"voice_id"`

	// ModelID is the model to use.
	// Options: "eleven_monolingual_v1", "eleven_multilingual_v2", "eleven_turbo_v2"
	// Default: "eleven_monolingual_v1"
	ModelID string `yaml:"model_id"`

	// OutputFormat is the audio format.
	// Options: "mp3_44100_128", "mp3_44100_192", "pcm_16000", "pcm_22050", "pcm_24000"
	// Default: "mp3_44100_128"
	OutputFormat string `yaml:"output_format"`

	// Stability controls voice stability (0.0 to 1.0).
	// Default: 0.5
	Stability float64 `yaml:"stability"`

	// SimilarityBoost controls voice similarity (0.0 to 1.0).
	// Default: 0.75
	SimilarityBoost float64 `yaml:"similarity_boost"`
}

// Result contains the outcome of a TTS operation.
type Result struct {
	// Success indicates whether TTS generation succeeded.
	Success bool `json:"success"`

	// AudioPath is the path to the generated audio file.
	AudioPath string `json:"audio_path,omitempty"`

	// Provider is the provider that generated the audio.
	Provider Provider `json:"provider"`

	// OutputFormat is the format of the generated audio.
	OutputFormat string `json:"output_format,omitempty"`

	// LatencyMs is the time taken to generate the audio in milliseconds.
	LatencyMs int64 `json:"latency_ms"`

	// Error contains the error message if generation failed.
	Error string `json:"error,omitempty"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Enabled:        false,
		Provider:       ProviderEdge,
		MaxTextLength:  4096,
		TimeoutSeconds: 30,
		Edge: EdgeConfig{
			Voice:        "en-US-AriaNeural",
			OutputFormat: "audio-24khz-48kbitrate-mono-mp3",
		},
		OpenAI: OpenAIConfig{
			Model:          "tts-1",
			Voice:          "alloy",
			ResponseFormat: "mp3",
			Speed:          1.0,
		},
		ElevenLabs: ElevenLabsConfig{
			VoiceID:         "21m00Tcm4TlvDq8ikWAM",
			ModelID:         "eleven_monolingual_v1",
			OutputFormat:    "mp3_44100_128",
			Stability:       0.5,
			SimilarityBoost: 0.75,
		},
	}
}

// ApplyDefaults applies default values to empty config fields.
func (c *Config) ApplyDefaults() {
	defaults := DefaultConfig()

	if c.MaxTextLength <= 0 {
		c.MaxTextLength = defaults.MaxTextLength
	}
	if c.TimeoutSeconds <= 0 {
		c.TimeoutSeconds = defaults.TimeoutSeconds
	}

	// Edge defaults
	if c.Edge.Voice == "" {
		c.Edge.Voice = defaults.Edge.Voice
	}
	if c.Edge.OutputFormat == "" {
		c.Edge.OutputFormat = defaults.Edge.OutputFormat
	}

	// OpenAI defaults
	if c.OpenAI.Model == "" {
		c.OpenAI.Model = defaults.OpenAI.Model
	}
	if c.OpenAI.Voice == "" {
		c.OpenAI.Voice = defaults.OpenAI.Voice
	}
	if c.OpenAI.ResponseFormat == "" {
		c.OpenAI.ResponseFormat = defaults.OpenAI.ResponseFormat
	}
	if c.OpenAI.Speed == 0 {
		c.OpenAI.Speed = defaults.OpenAI.Speed
	}

	// ElevenLabs defaults
	if c.ElevenLabs.VoiceID == "" {
		c.ElevenLabs.VoiceID = defaults.ElevenLabs.VoiceID
	}
	if c.ElevenLabs.ModelID == "" {
		c.ElevenLabs.ModelID = defaults.ElevenLabs.ModelID
	}
	if c.ElevenLabs.OutputFormat == "" {
		c.ElevenLabs.OutputFormat = defaults.ElevenLabs.OutputFormat
	}
	if c.ElevenLabs.Stability == 0 {
		c.ElevenLabs.Stability = defaults.ElevenLabs.Stability
	}
	if c.ElevenLabs.SimilarityBoost == 0 {
		c.ElevenLabs.SimilarityBoost = defaults.ElevenLabs.SimilarityBoost
	}
}

// TextToSpeech converts text to audio using the configured provider with fallback.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts
//   - cfg: TTS configuration
//   - text: Text to convert to speech
//   - channel: Optional channel identifier for format optimization (e.g., "telegram" for opus)
//
// Returns:
//   - *Result: The TTS result with audio path or error
//   - error: Error if all providers fail
func TextToSpeech(ctx context.Context, cfg *Config, text string, channel string) (*Result, error) {
	if cfg == nil {
		return nil, errors.New("tts: config is nil")
	}

	if !cfg.Enabled {
		return nil, errors.New("tts: not enabled")
	}

	if strings.TrimSpace(text) == "" {
		return nil, errors.New("tts: text is empty")
	}

	// Apply defaults
	cfg.ApplyDefaults()

	// Truncate text if too long
	if len(text) > cfg.MaxTextLength {
		text = text[:cfg.MaxTextLength]
	}

	// Build provider chain: primary + fallbacks
	providers := []Provider{cfg.Provider}
	providers = append(providers, cfg.FallbackChain...)

	// Create timeout context
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for _, provider := range providers {
		result, err := synthesize(ctx, cfg, text, channel, provider)
		if err == nil && result.Success {
			return result, nil
		}
		if err != nil {
			lastErr = err
		} else if result.Error != "" {
			lastErr = errors.New(result.Error)
		}
	}

	if lastErr != nil {
		return &Result{
			Success:  false,
			Provider: cfg.Provider,
			Error:    lastErr.Error(),
		}, lastErr
	}

	return &Result{
		Success:  false,
		Provider: cfg.Provider,
		Error:    "all providers failed",
	}, errors.New("tts: all providers failed")
}

// synthesize generates audio using a specific provider.
func synthesize(ctx context.Context, cfg *Config, text, channel string, provider Provider) (*Result, error) {
	start := time.Now()

	var result *Result
	var err error

	switch provider {
	case ProviderEdge:
		result, err = edgeTTS(ctx, cfg, text)
	case ProviderOpenAI:
		result, err = openaiTTS(ctx, cfg, text, channel)
	case ProviderElevenLabs:
		result, err = elevenlabsTTS(ctx, cfg, text)
	default:
		return nil, fmt.Errorf("tts: unknown provider: %s", provider)
	}

	if result != nil {
		result.LatencyMs = time.Since(start).Milliseconds()
		result.Provider = provider
	}

	return result, err
}

// edgeTTS uses Microsoft's Edge TTS service via the edge-tts CLI.
// This is a free service that uses the same voices as Microsoft Edge's Read Aloud feature.
func edgeTTS(ctx context.Context, cfg *Config, text string) (*Result, error) {
	// Check if edge-tts is installed
	_, err := exec.LookPath("edge-tts")
	if err != nil {
		return nil, errors.New("tts: edge-tts not installed (pip install edge-tts)")
	}

	// Create output file
	outputDir := cfg.OutputDir
	if outputDir == "" {
		outputDir = os.TempDir()
	}

	outputPath := filepath.Join(outputDir, fmt.Sprintf("tts_%s.mp3", uuid.New().String()))

	// Build command
	args := []string{
		"--voice", cfg.Edge.Voice,
		"--text", text,
		"--write-media", outputPath,
	}

	cmd := exec.CommandContext(ctx, "edge-tts", args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Check for context cancellation
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return &Result{
			Success: false,
			Error:   fmt.Sprintf("edge-tts failed: %v: %s", err, stderr.String()),
		}, err
	}

	// Verify file was created
	if _, err := os.Stat(outputPath); err != nil {
		return &Result{
			Success: false,
			Error:   "edge-tts: output file not created",
		}, errors.New("tts: edge-tts output file not created")
	}

	return &Result{
		Success:      true,
		AudioPath:    outputPath,
		OutputFormat: "mp3",
	}, nil
}

// openaiTTS uses OpenAI's TTS API.
func openaiTTS(ctx context.Context, cfg *Config, text, channel string) (*Result, error) {
	if cfg.OpenAI.APIKey == "" {
		return nil, errors.New("tts: OpenAI API key not configured")
	}

	// Determine output format based on channel
	format := cfg.OpenAI.ResponseFormat
	if channel == "telegram" {
		// Telegram voice notes use opus
		format = "opus"
	}

	// Build request body
	requestBody := map[string]interface{}{
		"model": cfg.OpenAI.Model,
		"input": text,
		"voice": cfg.OpenAI.Voice,
	}

	if format != "" {
		requestBody["response_format"] = format
	}

	if cfg.OpenAI.Speed != 1.0 && cfg.OpenAI.Speed > 0 {
		requestBody["speed"] = cfg.OpenAI.Speed
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("tts: failed to marshal request: %w", err)
	}

	// Build URL
	baseURL := cfg.OpenAI.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	url := baseURL + "/audio/speech"

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("tts: failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+cfg.OpenAI.APIKey)
	req.Header.Set("Content-Type", "application/json")

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts: OpenAI request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		if err != nil {
			return &Result{
				Success: false,
				Error:   fmt.Sprintf("OpenAI TTS error: %s", resp.Status),
			}, fmt.Errorf("tts: OpenAI returned %s", resp.Status)
		}
		return &Result{
			Success: false,
			Error:   fmt.Sprintf("OpenAI TTS error: %s - %s", resp.Status, string(body)),
		}, fmt.Errorf("tts: OpenAI returned %s", resp.Status)
	}

	// Create output file
	outputDir := cfg.OutputDir
	if outputDir == "" {
		outputDir = os.TempDir()
	}

	ext := format
	if ext == "" {
		ext = "mp3"
	}
	outputPath := filepath.Join(outputDir, fmt.Sprintf("tts_%s.%s", uuid.New().String(), ext))

	outFile, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("tts: failed to create output file: %w", err)
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, resp.Body); err != nil {
		return nil, fmt.Errorf("tts: failed to write audio: %w", err)
	}

	return &Result{
		Success:      true,
		AudioPath:    outputPath,
		OutputFormat: ext,
	}, nil
}

// elevenlabsTTS uses ElevenLabs' TTS API.
func elevenlabsTTS(ctx context.Context, cfg *Config, text string) (*Result, error) {
	if cfg.ElevenLabs.APIKey == "" {
		return nil, errors.New("tts: ElevenLabs API key not configured")
	}

	// Build request body
	requestBody := map[string]interface{}{
		"text":     text,
		"model_id": cfg.ElevenLabs.ModelID,
		"voice_settings": map[string]interface{}{
			"stability":        cfg.ElevenLabs.Stability,
			"similarity_boost": cfg.ElevenLabs.SimilarityBoost,
		},
	}

	jsonBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("tts: failed to marshal request: %w", err)
	}

	// Build URL
	url := fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s", cfg.ElevenLabs.VoiceID)

	// Add output format query parameter
	if cfg.ElevenLabs.OutputFormat != "" {
		url += "?output_format=" + cfg.ElevenLabs.OutputFormat
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("tts: failed to create request: %w", err)
	}

	req.Header.Set("xi-api-key", cfg.ElevenLabs.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/mpeg")

	// Send request
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tts: ElevenLabs request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		if err != nil {
			return &Result{
				Success: false,
				Error:   fmt.Sprintf("ElevenLabs TTS error: %s", resp.Status),
			}, fmt.Errorf("tts: ElevenLabs returned %s", resp.Status)
		}
		return &Result{
			Success: false,
			Error:   fmt.Sprintf("ElevenLabs TTS error: %s - %s", resp.Status, string(body)),
		}, fmt.Errorf("tts: ElevenLabs returned %s", resp.Status)
	}

	// Create output file
	outputDir := cfg.OutputDir
	if outputDir == "" {
		outputDir = os.TempDir()
	}

	// Determine extension from output format
	ext := "mp3"
	if strings.HasPrefix(cfg.ElevenLabs.OutputFormat, "pcm_") {
		ext = "pcm"
	}

	outputPath := filepath.Join(outputDir, fmt.Sprintf("tts_%s.%s", uuid.New().String(), ext))

	outFile, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("tts: failed to create output file: %w", err)
	}
	defer outFile.Close()

	if _, err := io.Copy(outFile, resp.Body); err != nil {
		return nil, fmt.Errorf("tts: failed to write audio: %w", err)
	}

	return &Result{
		Success:      true,
		AudioPath:    outputPath,
		OutputFormat: ext,
	}, nil
}

// GetOutputFormatForChannel returns the optimal audio format for a given channel.
func GetOutputFormatForChannel(channel string) string {
	switch strings.ToLower(channel) {
	case "telegram":
		// Telegram voice notes require opus
		return "opus"
	case "discord":
		// Discord supports opus natively
		return "opus"
	case "slack", "whatsapp", "signal":
		// These channels work well with mp3
		return "mp3"
	default:
		return "mp3"
	}
}

// AvailableEdgeVoices returns a list of commonly used Edge TTS voices.
func AvailableEdgeVoices() []string {
	return []string{
		// US English
		"en-US-AriaNeural",
		"en-US-JennyNeural",
		"en-US-GuyNeural",
		"en-US-MichelleNeural",
		"en-US-DavisNeural",
		"en-US-AmberNeural",
		"en-US-AnaNeural",
		"en-US-ChristopherNeural",
		"en-US-EricNeural",

		// UK English
		"en-GB-SoniaNeural",
		"en-GB-RyanNeural",
		"en-GB-LibbyNeural",

		// Australian English
		"en-AU-NatashaNeural",
		"en-AU-WilliamNeural",

		// Other languages
		"es-ES-ElviraNeural",
		"fr-FR-DeniseNeural",
		"de-DE-KatjaNeural",
		"it-IT-ElsaNeural",
		"ja-JP-NanamiNeural",
		"ko-KR-SunHiNeural",
		"zh-CN-XiaoxiaoNeural",
		"pt-BR-FranciscaNeural",
	}
}

// AvailableOpenAIVoices returns the list of OpenAI TTS voices.
func AvailableOpenAIVoices() []string {
	return []string{
		"alloy",
		"echo",
		"fable",
		"onyx",
		"nova",
		"shimmer",
	}
}

// ValidateConfig validates the TTS configuration.
func ValidateConfig(cfg *Config) error {
	if cfg == nil {
		return errors.New("tts: config is nil")
	}

	if !cfg.Enabled {
		return nil // Disabled config is always valid
	}

	// Validate primary provider
	switch cfg.Provider {
	case ProviderEdge, ProviderOpenAI, ProviderElevenLabs:
		// Valid
	default:
		return fmt.Errorf("tts: invalid provider: %s", cfg.Provider)
	}

	// Validate provider-specific config
	switch cfg.Provider {
	case ProviderOpenAI:
		if cfg.OpenAI.APIKey == "" {
			return errors.New("tts: OpenAI API key is required")
		}
	case ProviderElevenLabs:
		if cfg.ElevenLabs.APIKey == "" {
			return errors.New("tts: ElevenLabs API key is required")
		}
	}

	// Validate fallback chain
	for _, p := range cfg.FallbackChain {
		switch p {
		case ProviderEdge, ProviderOpenAI, ProviderElevenLabs:
			// Valid
		default:
			return fmt.Errorf("tts: invalid fallback provider: %s", p)
		}
	}

	if cfg.MaxTextLength < 0 {
		return errors.New("tts: max_text_length must be >= 0")
	}

	if cfg.TimeoutSeconds < 0 {
		return errors.New("tts: timeout_seconds must be >= 0")
	}

	if cfg.OpenAI.Speed < 0 || cfg.OpenAI.Speed > 4.0 {
		return errors.New("tts: OpenAI speed must be between 0 and 4.0")
	}

	if cfg.ElevenLabs.Stability < 0 || cfg.ElevenLabs.Stability > 1.0 {
		return errors.New("tts: ElevenLabs stability must be between 0 and 1.0")
	}

	if cfg.ElevenLabs.SimilarityBoost < 0 || cfg.ElevenLabs.SimilarityBoost > 1.0 {
		return errors.New("tts: ElevenLabs similarity_boost must be between 0 and 1.0")
	}

	return nil
}

// Cleanup removes a temporary audio file created by TTS.
func Cleanup(result *Result) error {
	if result == nil || result.AudioPath == "" {
		return nil
	}
	return os.Remove(result.AudioPath)
}
