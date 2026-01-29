package signal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/haasonsaas/nexus/internal/channels"
	"github.com/haasonsaas/nexus/internal/channels/personal"
)

type attachmentRecord struct {
	peerID     string
	groupID    string
	filename   string
	mimeType   string
	storedPath string
	size       int64
}

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

	record, ok := m.adapter.getAttachmentRecord(mediaID)
	if !ok {
		return nil, "", channels.ErrNotFound("media not found", nil)
	}

	if record.storedPath != "" {
		return readFileAttachment(record.storedPath)
	}

	data, err := m.adapter.fetchAttachment(ctx, mediaID, record)
	if err != nil {
		return nil, "", err
	}
	mimeType := record.mimeType
	if mimeType == "" {
		mimeType = detectMimeType(data, record.filename, "")
	}
	if path, err := m.adapter.storeAttachmentFile(mediaID, record, data); err == nil && path != "" {
		record.storedPath = path
		m.adapter.updateAttachmentRecord(mediaID, record)
	}
	return data, mimeType, nil
}

func (m *mediaHandler) Upload(ctx context.Context, data []byte, mimeType string, filename string) (string, error) {
	if m == nil || m.adapter == nil {
		return "", channels.ErrUnavailable("media handler unavailable", nil)
	}
	if len(data) == 0 {
		return "", channels.ErrInvalidInput("media data required", nil)
	}

	record := attachmentRecord{
		filename: strings.TrimSpace(filename),
		mimeType: strings.TrimSpace(mimeType),
		size:     int64(len(data)),
	}
	if record.filename == "" {
		record.filename = "signal-attachment"
	}
	mediaID := fmt.Sprintf("upload-%s-%s", strings.TrimSuffix(record.filename, filepath.Ext(record.filename)), uuid.NewString())
	path, err := m.adapter.storeAttachmentFile(mediaID, record, data)
	if err != nil {
		return "", err
	}
	record.storedPath = path
	m.adapter.trackAttachment(mediaID, record)
	return mediaID, nil
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

	record, ok := m.adapter.getAttachmentRecord(mediaID)
	if ok && record.storedPath != "" {
		return "file://" + record.storedPath, nil
	}

	data, mimeType, err := m.Download(ctx, mediaID)
	if err != nil {
		return "", err
	}
	_ = mimeType
	record, ok = m.adapter.getAttachmentRecord(mediaID)
	if ok && record.storedPath != "" {
		return "file://" + record.storedPath, nil
	}
	if record.filename == "" {
		record.filename = "signal-attachment"
	}
	path, err := m.adapter.storeAttachmentFile(mediaID, record, data)
	if err != nil {
		return "", err
	}
	record.storedPath = path
	m.adapter.updateAttachmentRecord(mediaID, record)
	return "file://" + path, nil
}

func (a *Adapter) trackAttachment(id string, record attachmentRecord) {
	if strings.TrimSpace(id) == "" {
		return
	}
	a.attachmentsMu.Lock()
	defer a.attachmentsMu.Unlock()
	if a.attachments == nil {
		a.attachments = make(map[string]attachmentRecord)
	}
	a.attachments[id] = record
}

func (a *Adapter) getAttachmentRecord(id string) (attachmentRecord, bool) {
	a.attachmentsMu.RLock()
	defer a.attachmentsMu.RUnlock()
	record, ok := a.attachments[id]
	return record, ok
}

func (a *Adapter) updateAttachmentRecord(id string, record attachmentRecord) {
	a.attachmentsMu.Lock()
	defer a.attachmentsMu.Unlock()
	if _, ok := a.attachments[id]; ok {
		a.attachments[id] = record
	}
}

func (a *Adapter) storeAttachmentFile(id string, record attachmentRecord, data []byte) (string, error) {
	if len(data) == 0 {
		return "", channels.ErrInvalidInput("media data required", nil)
	}
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

	filename := strings.TrimSpace(record.filename)
	if filename == "" {
		filename = "signal-attachment"
	}
	filename = filepath.Base(filename)
	if filename == "." || filename == string(os.PathSeparator) {
		filename = "signal-attachment"
	}
	if !strings.Contains(filename, id) {
		filename = fmt.Sprintf("%s-%s", id, filename)
	}
	path := filepath.Join(baseDir, filename)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return "", channels.ErrConnection("failed to write attachment file", err)
	}
	return path, nil
}

func (a *Adapter) fetchAttachment(ctx context.Context, id string, record attachmentRecord) ([]byte, error) {
	params := map[string]any{
		"id": id,
	}
	if record.groupID != "" {
		params["groupId"] = record.groupID
	} else if record.peerID != "" {
		params["recipient"] = record.peerID
	}

	req := map[string]any{
		"method": "getAttachment",
		"params": params,
	}
	raw, err := a.call(ctx, req)
	if err != nil {
		return nil, err
	}

	var payload string
	if err := json.Unmarshal(raw, &payload); err == nil && payload != "" {
		return decodeAttachmentPayload(payload)
	}

	var wrapper struct {
		Data       string `json:"data"`
		Attachment string `json:"attachment"`
		Contents   string `json:"contents"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, channels.ErrInternal("failed to decode attachment response", err)
	}
	switch {
	case wrapper.Data != "":
		return decodeAttachmentPayload(wrapper.Data)
	case wrapper.Attachment != "":
		return decodeAttachmentPayload(wrapper.Attachment)
	case wrapper.Contents != "":
		return decodeAttachmentPayload(wrapper.Contents)
	default:
		return nil, channels.ErrInternal("attachment response missing data", nil)
	}
}

func decodeAttachmentPayload(payload string) ([]byte, error) {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return nil, channels.ErrInvalidInput("attachment data is empty", nil)
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err == nil {
		return data, nil
	}
	data, altErr := base64.RawStdEncoding.DecodeString(payload)
	if altErr != nil {
		return nil, channels.ErrInternal("failed to decode attachment data", err)
	}
	return data, nil
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
	mimeType := detectMimeType(payload, path, path)
	return payload, mimeType, nil
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
	if ext := strings.ToLower(filepath.Ext(name)); ext != "" {
		if mimeType := mime.TypeByExtension(ext); mimeType != "" {
			return mimeType
		}
	}
	return ""
}

var _ personal.MediaHandler = (*mediaHandler)(nil)
