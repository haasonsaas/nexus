// Package naming provides canonical tool naming and collision detection.
//
// Tool names follow this canonical format:
//   - Core tools:  core.<tool>           (e.g., core.browser, core.execute_code)
//   - MCP tools:   mcp:<server>.<tool>   (e.g., mcp:filesystem.read_file)
//   - Edge tools:  edge:<edge_id>.<tool> (e.g., edge:macbook.camera_snap)
//
// The canonical format enables targeted policies and collision detection.
// Tools also have "safe names" for LLM compatibility (alphanumeric + underscores).
package naming

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// ToolSource identifies where a tool comes from.
type ToolSource string

const (
	// SourceCore is for built-in Nexus tools.
	SourceCore ToolSource = "core"

	// SourceMCP is for Model Context Protocol tools.
	SourceMCP ToolSource = "mcp"

	// SourceEdge is for tools from edge daemons.
	SourceEdge ToolSource = "edge"
)

// MaxSafeNameLength is the maximum length for safe tool names.
// This ensures compatibility with various LLM providers.
const MaxSafeNameLength = 64

// ToolIdentity represents a tool's full identity including source and naming.
type ToolIdentity struct {
	// Source indicates where the tool comes from (core, mcp, edge).
	Source ToolSource `json:"source"`

	// Namespace is the grouping within the source (e.g., server ID for MCP, edge ID for edge).
	Namespace string `json:"namespace,omitempty"`

	// Name is the tool's name within its namespace.
	Name string `json:"name"`

	// SafeName is the LLM-compatible name (alphanumeric + underscores).
	SafeName string `json:"safe_name"`

	// CanonicalName is the full hierarchical name for policy targeting.
	CanonicalName string `json:"canonical_name"`

	// Description is a human-readable description.
	Description string `json:"description,omitempty"`
}

// CoreTool creates a ToolIdentity for a core tool.
func CoreTool(name string) ToolIdentity {
	safeName := sanitizeName(name)
	return ToolIdentity{
		Source:        SourceCore,
		Name:          name,
		SafeName:      safeName,
		CanonicalName: fmt.Sprintf("core.%s", name),
	}
}

// MCPTool creates a ToolIdentity for an MCP tool.
func MCPTool(serverID, toolName string) ToolIdentity {
	safeName := safeNameWithNamespace("mcp", serverID, toolName)
	return ToolIdentity{
		Source:        SourceMCP,
		Namespace:     serverID,
		Name:          toolName,
		SafeName:      safeName,
		CanonicalName: fmt.Sprintf("mcp:%s.%s", serverID, toolName),
	}
}

// EdgeTool creates a ToolIdentity for an edge tool.
func EdgeTool(edgeID, toolName string) ToolIdentity {
	safeName := safeNameWithNamespace("edge", edgeID, toolName)
	return ToolIdentity{
		Source:        SourceEdge,
		Namespace:     edgeID,
		Name:          toolName,
		SafeName:      safeName,
		CanonicalName: fmt.Sprintf("edge:%s.%s", edgeID, toolName),
	}
}

// Parse parses a canonical name into a ToolIdentity.
// Accepts formats like: core.browser, mcp:server.tool, edge:id.tool
func Parse(canonical string) (ToolIdentity, error) {
	// Check for source prefix
	if strings.HasPrefix(canonical, "core.") {
		name := strings.TrimPrefix(canonical, "core.")
		if name == "" {
			return ToolIdentity{}, fmt.Errorf("invalid core tool name: %s", canonical)
		}
		return CoreTool(name), nil
	}

	if strings.HasPrefix(canonical, "mcp:") {
		rest := strings.TrimPrefix(canonical, "mcp:")
		parts := strings.SplitN(rest, ".", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return ToolIdentity{}, fmt.Errorf("invalid MCP tool name: %s", canonical)
		}
		return MCPTool(parts[0], parts[1]), nil
	}

	if strings.HasPrefix(canonical, "edge:") {
		rest := strings.TrimPrefix(canonical, "edge:")
		parts := strings.SplitN(rest, ".", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return ToolIdentity{}, fmt.Errorf("invalid edge tool name: %s", canonical)
		}
		return EdgeTool(parts[0], parts[1]), nil
	}

	// Legacy format - treat as core tool
	return CoreTool(canonical), nil
}

// Matches checks if a tool identity matches a pattern.
// Patterns can be:
//   - Exact match: "core.browser"
//   - Source wildcard: "core.*"
//   - Namespace wildcard: "mcp:server.*"
//   - Full wildcard: "*"
func (t ToolIdentity) Matches(pattern string) bool {
	// Full wildcard
	if pattern == "*" {
		return true
	}

	// Exact match
	if pattern == t.CanonicalName {
		return true
	}

	// Source wildcard (e.g., "core.*", "mcp:*", "edge:*")
	if strings.HasSuffix(pattern, ".*") || strings.HasSuffix(pattern, ":*") {
		prefix := strings.TrimSuffix(strings.TrimSuffix(pattern, "*"), ".")
		prefix = strings.TrimSuffix(prefix, ":")
		return strings.HasPrefix(t.CanonicalName, prefix)
	}

	// Namespace wildcard (e.g., "mcp:server.*")
	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(t.CanonicalName, prefix)
	}

	return false
}

// String returns the canonical name.
func (t ToolIdentity) String() string {
	return t.CanonicalName
}

// ToolRegistry tracks registered tools and detects collisions.
type ToolRegistry struct {
	mu sync.RWMutex

	// byCanonical maps canonical name to identity
	byCanonical map[string]ToolIdentity

	// bySafe maps safe name to identity
	bySafe map[string]ToolIdentity

	// aliases maps user-friendly names to canonical names
	aliases map[string]string
}

// NewToolRegistry creates a new tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		byCanonical: make(map[string]ToolIdentity),
		bySafe:      make(map[string]ToolIdentity),
		aliases:     make(map[string]string),
	}
}

// CollisionError is returned when a tool name collides with an existing one.
type CollisionError struct {
	New      ToolIdentity
	Existing ToolIdentity
	Field    string // "canonical", "safe", or "alias"
}

func (e CollisionError) Error() string {
	return fmt.Sprintf("tool name collision (%s): %s conflicts with %s",
		e.Field, e.New.CanonicalName, e.Existing.CanonicalName)
}

// Register adds a tool identity to the registry.
// Returns CollisionError if the tool would collide with an existing one.
func (r *ToolRegistry) Register(identity ToolIdentity) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check canonical collision
	if existing, ok := r.byCanonical[identity.CanonicalName]; ok {
		return CollisionError{New: identity, Existing: existing, Field: "canonical"}
	}

	// Check safe name collision
	if existing, ok := r.bySafe[identity.SafeName]; ok {
		return CollisionError{New: identity, Existing: existing, Field: "safe"}
	}

	r.byCanonical[identity.CanonicalName] = identity
	r.bySafe[identity.SafeName] = identity
	return nil
}

// RegisterAlias adds a user-friendly alias for a canonical name.
func (r *ToolRegistry) RegisterAlias(alias, canonical string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	alias = normalizeName(alias)
	canonical = normalizeName(canonical)

	// Check if alias collides with a canonical name
	if existing, ok := r.byCanonical[alias]; ok {
		return CollisionError{
			New:      ToolIdentity{CanonicalName: alias},
			Existing: existing,
			Field:    "alias",
		}
	}

	r.aliases[alias] = canonical
	return nil
}

// Resolve resolves a name (canonical, safe, or alias) to a ToolIdentity.
func (r *ToolRegistry) Resolve(name string) (ToolIdentity, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	normalized := normalizeName(name)

	// Try canonical
	if identity, ok := r.byCanonical[normalized]; ok {
		return identity, true
	}

	// Try safe name
	if identity, ok := r.bySafe[normalized]; ok {
		return identity, true
	}

	// Try alias
	if canonical, ok := r.aliases[normalized]; ok {
		if identity, ok := r.byCanonical[canonical]; ok {
			return identity, true
		}
	}

	return ToolIdentity{}, false
}

// ResolveCanonical resolves a name to its canonical form.
func (r *ToolRegistry) ResolveCanonical(name string) string {
	if identity, ok := r.Resolve(name); ok {
		return identity.CanonicalName
	}

	// Check alias directly
	r.mu.RLock()
	defer r.mu.RUnlock()
	if canonical, ok := r.aliases[normalizeName(name)]; ok {
		return canonical
	}

	return name
}

// All returns all registered tool identities.
func (r *ToolRegistry) All() []ToolIdentity {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]ToolIdentity, 0, len(r.byCanonical))
	for _, identity := range r.byCanonical {
		result = append(result, identity)
	}
	return result
}

// BySource returns tools filtered by source.
func (r *ToolRegistry) BySource(source ToolSource) []ToolIdentity {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []ToolIdentity
	for _, identity := range r.byCanonical {
		if identity.Source == source {
			result = append(result, identity)
		}
	}
	return result
}

// Matching returns tools matching a pattern.
func (r *ToolRegistry) Matching(pattern string) []ToolIdentity {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []ToolIdentity
	for _, identity := range r.byCanonical {
		if identity.Matches(pattern) {
			result = append(result, identity)
		}
	}
	return result
}

// Unregister removes a tool from the registry.
func (r *ToolRegistry) Unregister(canonical string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if identity, ok := r.byCanonical[canonical]; ok {
		delete(r.byCanonical, canonical)
		delete(r.bySafe, identity.SafeName)
	}

	// Remove any aliases pointing to this canonical name
	for alias, target := range r.aliases {
		if target == canonical {
			delete(r.aliases, alias)
		}
	}
}

// Clear removes all tools from the registry.
func (r *ToolRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.byCanonical = make(map[string]ToolIdentity)
	r.bySafe = make(map[string]ToolIdentity)
	r.aliases = make(map[string]string)
}

// Helper functions

var safeNameRegex = regexp.MustCompile(`[^a-zA-Z0-9]+`)

// sanitizeName converts a name to a safe format (lowercase, alphanumeric, underscores).
func sanitizeName(name string) string {
	// Replace non-alphanumeric with underscores
	safe := safeNameRegex.ReplaceAllString(name, "_")
	// Trim leading/trailing underscores
	safe = strings.Trim(safe, "_")
	// Lowercase
	safe = strings.ToLower(safe)
	// Collapse multiple underscores
	for strings.Contains(safe, "__") {
		safe = strings.ReplaceAll(safe, "__", "_")
	}
	if safe == "" {
		safe = "tool"
	}
	return safe
}

// safeNameWithNamespace creates a safe name with source and namespace.
func safeNameWithNamespace(source, namespace, name string) string {
	base := fmt.Sprintf("%s_%s_%s", source, sanitizeName(namespace), sanitizeName(name))
	if len(base) <= MaxSafeNameLength {
		return base
	}
	// Truncate with hash suffix for uniqueness
	hash := hashString(namespace + ":" + name)
	suffix := "_" + hash[:8]
	maxBase := MaxSafeNameLength - len(suffix)
	return base[:maxBase] + suffix
}

// normalizeName normalizes a name for comparison.
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// hashString returns a hex-encoded SHA256 hash prefix.
func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// DefaultCoreAliases returns standard aliases for core tools.
// These provide backwards compatibility with legacy tool names.
func DefaultCoreAliases() map[string]string {
	return map[string]string{
		// Backwards compatibility
		"browser":       "core.browser",
		"sandbox":       "core.execute_code",
		"execute_code":  "core.execute_code",
		"web_search":    "core.web_search",
		"memory_search": "core.memory_search",
		"job_status":    "core.job_status",

		// User-friendly shortcuts
		"code":   "core.execute_code",
		"run":    "core.execute_code",
		"search": "core.web_search",
		"web":    "core.browser",
	}
}
