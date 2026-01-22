// Package media provides media processing and understanding capabilities.
package media

import (
	"io"
	"time"
)

// MediaType identifies the category of media.
type MediaType string

const (
	MediaTypeImage    MediaType = "image"
	MediaTypeAudio    MediaType = "audio"
	MediaTypeVideo    MediaType = "video"
	MediaTypeDocument MediaType = "document"
	MediaTypeUnknown  MediaType = "unknown"
)

// Attachment represents a media attachment from a message.
type Attachment struct {
	// ID is a unique identifier for this attachment
	ID string `json:"id"`

	// Type is the media category
	Type MediaType `json:"type"`

	// MimeType is the MIME type (e.g., "image/png")
	MimeType string `json:"mime_type"`

	// Filename is the original filename
	Filename string `json:"filename,omitempty"`

	// Size is the file size in bytes
	Size int64 `json:"size,omitempty"`

	// URL is the remote URL if available
	URL string `json:"url,omitempty"`

	// LocalPath is the local file path if downloaded
	LocalPath string `json:"local_path,omitempty"`

	// Data holds the raw bytes (for small attachments)
	Data []byte `json:"-"`

	// Width/Height for images and videos
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`

	// Duration for audio and video
	Duration time.Duration `json:"duration,omitempty"`

	// Metadata holds additional format-specific data
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Content represents processed media content for LLM consumption.
type Content struct {
	// Type identifies how this content should be used
	Type ContentType `json:"type"`

	// Text is text content (transcription, description, etc)
	Text string `json:"text,omitempty"`

	// ImageData is base64-encoded image data
	ImageData string `json:"image_data,omitempty"`

	// ImageMediaType is the MIME type of the image
	ImageMediaType string `json:"image_media_type,omitempty"`

	// Source identifies where this content came from
	Source string `json:"source,omitempty"`
}

// ContentType identifies how processed content should be used.
type ContentType string

const (
	ContentTypeText  ContentType = "text"
	ContentTypeImage ContentType = "image"
)

// ProcessingResult holds the result of processing an attachment.
type ProcessingResult struct {
	// Attachment is the original attachment
	Attachment *Attachment `json:"attachment"`

	// Contents are the processed contents
	Contents []Content `json:"contents"`

	// Description is a text description of the media
	Description string `json:"description,omitempty"`

	// Transcription is text from audio/video
	Transcription string `json:"transcription,omitempty"`

	// Error is set if processing failed
	Error string `json:"error,omitempty"`

	// ProcessedAt is when processing completed
	ProcessedAt time.Time `json:"processed_at"`

	// ProcessingDuration is how long processing took
	ProcessingDuration time.Duration `json:"processing_duration,omitempty"`
}

// ProcessingOptions configures media processing.
type ProcessingOptions struct {
	// MaxImageSize limits image dimensions (will resize if larger)
	MaxImageSize int

	// MaxFileSize limits file size in bytes
	MaxFileSize int64

	// EnableTranscription enables audio/video transcription
	EnableTranscription bool

	// EnableVision enables vision model processing
	EnableVision bool

	// TranscriptionLanguage for audio transcription
	TranscriptionLanguage string

	// Quality for image processing (1-100)
	Quality int

	// Timeout for processing operations
	Timeout time.Duration
}

// DefaultOptions returns sensible default processing options.
func DefaultOptions() ProcessingOptions {
	return ProcessingOptions{
		MaxImageSize:        2048,
		MaxFileSize:         20 * 1024 * 1024, // 20MB
		EnableTranscription: true,
		EnableVision:        true,
		Quality:             85,
		Timeout:             30 * time.Second,
	}
}

// Processor handles media processing.
type Processor interface {
	// Process processes an attachment and returns content for LLM consumption.
	Process(attachment *Attachment, opts ProcessingOptions) (*ProcessingResult, error)

	// SupportedTypes returns the media types this processor handles.
	SupportedTypes() []MediaType
}

// Transcriber transcribes audio to text.
type Transcriber interface {
	// Transcribe converts audio to text.
	Transcribe(audio io.Reader, mimeType string, language string) (string, error)
}

// ImageProcessor processes images for vision models.
type ImageProcessor interface {
	// PrepareForVision prepares an image for vision model consumption.
	// Returns base64-encoded image data.
	PrepareForVision(image io.Reader, mimeType string, maxSize int) (string, string, error)
}
