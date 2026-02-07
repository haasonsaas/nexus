package web

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/doctor"
)

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
