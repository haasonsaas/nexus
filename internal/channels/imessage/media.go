//go:build darwin
// +build darwin

package imessage

import (
	"context"
	"database/sql"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/channels/personal"
)

type mediaHandler struct {
	adapter *Adapter
}

func (m *mediaHandler) Download(ctx context.Context, mediaID string) ([]byte, string, error) {
	if m == nil || m.adapter == nil {
		return nil, "", channels.ErrUnavailable("media handler unavailable", nil)
	}
	mediaID = strings.TrimSpace(mediaID)
	if mediaID == "" {
		return nil, "", channels.ErrInvalidInput("media id required", nil)
	}
	if path, ok := resolveFilePath(mediaID); ok {
		return readFileAttachment(path)
	}

	info, err := m.adapter.lookupAttachment(ctx, mediaID)
	if err != nil {
		return nil, "", err
	}
	return readFileAttachment(info.path)
}

func (m *mediaHandler) Upload(ctx context.Context, data []byte, mimeType string, filename string) (string, error) {
	if m == nil || m.adapter == nil {
		return "", channels.ErrUnavailable("media handler unavailable", nil)
	}
	if len(data) == 0 {
		return "", channels.ErrInvalidInput("media data required", nil)
	}

	path, err := m.adapter.writeAttachmentFile(data, mimeType, filename)
	if err != nil {
		return "", err
	}
	return "file://" + path, nil
}

func (m *mediaHandler) GetURL(ctx context.Context, mediaID string) (string, error) {
	if m == nil || m.adapter == nil {
		return "", channels.ErrUnavailable("media handler unavailable", nil)
	}
	mediaID = strings.TrimSpace(mediaID)
	if mediaID == "" {
		return "", channels.ErrInvalidInput("media id required", nil)
	}
	if path, ok := resolveFilePath(mediaID); ok {
		return "file://" + path, nil
	}
	info, err := m.adapter.lookupAttachment(ctx, mediaID)
	if err != nil {
		return "", err
	}
	return "file://" + info.path, nil
}

type attachmentInfo struct {
	path     string
	mimeType string
}

func (a *Adapter) lookupAttachment(ctx context.Context, mediaID string) (attachmentInfo, error) {
	if a == nil || a.db == nil {
		return attachmentInfo{}, channels.ErrUnavailable("media database unavailable", nil)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	id := strings.TrimSpace(mediaID)
	if id == "" {
		return attachmentInfo{}, channels.ErrInvalidInput("media id required", nil)
	}

	var (
		filename sql.NullString
		mimeType sql.NullString
	)

	if rowID, err := strconv.ParseInt(id, 10, 64); err == nil {
		err = a.db.QueryRowContext(ctx, `
			SELECT filename, mime_type
			FROM attachment
			WHERE ROWID = ?
		`, rowID).Scan(&filename, &mimeType)
		if err == nil {
			return a.attachmentInfoFromRow(filename, mimeType)
		}
		if err != sql.ErrNoRows {
			return attachmentInfo{}, channels.ErrInternal("failed to query attachment", err)
		}
	}

	err := a.db.QueryRowContext(ctx, `
		SELECT filename, mime_type
		FROM attachment
		WHERE guid = ?
	`, id).Scan(&filename, &mimeType)
	if err == sql.ErrNoRows {
		return attachmentInfo{}, channels.ErrNotFound("media not found", err)
	}
	if err != nil {
		return attachmentInfo{}, channels.ErrInternal("failed to query attachment", err)
	}
	return a.attachmentInfoFromRow(filename, mimeType)
}

func (a *Adapter) fetchMessageAttachments(ctx context.Context, messageRowID int64) ([]personal.RawAttachment, error) {
	if a == nil || a.db == nil {
		return nil, channels.ErrUnavailable("media database unavailable", nil)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := a.db.QueryContext(ctx, `
		SELECT a.ROWID, a.guid, a.filename, a.mime_type, a.total_bytes
		FROM attachment a
		JOIN message_attachment_join maj ON a.ROWID = maj.attachment_id
		WHERE maj.message_id = ?
	`, messageRowID)
	if err != nil {
		return nil, channels.ErrInternal("failed to query attachments", err)
	}
	defer rows.Close()

	attachments := make([]personal.RawAttachment, 0)
	for rows.Next() {
		var (
			rowID    int64
			guid     sql.NullString
			filename sql.NullString
			mimeType sql.NullString
			size     sql.NullInt64
		)
		if err := rows.Scan(&rowID, &guid, &filename, &mimeType, &size); err != nil {
			continue
		}
		id := ""
		if guid.Valid {
			id = strings.TrimSpace(guid.String)
		}
		if id == "" {
			id = fmt.Sprintf("%d", rowID)
		}
		path := ""
		if filename.Valid {
			path = strings.TrimSpace(filename.String)
		}
		attachment := personal.RawAttachment{
			ID:       id,
			Filename: filepath.Base(path),
		}
		if mimeType.Valid {
			attachment.MIMEType = strings.TrimSpace(mimeType.String)
		}
		if size.Valid {
			attachment.Size = size.Int64
		}
		if path != "" {
			attachment.URL = "file://" + expandPath(path)
		}
		attachments = append(attachments, attachment)
	}
	return attachments, nil
}

func (a *Adapter) attachmentInfoFromRow(filename sql.NullString, mimeType sql.NullString) (attachmentInfo, error) {
	if !filename.Valid || strings.TrimSpace(filename.String) == "" {
		return attachmentInfo{}, channels.ErrNotFound("media file path missing", nil)
	}
	path := expandPath(filename.String)
	path = strings.TrimSpace(path)
	if path == "" {
		return attachmentInfo{}, channels.ErrNotFound("media file path missing", nil)
	}
	info := attachmentInfo{
		path: path,
	}
	if mimeType.Valid {
		info.mimeType = strings.TrimSpace(mimeType.String)
	}
	return info, nil
}

func (a *Adapter) writeAttachmentFile(data []byte, mimeType string, filename string) (string, error) {
	baseDir := ""
	if a != nil && a.config != nil {
		baseDir = strings.TrimSpace(a.config.Personal.MediaPath)
	}
	if baseDir == "" {
		baseDir = os.TempDir()
	}
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return "", channels.ErrConnection("failed to prepare media directory", err)
	}
	name := strings.TrimSpace(filename)
	if name == "" {
		name = "imessage-attachment"
	}
	name = filepath.Base(name)
	if name == "." || name == string(os.PathSeparator) {
		name = "imessage-attachment"
	}
	if !strings.Contains(name, ".") {
		if ext := extensionForMime(mimeType); ext != "" {
			name += ext
		}
	}
	name = fmt.Sprintf("%s-%s", strings.TrimSuffix(name, filepath.Ext(name)), uuid.NewString()) + filepath.Ext(name)
	path := filepath.Join(baseDir, name)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", channels.ErrConnection("failed to write attachment file", err)
	}
	return path, nil
}

func resolveFilePath(mediaID string) (string, bool) {
	raw := strings.TrimSpace(mediaID)
	if raw == "" {
		return "", false
	}
	if strings.HasPrefix(raw, "file://") {
		raw = strings.TrimPrefix(raw, "file://")
	}
	if strings.HasPrefix(raw, "~/") || strings.HasPrefix(raw, string(os.PathSeparator)) {
		path := expandPath(raw)
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
	}
	if strings.Contains(raw, string(os.PathSeparator)) {
		path := expandPath(raw)
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
	}
	return "", false
}

func readFileAttachment(path string) ([]byte, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, "", channels.ErrInvalidInput("media path required", nil)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", channels.ErrNotFound("media not found", err)
		}
		return nil, "", channels.ErrConnection("failed to read media", err)
	}
	return payload, detectMimeType(payload, path, path), nil
}

func detectMimeType(data []byte, filename string, path string) string {
	if filename != "" {
		if mimeType := mimeTypeForName(filename); mimeType != "" {
			return mimeType
		}
	}
	if path != "" {
		if mimeType := mimeTypeForName(path); mimeType != "" {
			return mimeType
		}
	}
	if len(data) > 0 {
		return http.DetectContentType(data)
	}
	return ""
}

func mimeTypeForName(name string) string {
	if name == "" {
		return ""
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return ""
	}
	return mime.TypeByExtension(ext)
}

func extensionForMime(mimeType string) string {
	mimeType = strings.TrimSpace(mimeType)
	if mimeType == "" {
		return ""
	}
	extensions, err := mime.ExtensionsByType(mimeType)
	if err != nil || len(extensions) == 0 {
		return ""
	}
	return extensions[0]
}

var _ personal.MediaHandler = (*mediaHandler)(nil)
