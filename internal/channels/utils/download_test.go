package utils

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultDownloadOptions(t *testing.T) {
	opts := DefaultDownloadOptions()

	if opts.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want %v", opts.Timeout, 30*time.Second)
	}
	if opts.MaxSize != 50*1024*1024 {
		t.Errorf("MaxSize = %d, want %d", opts.MaxSize, 50*1024*1024)
	}
}

func TestDownloadURL(t *testing.T) {
	tests := []struct {
		name       string
		handler    http.HandlerFunc
		opts       DownloadOptions
		wantErr    bool
		wantData   string
		errContain string
	}{
		{
			name: "successful download",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("hello world"))
			},
			opts:     DefaultDownloadOptions(),
			wantErr:  false,
			wantData: "hello world",
		},
		{
			name: "respects custom headers",
			handler: func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Custom") != "value" {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("ok"))
			},
			opts: DownloadOptions{
				Timeout: 5 * time.Second,
				Headers: map[string]string{"X-Custom": "value"},
			},
			wantErr:  false,
			wantData: "ok",
		},
		{
			name: "handles 404 error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			opts:       DefaultDownloadOptions(),
			wantErr:    true,
			errContain: "unexpected status: 404",
		},
		{
			name: "handles 500 error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			opts:       DefaultDownloadOptions(),
			wantErr:    true,
			errContain: "unexpected status: 500",
		},
		{
			name: "respects max size limit",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				// Write more than the limit
				w.Write([]byte("this is a long string that exceeds the limit"))
			},
			opts: DownloadOptions{
				Timeout: 5 * time.Second,
				MaxSize: 10,
			},
			wantErr:  false,
			wantData: "this is a ", // Only first 10 bytes
		},
		{
			name: "uses default timeout when zero",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("ok"))
			},
			opts: DownloadOptions{
				Timeout: 0,
			},
			wantErr:  false,
			wantData: "ok",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			data, err := DownloadURL(context.Background(), server.URL, tt.opts)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
					return
				}
				if tt.errContain != "" && !containsString(err.Error(), tt.errContain) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.errContain)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if string(data) != tt.wantData {
				t.Errorf("data = %q, want %q", string(data), tt.wantData)
			}
		})
	}
}

func TestDownloadURL_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("slow response"))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := DownloadURL(ctx, server.URL, DownloadOptions{Timeout: 5 * time.Second})
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestDownloadURL_InvalidURL(t *testing.T) {
	_, err := DownloadURL(context.Background(), "not-a-valid-url", DefaultDownloadOptions())
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestDownloadToFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("file content"))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "subdir", "downloaded.txt")

	err := DownloadToFile(context.Background(), server.URL, destPath, DefaultDownloadOptions())
	if err != nil {
		t.Errorf("DownloadToFile error: %v", err)
		return
	}

	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Errorf("failed to read downloaded file: %v", err)
		return
	}

	if string(data) != "file content" {
		t.Errorf("file content = %q, want %q", string(data), "file content")
	}
}

func TestDownloadToFile_DownloadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "not-downloaded.txt")

	err := DownloadToFile(context.Background(), server.URL, destPath, DefaultDownloadOptions())
	if err == nil {
		t.Error("expected error for 404 response")
	}

	// File should not exist
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Error("file should not be created on error")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStringHelper(s, substr))
}

func containsStringHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
