package media

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/image/draw"
)

// DefaultProcessor is the default media processor implementation.
type DefaultProcessor struct {
	httpClient  *http.Client
	transcriber Transcriber
	logger      *slog.Logger
}

// NewDefaultProcessor creates a new default processor.
func NewDefaultProcessor(logger *slog.Logger) *DefaultProcessor {
	if logger == nil {
		logger = slog.Default()
	}
	return &DefaultProcessor{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     logger.With("component", "media"),
	}
}

// SetTranscriber sets the transcriber for audio processing.
func (p *DefaultProcessor) SetTranscriber(t Transcriber) {
	p.transcriber = t
}

// Process processes an attachment.
func (p *DefaultProcessor) Process(attachment *Attachment, opts ProcessingOptions) (*ProcessingResult, error) {
	start := time.Now()
	result := &ProcessingResult{
		Attachment:  attachment,
		ProcessedAt: start,
		Contents:    make([]Content, 0),
	}

	// Get the data
	data, err := p.getData(attachment, opts)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}

	// Process based on type
	switch attachment.Type {
	case MediaTypeImage:
		if opts.EnableVision {
			content, err := p.processImage(data, attachment.MimeType, opts)
			if err != nil {
				p.logger.Warn("image processing failed", "error", err)
				result.Error = err.Error()
			} else {
				result.Contents = append(result.Contents, *content)
			}
		}

	case MediaTypeAudio:
		if opts.EnableTranscription && p.transcriber != nil {
			text, err := p.transcriber.Transcribe(
				bytes.NewReader(data),
				attachment.MimeType,
				opts.TranscriptionLanguage,
			)
			if err != nil {
				p.logger.Warn("transcription failed", "error", err)
				result.Error = err.Error()
			} else {
				result.Transcription = text
				result.Contents = append(result.Contents, Content{
					Type:   ContentTypeText,
					Text:   text,
					Source: "transcription",
				})
			}
		}

	case MediaTypeVideo:
		// Video processing would extract frames and/or audio
		// For now, just note that we received a video
		result.Description = fmt.Sprintf("Video attachment: %s", attachment.Filename)

	case MediaTypeDocument:
		// Document processing depends on format (PDF, etc)
		result.Description = fmt.Sprintf("Document attachment: %s", attachment.Filename)
	}

	result.ProcessingDuration = time.Since(start)
	return result, nil
}

// SupportedTypes returns supported media types.
func (p *DefaultProcessor) SupportedTypes() []MediaType {
	return []MediaType{
		MediaTypeImage,
		MediaTypeAudio,
		MediaTypeVideo,
		MediaTypeDocument,
	}
}

func (p *DefaultProcessor) getData(attachment *Attachment, opts ProcessingOptions) ([]byte, error) {
	// Already have data
	if len(attachment.Data) > 0 {
		if opts.MaxFileSize > 0 && int64(len(attachment.Data)) > opts.MaxFileSize {
			return nil, fmt.Errorf("file too large: %d bytes (max %d)", len(attachment.Data), opts.MaxFileSize)
		}
		return attachment.Data, nil
	}

	// Read from local path
	if attachment.LocalPath != "" {
		data, err := os.ReadFile(attachment.LocalPath)
		if err != nil {
			return nil, fmt.Errorf("read local file: %w", err)
		}
		if opts.MaxFileSize > 0 && int64(len(data)) > opts.MaxFileSize {
			return nil, fmt.Errorf("file too large: %d bytes (max %d)", len(data), opts.MaxFileSize)
		}
		return data, nil
	}

	// Download from URL
	if attachment.URL != "" {
		return p.download(attachment.URL, opts)
	}

	return nil, fmt.Errorf("no data source available")
}

func (p *DefaultProcessor) download(url string, opts ProcessingOptions) ([]byte, error) {
	resp, err := p.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	// Limit read size
	reader := io.Reader(resp.Body)
	if opts.MaxFileSize > 0 {
		reader = io.LimitReader(resp.Body, opts.MaxFileSize+1)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if opts.MaxFileSize > 0 && int64(len(data)) > opts.MaxFileSize {
		return nil, fmt.Errorf("file too large: %d bytes (max %d)", len(data), opts.MaxFileSize)
	}

	return data, nil
}

func (p *DefaultProcessor) processImage(data []byte, mimeType string, opts ProcessingOptions) (*Content, error) {
	// Decode image
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	// Resize if needed
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	if opts.MaxImageSize > 0 && (width > opts.MaxImageSize || height > opts.MaxImageSize) {
		img = p.resize(img, opts.MaxImageSize)
		bounds = img.Bounds()
		width = bounds.Dx()
		height = bounds.Dy()
	}

	// Encode to PNG for consistent handling
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode image: %w", err)
	}

	// Base64 encode
	encoded := base64.StdEncoding.EncodeToString(buf.Bytes())

	return &Content{
		Type:           ContentTypeImage,
		ImageData:      encoded,
		ImageMediaType: "image/png",
		Source:         fmt.Sprintf("image:%dx%d", width, height),
	}, nil
}

func (p *DefaultProcessor) resize(img image.Image, maxSize int) image.Image {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Calculate new dimensions
	var newWidth, newHeight int
	if width > height {
		newWidth = maxSize
		newHeight = height * maxSize / width
	} else {
		newHeight = maxSize
		newWidth = width * maxSize / height
	}

	// Create resized image
	dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
	draw.BiLinear.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)

	return dst
}

// DetectMediaType determines the media type from MIME type or filename.
func DetectMediaType(mimeType, filename string) MediaType {
	// Check MIME type first
	if mimeType != "" {
		switch {
		case strings.HasPrefix(mimeType, "image/"):
			return MediaTypeImage
		case strings.HasPrefix(mimeType, "audio/"):
			return MediaTypeAudio
		case strings.HasPrefix(mimeType, "video/"):
			return MediaTypeVideo
		case mimeType == "application/pdf",
			strings.HasPrefix(mimeType, "application/vnd."),
			strings.HasPrefix(mimeType, "text/"):
			return MediaTypeDocument
		}
	}

	// Fall back to extension
	if filename != "" {
		ext := strings.ToLower(filepath.Ext(filename))
		switch ext {
		case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".tiff":
			return MediaTypeImage
		case ".mp3", ".wav", ".ogg", ".m4a", ".aac", ".flac":
			return MediaTypeAudio
		case ".mp4", ".mov", ".avi", ".mkv", ".webm":
			return MediaTypeVideo
		case ".pdf", ".doc", ".docx", ".txt", ".md", ".rtf":
			return MediaTypeDocument
		}
	}

	return MediaTypeUnknown
}

// IsSupported checks if a media type is supported for processing.
func IsSupported(mimeType string) bool {
	switch {
	case strings.HasPrefix(mimeType, "image/jpeg"),
		strings.HasPrefix(mimeType, "image/png"),
		strings.HasPrefix(mimeType, "image/gif"),
		strings.HasPrefix(mimeType, "image/webp"):
		return true
	case strings.HasPrefix(mimeType, "audio/"):
		return true
	}
	return false
}
