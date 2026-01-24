// Package media provides utilities for handling media files including
// MIME type detection, extension mapping, and size limits.
package media

import (
	"net/http"
	"path/filepath"
	"strings"
)

// Size limits for various media types.
const (
	MaxImageBytes    = 6 * 1024 * 1024   // 6MB
	MaxAudioBytes    = 16 * 1024 * 1024  // 16MB
	MaxVideoBytes    = 16 * 1024 * 1024  // 16MB
	MaxDocumentBytes = 100 * 1024 * 1024 // 100MB
)

// Kind represents the type of media.
type Kind string

const (
	KindImage    Kind = "image"
	KindAudio    Kind = "audio"
	KindVideo    Kind = "video"
	KindDocument Kind = "document"
	KindUnknown  Kind = "unknown"
)

// extensionToMIME maps file extensions to MIME types.
var extensionToMIME = map[string]string{
	".heic": "image/heic",
	".heif": "image/heif",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".gif":  "image/gif",
	".bmp":  "image/bmp",
	".svg":  "image/svg+xml",
	".ico":  "image/x-icon",

	".mp3":  "audio/mpeg",
	".ogg":  "audio/ogg",
	".wav":  "audio/wav",
	".flac": "audio/flac",
	".m4a":  "audio/mp4",
	".aac":  "audio/aac",
	".opus": "audio/opus",

	".mp4":  "video/mp4",
	".webm": "video/webm",
	".avi":  "video/x-msvideo",
	".mov":  "video/quicktime",
	".mkv":  "video/x-matroska",

	".pdf":  "application/pdf",
	".json": "application/json",
	".xml":  "application/xml",
	".zip":  "application/zip",
	".gz":   "application/gzip",
	".tar":  "application/x-tar",
	".7z":   "application/x-7z-compressed",
	".rar":  "application/vnd.rar",

	".doc":  "application/msword",
	".xls":  "application/vnd.ms-excel",
	".ppt":  "application/vnd.ms-powerpoint",
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",

	".txt": "text/plain",
	".csv": "text/csv",
	".md":  "text/markdown",
	".htm": "text/html",
	".html": "text/html",
}

// mimeToExtension maps MIME types to preferred file extensions.
var mimeToExtension = map[string]string{
	"image/heic":     ".heic",
	"image/heif":     ".heif",
	"image/jpeg":     ".jpg",
	"image/png":      ".png",
	"image/webp":     ".webp",
	"image/gif":      ".gif",
	"audio/mpeg":     ".mp3",
	"audio/ogg":      ".ogg",
	"video/mp4":      ".mp4",
	"application/pdf": ".pdf",
	"text/plain":    ".txt",
}

// audioExtensions contains extensions for audio files.
var audioExtensions = map[string]bool{
	".aac":  true,
	".flac": true,
	".m4a":  true,
	".mp3":  true,
	".oga":  true,
	".ogg":  true,
	".opus": true,
	".wav":  true,
}

// KindFromMIME returns the media kind based on MIME type.
func KindFromMIME(mime string) Kind {
	if mime == "" {
		return KindUnknown
	}
	mime = strings.ToLower(mime)

	if strings.HasPrefix(mime, "image/") {
		return KindImage
	}
	if strings.HasPrefix(mime, "audio/") {
		return KindAudio
	}
	if strings.HasPrefix(mime, "video/") {
		return KindVideo
	}
	if mime == "application/pdf" || strings.HasPrefix(mime, "application/") {
		return KindDocument
	}
	if strings.HasPrefix(mime, "text/") {
		return KindDocument
	}
	return KindUnknown
}

// MaxBytesForKind returns the maximum size for a media kind.
func MaxBytesForKind(kind Kind) int64 {
	switch kind {
	case KindImage:
		return MaxImageBytes
	case KindAudio:
		return MaxAudioBytes
	case KindVideo:
		return MaxVideoBytes
	case KindDocument:
		return MaxDocumentBytes
	default:
		return MaxDocumentBytes
	}
}

// GetExtension returns the file extension from a path or URL.
func GetExtension(path string) string {
	if path == "" {
		return ""
	}

	// Handle URLs
	if strings.HasPrefix(strings.ToLower(path), "http://") || strings.HasPrefix(strings.ToLower(path), "https://") {
		// Remove query string and fragment
		if idx := strings.Index(path, "?"); idx != -1 {
			path = path[:idx]
		}
		if idx := strings.Index(path, "#"); idx != -1 {
			path = path[:idx]
		}
	}

	ext := strings.ToLower(filepath.Ext(path))
	return ext
}

// MIMEFromExtension returns the MIME type for a file extension.
func MIMEFromExtension(ext string) string {
	ext = strings.ToLower(ext)
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	if mime, ok := extensionToMIME[ext]; ok {
		return mime
	}
	return ""
}

// ExtensionFromMIME returns the preferred file extension for a MIME type.
func ExtensionFromMIME(mime string) string {
	mime = strings.ToLower(strings.TrimSpace(mime))
	// Handle MIME types with parameters (e.g., "text/plain; charset=utf-8")
	if idx := strings.Index(mime, ";"); idx != -1 {
		mime = strings.TrimSpace(mime[:idx])
	}
	if ext, ok := mimeToExtension[mime]; ok {
		return ext
	}
	return ""
}

// IsAudioExtension checks if the extension is for an audio file.
func IsAudioExtension(ext string) bool {
	ext = strings.ToLower(ext)
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return audioExtensions[ext]
}

// IsAudioFile checks if a file path is an audio file.
func IsAudioFile(path string) bool {
	return IsAudioExtension(GetExtension(path))
}

// IsGIF checks if the content is a GIF based on MIME or filename.
func IsGIF(mime, filename string) bool {
	if strings.ToLower(mime) == "image/gif" {
		return true
	}
	return GetExtension(filename) == ".gif"
}

// DetectMIME attempts to detect the MIME type from various sources.
// It prefers: sniffed content > extension mapping > header MIME type.
func DetectMIME(data []byte, filename string, headerMIME string) string {
	// Try content sniffing if we have data
	if len(data) > 0 {
		sniffed := http.DetectContentType(data)
		if sniffed != "" && sniffed != "application/octet-stream" {
			return sniffed
		}
	}

	// Try extension mapping
	if ext := GetExtension(filename); ext != "" {
		if mime := MIMEFromExtension(ext); mime != "" {
			return mime
		}
	}

	// Use header MIME as fallback
	if headerMIME != "" {
		return normalizeHeaderMIME(headerMIME)
	}

	return ""
}

// normalizeHeaderMIME cleans up a MIME type from an HTTP header.
func normalizeHeaderMIME(mime string) string {
	mime = strings.TrimSpace(mime)
	if idx := strings.Index(mime, ";"); idx != -1 {
		mime = strings.TrimSpace(mime[:idx])
	}
	return strings.ToLower(mime)
}

// ValidateSize checks if a file size is within limits for its MIME type.
func ValidateSize(size int64, mime string) bool {
	kind := KindFromMIME(mime)
	return size <= MaxBytesForKind(kind)
}

// ImageMIMEFromFormat returns the MIME type for an image format name.
func ImageMIMEFromFormat(format string) string {
	switch strings.ToLower(format) {
	case "jpg", "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	case "heic":
		return "image/heic"
	case "heif":
		return "image/heif"
	case "bmp":
		return "image/bmp"
	case "svg":
		return "image/svg+xml"
	default:
		return ""
	}
}
