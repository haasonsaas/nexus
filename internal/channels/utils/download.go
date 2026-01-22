package utils

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// DownloadOptions configures HTTP download behavior.
type DownloadOptions struct {
	// Timeout for the entire download operation.
	Timeout time.Duration

	// MaxSize limits the download size in bytes (0 = unlimited).
	MaxSize int64

	// Headers to include in the request.
	Headers map[string]string
}

// DefaultDownloadOptions returns sensible defaults for downloads.
func DefaultDownloadOptions() DownloadOptions {
	return DownloadOptions{
		Timeout: 30 * time.Second,
		MaxSize: 50 * 1024 * 1024, // 50MB
	}
}

// DownloadURL downloads content from a URL and returns the bytes.
func DownloadURL(ctx context.Context, url string, opts DownloadOptions) ([]byte, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{
		Timeout: opts.Timeout,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}

	var reader io.Reader = resp.Body
	if opts.MaxSize > 0 {
		reader = io.LimitReader(resp.Body, opts.MaxSize)
	}

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	return data, nil
}

// DownloadToFile downloads a URL to a local file.
func DownloadToFile(ctx context.Context, url, destPath string, opts DownloadOptions) error {
	data, err := DownloadURL(ctx, url, opts)
	if err != nil {
		return err
	}

	if err := EnsureParentDir(destPath); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}
