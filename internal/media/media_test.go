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
