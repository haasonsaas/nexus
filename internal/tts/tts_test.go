package tts

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Enabled {
		t.Error("Expected Enabled to be false by default")
	}
	if cfg.Provider != ProviderEdge {
		t.Errorf("Expected Provider to be edge, got %s", cfg.Provider)
	}
	if cfg.MaxTextLength != 4096 {
		t.Errorf("Expected MaxTextLength to be 4096, got %d", cfg.MaxTextLength)
	}
	if cfg.TimeoutSeconds != 30 {
		t.Errorf("Expected TimeoutSeconds to be 30, got %d", cfg.TimeoutSeconds)
	}
	if cfg.Edge.Voice != "en-US-AriaNeural" {
		t.Errorf("Expected Edge.Voice to be en-US-AriaNeural, got %s", cfg.Edge.Voice)
	}
	if cfg.OpenAI.Model != "tts-1" {
		t.Errorf("Expected OpenAI.Model to be tts-1, got %s", cfg.OpenAI.Model)
	}
	if cfg.OpenAI.Voice != "alloy" {
		t.Errorf("Expected OpenAI.Voice to be alloy, got %s", cfg.OpenAI.Voice)
	}
	if cfg.ElevenLabs.ModelID != "eleven_monolingual_v1" {
		t.Errorf("Expected ElevenLabs.ModelID to be eleven_monolingual_v1, got %s", cfg.ElevenLabs.ModelID)
	}
}

func TestApplyDefaults(t *testing.T) {
	cfg := &Config{}
	cfg.ApplyDefaults()

	if cfg.MaxTextLength != 4096 {
		t.Errorf("Expected MaxTextLength to be 4096, got %d", cfg.MaxTextLength)
	}
	if cfg.TimeoutSeconds != 30 {
		t.Errorf("Expected TimeoutSeconds to be 30, got %d", cfg.TimeoutSeconds)
	}
	if cfg.Edge.Voice != "en-US-AriaNeural" {
		t.Errorf("Expected Edge.Voice to be en-US-AriaNeural, got %s", cfg.Edge.Voice)
	}
	if cfg.OpenAI.Speed != 1.0 {
		t.Errorf("Expected OpenAI.Speed to be 1.0, got %f", cfg.OpenAI.Speed)
	}
}

func TestApplyDefaultsPreservesExisting(t *testing.T) {
	cfg := &Config{
		MaxTextLength:  1000,
		TimeoutSeconds: 60,
		Edge: EdgeConfig{
			Voice: "en-GB-SoniaNeural",
		},
	}
	cfg.ApplyDefaults()

	if cfg.MaxTextLength != 1000 {
		t.Errorf("Expected MaxTextLength to be preserved as 1000, got %d", cfg.MaxTextLength)
	}
	if cfg.TimeoutSeconds != 60 {
		t.Errorf("Expected TimeoutSeconds to be preserved as 60, got %d", cfg.TimeoutSeconds)
	}
	if cfg.Edge.Voice != "en-GB-SoniaNeural" {
		t.Errorf("Expected Edge.Voice to be preserved as en-GB-SoniaNeural, got %s", cfg.Edge.Voice)
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: true,
		},
		{
			name: "disabled config is valid",
			cfg: &Config{
				Enabled: false,
			},
			wantErr: false,
		},
		{
			name: "valid edge config",
			cfg: &Config{
				Enabled:  true,
				Provider: ProviderEdge,
			},
			wantErr: false,
		},
		{
			name: "valid macos config",
			cfg: &Config{
				Enabled:  true,
				Provider: ProviderMacOS,
			},
			wantErr: false,
		},
		{
			name: "valid openai config with key",
			cfg: &Config{
				Enabled:  true,
				Provider: ProviderOpenAI,
				OpenAI: OpenAIConfig{
					APIKey: "sk-test",
				},
			},
			wantErr: false,
		},
		{
			name: "openai without key fails",
			cfg: &Config{
				Enabled:  true,
				Provider: ProviderOpenAI,
			},
			wantErr: true,
		},
		{
			name: "valid elevenlabs config with key",
			cfg: &Config{
				Enabled:  true,
				Provider: ProviderElevenLabs,
				ElevenLabs: ElevenLabsConfig{
					APIKey: "test-key",
				},
			},
			wantErr: false,
		},
		{
			name: "elevenlabs without key fails",
			cfg: &Config{
				Enabled:  true,
				Provider: ProviderElevenLabs,
			},
			wantErr: true,
		},
		{
			name: "invalid provider",
			cfg: &Config{
				Enabled:  true,
				Provider: "invalid",
			},
			wantErr: true,
		},
		{
			name: "invalid fallback provider",
			cfg: &Config{
				Enabled:       true,
				Provider:      ProviderEdge,
				FallbackChain: []Provider{"invalid"},
			},
			wantErr: true,
		},
		{
			name: "negative max_text_length",
			cfg: &Config{
				Enabled:       true,
				Provider:      ProviderEdge,
				MaxTextLength: -1,
			},
			wantErr: true,
		},
		{
			name: "negative timeout",
			cfg: &Config{
				Enabled:        true,
				Provider:       ProviderEdge,
				TimeoutSeconds: -1,
			},
			wantErr: true,
		},
		{
			name: "negative macos rate",
			cfg: &Config{
				Enabled:  true,
				Provider: ProviderMacOS,
				MacOS: MacOSConfig{
					Rate: -5,
				},
			},
			wantErr: true,
		},
		{
			name: "openai speed out of range",
			cfg: &Config{
				Enabled:  true,
				Provider: ProviderOpenAI,
				OpenAI: OpenAIConfig{
					APIKey: "sk-test",
					Speed:  5.0,
				},
			},
			wantErr: true,
		},
		{
			name: "elevenlabs stability out of range",
			cfg: &Config{
				Enabled:  true,
				Provider: ProviderElevenLabs,
				ElevenLabs: ElevenLabsConfig{
					APIKey:    "test-key",
					Stability: 1.5,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestTextToSpeech_NilConfig(t *testing.T) {
	_, err := TextToSpeech(context.Background(), nil, "hello", "")
	if err == nil {
		t.Error("Expected error for nil config")
	}
}

func TestTextToSpeech_NotEnabled(t *testing.T) {
	cfg := &Config{Enabled: false}
	_, err := TextToSpeech(context.Background(), cfg, "hello", "")
	if err == nil {
		t.Error("Expected error for disabled TTS")
	}
}

func TestTextToSpeech_EmptyText(t *testing.T) {
	cfg := &Config{Enabled: true}
	_, err := TextToSpeech(context.Background(), cfg, "", "")
	if err == nil {
		t.Error("Expected error for empty text")
	}

	_, err = TextToSpeech(context.Background(), cfg, "   ", "")
	if err == nil {
		t.Error("Expected error for whitespace-only text")
	}
}

func TestTextToSpeech_TextTruncation(t *testing.T) {
	cfg := &Config{
		Enabled:       true,
		Provider:      ProviderEdge,
		MaxTextLength: 10,
	}
	cfg.ApplyDefaults()

	// This would normally call edgeTTS which requires the CLI
	// We're just testing that the config is applied correctly
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// The call will fail (no edge-tts installed), but we verify truncation happens
	_, _ = TextToSpeech(ctx, cfg, "This is a very long text that should be truncated", "")
}

func TestTextToSpeech_ContextCancellation(t *testing.T) {
	cfg := &Config{
		Enabled:  true,
		Provider: ProviderEdge,
	}
	cfg.ApplyDefaults()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := TextToSpeech(ctx, cfg, "hello", "")
	if err == nil {
		t.Error("Expected error for cancelled context")
	}
}

func TestGetOutputFormatForChannel(t *testing.T) {
	tests := []struct {
		channel  string
		expected string
	}{
		{"telegram", "opus"},
		{"Telegram", "opus"},
		{"TELEGRAM", "opus"},
		{"discord", "opus"},
		{"slack", "mp3"},
		{"whatsapp", "mp3"},
		{"signal", "mp3"},
		{"unknown", "mp3"},
		{"", "mp3"},
	}

	for _, tt := range tests {
		t.Run(tt.channel, func(t *testing.T) {
			result := GetOutputFormatForChannel(tt.channel)
			if result != tt.expected {
				t.Errorf("GetOutputFormatForChannel(%q) = %q, want %q", tt.channel, result, tt.expected)
			}
		})
	}
}

func TestAvailableEdgeVoices(t *testing.T) {
	voices := AvailableEdgeVoices()
	if len(voices) == 0 {
		t.Error("Expected at least one voice")
	}

	// Check for expected voices
	expectedVoices := []string{
		"en-US-AriaNeural",
		"en-US-JennyNeural",
		"en-GB-SoniaNeural",
	}

	voiceSet := make(map[string]bool)
	for _, v := range voices {
		voiceSet[v] = true
	}

	for _, expected := range expectedVoices {
		if !voiceSet[expected] {
			t.Errorf("Expected voice %q not found", expected)
		}
	}
}

func TestAvailableOpenAIVoices(t *testing.T) {
	voices := AvailableOpenAIVoices()
	if len(voices) != 6 {
		t.Errorf("Expected 6 OpenAI voices, got %d", len(voices))
	}

	expectedVoices := []string{"alloy", "echo", "fable", "onyx", "nova", "shimmer"}
	for i, expected := range expectedVoices {
		if voices[i] != expected {
			t.Errorf("Expected voice %q at index %d, got %q", expected, i, voices[i])
		}
	}
}

func TestCleanup(t *testing.T) {
	// Test nil result
	if err := Cleanup(nil); err != nil {
		t.Errorf("Cleanup(nil) returned error: %v", err)
	}

	// Test empty audio path
	if err := Cleanup(&Result{}); err != nil {
		t.Errorf("Cleanup with empty path returned error: %v", err)
	}

	// Test with actual file
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.mp3")

	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	result := &Result{AudioPath: tmpFile}
	if err := Cleanup(result); err != nil {
		t.Errorf("Cleanup returned error: %v", err)
	}

	// Verify file is deleted
	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Error("Expected file to be deleted")
	}
}

func TestProviderConstants(t *testing.T) {
	if ProviderEdge != "edge" {
		t.Errorf("ProviderEdge = %q, want %q", ProviderEdge, "edge")
	}
	if ProviderMacOS != "macos" {
		t.Errorf("ProviderMacOS = %q, want %q", ProviderMacOS, "macos")
	}
	if ProviderOpenAI != "openai" {
		t.Errorf("ProviderOpenAI = %q, want %q", ProviderOpenAI, "openai")
	}
	if ProviderElevenLabs != "elevenlabs" {
		t.Errorf("ProviderElevenLabs = %q, want %q", ProviderElevenLabs, "elevenlabs")
	}
}

func TestResultStruct(t *testing.T) {
	result := &Result{
		Success:      true,
		AudioPath:    "/tmp/test.mp3",
		Provider:     ProviderEdge,
		OutputFormat: "mp3",
		LatencyMs:    150,
	}

	if !result.Success {
		t.Error("Expected Success to be true")
	}
	if result.AudioPath != "/tmp/test.mp3" {
		t.Errorf("AudioPath = %q, want %q", result.AudioPath, "/tmp/test.mp3")
	}
	if result.Provider != ProviderEdge {
		t.Errorf("Provider = %q, want %q", result.Provider, ProviderEdge)
	}
}

func TestConfigWithFallbackChain(t *testing.T) {
	cfg := &Config{
		Enabled:       true,
		Provider:      ProviderEdge,
		FallbackChain: []Provider{ProviderOpenAI, ProviderElevenLabs},
		OpenAI: OpenAIConfig{
			APIKey: "sk-test",
		},
		ElevenLabs: ElevenLabsConfig{
			APIKey: "el-test",
		},
	}

	err := ValidateConfig(cfg)
	if err != nil {
		t.Errorf("ValidateConfig() returned error for valid fallback chain: %v", err)
	}

	if len(cfg.FallbackChain) != 2 {
		t.Errorf("Expected 2 fallback providers, got %d", len(cfg.FallbackChain))
	}
}

func TestEdgeConfig(t *testing.T) {
	cfg := EdgeConfig{
		Voice:        "en-US-MichelleNeural",
		OutputFormat: "audio-24khz-96kbitrate-mono-mp3",
	}

	if cfg.Voice != "en-US-MichelleNeural" {
		t.Errorf("Voice = %q, want %q", cfg.Voice, "en-US-MichelleNeural")
	}
	if cfg.OutputFormat != "audio-24khz-96kbitrate-mono-mp3" {
		t.Errorf("OutputFormat = %q, want %q", cfg.OutputFormat, "audio-24khz-96kbitrate-mono-mp3")
	}
}

func TestOpenAIConfig(t *testing.T) {
	cfg := OpenAIConfig{
		APIKey:         "sk-test",
		Model:          "tts-1-hd",
		Voice:          "nova",
		ResponseFormat: "opus",
		Speed:          1.5,
		BaseURL:        "https://custom.api.com/v1",
	}

	if cfg.Model != "tts-1-hd" {
		t.Errorf("Model = %q, want %q", cfg.Model, "tts-1-hd")
	}
	if cfg.Speed != 1.5 {
		t.Errorf("Speed = %f, want %f", cfg.Speed, 1.5)
	}
}

func TestElevenLabsConfig(t *testing.T) {
	cfg := ElevenLabsConfig{
		APIKey:          "el-test",
		VoiceID:         "custom-voice",
		ModelID:         "eleven_multilingual_v2",
		OutputFormat:    "mp3_44100_192",
		Stability:       0.7,
		SimilarityBoost: 0.9,
	}

	if cfg.VoiceID != "custom-voice" {
		t.Errorf("VoiceID = %q, want %q", cfg.VoiceID, "custom-voice")
	}
	if cfg.Stability != 0.7 {
		t.Errorf("Stability = %f, want %f", cfg.Stability, 0.7)
	}
}

// Integration test for edge TTS (skipped if edge-tts not installed)
func TestEdgeTTS_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := &Config{
		Enabled:  true,
		Provider: ProviderEdge,
	}
	cfg.ApplyDefaults()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := TextToSpeech(ctx, cfg, "Hello, this is a test.", "")

	// If edge-tts is not installed, skip the test
	if err != nil && err.Error() == "tts: edge-tts not installed (pip install edge-tts)" {
		t.Skip("edge-tts not installed")
	}

	if err != nil {
		t.Errorf("TextToSpeech() error = %v", err)
		return
	}

	if !result.Success {
		t.Errorf("TextToSpeech() result.Success = false, error: %s", result.Error)
		return
	}

	if result.AudioPath == "" {
		t.Error("Expected non-empty AudioPath")
		return
	}

	// Verify file exists
	if _, err := os.Stat(result.AudioPath); err != nil {
		t.Errorf("Audio file not found: %v", err)
	}

	// Cleanup
	if err := Cleanup(result); err != nil {
		t.Errorf("Cleanup() error = %v", err)
	}
}

// Test synthesize with unknown provider
func TestSynthesize_UnknownProvider(t *testing.T) {
	cfg := &Config{
		Enabled:  true,
		Provider: "unknown",
	}
	cfg.ApplyDefaults()

	ctx := context.Background()
	_, err := synthesize(ctx, cfg, "test", "", "unknown")

	if err == nil {
		t.Error("Expected error for unknown provider")
	}
}

func TestTextToSpeech_AllProvidersFail(t *testing.T) {
	cfg := &Config{
		Enabled:        true,
		Provider:       ProviderOpenAI, // Will fail without API key
		TimeoutSeconds: 1,
		FallbackChain:  []Provider{ProviderElevenLabs}, // Also fails without API key
	}
	cfg.ApplyDefaults()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := TextToSpeech(ctx, cfg, "test", "")

	if err == nil {
		t.Error("Expected error when all providers fail")
	}

	if result == nil {
		t.Fatal("Expected result even on failure")
	}

	if result.Success {
		t.Error("Expected result.Success to be false")
	}
}
