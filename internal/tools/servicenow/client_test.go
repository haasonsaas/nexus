package servicenow

import (
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	t.Run("with custom timeout", func(t *testing.T) {
		cfg := Config{
			InstanceURL: "https://dev12345.service-now.com",
			Username:    "admin",
			Password:    "password",
			Timeout:     60 * time.Second,
		}

		client := NewClient(cfg)
		if client == nil {
			t.Fatal("client is nil")
		}
		if client.baseURL != cfg.InstanceURL {
			t.Errorf("baseURL = %q, want %q", client.baseURL, cfg.InstanceURL)
		}
		if client.username != cfg.Username {
			t.Errorf("username = %q, want %q", client.username, cfg.Username)
		}
		if client.httpClient.Timeout != 60*time.Second {
			t.Errorf("timeout = %v, want %v", client.httpClient.Timeout, 60*time.Second)
		}
	})

	t.Run("with default timeout", func(t *testing.T) {
		cfg := Config{
			InstanceURL: "https://dev12345.service-now.com",
			Username:    "admin",
			Password:    "password",
		}

		client := NewClient(cfg)
		if client.httpClient.Timeout != 30*time.Second {
			t.Errorf("default timeout = %v, want %v", client.httpClient.Timeout, 30*time.Second)
		}
	})
}

func TestIncident_Struct(t *testing.T) {
	inc := Incident{
		SysID:            "abc123",
		Number:           "INC0012345",
		ShortDescription: "Test incident",
		Description:      "A test incident for testing",
		State:            "1",
		Priority:         "2",
		Impact:           "2",
		Urgency:          "2",
		AssignedTo:       "John Doe",
		AssignmentGroup:  "IT Support",
		CallerID:         "Jane Smith",
		Category:         "Hardware",
		Subcategory:      "Laptop",
		OpenedAt:         "2024-01-15 10:30:00",
	}

	if inc.Number != "INC0012345" {
		t.Errorf("Number = %q, want %q", inc.Number, "INC0012345")
	}
	if inc.State != "1" {
		t.Errorf("State = %q, want %q", inc.State, "1")
	}
	if inc.Priority != "2" {
		t.Errorf("Priority = %q, want %q", inc.Priority, "2")
	}
}

func TestIncidentState(t *testing.T) {
	tests := []struct {
		code string
		name string
	}{
		{"1", "New"},
		{"2", "In Progress"},
		{"3", "On Hold"},
		{"6", "Resolved"},
		{"7", "Closed"},
		{"8", "Cancelled"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			if name, ok := IncidentState[tt.code]; !ok {
				t.Errorf("IncidentState[%q] not found", tt.code)
			} else if name != tt.name {
				t.Errorf("IncidentState[%q] = %q, want %q", tt.code, name, tt.name)
			}
		})
	}
}

func TestIncidentPriority(t *testing.T) {
	tests := []struct {
		code string
		name string
	}{
		{"1", "Critical"},
		{"2", "High"},
		{"3", "Moderate"},
		{"4", "Low"},
		{"5", "Planning"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			if name, ok := IncidentPriority[tt.code]; !ok {
				t.Errorf("IncidentPriority[%q] not found", tt.code)
			} else if name != tt.name {
				t.Errorf("IncidentPriority[%q] = %q, want %q", tt.code, name, tt.name)
			}
		})
	}
}

func TestListIncidentsOptions_Struct(t *testing.T) {
	opts := ListIncidentsOptions{
		AssignedTo:      "user123",
		AssignmentGroup: "IT Support",
		State:           "1",
		Priority:        "2",
		Limit:           25,
		Query:           "category=Hardware",
	}

	if opts.AssignedTo != "user123" {
		t.Errorf("AssignedTo = %q, want %q", opts.AssignedTo, "user123")
	}
	if opts.Limit != 25 {
		t.Errorf("Limit = %d, want 25", opts.Limit)
	}
}

func TestFormatIncident(t *testing.T) {
	tests := []struct {
		name     string
		incident Incident
		contains []string
	}{
		{
			name: "with known state and priority",
			incident: Incident{
				Number:           "INC0012345",
				ShortDescription: "Server down",
				State:            "1",
				Priority:         "2",
				AssignedTo:       "John Doe",
				OpenedAt:         "2024-01-15 10:30:00",
			},
			contains: []string{
				"INC0012345",
				"Server down",
				"New",  // State 1 = New
				"High", // Priority 2 = High
				"John Doe",
				"2024-01-15 10:30:00",
			},
		},
		{
			name: "with unknown state and priority",
			incident: Incident{
				Number:           "INC0099999",
				ShortDescription: "Unknown issue",
				State:            "99", // Unknown state
				Priority:         "99", // Unknown priority
				AssignedTo:       "",
				OpenedAt:         "2024-02-01 12:00:00",
			},
			contains: []string{
				"INC0099999",
				"Unknown issue",
				"99", // Should show raw value
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatIncident(&tt.incident)
			for _, s := range tt.contains {
				if !containsString(result, s) {
					t.Errorf("FormatIncident() missing %q in:\n%s", s, result)
				}
			}
		})
	}
}

func containsString(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && findString(haystack, needle) >= 0
}

func findString(haystack, needle string) int {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestConfig_Struct(t *testing.T) {
	cfg := Config{
		InstanceURL: "https://instance.service-now.com",
		Username:    "admin",
		Password:    "secret",
		Timeout:     45 * time.Second,
	}

	if cfg.InstanceURL != "https://instance.service-now.com" {
		t.Errorf("InstanceURL = %q", cfg.InstanceURL)
	}
	if cfg.Timeout != 45*time.Second {
		t.Errorf("Timeout = %v", cfg.Timeout)
	}
}
