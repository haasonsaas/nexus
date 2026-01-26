package transcribe

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/media"
)

// OpenAIConfig holds configuration for the OpenAI Whisper transcriber.
type OpenAIConfig struct {
	// APIKey is the OpenAI API key (required)
	APIKey string

	// BaseURL is the base URL for the API (default: https://api.openai.com/v1)
	BaseURL string

	// Model is the Whisper model to use (default: whisper-1)
	Model string

	// Language is the default language for transcription (ISO 639-1)
	// If empty, the API will auto-detect the language
	Language string

	// Timeout is the HTTP request timeout (default: 60s)
	Timeout time.Duration

	// Logger is an optional structured logger
	Logger *slog.Logger
}

// OpenAITranscriber implements the media.Transcriber interface using OpenAI's Whisper API.
type OpenAITranscriber struct {
	apiKey     string
	baseURL    string
	model      string
	language   string
	httpClient *http.Client
	logger     *slog.Logger
}

// Verify that OpenAITranscriber implements media.Transcriber.
var _ media.Transcriber = (*OpenAITranscriber)(nil)

// NewOpenAITranscriber creates a new OpenAI Whisper transcriber.
func NewOpenAITranscriber(cfg OpenAIConfig) (*OpenAITranscriber, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("OpenAI API key is required")
	}

	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	model := cfg.Model
	if model == "" {
		model = "whisper-1"
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	return &OpenAITranscriber{
		apiKey:   cfg.APIKey,
		baseURL:  baseURL,
		model:    model,
		language: cfg.Language,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		logger: logger.With("component", "openai-transcriber"),
	}, nil
}

// Transcribe converts audio to text using OpenAI's Whisper API.
//
// Parameters:
//   - audio: Reader containing the audio data
//   - mimeType: MIME type of the audio (e.g., "audio/ogg", "audio/mp3")
//   - language: ISO 639-1 language code (e.g., "en", "es"), empty for auto-detect
//
// Returns:
//   - string: The transcribed text
//   - error: Any error that occurred during transcription
func (t *OpenAITranscriber) Transcribe(audio io.Reader, mimeType string, language string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), t.httpClient.Timeout)
	defer cancel()

	return t.TranscribeWithContext(ctx, audio, mimeType, language)
}

// TranscribeWithContext transcribes audio with a custom context for cancellation.
func (t *OpenAITranscriber) TranscribeWithContext(ctx context.Context, audio io.Reader, mimeType string, language string) (string, error) {
	// Read all audio data
	const maxAudioBytes = 25 * 1024 * 1024
	audioData, err := io.ReadAll(io.LimitReader(audio, maxAudioBytes+1))
	if err != nil {
		return "", fmt.Errorf("failed to read audio data: %w", err)
	}

	if len(audioData) == 0 {
		return "", fmt.Errorf("audio data is empty")
	}
	if len(audioData) > maxAudioBytes {
		return "", fmt.Errorf("audio data too large (%d bytes)", len(audioData))
	}

	t.logger.Debug("transcribing audio",
		"size_bytes", len(audioData),
		"mime_type", mimeType,
		"language", language,
		"model", t.model)

	// Determine filename based on MIME type
	filename := getFilenameForMimeType(mimeType)

	// Create multipart form
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	// Add file field
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(audioData); err != nil {
		return "", fmt.Errorf("failed to write audio data: %w", err)
	}

	// Add model field
	if err := writer.WriteField("model", t.model); err != nil {
		return "", fmt.Errorf("failed to write model field: %w", err)
	}

	// Add response format field
	if err := writer.WriteField("response_format", "text"); err != nil {
		return "", fmt.Errorf("failed to write response_format field: %w", err)
	}

	// Add language field (use explicit or default, empty for auto-detect)
	lang := language
	if lang == "" {
		lang = t.language
	}
	if lang != "" {
		if err := writer.WriteField("language", lang); err != nil {
			return "", fmt.Errorf("failed to write language field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create HTTP request
	url := t.baseURL + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Execute request
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		respBody, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
		if err != nil {
			return "", fmt.Errorf("failed to read response: %w", err)
		}
		t.logger.Error("transcription API error",
			"status", resp.StatusCode,
			"response", string(respBody))
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	const maxTranscriptionResponseBytes = 10 * 1024 * 1024
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTranscriptionResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}
	if len(respBody) > maxTranscriptionResponseBytes {
		return "", fmt.Errorf("transcription response too large (%d bytes)", len(respBody))
	}

	// Since we requested "text" format, response is plain text
	text := strings.TrimSpace(string(respBody))

	t.logger.Debug("transcription complete",
		"text_length", len(text))

	return text, nil
}

// getFilenameForMimeType returns an appropriate filename with extension for the given MIME type.
// OpenAI's Whisper API requires a filename with a recognized extension.
func getFilenameForMimeType(mimeType string) string {
	// Map MIME types to file extensions
	// See: https://platform.openai.com/docs/api-reference/audio/createTranscription
	switch strings.ToLower(mimeType) {
	case "audio/flac":
		return "audio.flac"
	case "audio/m4a", "audio/mp4", "audio/x-m4a":
		return "audio.m4a"
	case "audio/mpeg", "audio/mp3":
		return "audio.mp3"
	case "audio/mpga":
		return "audio.mpga"
	case "audio/ogg", "audio/opus", "audio/ogg; codecs=opus":
		// Telegram voice messages are OGG with Opus codec
		return "audio.ogg"
	case "audio/wav", "audio/x-wav":
		return "audio.wav"
	case "audio/webm":
		return "audio.webm"
	default:
		// Default to mp3 if unknown, let the API handle it
		return "audio.mp3"
	}
}

// SupportedMimeTypes returns the MIME types supported by OpenAI's Whisper API.
func SupportedMimeTypes() []string {
	return []string{
		"audio/flac",
		"audio/m4a",
		"audio/mp3",
		"audio/mp4",
		"audio/mpeg",
		"audio/mpga",
		"audio/ogg",
		"audio/opus",
		"audio/wav",
		"audio/webm",
		"audio/x-m4a",
		"audio/x-wav",
	}
}

// IsSupportedMimeType checks if a MIME type is supported for transcription.
func IsSupportedMimeType(mimeType string) bool {
	lower := strings.ToLower(mimeType)
	// Handle MIME types with parameters (e.g., "audio/ogg; codecs=opus")
	if idx := strings.Index(lower, ";"); idx != -1 {
		lower = strings.TrimSpace(lower[:idx])
	}

	for _, supported := range SupportedMimeTypes() {
		if lower == supported {
			return true
		}
	}
	return false
}
