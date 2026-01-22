package gateway

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/media"
	"github.com/haasonsaas/nexus/pkg/models"
)

type stubTranscriber struct {
	text     string
	language string
}

func (s *stubTranscriber) Transcribe(audio io.Reader, mimeType string, language string) (string, error) {
	s.language = language
	return s.text, nil
}

func TestEnrichMessageWithMedia_AppendsTranscription(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	transcriber := &stubTranscriber{text: "hello from audio"}
	processor := media.NewDefaultProcessor(logger)
	processor.SetTranscriber(transcriber)
	aggregator := media.NewAggregator(processor, logger)

	server := &Server{
		config: &config.Config{
			Transcription: config.TranscriptionConfig{
				Enabled:  true,
				Language: "en",
			},
		},
		mediaAggregator: aggregator,
		logger:          logger,
		channels:        channels.NewRegistry(),
	}

	audioServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("fake audio bytes"))
	}))
	defer audioServer.Close()

	msg := &models.Message{
		ID:      "msg-1",
		Channel: models.ChannelTelegram,
		Role:    models.RoleUser,
		Content: "voice note:",
		Attachments: []models.Attachment{
			{
				ID:       "att-1",
				Type:     "audio",
				URL:      audioServer.URL,
				MimeType: "audio/ogg",
			},
		},
		Metadata: map[string]any{},
	}

	server.enrichMessageWithMedia(context.Background(), msg)

	if !strings.Contains(msg.Content, "hello from audio") {
		t.Fatalf("expected transcription appended to content, got %q", msg.Content)
	}
	if msg.Metadata["media_text"] == nil {
		t.Fatalf("expected media_text metadata to be set")
	}
	if transcriber.language != "en" {
		t.Fatalf("expected transcription language 'en', got %q", transcriber.language)
	}
}
