package media

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func createTestImage(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	// Fill with a color
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func TestDetectMediaType(t *testing.T) {
	tests := []struct {
		mimeType string
		filename string
		want     MediaType
	}{
		{"image/png", "", MediaTypeImage},
		{"image/jpeg", "", MediaTypeImage},
		{"audio/mp3", "", MediaTypeAudio},
		{"audio/wav", "", MediaTypeAudio},
		{"video/mp4", "", MediaTypeVideo},
		{"application/pdf", "", MediaTypeDocument},
		{"", "photo.jpg", MediaTypeImage},
		{"", "audio.mp3", MediaTypeAudio},
		{"", "video.mp4", MediaTypeVideo},
		{"", "document.pdf", MediaTypeDocument},
		{"", "unknown.xyz", MediaTypeUnknown},
		{"application/octet-stream", "", MediaTypeUnknown},
	}

	for _, tt := range tests {
		got := DetectMediaType(tt.mimeType, tt.filename)
		if got != tt.want {
			t.Errorf("DetectMediaType(%q, %q) = %v, want %v",
				tt.mimeType, tt.filename, got, tt.want)
		}
	}
}

func TestIsSupported(t *testing.T) {
	tests := []struct {
		mimeType string
		want     bool
	}{
		{"image/jpeg", true},
		{"image/png", true},
		{"image/gif", true},
		{"image/webp", true},
		{"audio/mp3", true},
		{"audio/wav", true},
		{"video/mp4", false}, // Video not fully supported yet
		{"application/pdf", false},
		{"text/plain", false},
	}

	for _, tt := range tests {
		got := IsSupported(tt.mimeType)
		if got != tt.want {
			t.Errorf("IsSupported(%q) = %v, want %v", tt.mimeType, got, tt.want)
		}
	}
}

func TestDefaultProcessor_Process_Image(t *testing.T) {
	processor := NewDefaultProcessor(nil)

	imageData := createTestImage(100, 100)

	attachment := &Attachment{
		ID:       "test-1",
		Type:     MediaTypeImage,
		MimeType: "image/png",
		Data:     imageData,
	}

	opts := DefaultOptions()
	result, err := processor.Process(attachment, opts)

	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}

	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result.Contents))
	}

	content := result.Contents[0]
	if content.Type != ContentTypeImage {
		t.Errorf("expected image content type, got %s", content.Type)
	}

	if content.ImageData == "" {
		t.Error("expected image data to be set")
	}

	if content.ImageMediaType != "image/png" {
		t.Errorf("expected image/png, got %s", content.ImageMediaType)
	}
}

func TestDefaultProcessor_Process_ImageResize(t *testing.T) {
	processor := NewDefaultProcessor(nil)

	// Create a large image
	imageData := createTestImage(3000, 2000)

	attachment := &Attachment{
		ID:       "test-1",
		Type:     MediaTypeImage,
		MimeType: "image/png",
		Data:     imageData,
	}

	opts := ProcessingOptions{
		MaxImageSize:  1000,
		EnableVision:  true,
		MaxFileSize:   50 * 1024 * 1024,
	}

	result, err := processor.Process(attachment, opts)

	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}

	// The image should have been resized
	if len(result.Contents) != 1 {
		t.Fatalf("expected 1 content, got %d", len(result.Contents))
	}

	// Source should show resized dimensions
	content := result.Contents[0]
	if content.Source == "image:3000x2000" {
		t.Error("image should have been resized")
	}
}

func TestDefaultProcessor_Process_VisionDisabled(t *testing.T) {
	processor := NewDefaultProcessor(nil)

	imageData := createTestImage(100, 100)

	attachment := &Attachment{
		ID:       "test-1",
		Type:     MediaTypeImage,
		MimeType: "image/png",
		Data:     imageData,
	}

	opts := ProcessingOptions{
		EnableVision: false,
	}

	result, err := processor.Process(attachment, opts)

	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Should have no content since vision is disabled
	if len(result.Contents) != 0 {
		t.Errorf("expected 0 contents with vision disabled, got %d", len(result.Contents))
	}
}

func TestDefaultProcessor_Process_FileTooLarge(t *testing.T) {
	processor := NewDefaultProcessor(nil)

	// Create image data
	imageData := createTestImage(100, 100)

	attachment := &Attachment{
		ID:       "test-1",
		Type:     MediaTypeImage,
		MimeType: "image/png",
		Data:     imageData,
	}

	opts := ProcessingOptions{
		MaxFileSize:  100, // Very small limit
		EnableVision: true,
	}

	result, err := processor.Process(attachment, opts)

	// Should fail due to file size
	if err == nil && result.Error == "" {
		t.Error("expected error for oversized file")
	}
}

func TestAggregator_Aggregate(t *testing.T) {
	processor := NewDefaultProcessor(nil)
	aggregator := NewAggregator(processor, nil)

	attachments := []*Attachment{
		{
			ID:       "img-1",
			Type:     MediaTypeImage,
			MimeType: "image/png",
			Data:     createTestImage(50, 50),
		},
		{
			ID:       "img-2",
			Type:     MediaTypeImage,
			MimeType: "image/png",
			Data:     createTestImage(75, 75),
		},
	}

	opts := DefaultOptions()
	content := aggregator.Aggregate(context.Background(), attachments, opts)

	if content.TotalCount != 2 {
		t.Errorf("expected total count 2, got %d", content.TotalCount)
	}

	if content.ProcessedCount != 2 {
		t.Errorf("expected processed count 2, got %d", content.ProcessedCount)
	}

	if len(content.Images) != 2 {
		t.Errorf("expected 2 images, got %d", len(content.Images))
	}

	if content.HasErrors() {
		t.Errorf("unexpected errors: %v", content.Errors)
	}
}

func TestAggregator_ProcessAll_Concurrent(t *testing.T) {
	processor := NewDefaultProcessor(nil)
	aggregator := NewAggregator(processor, nil)
	aggregator.SetConcurrency(2)

	// Create multiple attachments
	attachments := make([]*Attachment, 5)
	for i := 0; i < 5; i++ {
		attachments[i] = &Attachment{
			ID:       string(rune('A' + i)),
			Type:     MediaTypeImage,
			MimeType: "image/png",
			Data:     createTestImage(50, 50),
		}
	}

	opts := DefaultOptions()
	results := aggregator.ProcessAll(context.Background(), attachments, opts)

	if len(results) != 5 {
		t.Errorf("expected 5 results, got %d", len(results))
	}

	for i, result := range results {
		if result == nil {
			t.Errorf("result[%d] is nil", i)
			continue
		}
		if result.Error != "" {
			t.Errorf("result[%d] has error: %s", i, result.Error)
		}
	}
}

func TestAggregatedContent_HasContent(t *testing.T) {
	tests := []struct {
		name    string
		content AggregatedContent
		want    bool
	}{
		{
			name:    "empty",
			content: AggregatedContent{},
			want:    false,
		},
		{
			name: "has images",
			content: AggregatedContent{
				Images: []Content{{Type: ContentTypeImage}},
			},
			want: true,
		},
		{
			name: "has text",
			content: AggregatedContent{
				Text: "some text",
			},
			want: true,
		},
		{
			name: "only errors",
			content: AggregatedContent{
				Errors: []string{"error"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.content.HasContent(); got != tt.want {
				t.Errorf("HasContent() = %v, want %v", got, tt.want)
			}
		})
	}
}
