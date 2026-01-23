package web

import (
	"net/http/httptest"
	"testing"
)

func TestParseIntParam(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		paramName  string
		defaultVal int
		expected   int
	}{
		{
			name:       "valid int",
			query:      "page=5",
			paramName:  "page",
			defaultVal: 1,
			expected:   5,
		},
		{
			name:       "missing param",
			query:      "",
			paramName:  "page",
			defaultVal: 1,
			expected:   1,
		},
		{
			name:       "invalid int returns zero (parseIntSafe quirk)",
			query:      "page=abc",
			paramName:  "page",
			defaultVal: 10,
			expected:   0,
		},
		{
			name:       "zero",
			query:      "page=0",
			paramName:  "page",
			defaultVal: 1,
			expected:   0,
		},
		{
			name:       "large number",
			query:      "page=12345",
			paramName:  "page",
			defaultVal: 1,
			expected:   12345,
		},
		{
			name:       "negative number returns zero (parseIntSafe quirk)",
			query:      "page=-5",
			paramName:  "page",
			defaultVal: 42,
			expected:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/?"+tt.query, nil)
			result := parseIntParam(req, tt.paramName, tt.defaultVal)
			if result != tt.expected {
				t.Errorf("parseIntParam() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestNewHandler_NilConfig(t *testing.T) {
	h, err := NewHandler(nil)
	if err != nil {
		t.Fatalf("NewHandler error: %v", err)
	}
	if h == nil {
		t.Error("expected non-nil handler")
	}
}

func TestNewHandler_DefaultsApplied(t *testing.T) {
	cfg := &Config{}

	h, err := NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler error: %v", err)
	}

	if h.config.BasePath != "/ui" {
		t.Errorf("BasePath = %q, want %q", h.config.BasePath, "/ui")
	}
	if h.config.DefaultAgentID != "main" {
		t.Errorf("DefaultAgentID = %q, want %q", h.config.DefaultAgentID, "main")
	}
	if h.config.Logger == nil {
		t.Error("Logger should not be nil")
	}
}

func TestHandler_ServeHTTP_StripBasePath(t *testing.T) {
	cfg := &Config{
		BasePath: "/ui",
	}
	h, err := NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler error: %v", err)
	}

	// Request to /ui should redirect to /ui/sessions
	req := httptest.NewRequest("GET", "/ui/", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != 302 {
		t.Errorf("status = %d, want 302", rec.Code)
	}
}

func TestHandler_Mount(t *testing.T) {
	cfg := &Config{}
	h, err := NewHandler(cfg)
	if err != nil {
		t.Fatalf("NewHandler error: %v", err)
	}

	mounted := h.Mount()
	if mounted == nil {
		t.Error("Mount() returned nil")
	}
}

func TestSkillsData_Struct(t *testing.T) {
	data := SkillsData{
		PageData: PageData{
			Title: "Skills",
		},
		Skills: []*SkillSummary{},
	}

	if data.Title != "Skills" {
		t.Errorf("Title = %q", data.Title)
	}
}

func TestWebChatData_Struct(t *testing.T) {
	data := WebChatData{
		PageData: PageData{
			Title: "WebChat",
		},
	}

	if data.Title != "WebChat" {
		t.Errorf("Title = %q", data.Title)
	}
}

func TestProviderData_Struct(t *testing.T) {
	data := ProviderData{
		PageData: PageData{
			Title: "Providers",
		},
		Providers: []*ProviderStatus{},
	}

	if data.Title != "Providers" {
		t.Errorf("Title = %q", data.Title)
	}
}

