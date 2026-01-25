package media

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// createTestPNG creates a PNG image with the specified dimensions
func createTestPNG(width, height int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
		}
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// createTestJPEG creates a JPEG image with the specified dimensions
func createTestJPEG(width, height int, quality int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// Use varied colors to create more realistic compression
			img.Set(x, y, color.RGBA{
				R: uint8((x * 17) % 256),
				G: uint8((y * 13) % 256),
				B: uint8(((x + y) * 7) % 256),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	return buf.Bytes()
}

func TestNormalizeBrowserScreenshot_SmallImage(t *testing.T) {
	// Create a small image that doesn't need processing
	data := createTestPNG(100, 100)

	result, err := NormalizeBrowserScreenshot(data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Resized {
		t.Error("small image should not be resized")
	}

	if result.Width != 100 || result.Height != 100 {
		t.Errorf("expected 100x100, got %dx%d", result.Width, result.Height)
	}

	if result.ContentType != "image/png" {
		t.Errorf("expected image/png, got %s", result.ContentType)
	}

	// Data should be unchanged
	if !bytes.Equal(result.Buffer, data) {
		t.Error("buffer should be unchanged for small image")
	}
}

func TestNormalizeBrowserScreenshot_LargeDimensions(t *testing.T) {
	// Create an image larger than default max side
	data := createTestPNG(3000, 2000)

	result, err := NormalizeBrowserScreenshot(data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Resized {
		t.Error("large image should be resized")
	}

	// Should be resized to fit within 2000x2000
	if result.Width > DefaultScreenshotMaxSide || result.Height > DefaultScreenshotMaxSide {
		t.Errorf("image should be resized to fit within %d, got %dx%d",
			DefaultScreenshotMaxSide, result.Width, result.Height)
	}

	// Should be converted to JPEG
	if result.ContentType != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %s", result.ContentType)
	}

	// Aspect ratio should be preserved
	originalRatio := float64(3000) / float64(2000)
	newRatio := float64(result.Width) / float64(result.Height)
	if absFloat(originalRatio-newRatio) > 0.01 {
		t.Errorf("aspect ratio not preserved: original %.2f, new %.2f", originalRatio, newRatio)
	}
}

func TestNormalizeBrowserScreenshot_CustomMaxSide(t *testing.T) {
	data := createTestPNG(1500, 1000)

	opts := &ScreenshotOptions{
		MaxSide: 800,
	}

	result, err := NormalizeBrowserScreenshot(data, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Resized {
		t.Error("image should be resized with custom max side")
	}

	if result.Width > 800 || result.Height > 800 {
		t.Errorf("image should fit within 800, got %dx%d", result.Width, result.Height)
	}
}

func TestNormalizeBrowserScreenshot_CustomMaxBytes(t *testing.T) {
	// Create a reasonably sized image
	data := createTestPNG(800, 600)

	opts := &ScreenshotOptions{
		MaxBytes: 50 * 1024, // 50KB - very restrictive
	}

	result, err := NormalizeBrowserScreenshot(data, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Buffer) > opts.MaxBytes {
		t.Errorf("result should be under %d bytes, got %d", opts.MaxBytes, len(result.Buffer))
	}
}

func TestNormalizeBrowserScreenshot_JPEGInput(t *testing.T) {
	// Test with JPEG input
	data := createTestJPEG(500, 400, 90)

	result, err := NormalizeBrowserScreenshot(data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Resized {
		t.Error("small JPEG should not be resized")
	}

	if result.Width != 500 || result.Height != 400 {
		t.Errorf("expected 500x400, got %dx%d", result.Width, result.Height)
	}

	if result.ContentType != "image/jpeg" {
		t.Errorf("expected image/jpeg, got %s", result.ContentType)
	}
}

func TestNormalizeBrowserScreenshot_InvalidImage(t *testing.T) {
	data := []byte("not an image")

	_, err := NormalizeBrowserScreenshot(data, nil)
	if err == nil {
		t.Error("expected error for invalid image data")
	}
}

func TestNormalizeBrowserScreenshot_EmptyImage(t *testing.T) {
	data := []byte{}

	_, err := NormalizeBrowserScreenshot(data, nil)
	if err == nil {
		t.Error("expected error for empty data")
	}
}

func TestNormalizeBrowserScreenshot_PortraitOrientation(t *testing.T) {
	// Test portrait orientation (height > width)
	data := createTestPNG(1000, 2500)

	result, err := NormalizeBrowserScreenshot(data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Resized {
		t.Error("tall image should be resized")
	}

	// Height should be the limiting factor
	if result.Height > DefaultScreenshotMaxSide {
		t.Errorf("height should be <= %d, got %d", DefaultScreenshotMaxSide, result.Height)
	}

	// Width should be scaled proportionally
	expectedWidth := int(float64(1000) * float64(DefaultScreenshotMaxSide) / float64(2500))
	if abs(result.Width-expectedWidth) > 1 {
		t.Errorf("expected width ~%d, got %d", expectedWidth, result.Width)
	}
}

func TestNormalizeBrowserScreenshot_ExactlyAtLimit(t *testing.T) {
	// Create an image exactly at the default max side
	data := createTestPNG(2000, 1500)

	result, err := NormalizeBrowserScreenshot(data, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Image is at size limit but may exceed byte limit
	// Just verify it doesn't error
	if result.Width > DefaultScreenshotMaxSide || result.Height > DefaultScreenshotMaxSide {
		t.Errorf("result should fit within limits, got %dx%d", result.Width, result.Height)
	}
}

func TestGetImageMetadata_PNG(t *testing.T) {
	data := createTestPNG(640, 480)

	meta, err := GetImageMetadata(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.Width != 640 {
		t.Errorf("expected width 640, got %d", meta.Width)
	}

	if meta.Height != 480 {
		t.Errorf("expected height 480, got %d", meta.Height)
	}

	if meta.Format != "png" {
		t.Errorf("expected format png, got %s", meta.Format)
	}
}

func TestGetImageMetadata_JPEG(t *testing.T) {
	data := createTestJPEG(1920, 1080, 85)

	meta, err := GetImageMetadata(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if meta.Width != 1920 {
		t.Errorf("expected width 1920, got %d", meta.Width)
	}

	if meta.Height != 1080 {
		t.Errorf("expected height 1080, got %d", meta.Height)
	}

	if meta.Format != "jpeg" {
		t.Errorf("expected format jpeg, got %s", meta.Format)
	}
}

func TestGetImageMetadata_Invalid(t *testing.T) {
	data := []byte("not an image")

	_, err := GetImageMetadata(data)
	if err == nil {
		t.Error("expected error for invalid image data")
	}
}

func TestUniqueSorted(t *testing.T) {
	tests := []struct {
		name   string
		values []int
		maxVal int
		want   []int
	}{
		{
			name:   "basic",
			values: []int{100, 200, 300},
			maxVal: 300,
			want:   []int{300, 200, 100},
		},
		{
			name:   "with max filter",
			values: []int{100, 200, 300, 400},
			maxVal: 250,
			want:   []int{200, 100},
		},
		{
			name:   "with duplicates",
			values: []int{100, 200, 100, 200, 300},
			maxVal: 300,
			want:   []int{300, 200, 100},
		},
		{
			name:   "with zero values",
			values: []int{0, 100, 0, 200},
			maxVal: 300,
			want:   []int{200, 100},
		},
		{
			name:   "empty result",
			values: []int{500, 600},
			maxVal: 400,
			want:   []int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uniqueSorted(tt.values, tt.maxVal)
			if len(got) != len(tt.want) {
				t.Errorf("uniqueSorted() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("uniqueSorted()[%d] = %d, want %d", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestResizeAndCompress(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1000, 800))
	for y := 0; y < 800; y++ {
		for x := 0; x < 1000; x++ {
			img.Set(x, y, color.RGBA{R: 128, G: 128, B: 128, A: 255})
		}
	}

	tests := []struct {
		name      string
		maxSide   int
		quality   int
		wantWidth int
	}{
		{
			name:      "resize down",
			maxSide:   500,
			quality:   85,
			wantWidth: 500,
		},
		{
			name:      "no resize needed",
			maxSide:   1500,
			quality:   85,
			wantWidth: 1000,
		},
		{
			name:      "low quality",
			maxSide:   500,
			quality:   35,
			wantWidth: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := resizeAndCompress(img, tt.maxSide, tt.quality)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Width != tt.wantWidth {
				t.Errorf("expected width %d, got %d", tt.wantWidth, result.Width)
			}

			if result.ContentType != "image/jpeg" {
				t.Errorf("expected image/jpeg, got %s", result.ContentType)
			}

			if len(result.Buffer) == 0 {
				t.Error("buffer should not be empty")
			}
		})
	}
}

func TestNormalizeBrowserScreenshot_QualityProgression(t *testing.T) {
	// Create a large, complex image that will need multiple quality attempts
	img := image.NewRGBA(image.Rect(0, 0, 2500, 2000))
	for y := 0; y < 2000; y++ {
		for x := 0; x < 2500; x++ {
			// Create a complex pattern that doesn't compress well
			img.Set(x, y, color.RGBA{
				R: uint8((x * y) % 256),
				G: uint8((x + y*2) % 256),
				B: uint8((x*3 + y) % 256),
				A: 255,
			})
		}
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)
	data := buf.Bytes()

	// Use restrictive byte limit to force quality reduction
	opts := &ScreenshotOptions{
		MaxBytes: 200 * 1024, // 200KB
		MaxSide:  2000,
	}

	result, err := NormalizeBrowserScreenshot(data, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.Buffer) > opts.MaxBytes {
		t.Errorf("result should be under %d bytes, got %d", opts.MaxBytes, len(result.Buffer))
	}

	if !result.Resized {
		t.Error("image should be marked as resized")
	}
}

// abs returns absolute value of an int
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// absFloat returns absolute value of a float64
func absFloat(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
