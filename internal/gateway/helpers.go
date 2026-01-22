// Package gateway provides the main Nexus gateway server.
//
// helpers.go contains utility functions for message processing, media handling,
// and channel-specific operations.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/haasonsaas/nexus/internal/auth"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/media"
	"github.com/haasonsaas/nexus/internal/storage"
	"github.com/haasonsaas/nexus/pkg/models"
)

// resolveConversationID determines the unique conversation identifier for a message.
func (s *Server) resolveConversationID(msg *models.Message) (string, error) {
	switch msg.Channel {
	case models.ChannelTelegram:
		if msg.Metadata != nil {
			if chatID, ok := msg.Metadata["chat_id"]; ok {
				switch v := chatID.(type) {
				case int64:
					return strconv.FormatInt(v, 10), nil
				case int:
					return strconv.Itoa(v), nil
				case string:
					return v, nil
				}
			}
		}
		if msg.SessionID != "" {
			var id int64
			if _, err := fmt.Sscanf(msg.SessionID, "telegram:%d", &id); err == nil {
				return strconv.FormatInt(id, 10), nil
			}
		}
		return "", errors.New("telegram chat id missing")
	case models.ChannelSlack:
		channelID := ""
		if msg.Metadata != nil {
			channelID, _ = msg.Metadata["slack_channel"].(string)
		}
		if channelID == "" {
			return "", errors.New("slack channel id missing")
		}
		if !scopeUsesThread(s.config.Session.SlackScope) {
			return channelID, nil
		}
		threadTS := ""
		if msg.Metadata != nil {
			threadTS, _ = msg.Metadata["slack_thread_ts"].(string)
		}
		if threadTS == "" {
			if msg.Metadata != nil {
				if ts, ok := msg.Metadata["slack_ts"].(string); ok && ts != "" {
					threadTS = ts
				}
			}
		}
		if threadTS == "" {
			return channelID, nil
		}
		return fmt.Sprintf("%s:%s", channelID, threadTS), nil
	case models.ChannelDiscord:
		if msg.Metadata != nil {
			if channelID, ok := msg.Metadata["discord_channel_id"].(string); ok && channelID != "" {
				if scopeUsesThread(s.config.Session.DiscordScope) {
					if threadID, ok := msg.Metadata["discord_thread_id"].(string); ok && threadID != "" {
						return threadID, nil
					}
				}
				return channelID, nil
			}
		}
		return "", errors.New("discord channel id missing")
	default:
		return "", fmt.Errorf("unsupported channel %q", msg.Channel)
	}
}

// enrichMessageWithMedia processes media attachments and adds transcriptions.
func (s *Server) enrichMessageWithMedia(ctx context.Context, msg *models.Message) {
	if msg == nil || s.mediaAggregator == nil || s.config == nil || !s.config.Transcription.Enabled {
		return
	}
	if len(msg.Attachments) == 0 {
		return
	}

	var downloader channels.AttachmentDownloader
	if adapter, ok := s.channels.Get(msg.Channel); ok {
		if d, ok := adapter.(channels.AttachmentDownloader); ok {
			downloader = d
		}
	}

	mediaAttachments := make([]*media.Attachment, 0, len(msg.Attachments))
	for i := range msg.Attachments {
		att := msg.Attachments[i]
		mediaType := media.DetectMediaType(att.MimeType, att.Filename)
		switch strings.ToLower(att.Type) {
		case "voice":
			mediaType = media.MediaTypeAudio
			if att.MimeType == "" {
				att.MimeType = "audio/ogg"
			}
		case "audio":
			mediaType = media.MediaTypeAudio
		}
		if mediaType != media.MediaTypeAudio {
			continue
		}

		mediaAtt := &media.Attachment{
			ID:       att.ID,
			Type:     mediaType,
			MimeType: att.MimeType,
			Filename: att.Filename,
			Size:     att.Size,
			URL:      att.URL,
		}

		if downloader != nil {
			data, mimeType, filename, err := downloader.DownloadAttachment(ctx, msg, &att)
			if err != nil {
				s.logger.Warn("attachment download failed", "error", err, "channel", msg.Channel)
			} else {
				mediaAtt.Data = data
				if mimeType != "" {
					mediaAtt.MimeType = mimeType
				}
				if filename != "" {
					mediaAtt.Filename = filename
				}
			}
		}

		if len(mediaAtt.Data) == 0 && !isHTTPURL(mediaAtt.URL) {
			continue
		}

		mediaAttachments = append(mediaAttachments, mediaAtt)
	}

	if len(mediaAttachments) == 0 {
		return
	}

	opts := media.DefaultOptions()
	opts.EnableVision = false
	opts.EnableTranscription = true
	opts.TranscriptionLanguage = s.config.Transcription.Language

	mediaCtx := ctx
	if opts.Timeout > 0 {
		var cancel context.CancelFunc
		mediaCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
		defer cancel()
	}

	content := s.mediaAggregator.Aggregate(mediaCtx, mediaAttachments, opts)
	if content == nil || content.Text == "" {
		return
	}

	if msg.Metadata == nil {
		msg.Metadata = map[string]any{}
	}
	msg.Metadata["media_text"] = content.Text
	if len(content.Errors) > 0 {
		msg.Metadata["media_errors"] = content.Errors
	}

	if msg.Content == "" {
		msg.Content = content.Text
	} else {
		msg.Content = msg.Content + "\n\n" + content.Text
	}
}

// isHTTPURL checks if a string is an HTTP(S) URL.
func isHTTPURL(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

// buildReplyMetadata constructs metadata for an outbound message based on the inbound message.
func (s *Server) buildReplyMetadata(msg *models.Message) map[string]any {
	metadata := make(map[string]any)

	if msg.Metadata == nil {
		return metadata
	}

	switch msg.Channel {
	case models.ChannelTelegram:
		if chatID, ok := msg.Metadata["chat_id"]; ok {
			metadata["chat_id"] = chatID
		}
		if msg.ChannelID != "" {
			if id, err := strconv.Atoi(msg.ChannelID); err == nil {
				metadata["reply_to_message_id"] = id
			}
		}
	case models.ChannelSlack:
		if channelID, ok := msg.Metadata["slack_channel"].(string); ok {
			metadata["slack_channel"] = channelID
		}
		threadTS := ""
		if ts, ok := msg.Metadata["slack_thread_ts"].(string); ok && ts != "" {
			threadTS = ts
		} else if ts, ok := msg.Metadata["slack_ts"].(string); ok && ts != "" {
			threadTS = ts
		}
		if threadTS != "" {
			metadata["slack_thread_ts"] = threadTS
		}
	case models.ChannelDiscord:
		if threadID, ok := msg.Metadata["discord_thread_id"].(string); ok && threadID != "" {
			metadata["discord_channel_id"] = threadID
		} else if channelID, ok := msg.Metadata["discord_channel_id"].(string); ok {
			metadata["discord_channel_id"] = channelID
		}
	}

	return metadata
}

// scopeUsesThread determines if a channel scope should use thread-level session tracking.
func scopeUsesThread(scope string) bool {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "channel":
		return false
	default:
		return true
	}
}

// initStorageStores initializes the storage store set based on configuration.
func initStorageStores(cfg *config.Config) (storage.StoreSet, error) {
	if cfg == nil || strings.TrimSpace(cfg.Database.URL) == "" {
		return storage.NewMemoryStores(), nil
	}
	dbCfg := storage.DefaultCockroachConfig()
	if cfg.Database.MaxConnections > 0 {
		dbCfg.MaxOpenConns = cfg.Database.MaxConnections
	}
	if cfg.Database.ConnMaxLifetime > 0 {
		dbCfg.ConnMaxLifetime = cfg.Database.ConnMaxLifetime
	}
	stores, err := storage.NewCockroachStoresFromDSN(cfg.Database.URL, dbCfg)
	if err != nil {
		return storage.StoreSet{}, fmt.Errorf("storage database: %w", err)
	}
	return stores, nil
}

// registerOAuthProviders registers OAuth providers with the auth service.
func registerOAuthProviders(service *auth.Service, cfg config.OAuthConfig) {
	if service == nil {
		return
	}
	if strings.TrimSpace(cfg.Google.ClientID) != "" && strings.TrimSpace(cfg.Google.ClientSecret) != "" {
		service.RegisterProvider("google", auth.NewGoogleProvider(auth.OAuthProviderConfig{
			ClientID:     cfg.Google.ClientID,
			ClientSecret: cfg.Google.ClientSecret,
			RedirectURL:  cfg.Google.RedirectURL,
		}))
	}
	if strings.TrimSpace(cfg.GitHub.ClientID) != "" && strings.TrimSpace(cfg.GitHub.ClientSecret) != "" {
		service.RegisterProvider("github", auth.NewGitHubProvider(auth.OAuthProviderConfig{
			ClientID:     cfg.GitHub.ClientID,
			ClientSecret: cfg.GitHub.ClientSecret,
			RedirectURL:  cfg.GitHub.RedirectURL,
		}))
	}
}
