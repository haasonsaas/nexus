package transcribe

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockTranscriber is a test implementation of media.Transcriber
type mockTranscriber struct {
	transcribeFunc func(audio io.Reader, mimeType string, language string) (string, error)
}

func (m *mockTranscriber) Transcribe(audio io.Reader, mimeType string, language string) (string, error) {
	if m.transcribeFunc != nil {
		return m.transcribeFunc(audio, mimeType, language)
	}
	return "mock transcription", nil
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "valid openai config",
			cfg: Config{
				Provider: "openai",
				APIKey:   "test-key",
			},
			wantErr: false,
		},
		{
			name: "default provider is openai",
			cfg: Config{
				APIKey: "test-key",
			},
			wantErr: false,
		},
		{
			name: "missing api key",
			cfg: Config{
				Provider: "openai",
			},
			wantErr: true,
		},
		{
			name: "unsupported provider",
			cfg: Config{
				Provider: "unsupported",
				APIKey:   "test-key",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := New(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tr == nil {
				t.Error("New() returned nil transcriber")
			}
		})
	}
}

func TestNewWithProvider(t *testing.T) {
	mock := &mockTranscriber{}
	tr := NewWithProvider("test", mock, nil)

	if tr == nil {
		t.Fatal("NewWithProvider() returned nil")
	}

	if tr.Name() != "test" {
		t.Errorf("Name() = %q, want %q", tr.Name(), "test")
	}

	result, err := tr.Transcribe(strings.NewReader("audio data"), "audio/mp3", "en")
	if err != nil {
		t.Errorf("Transcribe() error = %v", err)
	}
	if result != "mock transcription" {
		t.Errorf("Transcribe() = %q, want %q", result, "mock transcription")
	}
}

func TestTranscriber_Transcribe(t *testing.T) {
	called := false
	mock := &mockTranscriber{
		transcribeFunc: func(audio io.Reader, mimeType string, language string) (string, error) {
			called = true
			if mimeType != "audio/ogg" {
				t.Errorf("mimeType = %q, want %q", mimeType, "audio/ogg")
			}
			if language != "en" {
				t.Errorf("language = %q, want %q", language, "en")
			}
			return "Hello, world!", nil
		},
	}

	tr := NewWithProvider("test", mock, nil)

	result, err := tr.Transcribe(strings.NewReader("audio"), "audio/ogg", "en")
	if err != nil {
		t.Errorf("Transcribe() error = %v", err)
	}

	if !called {
		t.Error("underlying transcriber was not called")
	}

	if result != "Hello, world!" {
		t.Errorf("Transcribe() = %q, want %q", result, "Hello, world!")
	}
}

func TestOpenAITranscriber_NewOpenAITranscriber(t *testing.T) {
	tests := []struct {
		name    string
		cfg     OpenAIConfig
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: OpenAIConfig{
				APIKey: "test-key",
			},
			wantErr: false,
		},
		{
			name: "custom base url",
			cfg: OpenAIConfig{
				APIKey:  "test-key",
				BaseURL: "https://custom.api.com/v1/",
			},
			wantErr: false,
		},
		{
			name: "missing api key",
			cfg: OpenAIConfig{
				APIKey: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := NewOpenAITranscriber(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewOpenAITranscriber() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tr == nil {
				t.Error("NewOpenAITranscriber() returned nil transcriber")
			}
		})
	}
}

func TestOpenAITranscriber_Transcribe(t *testing.T) {
	// Create a test server that simulates OpenAI's API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Verify path
		if !strings.HasSuffix(r.URL.Path, "/audio/transcriptions") {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		// Verify authorization header
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Error("missing or invalid Authorization header")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		// Verify content type is multipart
		contentType := r.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "multipart/form-data") {
			t.Errorf("expected multipart/form-data, got %s", contentType)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Parse multipart form
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Errorf("failed to parse multipart form: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Verify model field
		model := r.FormValue("model")
		if model != "whisper-1" {
			t.Errorf("model = %q, want %q", model, "whisper-1")
		}

		// Verify response_format field
		format := r.FormValue("response_format")
		if format != "text" {
			t.Errorf("response_format = %q, want %q", format, "text")
		}

		// Return mock transcription
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("This is a test transcription."))
	}))
	defer server.Close()

	// Create transcriber with test server
	tr, err := NewOpenAITranscriber(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
		Model:   "whisper-1",
	})
	if err != nil {
		t.Fatalf("failed to create transcriber: %v", err)
	}

	// Test transcription
	audioData := []byte("fake audio data")
	result, err := tr.Transcribe(bytes.NewReader(audioData), "audio/ogg", "")
	if err != nil {
		t.Errorf("Transcribe() error = %v", err)
	}

	expected := "This is a test transcription."
	if result != expected {
		t.Errorf("Transcribe() = %q, want %q", result, expected)
	}
}

func TestOpenAITranscriber_Transcribe_WithLanguage(t *testing.T) {
	var receivedLanguage string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(10 << 20)
		receivedLanguage = r.FormValue("language")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("transcribed text"))
	}))
	defer server.Close()

	tr, _ := NewOpenAITranscriber(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})

	// Test with explicit language
	tr.Transcribe(bytes.NewReader([]byte("audio")), "audio/mp3", "es")
	if receivedLanguage != "es" {
		t.Errorf("language = %q, want %q", receivedLanguage, "es")
	}
}

func TestOpenAITranscriber_Transcribe_DefaultLanguage(t *testing.T) {
	var receivedLanguage string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseMultipartForm(10 << 20)
		receivedLanguage = r.FormValue("language")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("transcribed text"))
	}))
	defer server.Close()

	tr, _ := NewOpenAITranscriber(OpenAIConfig{
		APIKey:   "test-key",
		BaseURL:  server.URL,
		Language: "de", // Default language
	})

	// Test with no explicit language - should use default
	tr.Transcribe(bytes.NewReader([]byte("audio")), "audio/mp3", "")
	if receivedLanguage != "de" {
		t.Errorf("language = %q, want default %q", receivedLanguage, "de")
	}
}

func TestOpenAITranscriber_Transcribe_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": {"message": "Invalid file format"}}`))
	}))
	defer server.Close()

	tr, _ := NewOpenAITranscriber(OpenAIConfig{
		APIKey:  "test-key",
		BaseURL: server.URL,
	})

	_, err := tr.Transcribe(bytes.NewReader([]byte("bad audio")), "audio/mp3", "")
	if err == nil {
		t.Error("expected error for API error response")
	}

	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention status code, got: %v", err)
	}
}

func TestOpenAITranscriber_Transcribe_EmptyAudio(t *testing.T) {
	tr, _ := NewOpenAITranscriber(OpenAIConfig{
		APIKey: "test-key",
	})

	_, err := tr.Transcribe(bytes.NewReader([]byte{}), "audio/mp3", "")
	if err == nil {
		t.Error("expected error for empty audio")
	}

	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention empty audio, got: %v", err)
	}
}

func TestGetFilenameForMimeType(t *testing.T) {
	tests := []struct {
		mimeType string
		want     string
	}{
		{"audio/mp3", "audio.mp3"},
		{"audio/mpeg", "audio.mp3"},
		{"audio/ogg", "audio.ogg"},
		{"audio/opus", "audio.ogg"},
		{"audio/ogg; codecs=opus", "audio.ogg"},
		{"audio/wav", "audio.wav"},
		{"audio/x-wav", "audio.wav"},
		{"audio/flac", "audio.flac"},
		{"audio/m4a", "audio.m4a"},
		{"audio/mp4", "audio.m4a"},
		{"audio/webm", "audio.webm"},
		{"audio/unknown", "audio.mp3"}, // default
		{"", "audio.mp3"},              // default
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			got := getFilenameForMimeType(tt.mimeType)
			if got != tt.want {
				t.Errorf("getFilenameForMimeType(%q) = %q, want %q", tt.mimeType, got, tt.want)
			}
		})
	}
}

func TestSupportedMimeTypes(t *testing.T) {
	types := SupportedMimeTypes()
	if len(types) == 0 {
		t.Error("SupportedMimeTypes() returned empty slice")
	}

	// Check that common types are included
	expected := []string{"audio/mp3", "audio/ogg", "audio/wav"}
	for _, e := range expected {
		found := false
		for _, s := range types {
			if s == e {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %q in SupportedMimeTypes()", e)
		}
	}
}

func TestIsSupportedMimeType(t *testing.T) {
	tests := []struct {
		mimeType string
		want     bool
	}{
		{"audio/mp3", true},
		{"audio/ogg", true},
		{"audio/wav", true},
		{"audio/flac", true},
		{"audio/webm", true},
		{"audio/ogg; codecs=opus", true}, // with parameters
		{"AUDIO/MP3", true},              // case insensitive
		{"video/mp4", false},
		{"image/png", false},
		{"text/plain", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			got := IsSupportedMimeType(tt.mimeType)
			if got != tt.want {
				t.Errorf("IsSupportedMimeType(%q) = %v, want %v", tt.mimeType, got, tt.want)
			}
		})
	}
}

func TestConfig_ApplyDefaults(t *testing.T) {
	cfg := Config{}
	cfg.applyDefaults()

	if cfg.Provider != "openai" {
		t.Errorf("default Provider = %q, want %q", cfg.Provider, "openai")
	}

	if cfg.Model != "whisper-1" {
		t.Errorf("default Model = %q, want %q", cfg.Model, "whisper-1")
	}

	if cfg.Logger == nil {
		t.Error("default Logger should not be nil")
	}
}
