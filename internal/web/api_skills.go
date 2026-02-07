package web

import (
	"context"
	"net/http"
	"sort"
	"time"
)

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

// apiSkills handles GET /api/skills.
func (h *Handler) apiSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		h.jsonError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.jsonResponse(w, apiSkillsResponse{Skills: h.listSkills(r.Context())})
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
