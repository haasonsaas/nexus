package skills

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// GatingContext provides context for skill eligibility checks.
type GatingContext struct {
	// OS is the current operating system (darwin, linux, windows).
	OS string

	// PathBins maps binary names to whether they exist on PATH.
	PathBins map[string]bool

	// EnvVars maps environment variable names to whether they are set.
	EnvVars map[string]bool

	// ConfigValues maps config paths to their values for truthiness checks.
	ConfigValues map[string]any

	// Overrides provides per-skill configuration.
	Overrides map[string]*SkillConfig
}

// NewGatingContext creates a GatingContext with the current environment.
func NewGatingContext(overrides map[string]*SkillConfig, configValues map[string]any) *GatingContext {
	return &GatingContext{
		OS:           runtime.GOOS,
		PathBins:     make(map[string]bool),
		EnvVars:      make(map[string]bool),
		ConfigValues: configValues,
		Overrides:    overrides,
	}
}

// CheckBinary checks if a binary exists on PATH and caches the result.
func (c *GatingContext) CheckBinary(name string) bool {
	if result, ok := c.PathBins[name]; ok {
		return result
	}

	_, err := exec.LookPath(name)
	result := err == nil
	c.PathBins[name] = result
	return result
}

// CheckEnv checks if an environment variable is set.
func (c *GatingContext) CheckEnv(name string) bool {
	if result, ok := c.EnvVars[name]; ok {
		return result
	}

	_, exists := os.LookupEnv(name)
	c.EnvVars[name] = exists
	return exists
}

// CheckEnvOrConfig checks if an env var is set or available in skill config.
func (c *GatingContext) CheckEnvOrConfig(skillKey, envVar string) bool {
	// Check actual environment
	if c.CheckEnv(envVar) {
		return true
	}

	// Check skill config overrides
	if cfg, ok := c.Overrides[skillKey]; ok {
		if cfg.APIKey != "" {
			return true
		}
		if _, ok := cfg.Env[envVar]; ok {
			return true
		}
	}

	return false
}

// CheckConfig checks if a config path is truthy.
func (c *GatingContext) CheckConfig(path string) bool {
	if c.ConfigValues == nil {
		return false
	}

	// Navigate the config path (e.g., "tools.browser.enabled")
	parts := strings.Split(path, ".")
	var current any = c.ConfigValues

	for _, part := range parts {
		if m, ok := current.(map[string]any); ok {
			current = m[part]
		} else {
			return false
		}
	}

	return isTruthy(current)
}

// isTruthy checks if a value is truthy.
func isTruthy(v any) bool {
	if v == nil {
		return false
	}

	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val != "" && val != "false" && val != "0"
	case int, int8, int16, int32, int64:
		return val != 0
	case uint, uint8, uint16, uint32, uint64:
		return val != 0
	case float32, float64:
		return val != 0
	default:
		return true
	}
}

// EligibilityResult contains the result of an eligibility check.
type EligibilityResult struct {
	Eligible bool
	Reason   string
}

// CheckEligibility checks if a skill is eligible to be loaded.
func (s *SkillEntry) CheckEligibility(ctx *GatingContext) EligibilityResult {
	meta := s.Metadata

	// No metadata = always eligible (unless disabled)
	if meta == nil {
		if !s.IsEnabled(ctx.Overrides) {
			return EligibilityResult{false, "disabled in config"}
		}
		return EligibilityResult{true, ""}
	}

	// Always flag skips all checks
	if meta.Always {
		return EligibilityResult{true, "always enabled"}
	}

	// Check explicit disable
	if !s.IsEnabled(ctx.Overrides) {
		return EligibilityResult{false, "disabled in config"}
	}

	// OS check
	if len(meta.OS) > 0 {
		found := false
		for _, os := range meta.OS {
			if os == ctx.OS {
				found = true
				break
			}
		}
		if !found {
			return EligibilityResult{
				false,
				fmt.Sprintf("requires OS %v, have %s", meta.OS, ctx.OS),
			}
		}
	}

	// Requirements checks
	if meta.Requires != nil {
		// All required binaries
		for _, bin := range meta.Requires.Bins {
			if !ctx.CheckBinary(bin) {
				return EligibilityResult{
					false,
					fmt.Sprintf("missing required binary: %s", bin),
				}
			}
		}

		// Any-of binaries
		if len(meta.Requires.AnyBins) > 0 {
			found := false
			for _, bin := range meta.Requires.AnyBins {
				if ctx.CheckBinary(bin) {
					found = true
					break
				}
			}
			if !found {
				return EligibilityResult{
					false,
					fmt.Sprintf("requires one of: %v", meta.Requires.AnyBins),
				}
			}
		}

		// Environment variables
		for _, env := range meta.Requires.Env {
			if !ctx.CheckEnvOrConfig(s.ConfigKey(), env) {
				return EligibilityResult{
					false,
					fmt.Sprintf("missing environment variable: %s", env),
				}
			}
		}

		// Config paths
		for _, path := range meta.Requires.Config {
			if !ctx.CheckConfig(path) {
				return EligibilityResult{
					false,
					fmt.Sprintf("config not truthy: %s", path),
				}
			}
		}
	}

	return EligibilityResult{true, ""}
}

// FilterEligible filters skills to only those that are eligible.
func FilterEligible(skills []*SkillEntry, ctx *GatingContext) []*SkillEntry {
	var eligible []*SkillEntry
	for _, skill := range skills {
		result := skill.CheckEligibility(ctx)
		if result.Eligible {
			eligible = append(eligible, skill)
		}
	}
	return eligible
}

// GetIneligibleReasons returns reasons for all ineligible skills.
func GetIneligibleReasons(skills []*SkillEntry, ctx *GatingContext) map[string]string {
	reasons := make(map[string]string)
	for _, skill := range skills {
		result := skill.CheckEligibility(ctx)
		if !result.Eligible {
			reasons[skill.Name] = result.Reason
		}
	}
	return reasons
}
