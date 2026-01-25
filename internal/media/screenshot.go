package media

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png" // Register PNG decoder

	"golang.org/x/image/draw"
)

// Default limits for browser screenshots
const (
	DefaultScreenshotMaxSide  = 2000
	DefaultScreenshotMaxBytes = 5 * 1024 * 1024 // 5MB
)

// ScreenshotOptions for normalization
type ScreenshotOptions struct {
	MaxSide  int
	MaxBytes int
}

// ScreenshotResult from normalization
type ScreenshotResult struct {
	Buffer      []byte
	ContentType string
	Width       int
	Height      int
	Resized     bool
}

// NormalizeBrowserScreenshot resizes and compresses a screenshot to fit limits.
// It follows the clawdbot browser/screenshot.ts pattern, trying various
// combinations of size and quality to fit within the specified limits.
func NormalizeBrowserScreenshot(data []byte, opts *ScreenshotOptions) (*ScreenshotResult, error) {
	maxSide := DefaultScreenshotMaxSide
	maxBytes := DefaultScreenshotMaxBytes

	if opts != nil {
		if opts.MaxSide > 0 {
			maxSide = opts.MaxSide
		}
		if opts.MaxBytes > 0 {
			maxBytes = opts.MaxBytes
		}
	}

	// Decode the image
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	maxDim := max(width, height)

	// Check if we need to process
	if len(data) <= maxBytes && width <= maxSide && height <= maxSide {
		return &ScreenshotResult{
			Buffer:      data,
			ContentType: "image/" + format,
			Width:       width,
			Height:      height,
			Resized:     false,
		}, nil
	}

	// Quality grid for compression attempts
	qualities := []int{85, 75, 65, 55, 45, 35}

	// Size grid for resizing attempts
	sideStart := min(maxSide, maxDim)
	sideGrid := uniqueSorted([]int{sideStart, 1800, 1600, 1400, 1200, 1000, 800}, maxSide)

	var smallest *ScreenshotResult

	for _, side := range sideGrid {
		for _, quality := range qualities {
			result, err := resizeAndCompress(img, side, quality)
			if err != nil {
				continue
			}

			if smallest == nil || len(result.Buffer) < len(smallest.Buffer) {
				smallest = result
			}

			if len(result.Buffer) <= maxBytes {
				result.Resized = true
				return result, nil
			}
		}
	}

	if smallest != nil {
		return nil, fmt.Errorf("screenshot could not be reduced below %dMB (got %.2fMB)",
			maxBytes/(1024*1024), float64(len(smallest.Buffer))/(1024*1024))
	}

	return nil, fmt.Errorf("failed to process screenshot")
}

// resizeAndCompress resizes an image and compresses as JPEG
func resizeAndCompress(img image.Image, maxSide, quality int) (*ScreenshotResult, error) {
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Calculate new dimensions
	newWidth, newHeight := width, height
	if width > maxSide || height > maxSide {
		if width > height {
			newWidth = maxSide
			newHeight = int(float64(height) * float64(maxSide) / float64(width))
		} else {
			newHeight = maxSide
			newWidth = int(float64(width) * float64(maxSide) / float64(height))
		}
	}

	// Resize if needed
	var resized image.Image
	if newWidth != width || newHeight != height {
		dst := image.NewRGBA(image.Rect(0, 0, newWidth, newHeight))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, bounds, draw.Over, nil)
		resized = dst
	} else {
		resized = img
	}

	// Encode as JPEG
	var buf bytes.Buffer
	err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: quality})
	if err != nil {
		return nil, err
	}

	return &ScreenshotResult{
		Buffer:      buf.Bytes(),
		ContentType: "image/jpeg",
		Width:       newWidth,
		Height:      newHeight,
	}, nil
}

// GetImageMetadata extracts image dimensions without full decode
func GetImageMetadata(data []byte) (*ImageMetadata, error) {
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	return &ImageMetadata{
		Width:  config.Width,
		Height: config.Height,
		Format: format,
	}, nil
}

// ImageMetadata contains basic image information
type ImageMetadata struct {
	Width  int
	Height int
	Format string
}

// uniqueSorted returns sorted unique values <= max in descending order
func uniqueSorted(values []int, maxVal int) []int {
	seen := make(map[int]bool)
	var result []int

	for _, v := range values {
		if v > 0 && v <= maxVal && !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}

	// Sort descending
	for i := 0; i < len(result)-1; i++ {
		for j := i + 1; j < len(result); j++ {
			if result[i] < result[j] {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result
}
