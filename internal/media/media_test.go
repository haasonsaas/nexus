package media

import (
	"testing"
)

func TestKindFromMIME(t *testing.T) {
	tests := []struct {
		mime string
		want Kind
	}{
		{"image/jpeg", KindImage},
		{"image/png", KindImage},
		{"IMAGE/GIF", KindImage},
		{"audio/mpeg", KindAudio},
		{"audio/ogg", KindAudio},
		{"video/mp4", KindVideo},
		{"video/webm", KindVideo},
		{"application/pdf", KindDocument},
		{"application/json", KindDocument},
		{"text/plain", KindDocument},
		{"", KindUnknown},
		{"something/weird", KindUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			got := KindFromMIME(tt.mime)
			if got != tt.want {
				t.Errorf("KindFromMIME(%q) = %v, want %v", tt.mime, got, tt.want)
			}
		})
	}
}

func TestMaxBytesForKind(t *testing.T) {
	if MaxBytesForKind(KindImage) != MaxImageBytes {
		t.Error("wrong max for image")
	}
	if MaxBytesForKind(KindAudio) != MaxAudioBytes {
		t.Error("wrong max for audio")
	}
	if MaxBytesForKind(KindVideo) != MaxVideoBytes {
		t.Error("wrong max for video")
	}
	if MaxBytesForKind(KindDocument) != MaxDocumentBytes {
		t.Error("wrong max for document")
	}
}

func TestGetExtension(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/path/to/file.jpg", ".jpg"},
		{"/path/to/FILE.PNG", ".png"},
		{"document.pdf", ".pdf"},
		{"https://example.com/image.jpg", ".jpg"},
		{"https://example.com/image.jpg?width=100", ".jpg"},
		{"https://example.com/image.jpg#section", ".jpg"},
		{"noextension", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := GetExtension(tt.path)
			if got != tt.want {
				t.Errorf("GetExtension(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestMIMEFromExtension(t *testing.T) {
	tests := []struct {
		ext  string
		want string
	}{
		{".jpg", "image/jpeg"},
		{".jpeg", "image/jpeg"},
		{".png", "image/png"},
		{"png", "image/png"},
		{".mp3", "audio/mpeg"},
		{".pdf", "application/pdf"},
		{".unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			got := MIMEFromExtension(tt.ext)
			if got != tt.want {
				t.Errorf("MIMEFromExtension(%q) = %q, want %q", tt.ext, got, tt.want)
			}
		})
	}
}

func TestExtensionFromMIME(t *testing.T) {
	tests := []struct {
		mime string
		want string
	}{
		{"image/jpeg", ".jpg"},
		{"image/png", ".png"},
		{"audio/mpeg", ".mp3"},
		{"text/plain; charset=utf-8", ".txt"},
		{"unknown/type", ""},
	}

	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			got := ExtensionFromMIME(tt.mime)
			if got != tt.want {
				t.Errorf("ExtensionFromMIME(%q) = %q, want %q", tt.mime, got, tt.want)
			}
		})
	}
}

func TestIsAudioExtension(t *testing.T) {
	audioExts := []string{".mp3", ".ogg", ".wav", ".flac", "m4a"}
	for _, ext := range audioExts {
		if !IsAudioExtension(ext) {
			t.Errorf("IsAudioExtension(%q) = false, want true", ext)
		}
	}

	nonAudio := []string{".jpg", ".pdf", ".mp4", ".txt"}
	for _, ext := range nonAudio {
		if IsAudioExtension(ext) {
			t.Errorf("IsAudioExtension(%q) = true, want false", ext)
		}
	}
}

func TestIsAudioFile(t *testing.T) {
	if !IsAudioFile("/path/to/song.mp3") {
		t.Error("mp3 should be audio")
	}
	if IsAudioFile("/path/to/image.jpg") {
		t.Error("jpg should not be audio")
	}
}

func TestIsGIF(t *testing.T) {
	if !IsGIF("image/gif", "") {
		t.Error("image/gif MIME should be GIF")
	}
	if !IsGIF("", "animation.gif") {
		t.Error(".gif extension should be GIF")
	}
	if IsGIF("image/png", "image.png") {
		t.Error("png should not be GIF")
	}
}

func TestDetectMIME(t *testing.T) {
	// Test with PNG magic bytes
	pngData := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}
	mime := DetectMIME(pngData, "", "")
	if mime != "image/png" {
		t.Errorf("DetectMIME for PNG = %q, want image/png", mime)
	}

	// Test with extension fallback
	mime = DetectMIME(nil, "file.pdf", "")
	if mime != "application/pdf" {
		t.Errorf("DetectMIME for .pdf = %q, want application/pdf", mime)
	}

	// Test with header fallback
	mime = DetectMIME(nil, "", "text/plain")
	if mime != "text/plain" {
		t.Errorf("DetectMIME with header = %q, want text/plain", mime)
	}
}

func TestValidateSize(t *testing.T) {
	// Image under limit
	if !ValidateSize(1024*1024, "image/jpeg") {
		t.Error("1MB image should be valid")
	}

	// Image over limit
	if ValidateSize(10*1024*1024, "image/jpeg") {
		t.Error("10MB image should be invalid")
	}

	// Document under limit
	if !ValidateSize(50*1024*1024, "application/pdf") {
		t.Error("50MB PDF should be valid")
	}
}

func TestImageMIMEFromFormat(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{"jpg", "image/jpeg"},
		{"JPEG", "image/jpeg"},
		{"png", "image/png"},
		{"gif", "image/gif"},
		{"webp", "image/webp"},
		{"unknown", ""},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			got := ImageMIMEFromFormat(tt.format)
			if got != tt.want {
				t.Errorf("ImageMIMEFromFormat(%q) = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}

func TestNormalizeHeaderMIME(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"text/plain", "text/plain"},
		{"text/plain; charset=utf-8", "text/plain"},
		{"  IMAGE/JPEG  ", "image/jpeg"},
		{"application/json; charset=utf-8; boundary=something", "application/json"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeHeaderMIME(tt.input)
			if got != tt.want {
				t.Errorf("normalizeHeaderMIME(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// Additional edge case tests

func TestMaxBytesForKind_Unknown(t *testing.T) {
	// Unknown kind should return document max (fallback)
	got := MaxBytesForKind(KindUnknown)
	if got != MaxDocumentBytes {
		t.Errorf("MaxBytesForKind(KindUnknown) = %d, want %d", got, MaxDocumentBytes)
	}
}

func TestGetExtension_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"http url", "http://example.com/file.mp3", ".mp3"},
		{"mixed case url", "HTTP://EXAMPLE.COM/FILE.MP3", ".mp3"},
		{"url with both query and fragment", "https://x.com/a.png?q=1#frag", ".png"},
		{"dotfile", ".gitignore", ".gitignore"}, // filepath.Ext returns the whole thing
		{"multiple dots", "file.tar.gz", ".gz"},
		{"trailing dot", "file.", "."}, // filepath.Ext returns "."
		{"only dots", "...", "."},      // filepath.Ext returns "."
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetExtension(tt.path)
			if got != tt.want {
				t.Errorf("GetExtension(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestMIMEFromExtension_AllMappings(t *testing.T) {
	// Test more extension mappings
	tests := []struct {
		ext  string
		want string
	}{
		{".heic", "image/heic"},
		{".heif", "image/heif"},
		{".webp", "image/webp"},
		{".gif", "image/gif"},
		{".bmp", "image/bmp"},
		{".svg", "image/svg+xml"},
		{".ico", "image/x-icon"},
		{".ogg", "audio/ogg"},
		{".wav", "audio/wav"},
		{".flac", "audio/flac"},
		{".m4a", "audio/mp4"},
		{".aac", "audio/aac"},
		{".opus", "audio/opus"},
		{".mp4", "video/mp4"},
		{".webm", "video/webm"},
		{".avi", "video/x-msvideo"},
		{".mov", "video/quicktime"},
		{".mkv", "video/x-matroska"},
		{".json", "application/json"},
		{".xml", "application/xml"},
		{".zip", "application/zip"},
		{".gz", "application/gzip"},
		{".tar", "application/x-tar"},
		{".7z", "application/x-7z-compressed"},
		{".rar", "application/vnd.rar"},
		{".doc", "application/msword"},
		{".docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		{".xls", "application/vnd.ms-excel"},
		{".xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
		{".ppt", "application/vnd.ms-powerpoint"},
		{".pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
		{".txt", "text/plain"},
		{".csv", "text/csv"},
		{".md", "text/markdown"},
		{".htm", "text/html"},
		{".html", "text/html"},
	}

	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			got := MIMEFromExtension(tt.ext)
			if got != tt.want {
				t.Errorf("MIMEFromExtension(%q) = %q, want %q", tt.ext, got, tt.want)
			}
		})
	}
}

func TestExtensionFromMIME_AllMappings(t *testing.T) {
	tests := []struct {
		mime string
		want string
	}{
		{"image/heic", ".heic"},
		{"image/heif", ".heif"},
		{"image/webp", ".webp"},
		{"image/gif", ".gif"},
		{"audio/ogg", ".ogg"},
		{"video/mp4", ".mp4"},
		{"  IMAGE/PNG  ", ".png"}, // with whitespace
	}

	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			got := ExtensionFromMIME(tt.mime)
			if got != tt.want {
				t.Errorf("ExtensionFromMIME(%q) = %q, want %q", tt.mime, got, tt.want)
			}
		})
	}
}

func TestIsAudioExtension_AllAudioTypes(t *testing.T) {
	audioExts := []string{".aac", ".flac", ".m4a", ".mp3", ".oga", ".ogg", ".opus", ".wav"}
	for _, ext := range audioExts {
		if !IsAudioExtension(ext) {
			t.Errorf("IsAudioExtension(%q) = false, want true", ext)
		}
	}
}

func TestDetectMIME_AllFallbacks(t *testing.T) {
	// JPEG magic bytes
	jpegData := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	mime := DetectMIME(jpegData, "", "")
	if mime != "image/jpeg" {
		t.Errorf("DetectMIME for JPEG = %q, want image/jpeg", mime)
	}

	// GIF magic bytes
	gifData := []byte{0x47, 0x49, 0x46, 0x38, 0x39, 0x61} // GIF89a
	mime = DetectMIME(gifData, "", "")
	if mime != "image/gif" {
		t.Errorf("DetectMIME for GIF = %q, want image/gif", mime)
	}

	// Octet-stream should fallback to extension
	unknownData := []byte{0x00, 0x01, 0x02, 0x03}
	mime = DetectMIME(unknownData, "file.mp3", "")
	if mime != "audio/mpeg" {
		t.Errorf("DetectMIME with unknown data = %q, want audio/mpeg", mime)
	}

	// Empty everything returns empty
	mime = DetectMIME(nil, "", "")
	if mime != "" {
		t.Errorf("DetectMIME with nothing = %q, want empty", mime)
	}

	// Header normalization
	mime = DetectMIME(nil, "", "  TEXT/HTML; charset=utf-8  ")
	if mime != "text/html" {
		t.Errorf("DetectMIME with header = %q, want text/html", mime)
	}
}

func TestValidateSize_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		size  int64
		mime  string
		valid bool
	}{
		{"image at limit", MaxImageBytes, "image/png", true},
		{"image over limit", MaxImageBytes + 1, "image/png", false},
		{"audio at limit", MaxAudioBytes, "audio/mpeg", true},
		{"audio over limit", MaxAudioBytes + 1, "audio/mpeg", false},
		{"video at limit", MaxVideoBytes, "video/mp4", true},
		{"video over limit", MaxVideoBytes + 1, "video/mp4", false},
		{"document at limit", MaxDocumentBytes, "application/pdf", true},
		{"document over limit", MaxDocumentBytes + 1, "application/pdf", false},
		{"zero size", 0, "image/png", true},
		{"negative size", -1, "image/png", true}, // negative is less than limit
		{"unknown mime uses document limit", MaxDocumentBytes, "unknown/type", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateSize(tt.size, tt.mime)
			if got != tt.valid {
				t.Errorf("ValidateSize(%d, %q) = %v, want %v", tt.size, tt.mime, got, tt.valid)
			}
		})
	}
}

func TestImageMIMEFromFormat_AllFormats(t *testing.T) {
	tests := []struct {
		format string
		want   string
	}{
		{"heic", "image/heic"},
		{"heif", "image/heif"},
		{"bmp", "image/bmp"},
		{"svg", "image/svg+xml"},
		{"WEBP", "image/webp"},
		{"GIF", "image/gif"},
		{"PNG", "image/png"},
	}

	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			got := ImageMIMEFromFormat(tt.format)
			if got != tt.want {
				t.Errorf("ImageMIMEFromFormat(%q) = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}

func TestIsGIF_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		mime     string
		filename string
		want     bool
	}{
		{"mime only", "image/gif", "", true},
		{"filename only", "", "file.gif", true},
		{"both match", "image/gif", "file.gif", true},
		{"mime uppercase", "IMAGE/GIF", "", true},
		{"filename uppercase", "", "FILE.GIF", true},
		{"neither", "image/png", "file.png", false},
		{"empty both", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsGIF(tt.mime, tt.filename)
			if got != tt.want {
				t.Errorf("IsGIF(%q, %q) = %v, want %v", tt.mime, tt.filename, got, tt.want)
			}
		})
	}
}

func TestKindFromMIME_MoreTypes(t *testing.T) {
	tests := []struct {
		mime string
		want Kind
	}{
		{"application/octet-stream", KindDocument},
		{"application/x-tar", KindDocument},
		{"text/html", KindDocument},
		{"text/css", KindDocument},
		{"text/javascript", KindDocument},
		{"audio/wav", KindAudio},
		{"audio/x-wav", KindAudio},
		{"video/x-msvideo", KindVideo},
		{"image/svg+xml", KindImage},
		{"image/x-icon", KindImage},
	}

	for _, tt := range tests {
		t.Run(tt.mime, func(t *testing.T) {
			got := KindFromMIME(tt.mime)
			if got != tt.want {
				t.Errorf("KindFromMIME(%q) = %v, want %v", tt.mime, got, tt.want)
			}
		})
	}
}
