// Package servicenow provides tools for interacting with ServiceNow.
package servicenow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client is a ServiceNow REST API client.
type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
}

// Config holds ServiceNow client configuration.
type Config struct {
	// InstanceURL is the ServiceNow instance URL (e.g., https://dev12345.service-now.com)
	InstanceURL string
	// Username for basic auth
	Username string
	// Password for basic auth
	Password string
	// Timeout for API requests
	Timeout time.Duration
}

// NewClient creates a new ServiceNow API client.
func NewClient(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &Client{
		baseURL:  cfg.InstanceURL,
		username: cfg.Username,
		password: cfg.Password,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Incident represents a ServiceNow incident record.
type Incident struct {
	SysID            string `json:"sys_id"`
	Number           string `json:"number"`
	ShortDescription string `json:"short_description"`
	Description      string `json:"description"`
	State            string `json:"state"`
	Priority         string `json:"priority"`
	Impact           string `json:"impact"`
	Urgency          string `json:"urgency"`
	AssignedTo       string `json:"assigned_to"`
	AssignmentGroup  string `json:"assignment_group"`
	CallerID         string `json:"caller_id"`
	Category         string `json:"category"`
	Subcategory      string `json:"subcategory"`
	OpenedAt         string `json:"opened_at"`
	ResolvedAt       string `json:"resolved_at"`
	ClosedAt         string `json:"closed_at"`
	CloseCode        string `json:"close_code"`
	CloseNotes       string `json:"close_notes"`
	WorkNotes        string `json:"work_notes"`
	Comments         string `json:"comments"`
}

// IncidentState maps state numbers to human-readable names.
var IncidentState = map[string]string{
	"1": "New",
	"2": "In Progress",
	"3": "On Hold",
	"6": "Resolved",
	"7": "Closed",
	"8": "Cancelled",
}

// IncidentPriority maps priority numbers to names.
var IncidentPriority = map[string]string{
	"1": "Critical",
	"2": "High",
	"3": "Moderate",
	"4": "Low",
	"5": "Planning",
}

// ListIncidentsOptions specifies filters for listing incidents.
type ListIncidentsOptions struct {
	AssignedTo      string
	AssignmentGroup string
	State           string
	Priority        string
	Limit           int
	Query           string // Additional sysparm_query
}

// ListIncidents retrieves incidents matching the given options.
func (c *Client) ListIncidents(ctx context.Context, opts ListIncidentsOptions) ([]Incident, error) {
	endpoint := "/api/now/table/incident"

	// Build query parameters
	params := url.Values{}
	params.Set("sysparm_display_value", "true")

	if opts.Limit > 0 {
		params.Set("sysparm_limit", fmt.Sprintf("%d", opts.Limit))
	} else {
		params.Set("sysparm_limit", "20")
	}

	// Build query string
	var queryParts []string
	if opts.AssignedTo != "" {
		queryParts = append(queryParts, fmt.Sprintf("assigned_to=%s", opts.AssignedTo))
	}
	if opts.AssignmentGroup != "" {
		queryParts = append(queryParts, fmt.Sprintf("assignment_group=%s", opts.AssignmentGroup))
	}
	if opts.State != "" {
		queryParts = append(queryParts, fmt.Sprintf("state=%s", opts.State))
	}
	if opts.Priority != "" {
		queryParts = append(queryParts, fmt.Sprintf("priority=%s", opts.Priority))
	}
	if opts.Query != "" {
		queryParts = append(queryParts, opts.Query)
	}

	if len(queryParts) > 0 {
		query := ""
		for i, part := range queryParts {
			if i > 0 {
				query += "^"
			}
			query += part
		}
		params.Set("sysparm_query", query+"^ORDERBYDESCopened_at")
	} else {
		params.Set("sysparm_query", "ORDERBYDESCopened_at")
	}

	fullURL := c.baseURL + endpoint + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	c.setAuth(req)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			body = []byte("(failed to read response body)")
		}
		return nil, fmt.Errorf("ServiceNow API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Result []Incident `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return result.Result, nil
}

// GetIncident retrieves a single incident by sys_id or number.
func (c *Client) GetIncident(ctx context.Context, idOrNumber string) (*Incident, error) {
	endpoint := "/api/now/table/incident"

	params := url.Values{}
	params.Set("sysparm_display_value", "true")
	params.Set("sysparm_limit", "1")

	// Determine if it's a sys_id or number
	if len(idOrNumber) == 32 {
		params.Set("sysparm_query", fmt.Sprintf("sys_id=%s", idOrNumber))
	} else {
		params.Set("sysparm_query", fmt.Sprintf("number=%s", idOrNumber))
	}

	fullURL := c.baseURL + endpoint + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "GET", fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	c.setAuth(req)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			body = []byte("(failed to read response body)")
		}
		return nil, fmt.Errorf("ServiceNow API error %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Result []Incident `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if len(result.Result) == 0 {
		return nil, fmt.Errorf("incident not found: %s", idOrNumber)
	}

	return &result.Result[0], nil
}

// UpdateIncident updates an incident.
func (c *Client) UpdateIncident(ctx context.Context, sysID string, updates map[string]string) (*Incident, error) {
	endpoint := fmt.Sprintf("/api/now/table/incident/%s", sysID)

	body, err := json.Marshal(updates)
	if err != nil {
		return nil, fmt.Errorf("marshal updates: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PATCH", c.baseURL+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	c.setAuth(req)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ServiceNow API error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Result Incident `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result.Result, nil
}

// AddWorkNote adds a work note to an incident.
func (c *Client) AddWorkNote(ctx context.Context, sysID, note string) error {
	updates := map[string]string{
		"work_notes": note,
	}
	_, err := c.UpdateIncident(ctx, sysID, updates)
	return err
}

// AddComment adds a customer-visible comment to an incident.
func (c *Client) AddComment(ctx context.Context, sysID, comment string) error {
	updates := map[string]string{
		"comments": comment,
	}
	_, err := c.UpdateIncident(ctx, sysID, updates)
	return err
}

// ResolveIncident resolves an incident with the given resolution.
func (c *Client) ResolveIncident(ctx context.Context, sysID, resolution, closeCode string) (*Incident, error) {
	updates := map[string]string{
		"state":       "6", // Resolved
		"close_notes": resolution,
	}
	if closeCode != "" {
		updates["close_code"] = closeCode
	}
	return c.UpdateIncident(ctx, sysID, updates)
}

// setAuth sets the authentication header on a request.
func (c *Client) setAuth(req *http.Request) {
	req.SetBasicAuth(c.username, c.password)
}

// FormatIncident returns a human-readable string for an incident.
func FormatIncident(inc *Incident) string {
	state := inc.State
	if name, ok := IncidentState[inc.State]; ok {
		state = name
	}

	priority := inc.Priority
	if name, ok := IncidentPriority[inc.Priority]; ok {
		priority = name
	}

	return fmt.Sprintf("%s: %s\nPriority: %s | State: %s\nAssigned to: %s\nOpened: %s",
		inc.Number,
		inc.ShortDescription,
		priority,
		state,
		inc.AssignedTo,
		inc.OpenedAt,
	)
}
