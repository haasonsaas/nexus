package slack

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/haasonsaas/nexus/internal/channels"
	channelcontext "github.com/haasonsaas/nexus/internal/channels/context"
	"github.com/haasonsaas/nexus/pkg/models"
	"github.com/slack-go/slack"
)

func uploadSlackAttachments(ctx context.Context, cfg Config, client SlackAPIClient, limiter *channels.RateLimiter, logger *slog.Logger, health *channels.BaseHealthAdapter, channelID, threadTS string, attachments []models.Attachment) {
	if !cfg.UploadAttachments || client == nil || len(attachments) == 0 {
		return
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	maxBytes := channelcontext.GetChannelInfo(string(models.ChannelSlack)).MaxAttachmentBytes
	for _, att := range attachments {
		if err := uploadSlackAttachment(ctx, client, httpClient, limiter, channelID, threadTS, maxBytes, att); err != nil {
			if logger != nil {
				logger.Warn("failed to upload slack attachment", "error", err, "filename", att.Filename, "url", att.URL)
			}
			if health != nil {
				health.RecordError(channels.ErrCodeInternal)
			}
		}
	}
}

func uploadSlackAttachment(ctx context.Context, client SlackAPIClient, httpClient *http.Client, limiter *channels.RateLimiter, channelID, threadTS string, maxBytes int64, att models.Attachment) error {
	if strings.TrimSpace(att.URL) == "" {
		return errors.New("attachment url is empty")
	}
	if limiter != nil {
		if err := limiter.Wait(ctx); err != nil {
			return fmt.Errorf("attachment upload rate limit: %w", err)
		}
	}

	data, filename, _, err := fetchSlackAttachment(ctx, httpClient, att, maxBytes)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return errors.New("attachment download returned empty payload")
	}

	params := slack.UploadFileV2Parameters{
		Reader:          bytes.NewReader(data),
		FileSize:        len(data),
		Filename:        filename,
		Title:           filename,
		Channel:         channelID,
		ThreadTimestamp: threadTS,
	}

	if _, err := client.UploadFileV2Context(ctx, params); err != nil {
		return fmt.Errorf("upload file: %w", err)
	}
	return nil
}

func fetchSlackAttachment(ctx context.Context, httpClient *http.Client, att models.Attachment, maxBytes int64) ([]byte, string, string, error) {
	parsed, err := url.Parse(att.URL)
	if err != nil {
		return nil, "", "", fmt.Errorf("parse attachment url: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, "", "", fmt.Errorf("unsupported attachment url scheme: %q", parsed.Scheme)
	}

	if maxBytes <= 0 {
		maxBytes = 50 * 1024 * 1024
	}
	if att.Size > 0 && att.Size > maxBytes {
		return nil, "", "", fmt.Errorf("attachment too large: %d > %d bytes", att.Size, maxBytes)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, att.URL, nil)
	if err != nil {
		return nil, "", "", fmt.Errorf("build attachment request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("download attachment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, "", "", fmt.Errorf("download attachment: unexpected status %d", resp.StatusCode)
	}
	if resp.ContentLength > 0 && resp.ContentLength > maxBytes {
		return nil, "", "", fmt.Errorf("attachment too large: %d > %d bytes", resp.ContentLength, maxBytes)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", "", fmt.Errorf("read attachment: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, "", "", fmt.Errorf("attachment too large: %d > %d bytes", len(data), maxBytes)
	}

	filename := strings.TrimSpace(att.Filename)
	if filename == "" {
		if disposition := resp.Header.Get("Content-Disposition"); disposition != "" {
			if _, params, parseErr := mime.ParseMediaType(disposition); parseErr == nil {
				filename = strings.TrimSpace(params["filename"])
			}
		}
	}
	if filename == "" {
		filename = filenameFromURL(parsed)
	}
	if filename == "" {
		filename = "attachment"
	}

	mimeType := strings.TrimSpace(att.MimeType)
	if mimeType == "" {
		if contentType := resp.Header.Get("Content-Type"); contentType != "" {
			mimeType = strings.TrimSpace(strings.Split(contentType, ";")[0])
		}
	}
	if mimeType == "" {
		mimeType = mime.TypeByExtension(path.Ext(filename))
	}

	return data, filename, mimeType, nil
}

func filenameFromURL(parsed *url.URL) string {
	if parsed == nil {
		return ""
	}
	base := path.Base(parsed.Path)
	if base == "." || base == "/" {
		return ""
	}
	return base
}
