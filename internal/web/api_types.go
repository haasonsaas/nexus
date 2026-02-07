package web

import "github.com/haasonsaas/nexus/pkg/models"

// apiProvidersResponse is the typed response for GET /api/providers.
type apiProvidersResponse struct {
	Providers []*ProviderStatus `json:"providers"`
}

// apiCronResponse is the typed response for GET /api/cron.
type apiCronResponse struct {
	Enabled bool              `json:"enabled"`
	Jobs    []*CronJobSummary `json:"jobs"`
}

// apiSkillsResponse is the typed response for GET /api/skills.
type apiSkillsResponse struct {
	Skills []*SkillSummary `json:"skills"`
}

// apiToolsResponse is the typed response for GET /api/tools.
type apiToolsResponse struct {
	Tools []models.ToolSummary `json:"tools"`
}

// apiNodesResponse is the typed response for GET /api/nodes.
type apiNodesResponse struct {
	Nodes []*NodeSummary `json:"nodes"`
}

// apiNodeToolsResponse is the typed response for GET /api/nodes/:id/tools.
type apiNodeToolsResponse struct {
	Tools []*NodeToolSummary `json:"tools"`
}

// apiToolExecResponse is the typed response for POST /api/nodes/:id/tools/:name.
type apiToolExecResponse struct {
	Content      string `json:"content"`
	IsError      bool   `json:"is_error"`
	DurationMs   int64  `json:"duration_ms"`
	ErrorDetails string `json:"error_details,omitempty"`
	Artifacts    any    `json:"artifacts,omitempty"`
}

// apiConfigResponse is the typed response for GET/PATCH /api/config.
type apiConfigResponse struct {
	Path   string `json:"path"`
	Config string `json:"config"`
	Apply  any    `json:"apply,omitempty"`
}
