package web

import (
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/haasonsaas/nexus/internal/artifacts"
)

// APIArtifactSummary is a compact artifact representation.
type APIArtifactSummary struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	MimeType   string `json:"mime_type"`
	Filename   string `json:"filename"`
	Size       int64  `json:"size"`
	Reference  string `json:"reference"`
	TTLSeconds int32  `json:"ttl_seconds"`
	Redacted   bool   `json:"redacted"`
}

// APIArtifactListResponse is the JSON response for artifact list.
type APIArtifactListResponse struct {
	Artifacts []*APIArtifactSummary `json:"artifacts"`
	Total     int                   `json:"total"`
}

// apiArtifacts handles GET /api/artifacts.
func (h *Handler) apiArtifacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.config.ArtifactRepo == nil {
		h.jsonError(w, "Artifacts not configured (set artifacts.backend)", http.StatusServiceUnavailable)
		return
	}

	filter := artifacts.Filter{
		SessionID: clampQueryParam(r, "session_id"),
		EdgeID:    clampQueryParam(r, "edge_id"),
		Type:      clampQueryParam(r, "type"),
		Limit:     parseIntParam(r, "limit", 50),
	}

	results, err := h.config.ArtifactRepo.ListArtifacts(r.Context(), filter)
	if err != nil {
		h.jsonError(w, "Failed to list artifacts", http.StatusInternalServerError)
		return
	}

	items := make([]*APIArtifactSummary, 0, len(results))
	for _, art := range results {
		if art == nil {
			continue
		}
		items = append(items, &APIArtifactSummary{
			ID:         art.Id,
			Type:       art.Type,
			MimeType:   art.MimeType,
			Filename:   art.Filename,
			Size:       art.Size,
			Reference:  art.Reference,
			TTLSeconds: art.TtlSeconds,
			Redacted:   strings.HasPrefix(art.Reference, "redacted://"),
		})
	}

	h.jsonResponse(w, APIArtifactListResponse{
		Artifacts: items,
		Total:     len(items),
	})
}

// apiArtifact handles GET /api/artifacts/{id}.
func (h *Handler) apiArtifact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.config.ArtifactRepo == nil {
		h.jsonError(w, "Artifacts not configured (set artifacts.backend)", http.StatusServiceUnavailable)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/artifacts/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		h.jsonError(w, "Artifact ID required", http.StatusBadRequest)
		return
	}
	artifactID := parts[0]

	artifact, reader, err := h.config.ArtifactRepo.GetArtifact(r.Context(), artifactID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "expired") {
			h.jsonError(w, "Artifact not found", http.StatusNotFound)
		} else {
			h.config.Logger.Error("failed to get artifact", "id", artifactID, "error", err)
			h.jsonError(w, "Failed to retrieve artifact", http.StatusInternalServerError)
		}
		return
	}
	defer reader.Close()

	raw := strings.EqualFold(r.URL.Query().Get("raw"), "1") || strings.EqualFold(r.URL.Query().Get("raw"), "true")
	download := strings.EqualFold(r.URL.Query().Get("download"), "1") || strings.EqualFold(r.URL.Query().Get("download"), "true")

	if raw {
		if strings.HasPrefix(artifact.Reference, "redacted://") {
			http.Error(w, "Artifact redacted", http.StatusGone)
			return
		}
		contentType := artifact.MimeType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		if download && artifact.Filename != "" {
			safeName := sanitizeAttachmentFilename(artifact.Filename)
			if safeName != "" {
				w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
					"filename": safeName,
				}))
			}
		}
		if _, err := io.Copy(w, reader); err != nil {
			h.config.Logger.Error("artifact download failed", "error", err)
		}
		return
	}

	h.jsonResponse(w, APIArtifactSummary{
		ID:         artifact.Id,
		Type:       artifact.Type,
		MimeType:   artifact.MimeType,
		Filename:   artifact.Filename,
		Size:       artifact.Size,
		Reference:  artifact.Reference,
		TTLSeconds: artifact.TtlSeconds,
		Redacted:   strings.HasPrefix(artifact.Reference, "redacted://"),
	})
}

func sanitizeAttachmentFilename(name string) string {
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	name = strings.ReplaceAll(name, "\"", "")
	name = strings.ReplaceAll(name, "\\", "")
	return strings.TrimSpace(name)
}
