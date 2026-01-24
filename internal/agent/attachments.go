package agent

import (
	"encoding/base64"
	"strings"

	"github.com/haasonsaas/nexus/pkg/models"
)

func artifactsToAttachments(artifacts []Artifact) []models.Attachment {
	if len(artifacts) == 0 {
		return nil
	}
	attachments := make([]models.Attachment, 0, len(artifacts))
	for _, art := range artifacts {
		attType := "file"
		switch art.Type {
		case "screenshot", "image":
			attType = "image"
		case "recording", "video":
			attType = "video"
		case "audio":
			attType = "audio"
		default:
			if strings.HasPrefix(art.MimeType, "image/") {
				attType = "image"
			} else if strings.HasPrefix(art.MimeType, "video/") {
				attType = "video"
			} else if strings.HasPrefix(art.MimeType, "audio/") {
				attType = "audio"
			}
		}

		attachment := models.Attachment{
			ID:       art.ID,
			Type:     attType,
			Filename: art.Filename,
			MimeType: art.MimeType,
			Size:     int64(len(art.Data)),
			URL:      art.URL,
		}
		if attachment.URL == "" && len(art.Data) > 0 && art.MimeType != "" {
			attachment.URL = "data:" + art.MimeType + ";base64," + base64.StdEncoding.EncodeToString(art.Data)
		}
		attachments = append(attachments, attachment)
	}
	return attachments
}
