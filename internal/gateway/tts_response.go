package gateway

import (
	"context"
	"mime"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/haasonsaas/nexus/internal/tts"
	"github.com/haasonsaas/nexus/pkg/models"
)

func (s *Server) maybeAttachTTSAudio(ctx context.Context, inbound *models.Message, outbound *models.Message) func() {
	if s == nil || s.config == nil || outbound == nil || inbound == nil {
		return func() {}
	}
	if !s.config.TTS.Enabled {
		return func() {}
	}
	if strings.TrimSpace(outbound.Content) == "" {
		return func() {}
	}
	if !shouldGenerateTTSAudio(inbound) {
		return func() {}
	}

	result, err := tts.TextToSpeech(ctx, &s.config.TTS, outbound.Content, string(inbound.Channel))
	if err != nil {
		if s.logger != nil {
			s.logger.Debug("tts synthesis failed", "error", err)
		}
		return func() {}
	}
	if result == nil || !result.Success || strings.TrimSpace(result.AudioPath) == "" {
		if s.logger != nil {
			errMsg := ""
			if result != nil {
				errMsg = result.Error
			}
			s.logger.Debug("tts synthesis unsuccessful", "error", errMsg)
		}
		return func() {}
	}

	filename := strings.TrimSpace(filepath.Base(result.AudioPath))
	if filename == "" || filename == "." || filename == string(filepath.Separator) {
		filename = "tts"
	}

	mimeType := mime.TypeByExtension(filepath.Ext(filename))
	if mimeType == "" {
		mimeType = mimeTypeFromTTSExt(result.OutputFormat)
	}

	outbound.Attachments = append(outbound.Attachments, models.Attachment{
		ID:       uuid.NewString(),
		Type:     "audio",
		URL:      result.AudioPath,
		Filename: filename,
		MimeType: mimeType,
	})

	if outbound.Metadata == nil {
		outbound.Metadata = map[string]any{}
	}
	outbound.Metadata["tts_provider"] = string(result.Provider)

	return func() {
		_ = tts.Cleanup(result)
	}
}

func shouldGenerateTTSAudio(msg *models.Message) bool {
	if msg == nil {
		return false
	}
	if msg.Metadata != nil {
		if hasVoice, ok := msg.Metadata["has_voice"].(bool); ok && hasVoice {
			return true
		}
		if mediaText, ok := msg.Metadata[MetaMediaText].(string); ok && strings.TrimSpace(mediaText) != "" {
			return true
		}
	}
	for _, att := range msg.Attachments {
		typ := strings.ToLower(strings.TrimSpace(att.Type))
		if typ == "voice" || typ == "audio" {
			return true
		}
	}
	return false
}

func mimeTypeFromTTSExt(ext string) string {
	normalized := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(ext, ".")))
	switch normalized {
	case "mp3":
		return "audio/mpeg"
	case "opus":
		return "audio/opus"
	case "wav", "wave":
		return "audio/wav"
	case "aac":
		return "audio/aac"
	case "flac":
		return "audio/flac"
	case "aiff", "aif":
		return "audio/aiff"
	default:
		return "application/octet-stream"
	}
}
