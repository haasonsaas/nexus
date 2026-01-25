package links

import (
	"testing"
)

func TestExtractLinksFromMessage(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		maxLinks int
		want     []string
	}{
		{
			name:     "single http url",
			message:  "Check out http://example.com for more info",
			maxLinks: 5,
			want:     []string{"http://example.com"},
		},
		{
			name:     "single https url",
			message:  "Check out https://example.com for more info",
			maxLinks: 5,
			want:     []string{"https://example.com"},
		},
		{
			name:     "multiple urls",
			message:  "Visit https://example.com and https://test.com for details",
			maxLinks: 5,
			want:     []string{"https://example.com", "https://test.com"},
		},
		{
			name:     "url with path",
			message:  "See https://example.com/path/to/page for more",
			maxLinks: 5,
			want:     []string{"https://example.com/path/to/page"},
		},
		{
			name:     "url with query parameters",
			message:  "Link: https://example.com/search?q=test&page=1",
			maxLinks: 5,
			want:     []string{"https://example.com/search?q=test&page=1"},
		},
		{
			name:     "url with trailing punctuation",
			message:  "Check https://example.com. It's great!",
			maxLinks: 5,
			want:     []string{"https://example.com"},
		},
		{
			name:     "url with trailing comma",
			message:  "Sites: https://example.com, https://test.com, and more",
			maxLinks: 5,
			want:     []string{"https://example.com", "https://test.com"},
		},
		{
			name:     "url with trailing semicolon",
			message:  "See https://example.com; for more details",
			maxLinks: 5,
			want:     []string{"https://example.com"},
		},
		{
			name:     "duplicate urls",
			message:  "https://example.com is great, https://example.com is awesome",
			maxLinks: 5,
			want:     []string{"https://example.com"},
		},
		{
			name:     "max links limit",
			message:  "https://a.com https://b.com https://c.com https://d.com https://e.com https://f.com",
			maxLinks: 3,
			want:     []string{"https://a.com", "https://b.com", "https://c.com"},
		},
		{
			name:     "default max links",
			message:  "https://a.com https://b.com https://c.com https://d.com https://e.com https://f.com https://g.com",
			maxLinks: 0,
			want:     []string{"https://a.com", "https://b.com", "https://c.com", "https://d.com", "https://e.com"},
		},
		{
			name:     "no urls",
			message:  "This message has no URLs",
			maxLinks: 5,
			want:     nil,
		},
		{
			name:     "empty message",
			message:  "",
			maxLinks: 5,
			want:     nil,
		},
		{
			name:     "url with fragment",
			message:  "See https://example.com/page#section for details",
			maxLinks: 5,
			want:     []string{"https://example.com/page#section"},
		},
		{
			name:     "url with port",
			message:  "Connect to https://example.com:8080/api",
			maxLinks: 5,
			want:     []string{"https://example.com:8080/api"},
		},
		{
			name:     "url in parentheses",
			message:  "More info (https://example.com)",
			maxLinks: 5,
			want:     []string{"https://example.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractLinksFromMessage(tt.message, tt.maxLinks)
			if len(got) != len(tt.want) {
				t.Errorf("ExtractLinksFromMessage() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ExtractLinksFromMessage()[%d] = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestIsShortenerURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://bit.ly/abc123", true},
		{"http://t.co/xyz", true},
		{"https://goo.gl/maps/abc", true},
		{"http://tinyurl.com/abc", true},
		{"https://ow.ly/12345", true},
		{"https://is.gd/short", true},
		{"https://buff.ly/abc", true},
		{"https://j.mp/abc", true},
		{"https://rb.gy/abc", true},
		{"https://cutt.ly/abc", true},
		{"https://www.bit.ly/abc", true},
		{"https://example.com", false},
		{"https://google.com", false},
		{"https://bit.ly.fake.com/abc", false},
		{"not a url", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := IsShortenerURL(tt.url)
			if got != tt.want {
				t.Errorf("IsShortenerURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestNormalizeURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "lowercase scheme and host",
			raw:  "HTTPS://EXAMPLE.COM/Path",
			want: "https://example.com/Path",
		},
		{
			name: "remove default http port",
			raw:  "http://example.com:80/path",
			want: "http://example.com/path",
		},
		{
			name: "remove default https port",
			raw:  "https://example.com:443/path",
			want: "https://example.com/path",
		},
		{
			name: "keep non-default port",
			raw:  "https://example.com:8080/path",
			want: "https://example.com:8080/path",
		},
		{
			name: "remove trailing slash",
			raw:  "https://example.com/path/",
			want: "https://example.com/path",
		},
		{
			name: "keep root slash",
			raw:  "https://example.com/",
			want: "https://example.com/",
		},
		{
			name: "sort query parameters",
			raw:  "https://example.com/path?z=1&a=2",
			want: "https://example.com/path?a=2&z=1",
		},
		{
			name: "remove fragment",
			raw:  "https://example.com/path#section",
			want: "https://example.com/path",
		},
		{
			name: "relative path gets url encoded",
			raw:  "not a url",
			want: "not%20a%20url",
		},
		{
			name: "empty string",
			raw:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeURL(tt.raw)
			if got != tt.want {
				t.Errorf("NormalizeURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestExtractDomain(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://example.com/path", "example.com"},
		{"http://SUB.EXAMPLE.COM:8080/path", "sub.example.com"},
		{"https://localhost:3000", "localhost"},
		{"invalid", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := ExtractDomain(tt.url)
			if got != tt.want {
				t.Errorf("ExtractDomain(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestIsValidURL(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://example.com", true},
		{"http://example.com", true},
		{"https://example.com/path?query=1", true},
		{"ftp://example.com", false},
		{"file:///path/to/file", false},
		{"example.com", false},
		{"not a url", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := IsValidURL(tt.url)
			if got != tt.want {
				t.Errorf("IsValidURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}
