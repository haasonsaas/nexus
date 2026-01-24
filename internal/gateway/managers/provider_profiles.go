package managers

import (
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/internal/config"
)

func normalizeProviderID(value string) string {
	providerID, profileID := splitProviderProfileID(value)
	providerID = strings.ToLower(strings.TrimSpace(providerID))
	if profileID == "" {
		return providerID
	}
	return providerID + ":" + strings.TrimSpace(profileID)
}

func splitProviderProfileID(value string) (string, string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ""
	}
	for _, sep := range []string{":", "@", "/"} {
		if parts := strings.SplitN(value, sep, 2); len(parts) == 2 {
			return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		}
	}
	return value, ""
}

func resolveProviderProfile(cfg config.LLMProviderConfig, profileID string) (config.LLMProviderConfig, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return cfg, nil
	}
	if cfg.Profiles == nil {
		return cfg, fmt.Errorf("provider profile %q not configured", profileID)
	}
	profile, ok := cfg.Profiles[profileID]
	if !ok {
		return cfg, fmt.Errorf("provider profile %q not configured", profileID)
	}
	effective := cfg
	if profile.APIKey != "" {
		effective.APIKey = profile.APIKey
	}
	if profile.DefaultModel != "" {
		effective.DefaultModel = profile.DefaultModel
	}
	if profile.BaseURL != "" {
		effective.BaseURL = profile.BaseURL
	}
	return effective, nil
}
