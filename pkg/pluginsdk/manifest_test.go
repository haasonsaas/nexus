package pluginsdk

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDecodeManifest(t *testing.T) {
	tests := []struct {
		name    string
		data    string
		wantErr bool
		check   func(*testing.T, *Manifest)
	}{
		{
			name: "valid manifest",
			data: `{"id": "test-plugin", "configSchema": {"type": "object"}}`,
			check: func(t *testing.T, m *Manifest) {
				if m.ID != "test-plugin" {
					t.Errorf("ID = %q, want %q", m.ID, "test-plugin")
				}
			},
		},
		{
			name: "manifest with all fields",
			data: `{
				"id": "test-plugin",
				"kind": "channel",
				"name": "Test Plugin",
				"description": "A test plugin",
				"version": "1.0.0",
				"tools": ["search", "fetch"],
				"channels": ["slack", "telegram"],
				"providers": ["openai"],
				"commands": ["plugins.install", "plugins.list"],
				"services": ["cron-worker"],
				"hooks": ["session.created", "message.received"],
				"configSchema": {"type": "object"},
				"metadata": {"key": "value"}
			}`,
			check: func(t *testing.T, m *Manifest) {
				if m.Kind != "channel" {
					t.Errorf("Kind = %q, want %q", m.Kind, "channel")
				}
				if m.Name != "Test Plugin" {
					t.Errorf("Name = %q, want %q", m.Name, "Test Plugin")
				}
				if m.Version != "1.0.0" {
					t.Errorf("Version = %q, want %q", m.Version, "1.0.0")
				}
				if len(m.Tools) != 2 {
					t.Errorf("len(Tools) = %d, want 2", len(m.Tools))
				}
				if len(m.Channels) != 2 {
					t.Errorf("len(Channels) = %d, want 2", len(m.Channels))
				}
				if len(m.Commands) != 2 {
					t.Errorf("len(Commands) = %d, want 2", len(m.Commands))
				}
				if len(m.Services) != 1 {
					t.Errorf("len(Services) = %d, want 1", len(m.Services))
				}
				if len(m.Hooks) != 2 {
					t.Errorf("len(Hooks) = %d, want 2", len(m.Hooks))
				}
			},
		},
		{
			name:    "invalid JSON",
			data:    `{invalid json}`,
			wantErr: true,
		},
		{
			name:    "empty JSON",
			data:    `{}`,
			wantErr: false,
			check: func(t *testing.T, m *Manifest) {
				if m.ID != "" {
					t.Errorf("ID = %q, want empty", m.ID)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := DecodeManifest([]byte(tt.data))
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeManifest() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.check != nil && err == nil {
				tt.check(t, m)
			}
		})
	}
}

func TestDecodeManifestFile(t *testing.T) {
	t.Run("valid file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "manifest.json")
		data := `{"id": "file-plugin", "configSchema": {"type": "object"}}`
		if err := os.WriteFile(path, []byte(data), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		m, err := DecodeManifestFile(path)
		if err != nil {
			t.Fatalf("DecodeManifestFile() error = %v", err)
		}
		if m.ID != "file-plugin" {
			t.Errorf("ID = %q, want %q", m.ID, "file-plugin")
		}
	})

	t.Run("file not found", func(t *testing.T) {
		_, err := DecodeManifestFile("/nonexistent/path/manifest.json")
		if err == nil {
			t.Error("DecodeManifestFile() expected error for nonexistent file")
		}
	})

	t.Run("invalid JSON in file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "invalid.json")
		if err := os.WriteFile(path, []byte(`{invalid}`), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}

		_, err := DecodeManifestFile(path)
		if err == nil {
			t.Error("DecodeManifestFile() expected error for invalid JSON")
		}
	})
}

func TestManifestValidate(t *testing.T) {
	tests := []struct {
		name     string
		manifest *Manifest
		wantErr  bool
	}{
		{
			name:     "nil manifest",
			manifest: nil,
			wantErr:  true,
		},
		{
			name:     "missing ID",
			manifest: &Manifest{ConfigSchema: []byte(`{}`)},
			wantErr:  true,
		},
		{
			name:     "whitespace-only ID",
			manifest: &Manifest{ID: "   ", ConfigSchema: []byte(`{}`)},
			wantErr:  true,
		},
		{
			name:     "missing configSchema",
			manifest: &Manifest{ID: "test"},
			wantErr:  true,
		},
		{
			name:     "empty configSchema",
			manifest: &Manifest{ID: "test", ConfigSchema: []byte{}},
			wantErr:  true,
		},
		{
			name:     "valid manifest",
			manifest: &Manifest{ID: "test", ConfigSchema: []byte(`{}`)},
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.manifest.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestManifestCapabilities(t *testing.T) {
	manifest := &Manifest{
		ID:           "cap-plugin",
		ConfigSchema: []byte(`{}`),
		Capabilities: &Capabilities{
			Required: []string{"tool:echo", "channel:slack"},
			Optional: []string{"cli:*", " "},
		},
	}

	declared := manifest.DeclaredCapabilities()
	expected := []string{"tool:echo", "channel:slack", "cli:*"}
	if !reflect.DeepEqual(declared, expected) {
		t.Errorf("DeclaredCapabilities() = %v, want %v", declared, expected)
	}
	if !manifest.HasCapability("tool:echo") {
		t.Error("expected tool:echo to be allowed")
	}
	if !manifest.HasCapability("cli:search") {
		t.Error("expected cli:* to allow cli:search")
	}
	if manifest.HasCapability("tool:other") {
		t.Error("expected tool:other to be denied")
	}
}

func TestCapabilityMatches(t *testing.T) {
	tests := []struct {
		allowed   string
		requested string
		want      bool
	}{
		{allowed: "tool:echo", requested: "tool:echo", want: true},
		{allowed: "tool:*", requested: "tool:search", want: true},
		{allowed: "cli:*", requested: "cli:", want: true},
		{allowed: "*", requested: "hook:message", want: true},
		{allowed: "channel:slack", requested: "channel:telegram", want: false},
		{allowed: "", requested: "tool:echo", want: false},
	}

	for _, tt := range tests {
		if got := CapabilityMatches(tt.allowed, tt.requested); got != tt.want {
			t.Errorf("CapabilityMatches(%q, %q) = %v, want %v", tt.allowed, tt.requested, got, tt.want)
		}
	}
}

func TestGetFieldHint(t *testing.T) {
	t.Run("nil manifest", func(t *testing.T) {
		var m *Manifest
		hint := m.GetFieldHint("token")
		if hint != nil {
			t.Error("expected nil for nil manifest")
		}
	})

	t.Run("nil UIHints", func(t *testing.T) {
		m := &Manifest{ID: "test"}
		hint := m.GetFieldHint("token")
		if hint != nil {
			t.Error("expected nil for nil UIHints")
		}
	})

	t.Run("nil ConfigFields", func(t *testing.T) {
		m := &Manifest{
			ID:      "test",
			UIHints: &UIHints{},
		}
		hint := m.GetFieldHint("token")
		if hint != nil {
			t.Error("expected nil for nil ConfigFields")
		}
	})

	t.Run("path not found", func(t *testing.T) {
		m := &Manifest{
			ID: "test",
			UIHints: &UIHints{
				ConfigFields: map[string]*FieldHint{
					"other": {Label: "Other"},
				},
			},
		}
		hint := m.GetFieldHint("token")
		if hint != nil {
			t.Error("expected nil for missing path")
		}
	})

	t.Run("valid path", func(t *testing.T) {
		m := &Manifest{
			ID: "test",
			UIHints: &UIHints{
				ConfigFields: map[string]*FieldHint{
					"token": {Label: "API Token", Sensitive: true},
				},
			},
		}
		hint := m.GetFieldHint("token")
		if hint == nil {
			t.Fatal("expected non-nil hint")
		}
		if hint.Label != "API Token" {
			t.Errorf("Label = %q, want %q", hint.Label, "API Token")
		}
		if !hint.Sensitive {
			t.Error("expected Sensitive = true")
		}
	})
}

func TestGetSetupSteps(t *testing.T) {
	t.Run("nil manifest", func(t *testing.T) {
		var m *Manifest
		steps := m.GetSetupSteps()
		if steps != nil {
			t.Error("expected nil for nil manifest")
		}
	})

	t.Run("nil UIHints", func(t *testing.T) {
		m := &Manifest{ID: "test"}
		steps := m.GetSetupSteps()
		if steps != nil {
			t.Error("expected nil for nil UIHints")
		}
	})

	t.Run("valid steps", func(t *testing.T) {
		m := &Manifest{
			ID: "test",
			UIHints: &UIHints{
				SetupSteps: []*SetupStep{
					{Title: "Step 1", Description: "Do something"},
					{Title: "Step 2", Description: "Do something else"},
				},
			},
		}
		steps := m.GetSetupSteps()
		if len(steps) != 2 {
			t.Fatalf("len(steps) = %d, want 2", len(steps))
		}
		if steps[0].Title != "Step 1" {
			t.Errorf("steps[0].Title = %q, want %q", steps[0].Title, "Step 1")
		}
	})
}

func TestGetRequirements(t *testing.T) {
	t.Run("nil manifest", func(t *testing.T) {
		var m *Manifest
		reqs := m.GetRequirements()
		if reqs != nil {
			t.Error("expected nil for nil manifest")
		}
	})

	t.Run("nil UIHints", func(t *testing.T) {
		m := &Manifest{ID: "test"}
		reqs := m.GetRequirements()
		if reqs != nil {
			t.Error("expected nil for nil UIHints")
		}
	})

	t.Run("valid requirements", func(t *testing.T) {
		m := &Manifest{
			ID: "test",
			UIHints: &UIHints{
				Requirements: []*Requirement{
					{Name: "API Key", Description: "Get from dashboard"},
					{Name: "Bot Token", Description: "Create a bot", Optional: true},
				},
			},
		}
		reqs := m.GetRequirements()
		if len(reqs) != 2 {
			t.Fatalf("len(reqs) = %d, want 2", len(reqs))
		}
		if reqs[0].Name != "API Key" {
			t.Errorf("reqs[0].Name = %q, want %q", reqs[0].Name, "API Key")
		}
		if reqs[1].Optional != true {
			t.Error("expected reqs[1].Optional = true")
		}
	})
}

func TestGetRequiredFields(t *testing.T) {
	t.Run("nil manifest", func(t *testing.T) {
		var m *Manifest
		fields := m.GetRequiredFields()
		if fields != nil {
			t.Error("expected nil for nil manifest")
		}
	})

	t.Run("nil UIHints", func(t *testing.T) {
		m := &Manifest{ID: "test"}
		fields := m.GetRequiredFields()
		if fields != nil {
			t.Error("expected nil for nil UIHints")
		}
	})

	t.Run("nil ConfigFields", func(t *testing.T) {
		m := &Manifest{
			ID:      "test",
			UIHints: &UIHints{},
		}
		fields := m.GetRequiredFields()
		if fields != nil {
			t.Error("expected nil for nil ConfigFields")
		}
	})

	t.Run("mixed fields", func(t *testing.T) {
		m := &Manifest{
			ID: "test",
			UIHints: &UIHints{
				ConfigFields: map[string]*FieldHint{
					"token":    {Required: true},
					"optional": {Required: false},
					"api_key":  {Required: true},
					"nil_hint": nil,
				},
			},
		}
		fields := m.GetRequiredFields()
		if len(fields) != 2 {
			t.Fatalf("len(fields) = %d, want 2", len(fields))
		}
		// Check that both required fields are present
		found := make(map[string]bool)
		for _, f := range fields {
			found[f] = true
		}
		if !found["token"] || !found["api_key"] {
			t.Errorf("expected token and api_key in required fields, got %v", fields)
		}
	})
}

func TestGetSensitiveFields(t *testing.T) {
	t.Run("nil manifest", func(t *testing.T) {
		var m *Manifest
		fields := m.GetSensitiveFields()
		if fields != nil {
			t.Error("expected nil for nil manifest")
		}
	})

	t.Run("nil UIHints", func(t *testing.T) {
		m := &Manifest{ID: "test"}
		fields := m.GetSensitiveFields()
		if fields != nil {
			t.Error("expected nil for nil UIHints")
		}
	})

	t.Run("mixed fields", func(t *testing.T) {
		m := &Manifest{
			ID: "test",
			UIHints: &UIHints{
				ConfigFields: map[string]*FieldHint{
					"token":    {Sensitive: true},
					"name":     {Sensitive: false},
					"password": {Sensitive: true},
					"nil_hint": nil,
				},
			},
		}
		fields := m.GetSensitiveFields()
		if len(fields) != 2 {
			t.Fatalf("len(fields) = %d, want 2", len(fields))
		}
		found := make(map[string]bool)
		for _, f := range fields {
			found[f] = true
		}
		if !found["token"] || !found["password"] {
			t.Errorf("expected token and password in sensitive fields, got %v", fields)
		}
	})
}

func TestManifestConstants(t *testing.T) {
	if ManifestFilename != "nexus.plugin.json" {
		t.Errorf("ManifestFilename = %q, want %q", ManifestFilename, "nexus.plugin.json")
	}
	if LegacyManifestFilename != "clawdbot.plugin.json" {
		t.Errorf("LegacyManifestFilename = %q, want %q", LegacyManifestFilename, "clawdbot.plugin.json")
	}
}

func TestFieldHintStruct(t *testing.T) {
	minVal := 0.0
	maxVal := 100.0
	hint := FieldHint{
		Label:       "Test Field",
		Description: "A test field",
		Placeholder: "Enter value",
		HelpURL:     "https://docs.example.com",
		InputType:   "text",
		Options: []FieldOption{
			{Value: "opt1", Label: "Option 1"},
		},
		Required:  true,
		Sensitive: true,
		EnvVar:    "TEST_VAR",
		Default:   "default",
		Validation: &FieldValidation{
			Pattern:   "^[a-z]+$",
			MinLength: 1,
			MaxLength: 100,
			Min:       &minVal,
			Max:       &maxVal,
		},
	}

	if hint.Label != "Test Field" {
		t.Errorf("Label = %q", hint.Label)
	}
	if hint.InputType != "text" {
		t.Errorf("InputType = %q", hint.InputType)
	}
	if len(hint.Options) != 1 {
		t.Errorf("len(Options) = %d", len(hint.Options))
	}
	if hint.Validation.Pattern != "^[a-z]+$" {
		t.Errorf("Validation.Pattern = %q", hint.Validation.Pattern)
	}
}

func TestSetupStepStruct(t *testing.T) {
	step := SetupStep{
		Title:        "Create API Key",
		Description:  "Go to dashboard and create an API key",
		Commands:     []string{"nexus config set api_key"},
		ConfigFields: []string{"api_key"},
		URL:          "https://dashboard.example.com",
	}

	if step.Title != "Create API Key" {
		t.Errorf("Title = %q", step.Title)
	}
	if len(step.Commands) != 1 {
		t.Errorf("len(Commands) = %d", len(step.Commands))
	}
	if len(step.ConfigFields) != 1 {
		t.Errorf("len(ConfigFields) = %d", len(step.ConfigFields))
	}
}

func TestRequirementStruct(t *testing.T) {
	req := Requirement{
		Name:        "Bot Token",
		Description: "Create a bot with @BotFather",
		URL:         "https://t.me/BotFather",
		Optional:    false,
	}

	if req.Name != "Bot Token" {
		t.Errorf("Name = %q", req.Name)
	}
	if req.Optional {
		t.Error("expected Optional = false")
	}
}

func TestUIHintsStruct(t *testing.T) {
	hints := UIHints{
		ConfigFields: map[string]*FieldHint{
			"token": {Label: "Token"},
		},
		SetupSteps: []*SetupStep{
			{Title: "Step 1"},
		},
		Requirements: []*Requirement{
			{Name: "Req 1"},
		},
		Links: map[string]string{
			"docs": "https://docs.example.com",
		},
	}

	if len(hints.ConfigFields) != 1 {
		t.Errorf("len(ConfigFields) = %d", len(hints.ConfigFields))
	}
	if len(hints.SetupSteps) != 1 {
		t.Errorf("len(SetupSteps) = %d", len(hints.SetupSteps))
	}
	if len(hints.Requirements) != 1 {
		t.Errorf("len(Requirements) = %d", len(hints.Requirements))
	}
	if hints.Links["docs"] != "https://docs.example.com" {
		t.Errorf("Links[docs] = %q", hints.Links["docs"])
	}
}
