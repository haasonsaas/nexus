package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/haasonsaas/nexus/internal/artifacts"
	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/cron"
	"github.com/haasonsaas/nexus/internal/doctor"
	"github.com/haasonsaas/nexus/internal/edge"
	"github.com/haasonsaas/nexus/internal/tools/naming"
	"github.com/haasonsaas/nexus/pkg/models"
)

// CronJobSummary is a safe representation of a cron job for UI/API.
type CronJobSummary struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Enabled   bool      `json:"enabled"`
	Schedule  string    `json:"schedule"`
	NextRun   time.Time `json:"next_run"`
	LastRun   time.Time `json:"last_run"`
	LastError string    `json:"last_error,omitempty"`
}

type cronExecutionsResponse struct {
	Executions []*cron.JobExecution `json:"executions"`
}

// SkillSummary is a UI-friendly skill snapshot.
type SkillSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Path        string `json:"path"`
	Emoji       string `json:"emoji,omitempty"`
	Execution   string `json:"execution,omitempty"`
	Eligible    bool   `json:"eligible"`
	Reason      string `json:"reason,omitempty"`
}

// NodeSummary is a UI-friendly edge node snapshot.
type NodeSummary struct {
	EdgeID        string            `json:"edge_id"`
	Name          string            `json:"name"`
	Status        string            `json:"status"`
	ConnectedAt   time.Time         `json:"connected_at"`
	LastHeartbeat time.Time         `json:"last_heartbeat"`
	Tools         []string          `json:"tools"`
	ChannelTypes  []string          `json:"channel_types,omitempty"`
	Version       string            `json:"version,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// NodeToolSummary is a UI-friendly tool snapshot for a node.
type NodeToolSummary struct {
	EdgeID            string `json:"edge_id"`
	Name              string `json:"name"`
	Description       string `json:"description,omitempty"`
	InputSchema       string `json:"input_schema,omitempty"`
	RequiresApproval  bool   `json:"requires_approval,omitempty"`
	ProducesArtifacts bool   `json:"produces_artifacts,omitempty"`
	TimeoutSeconds    int    `json:"timeout_seconds,omitempty"`
}

// APIArtifactSummary is a compact artifact representation.
type APIArtifactSummary struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	MimeType   string `json:"mime_type"`
	Filename   string `json:"filename"`
	Size       int64  `json:"size"`
	Reference  string `json:"reference"`
	TTLSeconds int32  `json:"ttl_seconds"`
	Redacted   bool   `json:"redacted"`
}

type APIArtifactListResponse struct {
	Artifacts []*APIArtifactSummary `json:"artifacts"`
	Total     int                   `json:"total"`
}

// apiCron handles GET /api/cron.
func (h *Handler) apiCron(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jobs := h.listCronJobs()
	h.jsonResponse(w, apiCronResponse{
		Enabled: h.config != nil && h.config.GatewayConfig != nil && h.config.GatewayConfig.Cron.Enabled,
		Jobs:    jobs,
	})
}

// apiCronExecutions handles GET /api/cron/executions.
func (h *Handler) apiCronExecutions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.config == nil || h.config.CronScheduler == nil {
		h.jsonResponse(w, cronExecutionsResponse{})
		return
	}
	jobID := strings.TrimSpace(clampQueryParam(r, "job_id"))
	limit := parseIntParam(r, "limit", 50)
	if limit < 1 || limit > 200 {
		limit = 50
	}
	offset := parseIntParam(r, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	executions, err := h.config.CronScheduler.Executions(ctx, jobID, limit, offset)
	if err != nil {
		h.jsonError(w, "Failed to fetch cron executions", http.StatusInternalServerError)
		return
	}
	h.jsonResponse(w, cronExecutionsResponse{Executions: executions})
}

// apiSkills handles GET /api/skills.
func (h *Handler) apiSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.jsonResponse(w, apiSkillsResponse{Skills: h.listSkills(r.Context())})
}

// apiTools handles GET /api/tools.
func (h *Handler) apiTools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tools := h.listTools(r.Context())
	if r.Header.Get("HX-Request") == "true" {
		h.renderPartial(w, "tools/list.html", tools)
		return
	}
	h.jsonResponse(w, apiToolsResponse{Tools: tools})
}

// apiSkillsRefresh triggers skill discovery.
func (h *Handler) apiSkillsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.config.SkillsManager == nil {
		h.jsonError(w, "Skills not configured (skills manager unavailable)", http.StatusServiceUnavailable)
		return
	}
	go func() {
		discoverCtx, discoverCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer discoverCancel()
		if err := h.config.SkillsManager.Discover(discoverCtx); err != nil {
			h.config.Logger.Error("skills discovery failed", "error", err)
		}
	}()
	h.jsonResponse(w, map[string]string{"status": "refreshing"})
}

// apiNodes handles GET /api/nodes.
func (h *Handler) apiNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.jsonResponse(w, apiNodesResponse{Nodes: h.listNodes()})
}

// apiNode handles node-specific API actions.
func (h *Handler) apiNode(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/nodes/")
	parts := strings.Split(path, "/")
	if len(parts) < 1 || parts[0] == "" {
		h.jsonError(w, "Node ID required", http.StatusBadRequest)
		return
	}
	nodeID := parts[0]
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		for _, node := range h.listNodes() {
			if node.EdgeID == nodeID {
				h.jsonResponse(w, node)
				return
			}
		}
		h.jsonError(w, "Node not found", http.StatusNotFound)
		return
	}

	if parts[1] == "tools" {
		h.apiNodeTools(w, r, nodeID, parts[2:])
		return
	}

	h.jsonError(w, "Not found", http.StatusNotFound)
}

// apiConfig handles GET/PATCH /api/config.
func (h *Handler) apiConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		configYAML, configPath := h.configSnapshot()
		if strings.EqualFold(r.URL.Query().Get("format"), "yaml") {
			w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(configYAML)) //nolint:errcheck
			return
		}
		h.jsonResponse(w, map[string]string{
			"path":   configPath,
			"config": configYAML,
		})
	case http.MethodPatch, http.MethodPost:
		h.apiConfigPatch(w, r)
	default:
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// apiConfigSchema handles GET /api/config/schema.
func (h *Handler) apiConfigSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var schema []byte
	var err error
	if h != nil && h.config != nil && h.config.ConfigManager != nil {
		schema, err = h.config.ConfigManager.ConfigSchema(r.Context())
	} else {
		schema, err = config.JSONSchema()
	}
	if err != nil {
		h.jsonError(w, "Failed to build config schema", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(schema) //nolint:errcheck
}

// apiArtifacts handles GET /api/artifacts.
func (h *Handler) apiArtifacts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.config.ArtifactRepo == nil {
		h.jsonError(w, "Artifacts not configured (set artifacts.backend)", http.StatusServiceUnavailable)
		return
	}

	filter := artifacts.Filter{
		SessionID: clampQueryParam(r, "session_id"),
		EdgeID:    clampQueryParam(r, "edge_id"),
		Type:      clampQueryParam(r, "type"),
		Limit:     parseIntParam(r, "limit", 50),
	}

	results, err := h.config.ArtifactRepo.ListArtifacts(r.Context(), filter)
	if err != nil {
		h.jsonError(w, "Failed to list artifacts", http.StatusInternalServerError)
		return
	}

	items := make([]*APIArtifactSummary, 0, len(results))
	for _, art := range results {
		if art == nil {
			continue
		}
		items = append(items, &APIArtifactSummary{
			ID:         art.Id,
			Type:       art.Type,
			MimeType:   art.MimeType,
			Filename:   art.Filename,
			Size:       art.Size,
			Reference:  art.Reference,
			TTLSeconds: art.TtlSeconds,
			Redacted:   strings.HasPrefix(art.Reference, "redacted://"),
		})
	}

	h.jsonResponse(w, APIArtifactListResponse{
		Artifacts: items,
		Total:     len(items),
	})
}

// apiArtifact handles GET /api/artifacts/{id}.
func (h *Handler) apiArtifact(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.config.ArtifactRepo == nil {
		h.jsonError(w, "Artifacts not configured (set artifacts.backend)", http.StatusServiceUnavailable)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/artifacts/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		h.jsonError(w, "Artifact ID required", http.StatusBadRequest)
		return
	}
	artifactID := parts[0]

	artifact, reader, err := h.config.ArtifactRepo.GetArtifact(r.Context(), artifactID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "expired") {
			h.jsonError(w, "Artifact not found", http.StatusNotFound)
		} else {
			h.config.Logger.Error("failed to get artifact", "id", artifactID, "error", err)
			h.jsonError(w, "Failed to retrieve artifact", http.StatusInternalServerError)
		}
		return
	}
	defer reader.Close()

	raw := strings.EqualFold(r.URL.Query().Get("raw"), "1") || strings.EqualFold(r.URL.Query().Get("raw"), "true")
	download := strings.EqualFold(r.URL.Query().Get("download"), "1") || strings.EqualFold(r.URL.Query().Get("download"), "true")

	if raw {
		if strings.HasPrefix(artifact.Reference, "redacted://") {
			http.Error(w, "Artifact redacted", http.StatusGone)
			return
		}
		contentType := artifact.MimeType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		w.Header().Set("Content-Type", contentType)
		if download && artifact.Filename != "" {
			safeName := sanitizeAttachmentFilename(artifact.Filename)
			if safeName != "" {
				w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
					"filename": safeName,
				}))
			}
		}
		if _, err := io.Copy(w, reader); err != nil {
			h.config.Logger.Error("artifact download failed", "error", err)
		}
		return
	}

	h.jsonResponse(w, APIArtifactSummary{
		ID:         artifact.Id,
		Type:       artifact.Type,
		MimeType:   artifact.MimeType,
		Filename:   artifact.Filename,
		Size:       artifact.Size,
		Reference:  artifact.Reference,
		TTLSeconds: artifact.TtlSeconds,
		Redacted:   strings.HasPrefix(artifact.Reference, "redacted://"),
	})
}

func (h *Handler) apiNodeTools(w http.ResponseWriter, r *http.Request, nodeID string, rest []string) {
	if h.config.EdgeManager == nil {
		h.jsonError(w, "Edge manager not configured (set edge.enabled)", http.StatusServiceUnavailable)
		return
	}

	if len(rest) == 0 || rest[0] == "" {
		if r.Method != http.MethodGet {
			h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		tools := h.config.EdgeManager.GetTools()
		summaries := make([]*NodeToolSummary, 0, len(tools))
		for _, tool := range tools {
			if tool == nil || tool.EdgeID != nodeID {
				continue
			}
			summaries = append(summaries, &NodeToolSummary{
				EdgeID:            tool.EdgeID,
				Name:              tool.Name,
				Description:       tool.Description,
				InputSchema:       tool.InputSchema,
				RequiresApproval:  tool.RequiresApproval,
				ProducesArtifacts: tool.ProducesArtifacts,
				TimeoutSeconds:    tool.TimeoutSeconds,
			})
		}
		h.jsonResponse(w, apiNodeToolsResponse{Tools: summaries})
		return
	}

	toolName := rest[0]
	if r.Method != http.MethodPost {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var input string
	opts := edgeExecuteOptions{}

	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var payload struct {
			Input          string            `json:"input"`
			TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
			Approved       bool              `json:"approved,omitempty"`
			SessionID      string            `json:"session_id,omitempty"`
			RunID          string            `json:"run_id,omitempty"`
			Metadata       map[string]string `json:"metadata,omitempty"`
		}
		status, err := decodeJSONRequest(w, r, &payload)
		if err != nil {
			msg := "Invalid JSON body"
			if status == http.StatusRequestEntityTooLarge {
				msg = "Request entity too large"
			}
			h.jsonError(w, msg, status)
			return
		}
		input = payload.Input
		opts.timeoutSeconds = payload.TimeoutSeconds
		opts.approved = payload.Approved
		opts.sessionID = payload.SessionID
		opts.runID = payload.RunID
		opts.metadata = payload.Metadata
	} else {
		if err := r.ParseForm(); err != nil {
			h.jsonError(w, "Invalid form data", http.StatusBadRequest)
			return
		}
		input = r.FormValue("input")
		opts.timeoutSeconds = parseIntParam(r, "timeout_seconds", 0)
		opts.approved = strings.EqualFold(r.FormValue("approved"), "true")
		opts.sessionID = r.FormValue("session_id")
		opts.runID = r.FormValue("run_id")
	}

	result, err := h.config.EdgeManager.ExecuteTool(r.Context(), nodeID, toolName, input, opts.toExecuteOptions())
	if err != nil {
		h.jsonError(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.jsonResponse(w, apiToolExecResponse{
		Content:      result.Content,
		IsError:      result.IsError,
		DurationMs:   result.DurationMs,
		ErrorDetails: result.ErrorDetails,
		Artifacts:    result.Artifacts,
	})
}

func (h *Handler) apiConfigPatch(w http.ResponseWriter, r *http.Request) {
	if h.config == nil || strings.TrimSpace(h.config.ConfigPath) == "" {
		h.jsonError(w, "Config path not available", http.StatusServiceUnavailable)
		return
	}
	applyRequested := strings.EqualFold(r.URL.Query().Get("apply"), "true") || strings.EqualFold(r.URL.Query().Get("apply"), "1")
	baseHash := strings.TrimSpace(r.URL.Query().Get("base_hash"))
	rawContent := ""

	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var payload map[string]any
		status, err := decodeJSONRequest(w, r, &payload)
		if err != nil {
			msg := "Invalid JSON body"
			if status == http.StatusRequestEntityTooLarge {
				msg = "Request entity too large"
			}
			h.jsonError(w, msg, status)
			return
		}
		if apply, ok := payload["apply"].(bool); ok && apply {
			applyRequested = true
		}
		if hash, ok := payload["base_hash"].(string); ok && strings.TrimSpace(hash) != "" {
			baseHash = strings.TrimSpace(hash)
		}
		if rawPayload, ok := payload["raw"].(string); ok && strings.TrimSpace(rawPayload) != "" {
			rawContent = rawPayload
		}

		if rawContent == "" {
			raw, err := doctor.LoadRawConfig(h.config.ConfigPath)
			if err != nil {
				h.jsonError(w, "Failed to read config", http.StatusInternalServerError)
				return
			}
			if path, ok := payload["path"].(string); ok && strings.TrimSpace(path) != "" {
				setPathValue(raw, path, payload["value"])
			} else {
				delete(payload, "path")
				delete(payload, "value")
				delete(payload, "apply")
				delete(payload, "base_hash")
				delete(payload, "raw")
				mergeMaps(raw, payload)
			}
			if err := doctor.WriteRawConfig(h.config.ConfigPath, raw); err != nil {
				h.jsonError(w, "Failed to write config", http.StatusInternalServerError)
				return
			}
		} else if err := writeRawConfigFile(h.config.ConfigPath, rawContent); err != nil {
			h.jsonError(w, "Failed to write config", http.StatusInternalServerError)
			return
		}
	} else {
		if err := r.ParseForm(); err != nil {
			h.jsonError(w, "Invalid form data", http.StatusBadRequest)
			return
		}
		if strings.EqualFold(r.FormValue("apply"), "true") || strings.EqualFold(r.FormValue("apply"), "1") {
			applyRequested = true
		}
		if hash := strings.TrimSpace(r.FormValue("base_hash")); hash != "" {
			baseHash = hash
		}
		path := strings.TrimSpace(r.FormValue("path"))
		value := strings.TrimSpace(r.FormValue("value"))
		if path == "" {
			h.jsonError(w, "path is required", http.StatusBadRequest)
			return
		}
		raw, err := doctor.LoadRawConfig(h.config.ConfigPath)
		if err != nil {
			h.jsonError(w, "Failed to read config", http.StatusInternalServerError)
			return
		}
		var decoded any
		if value != "" {
			if err := json.Unmarshal([]byte(value), &decoded); err == nil {
				setPathValue(raw, path, decoded)
			} else {
				setPathValue(raw, path, value)
			}
		} else {
			setPathValue(raw, path, value)
		}
		if err := doctor.WriteRawConfig(h.config.ConfigPath, raw); err != nil {
			h.jsonError(w, "Failed to write config", http.StatusInternalServerError)
			return
		}
	}

	var applyResult any
	if applyRequested {
		if h.config.ConfigManager == nil {
			h.jsonError(w, "Config apply not available", http.StatusServiceUnavailable)
			return
		}
		if rawContent == "" {
			if data, err := os.ReadFile(h.config.ConfigPath); err == nil {
				rawContent = string(data)
			}
		}
		result, err := h.config.ConfigManager.ApplyConfig(r.Context(), rawContent, baseHash)
		if err != nil {
			h.jsonError(w, err.Error(), http.StatusBadRequest)
			return
		}
		applyResult = result
	}

	configYAML, configPath := h.configSnapshot()
	if r.Header.Get("HX-Request") == "true" {
		h.renderPartial(w, "config/raw.html", map[string]string{
			"ConfigYAML": configYAML,
			"ConfigPath": configPath,
		})
		return
	}
	resp := apiConfigResponse{
		Path:   configPath,
		Config: configYAML,
	}
	if applyResult != nil {
		resp.Apply = applyResult
	}
	h.jsonResponse(w, resp)
}

type edgeExecuteOptions struct {
	timeoutSeconds int
	approved       bool
	sessionID      string
	runID          string
	metadata       map[string]string
}

func (o edgeExecuteOptions) toExecuteOptions() edge.ExecuteOptions {
	opts := edge.ExecuteOptions{
		RunID:     o.runID,
		SessionID: o.sessionID,
		Approved:  o.approved,
		Metadata:  o.metadata,
	}
	if o.timeoutSeconds > 0 {
		opts.Timeout = time.Duration(o.timeoutSeconds) * time.Second
	}
	return opts
}

func (h *Handler) listTools(_ context.Context) []models.ToolSummary {
	if h == nil || h.config == nil {
		return nil
	}

	results := make([]models.ToolSummary, 0)
	if h.config.ToolSummaryProvider != nil {
		results = append(results, h.config.ToolSummaryProvider.ToolSummaries()...)
	}

	if h.config.EdgeManager != nil {
		for _, tool := range h.config.EdgeManager.GetTools() {
			if tool == nil {
				continue
			}
			identity := naming.EdgeTool(tool.EdgeID, tool.Name)
			entry := models.ToolSummary{
				Name:        identity.SafeName,
				Description: tool.Description,
				Source:      "edge",
				Namespace:   tool.EdgeID,
				Canonical:   identity.CanonicalName,
			}
			if raw := strings.TrimSpace(tool.InputSchema); raw != "" && json.Valid([]byte(raw)) {
				entry.Schema = json.RawMessage(raw)
			}
			results = append(results, entry)
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Source != results[j].Source {
			return results[i].Source < results[j].Source
		}
		if results[i].Namespace != results[j].Namespace {
			return results[i].Namespace < results[j].Namespace
		}
		return results[i].Name < results[j].Name
	})

	return results
}

func (h *Handler) listCronJobs() []*CronJobSummary {
	if h == nil || h.config == nil || h.config.CronScheduler == nil {
		return nil
	}
	jobs := h.config.CronScheduler.Jobs()
	out := make([]*CronJobSummary, 0, len(jobs))
	for _, job := range jobs {
		if job == nil {
			continue
		}
		out = append(out, &CronJobSummary{
			ID:        job.ID,
			Name:      job.Name,
			Type:      string(job.Type),
			Enabled:   job.Enabled,
			Schedule:  formatSchedule(job.Schedule),
			NextRun:   job.NextRun,
			LastRun:   job.LastRun,
			LastError: job.LastError,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (h *Handler) listSkills(ctx context.Context) []*SkillSummary {
	if h == nil || h.config == nil || h.config.SkillsManager == nil {
		return nil
	}
	entries := h.config.SkillsManager.ListAll()
	out := make([]*SkillSummary, 0, len(entries))
	for _, skill := range entries {
		if skill == nil {
			continue
		}
		eligible := false
		reason := ""
		if _, ok := h.config.SkillsManager.GetEligible(skill.Name); ok {
			eligible = true
		} else if result, err := h.config.SkillsManager.CheckEligibility(skill.Name); err == nil {
			reason = result.Reason
		}
		emoji := ""
		execution := ""
		if skill.Metadata != nil {
			emoji = skill.Metadata.Emoji
			if skill.Metadata.Execution != "" {
				execution = string(skill.Metadata.Execution)
			}
		}
		out = append(out, &SkillSummary{
			Name:        skill.Name,
			Description: skill.Description,
			Source:      string(skill.Source),
			Path:        skill.Path,
			Emoji:       emoji,
			Execution:   execution,
			Eligible:    eligible,
			Reason:      reason,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Name < out[j].Name
	})
	return out
}

func (h *Handler) listNodes() []*NodeSummary {
	if h == nil || h.config == nil || h.config.EdgeManager == nil {
		return nil
	}
	edges := h.config.EdgeManager.ListEdges()
	out := make([]*NodeSummary, 0, len(edges))
	for _, edgeStatus := range edges {
		if edgeStatus == nil {
			continue
		}
		status := "unknown"
		if edgeStatus.ConnectionStatus != 0 {
			status = edgeStatus.ConnectionStatus.String()
		}
		connectedAt := time.Time{}
		if edgeStatus.ConnectedAt != nil {
			connectedAt = edgeStatus.ConnectedAt.AsTime()
		}
		lastHeartbeat := time.Time{}
		if edgeStatus.LastHeartbeat != nil {
			lastHeartbeat = edgeStatus.LastHeartbeat.AsTime()
		}
		out = append(out, &NodeSummary{
			EdgeID:        edgeStatus.EdgeId,
			Name:          edgeStatus.Name,
			Status:        status,
			ConnectedAt:   connectedAt,
			LastHeartbeat: lastHeartbeat,
			Tools:         edgeStatus.Tools,
			ChannelTypes:  edgeStatus.ChannelTypes,
			Version:       edgeStatus.Version,
			Metadata:      edgeStatus.Metadata,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].EdgeID < out[j].EdgeID
	})
	return out
}

func (h *Handler) configSnapshot() (string, string) {
	configPath := ""
	if h != nil && h.config != nil {
		configPath = h.config.ConfigPath
	}

	var raw map[string]any
	if configPath != "" {
		if loaded, err := doctor.LoadRawConfig(configPath); err == nil {
			raw = loaded
		}
	}
	if raw == nil && h != nil && h.config != nil && h.config.GatewayConfig != nil {
		raw = configToMap(h.config.GatewayConfig)
	}
	if raw == nil {
		return "", configPath
	}

	redacted := redactConfigMap(raw)
	payload, err := yaml.Marshal(redacted)
	if err != nil {
		return "", configPath
	}
	return string(payload), configPath
}

func writeRawConfigFile(path string, raw string) error {
	data := []byte(raw)
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(path, data, mode)
}

func formatSchedule(schedule cron.Schedule) string {
	switch schedule.Kind {
	case "cron":
		return fmt.Sprintf("cron: %s", schedule.CronExpr)
	case "every":
		if schedule.Timezone != "" {
			return fmt.Sprintf("every %s (%s)", schedule.Every, schedule.Timezone)
		}
		return fmt.Sprintf("every %s", schedule.Every)
	case "at":
		if schedule.Timezone != "" {
			return fmt.Sprintf("at %s (%s)", schedule.At.Format(time.RFC3339), schedule.Timezone)
		}
		return fmt.Sprintf("at %s", schedule.At.Format(time.RFC3339))
	default:
		return schedule.Kind
	}
}

func configToMap(cfg *config.Config) map[string]any {
	if cfg == nil {
		return nil
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil
	}
	return raw
}

func redactConfigMap(raw map[string]any) map[string]any {
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		if isSensitiveKey(key) {
			out[key] = "***"
			continue
		}
		switch typed := value.(type) {
		case map[string]any:
			out[key] = redactConfigMap(typed)
		case []any:
			out[key] = redactConfigSlice(typed)
		default:
			out[key] = value
		}
	}
	return out
}

func redactConfigSlice(values []any) []any {
	out := make([]any, len(values))
	for i, value := range values {
		switch typed := value.(type) {
		case map[string]any:
			out[i] = redactConfigMap(typed)
		case []any:
			out[i] = redactConfigSlice(typed)
		default:
			out[i] = value
		}
	}
	return out
}

func isSensitiveKey(key string) bool {
	lower := strings.ToLower(key)
	for _, needle := range []string{
		"token",
		"secret",
		"api_key",
		"apikey",
		"password",
		"jwt",
		"signing",
		"client_secret",
		"private",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func mergeMaps(dst map[string]any, src map[string]any) {
	for key, value := range src {
		if existing, ok := dst[key]; ok {
			existingMap, okExisting := existing.(map[string]any)
			valueMap, okValue := value.(map[string]any)
			if okExisting && okValue {
				mergeMaps(existingMap, valueMap)
				dst[key] = existingMap
				continue
			}
		}
		dst[key] = value
	}
}

func setPathValue(raw map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	current := raw
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i == len(parts)-1 {
			current[part] = value
			return
		}
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
}

func sanitizeAttachmentFilename(name string) string {
	name = strings.ReplaceAll(name, "\r", "")
	name = strings.ReplaceAll(name, "\n", "")
	name = strings.ReplaceAll(name, "\"", "")
	name = strings.ReplaceAll(name, "\\", "")
	return strings.TrimSpace(name)
}
