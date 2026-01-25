package media

import (
	"strings"
	"testing"
)

func TestFormatMediaAttachedLine(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		mediaType string
		url       string
		index     int
		total     int
		want      string
	}{
		{
			name: "simple path only",
			path: "image.png",
			want: "[media attached: image.png]",
		},
		{
			name:      "path with type",
			path:      "photo.jpg",
			mediaType: "image",
			want:      "[media attached: photo.jpg (image)]",
		},
		{
			name: "path with url",
			path: "doc.pdf",
			url:  "https://example.com/doc.pdf",
			want: "[media attached: doc.pdf | https://example.com/doc.pdf]",
		},
		{
			name:      "path with type and url",
			path:      "video.mp4",
			mediaType: "video",
			url:       "https://cdn.example.com/video.mp4",
			want:      "[media attached: video.mp4 (video) | https://cdn.example.com/video.mp4]",
		},
		{
			name:  "with index and total",
			path:  "img1.png",
			index: 1,
			total: 3,
			want:  "[media attached 1/3: img1.png]",
		},
		{
			name:      "with index, total, type and url",
			path:      "audio.mp3",
			mediaType: "audio",
			url:       "https://music.example.com/audio.mp3",
			index:     2,
			total:     5,
			want:      "[media attached 2/5: audio.mp3 (audio) | https://music.example.com/audio.mp3]",
		},
		{
			name:      "whitespace in type is trimmed",
			path:      "file.txt",
			mediaType: "  document  ",
			want:      "[media attached: file.txt (document)]",
		},
		{
			name: "whitespace in url is trimmed",
			path: "file.txt",
			url:  "  https://example.com/file  ",
			want: "[media attached: file.txt | https://example.com/file]",
		},
		{
			name:      "empty type is omitted",
			path:      "file.txt",
			mediaType: "",
			want:      "[media attached: file.txt]",
		},
		{
			name:      "whitespace-only type is omitted",
			path:      "file.txt",
			mediaType: "   ",
			want:      "[media attached: file.txt]",
		},
		{
			name: "empty url is omitted",
			path: "file.txt",
			url:  "",
			want: "[media attached: file.txt]",
		},
		{
			name: "whitespace-only url is omitted",
			path: "file.txt",
			url:  "   ",
			want: "[media attached: file.txt]",
		},
		{
			name:  "index without total uses no-index format",
			path:  "file.txt",
			index: 2,
			total: 0,
			want:  "[media attached: file.txt]",
		},
		{
			name:  "total without index uses no-index format",
			path:  "file.txt",
			index: 0,
			total: 5,
			want:  "[media attached: file.txt]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatMediaAttachedLine(tt.path, tt.mediaType, tt.url, tt.index, tt.total)
			if got != tt.want {
				t.Errorf("FormatMediaAttachedLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildMediaNote_SingleAttachment(t *testing.T) {
	tests := []struct {
		name string
		ctx  MediaContext
		want string
	}{
		{
			name: "single path from MediaPath",
			ctx: MediaContext{
				MediaPath: "photo.jpg",
			},
			want: "[media attached: photo.jpg]",
		},
		{
			name: "single path with type",
			ctx: MediaContext{
				MediaPath: "photo.jpg",
				MediaType: "image",
			},
			want: "[media attached: photo.jpg (image)]",
		},
		{
			name: "single path with url",
			ctx: MediaContext{
				MediaPath: "doc.pdf",
				MediaUrl:  "https://example.com/doc.pdf",
			},
			want: "[media attached: doc.pdf | https://example.com/doc.pdf]",
		},
		{
			name: "single path with type and url",
			ctx: MediaContext{
				MediaPath: "video.mp4",
				MediaType: "video",
				MediaUrl:  "https://cdn.example.com/video.mp4",
			},
			want: "[media attached: video.mp4 (video) | https://cdn.example.com/video.mp4]",
		},
		{
			name: "single element in arrays",
			ctx: MediaContext{
				MediaPaths: []string{"image.png"},
				MediaTypes: []string{"image"},
				MediaUrls:  []string{"https://example.com/image.png"},
			},
			want: "[media attached: image.png (image) | https://example.com/image.png]",
		},
		{
			name: "whitespace-only MediaPath returns empty",
			ctx: MediaContext{
				MediaPath: "   ",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildMediaNote(tt.ctx)
			if got != tt.want {
				t.Errorf("BuildMediaNote() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildMediaNote_MultipleAttachments(t *testing.T) {
	tests := []struct {
		name      string
		ctx       MediaContext
		wantLines []string
	}{
		{
			name: "two paths",
			ctx: MediaContext{
				MediaPaths: []string{"img1.png", "img2.png"},
			},
			wantLines: []string{
				"[media attached: 2 files]",
				"[media attached 1/2: img1.png]",
				"[media attached 2/2: img2.png]",
			},
		},
		{
			name: "three paths with types",
			ctx: MediaContext{
				MediaPaths: []string{"img.png", "audio.mp3", "doc.pdf"},
				MediaTypes: []string{"image", "audio", "document"},
			},
			wantLines: []string{
				"[media attached: 3 files]",
				"[media attached 1/3: img.png (image)]",
				"[media attached 2/3: audio.mp3 (audio)]",
				"[media attached 3/3: doc.pdf (document)]",
			},
		},
		{
			name: "paths with types and urls",
			ctx: MediaContext{
				MediaPaths: []string{"a.jpg", "b.jpg"},
				MediaTypes: []string{"image", "image"},
				MediaUrls:  []string{"https://example.com/a.jpg", "https://example.com/b.jpg"},
			},
			wantLines: []string{
				"[media attached: 2 files]",
				"[media attached 1/2: a.jpg (image) | https://example.com/a.jpg]",
				"[media attached 2/2: b.jpg (image) | https://example.com/b.jpg]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildMediaNote(tt.ctx)
			want := strings.Join(tt.wantLines, "\n")
			if got != want {
				t.Errorf("BuildMediaNote() = %q, want %q", got, want)
			}
		})
	}
}

func TestBuildMediaNote_SuppressedIndices(t *testing.T) {
	tests := []struct {
		name      string
		ctx       MediaContext
		wantLines []string
	}{
		{
			name: "suppress first of three",
			ctx: MediaContext{
				MediaPaths:        []string{"img1.png", "img2.png", "img3.png"},
				SuppressedIndices: map[int]bool{0: true},
			},
			wantLines: []string{
				"[media attached: 2 files]",
				"[media attached 1/2: img2.png]",
				"[media attached 2/2: img3.png]",
			},
		},
		{
			name: "suppress middle of three",
			ctx: MediaContext{
				MediaPaths:        []string{"img1.png", "img2.png", "img3.png"},
				SuppressedIndices: map[int]bool{1: true},
			},
			wantLines: []string{
				"[media attached: 2 files]",
				"[media attached 1/2: img1.png]",
				"[media attached 2/2: img3.png]",
			},
		},
		{
			name: "suppress last of three",
			ctx: MediaContext{
				MediaPaths:        []string{"img1.png", "img2.png", "img3.png"},
				SuppressedIndices: map[int]bool{2: true},
			},
			wantLines: []string{
				"[media attached: 2 files]",
				"[media attached 1/2: img1.png]",
				"[media attached 2/2: img2.png]",
			},
		},
		{
			name: "suppress two of three leaves single",
			ctx: MediaContext{
				MediaPaths:        []string{"img1.png", "img2.png", "img3.png"},
				SuppressedIndices: map[int]bool{0: true, 2: true},
			},
			wantLines: []string{
				"[media attached: img2.png]",
			},
		},
		{
			name: "suppress all returns empty",
			ctx: MediaContext{
				MediaPaths:        []string{"img1.png", "img2.png"},
				SuppressedIndices: map[int]bool{0: true, 1: true},
			},
			wantLines: nil, // empty result
		},
		{
			name: "suppress with types preserved",
			ctx: MediaContext{
				MediaPaths:        []string{"img1.png", "audio.mp3", "doc.pdf"},
				MediaTypes:        []string{"image", "audio", "document"},
				SuppressedIndices: map[int]bool{1: true},
			},
			wantLines: []string{
				"[media attached: 2 files]",
				"[media attached 1/2: img1.png (image)]",
				"[media attached 2/2: doc.pdf (document)]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildMediaNote(tt.ctx)
			var want string
			if len(tt.wantLines) > 0 {
				want = strings.Join(tt.wantLines, "\n")
			}
			if got != want {
				t.Errorf("BuildMediaNote() = %q, want %q", got, want)
			}
		})
	}
}

func TestBuildMediaNote_EmptyAndEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		ctx  MediaContext
		want string
	}{
		{
			name: "empty context",
			ctx:  MediaContext{},
			want: "",
		},
		{
			name: "empty paths array",
			ctx: MediaContext{
				MediaPaths: []string{},
			},
			want: "",
		},
		{
			name: "nil suppressed indices",
			ctx: MediaContext{
				MediaPaths:        []string{"img.png"},
				SuppressedIndices: nil,
			},
			want: "[media attached: img.png]",
		},
		{
			name: "empty suppressed indices",
			ctx: MediaContext{
				MediaPaths:        []string{"img.png"},
				SuppressedIndices: map[int]bool{},
			},
			want: "[media attached: img.png]",
		},
		{
			name: "suppressed index out of range is ignored",
			ctx: MediaContext{
				MediaPaths:        []string{"img.png"},
				SuppressedIndices: map[int]bool{5: true, 10: true},
			},
			want: "[media attached: img.png]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildMediaNote(tt.ctx)
			if got != tt.want {
				t.Errorf("BuildMediaNote() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildMediaNote_ArraysVsSingleValueFallback(t *testing.T) {
	tests := []struct {
		name      string
		ctx       MediaContext
		wantLines []string
	}{
		{
			name: "types array shorter than paths uses single type fallback",
			ctx: MediaContext{
				MediaPaths: []string{"a.png", "b.png", "c.png"},
				MediaTypes: []string{"image"}, // only 1 element, doesn't match paths length
				MediaType:  "document",        // fallback
			},
			wantLines: []string{
				"[media attached: 3 files]",
				"[media attached 1/3: a.png (document)]",
				"[media attached 2/3: b.png (document)]",
				"[media attached 3/3: c.png (document)]",
			},
		},
		{
			name: "urls array shorter than paths uses single url fallback",
			ctx: MediaContext{
				MediaPaths: []string{"a.png", "b.png"},
				MediaUrls:  []string{"https://example.com/a.png"}, // only 1 element
				MediaUrl:   "https://default.com/file",            // fallback
			},
			wantLines: []string{
				"[media attached: 2 files]",
				"[media attached 1/2: a.png | https://default.com/file]",
				"[media attached 2/2: b.png | https://default.com/file]",
			},
		},
		{
			name: "arrays match paths length - single values ignored",
			ctx: MediaContext{
				MediaPaths: []string{"a.png", "b.pdf"},
				MediaTypes: []string{"image", "document"},
				MediaUrls:  []string{"https://img.com/a", "https://doc.com/b"},
				MediaType:  "fallback-type",  // should be ignored
				MediaUrl:   "https://fb.com", // should be ignored
			},
			wantLines: []string{
				"[media attached: 2 files]",
				"[media attached 1/2: a.png (image) | https://img.com/a]",
				"[media attached 2/2: b.pdf (document) | https://doc.com/b]",
			},
		},
		{
			name: "MediaPaths array takes precedence over MediaPath",
			ctx: MediaContext{
				MediaPaths: []string{"array1.png", "array2.png"},
				MediaPath:  "single.png", // should be ignored
			},
			wantLines: []string{
				"[media attached: 2 files]",
				"[media attached 1/2: array1.png]",
				"[media attached 2/2: array2.png]",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildMediaNote(tt.ctx)
			want := strings.Join(tt.wantLines, "\n")
			if got != want {
				t.Errorf("BuildMediaNote() = %q, want %q", got, want)
			}
		})
	}
}

func TestBuildMediaNote_MixedSingleAndArrays(t *testing.T) {
	// Test that single values are used as fallback when arrays don't match
	ctx := MediaContext{
		MediaPaths: []string{"file1.txt", "file2.txt", "file3.txt"},
		MediaTypes: []string{}, // empty, should use MediaType fallback
		MediaUrls:  []string{}, // empty, should use MediaUrl fallback
		MediaType:  "text",
		MediaUrl:   "https://files.example.com",
	}

	got := BuildMediaNote(ctx)
	wantLines := []string{
		"[media attached: 3 files]",
		"[media attached 1/3: file1.txt (text) | https://files.example.com]",
		"[media attached 2/3: file2.txt (text) | https://files.example.com]",
		"[media attached 3/3: file3.txt (text) | https://files.example.com]",
	}
	want := strings.Join(wantLines, "\n")

	if got != want {
		t.Errorf("BuildMediaNote() = %q, want %q", got, want)
	}
}
