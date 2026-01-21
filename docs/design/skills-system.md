# Nexus Skills System Design

## Overview

This document specifies the design for a comprehensive skills system in Nexus, inspired by Clawdbot's AgentSkills-compatible approach but adapted for Go idioms and Nexus architecture.

## Goals

1. **Extensibility**: Allow users to add domain-specific knowledge and tools
2. **Discoverability**: Multiple discovery mechanisms (local, git, HTTP registry)
3. **Security**: Per-agent tool authorization with allowlists/denylists
4. **Efficiency**: Lazy loading, progressive disclosure, minimal context overhead
5. **Compatibility**: Similar format to Clawdbot for cross-pollination

---

## 1. Skill Format (SKILL.md)

### 1.1 File Structure

```
skill-name/
├── SKILL.md           # Required: metadata + instructions
├── scripts/           # Optional: executable code
├── references/        # Optional: documentation for context loading
└── assets/            # Optional: files used in output (templates, etc.)
```

### 1.2 SKILL.md Format

```markdown
---
name: github
description: "Interact with GitHub using the `gh` CLI. Use for issues, PRs, CI runs, and API queries."
homepage: https://cli.github.com
metadata:
  emoji: "🐙"
  requires:
    bins: ["gh"]
    env: ["GITHUB_TOKEN"]
    config: ["tools.github.enabled"]
  anyBins: []
  os: ["darwin", "linux"]
  primaryEnv: "GITHUB_TOKEN"
  install:
    - id: brew
      kind: brew
      formula: gh
      bins: ["gh"]
      label: "Install GitHub CLI (brew)"
    - id: apt
      kind: apt
      package: gh
      bins: ["gh"]
      label: "Install GitHub CLI (apt)"
      os: ["linux"]
---

# GitHub Skill

Use the `gh` CLI to interact with GitHub...

## Pull Requests

Check CI status:
```bash
gh pr checks 55 --repo owner/repo
```
...
```

### 1.3 Frontmatter Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | ✓ | Unique skill identifier (lowercase, hyphens) |
| `description` | string | ✓ | What the skill does + when to use it (triggers) |
| `homepage` | string | | URL to skill documentation |
| `metadata` | object | | Gating, install, and UI hints |

### 1.4 Metadata Fields

```go
type SkillMetadata struct {
    Emoji      string            `yaml:"emoji"`
    Homepage   string            `yaml:"homepage"`
    OS         []string          `yaml:"os"`          // darwin, linux, windows
    Requires   *SkillRequires    `yaml:"requires"`
    PrimaryEnv string            `yaml:"primaryEnv"`  // Main API key env var
    Install    []InstallSpec     `yaml:"install"`
    Always     bool              `yaml:"always"`      // Skip gating checks
    SkillKey   string            `yaml:"skillKey"`    // Config key override
}

type SkillRequires struct {
    Bins    []string `yaml:"bins"`    // All must exist on PATH
    AnyBins []string `yaml:"anyBins"` // At least one must exist
    Env     []string `yaml:"env"`     // Env vars (or config equivalents)
    Config  []string `yaml:"config"`  // Config paths that must be truthy
}

type InstallSpec struct {
    ID      string   `yaml:"id"`
    Kind    string   `yaml:"kind"`    // brew, apt, npm, go, download
    Formula string   `yaml:"formula"` // brew formula
    Package string   `yaml:"package"` // npm/apt package
    Module  string   `yaml:"module"`  // go module
    URL     string   `yaml:"url"`     // download URL
    Bins    []string `yaml:"bins"`    // Binaries provided
    Label   string   `yaml:"label"`   // Human-readable label
    OS      []string `yaml:"os"`      // Platform filter
}
```

---

## 2. Skill Discovery

### 2.1 Discovery Sources (Precedence Order)

1. **Workspace skills** (highest): `<workspace>/skills/`
2. **Local skills**: `~/.nexus/skills/`
3. **Bundled skills**: `<nexus>/skills/` (shipped with binary)
4. **Extra directories** (lowest): `skills.load.extraDirs[]`

### 2.2 Git-Based Discovery

```yaml
skills:
  sources:
    - type: git
      url: https://github.com/example/nexus-skills.git
      branch: main
      path: skills/  # Optional subdirectory
      refresh: 24h   # Auto-pull interval
```

### 2.3 HTTP Registry Discovery

```yaml
skills:
  sources:
    - type: registry
      url: https://registry.example.com/api/v1
      auth: ${REGISTRY_TOKEN}  # Optional
```

Registry API endpoints:
- `GET /skills` - List available skills
- `GET /skills/{name}` - Get skill metadata
- `GET /skills/{name}/download` - Download skill archive

### 2.4 Discovery Implementation

```go
// internal/skills/discovery.go

type DiscoverySource interface {
    Type() string
    Discover(ctx context.Context) ([]*SkillEntry, error)
    Refresh(ctx context.Context) error
}

type LocalSource struct {
    Path string
}

type GitSource struct {
    URL     string
    Branch  string
    SubPath string
    Refresh time.Duration
}

type RegistrySource struct {
    URL      string
    AuthFunc func() string
}

func DiscoverSkills(ctx context.Context, sources []DiscoverySource) ([]*SkillEntry, error) {
    var skills []*SkillEntry
    seen := make(map[string]bool)

    for _, src := range sources {
        entries, err := src.Discover(ctx)
        if err != nil {
            log.Warn("skill discovery failed", "source", src.Type(), "error", err)
            continue
        }
        for _, entry := range entries {
            if seen[entry.Name] {
                continue // Higher precedence already loaded
            }
            seen[entry.Name] = true
            skills = append(skills, entry)
        }
    }
    return skills, nil
}
```

---

## 3. Skill Loading & Gating

### 3.1 Load-Time Filtering

Skills are filtered at load time based on:

1. **OS check**: `metadata.os` matches current GOOS
2. **Binary check**: `metadata.requires.bins` all exist on PATH
3. **AnyBins check**: At least one of `metadata.requires.anyBins` exists
4. **Env check**: `metadata.requires.env` vars exist or are in config
5. **Config check**: `metadata.requires.config` paths are truthy
6. **Enabled check**: Not disabled in `skills.entries.<name>.enabled`

```go
// internal/skills/gating.go

type GatingContext struct {
    OS        string
    PathBins  map[string]bool
    EnvVars   map[string]bool
    Config    map[string]any
    Overrides map[string]*SkillConfig
}

func (s *SkillEntry) IsEligible(ctx *GatingContext) (bool, string) {
    meta := s.Metadata

    // Always-include override
    if meta.Always {
        return true, ""
    }

    // Explicit disable
    if override, ok := ctx.Overrides[s.ConfigKey()]; ok && !override.Enabled {
        return false, "disabled in config"
    }

    // OS check
    if len(meta.OS) > 0 && !contains(meta.OS, ctx.OS) {
        return false, fmt.Sprintf("requires OS %v, have %s", meta.OS, ctx.OS)
    }

    // Required binaries
    if meta.Requires != nil {
        for _, bin := range meta.Requires.Bins {
            if !ctx.PathBins[bin] {
                return false, fmt.Sprintf("missing binary: %s", bin)
            }
        }

        // Any-of binaries
        if len(meta.Requires.AnyBins) > 0 {
            found := false
            for _, bin := range meta.Requires.AnyBins {
                if ctx.PathBins[bin] {
                    found = true
                    break
                }
            }
            if !found {
                return false, fmt.Sprintf("missing any of: %v", meta.Requires.AnyBins)
            }
        }

        // Env vars (check both env and config)
        for _, envVar := range meta.Requires.Env {
            if !ctx.EnvVars[envVar] && !ctx.hasConfigEnv(s.ConfigKey(), envVar) {
                return false, fmt.Sprintf("missing env: %s", envVar)
            }
        }

        // Config paths
        for _, path := range meta.Requires.Config {
            if !ctx.configTruthy(path) {
                return false, fmt.Sprintf("config not truthy: %s", path)
            }
        }
    }

    return true, ""
}
```

### 3.2 Environment Injection

When a session starts, inject skill-specific env vars:

```go
func InjectSkillEnv(skills []*SkillEntry, overrides map[string]*SkillConfig) map[string]string {
    injected := make(map[string]string)

    for _, skill := range skills {
        cfg := overrides[skill.ConfigKey()]
        if cfg == nil {
            continue
        }

        // API key shorthand
        if cfg.APIKey != "" && skill.Metadata.PrimaryEnv != "" {
            if os.Getenv(skill.Metadata.PrimaryEnv) == "" {
                injected[skill.Metadata.PrimaryEnv] = cfg.APIKey
            }
        }

        // Explicit env overrides
        for k, v := range cfg.Env {
            if os.Getenv(k) == "" {
                injected[k] = v
            }
        }
    }

    return injected
}
```

---

## 4. Tool Authorization (Policy System)

### 4.1 Configuration

```yaml
# nexus.yaml
agents:
  - id: main
    tools:
      profile: full        # minimal | coding | messaging | full
      allow:
        - browser
        - group:fs
        - mcp:github       # MCP server tools
      deny:
        - sandbox.network  # Specific tool variants
        - nodes.camera

tools:
  groups:
    "group:fs": ["read", "write", "edit", "exec"]
    "group:web": ["websearch", "webfetch"]
    "group:runtime": ["sandbox"]
    "group:messaging": ["send_message"]
```

### 4.2 Policy Resolution

```go
// internal/tools/policy/policy.go

type PolicyProfile string

const (
    ProfileMinimal   PolicyProfile = "minimal"
    ProfileCoding    PolicyProfile = "coding"
    ProfileMessaging PolicyProfile = "messaging"
    ProfileFull      PolicyProfile = "full"
)

var ProfileDefaults = map[PolicyProfile]*Policy{
    ProfileMinimal: {
        Allow: []string{"status"},
    },
    ProfileCoding: {
        Allow: []string{"group:fs", "group:runtime", "group:web", "memorysearch"},
    },
    ProfileMessaging: {
        Allow: []string{"group:messaging", "status"},
    },
    ProfileFull: {}, // No restrictions
}

type Policy struct {
    Profile PolicyProfile
    Allow   []string
    Deny    []string
}

type Resolver struct {
    groups map[string][]string
}

func (r *Resolver) IsAllowed(policy *Policy, toolName string) bool {
    normalized := strings.ToLower(strings.TrimSpace(toolName))

    // Expand profile defaults
    basePolicy := ProfileDefaults[policy.Profile]

    // Merge allow lists
    allowed := r.expandGroups(basePolicy.Allow)
    allowed = append(allowed, r.expandGroups(policy.Allow)...)

    // Merge deny lists
    denied := r.expandGroups(basePolicy.Deny)
    denied = append(denied, r.expandGroups(policy.Deny)...)

    // Full profile allows everything not explicitly denied
    if policy.Profile == ProfileFull {
        return !contains(denied, normalized)
    }

    // Other profiles: must be in allow list and not in deny list
    return contains(allowed, normalized) && !contains(denied, normalized)
}

func (r *Resolver) expandGroups(items []string) []string {
    var result []string
    for _, item := range items {
        if group, ok := r.groups[item]; ok {
            result = append(result, group...)
        } else {
            result = append(result, item)
        }
    }
    return result
}
```

### 4.3 MCP Tool Authorization

MCP tools use namespaced names: `mcp:<server>.<tool>`

```go
func (r *Resolver) IsAllowed(policy *Policy, toolName string) bool {
    // Handle MCP tools
    if strings.HasPrefix(toolName, "mcp:") {
        // Check specific tool
        if r.checkAllowed(policy, toolName) {
            return true
        }
        // Check server wildcard: mcp:github.*
        parts := strings.SplitN(toolName, ".", 2)
        if len(parts) == 2 {
            return r.checkAllowed(policy, parts[0]+".*")
        }
        return false
    }

    return r.checkAllowed(policy, toolName)
}
```

---

## 5. Skill Manager

### 5.1 Interface

```go
// internal/skills/manager.go

type Manager struct {
    sources     []DiscoverySource
    skills      map[string]*SkillEntry
    eligible    map[string]*SkillEntry  // After gating
    config      *config.SkillsConfig
    mu          sync.RWMutex
    watcher     *fsnotify.Watcher
}

type SkillEntry struct {
    Name        string
    Description string
    Homepage    string
    Metadata    *SkillMetadata
    Content     string          // Markdown body (lazy loaded)
    Path        string          // Directory path
    Source      DiscoverySource // Where it came from
}

func NewManager(cfg *config.SkillsConfig) (*Manager, error)
func (m *Manager) Discover(ctx context.Context) error
func (m *Manager) RefreshEligible(ctx *GatingContext) error
func (m *Manager) GetSkill(name string) (*SkillEntry, bool)
func (m *Manager) ListEligible() []*SkillEntry
func (m *Manager) LoadContent(name string) (string, error)  // Lazy load body
func (m *Manager) Watch() error
func (m *Manager) Close() error
```

### 5.2 Session Snapshot

Skills are snapshotted when a session starts:

```go
// internal/sessions/session.go

type Session struct {
    // ...existing fields...
    SkillsSnapshot []*SkillSnapshot `json:"skills_snapshot"`
}

type SkillSnapshot struct {
    Name        string `json:"name"`
    Description string `json:"description"`
    Path        string `json:"path"`
}

func (s *Session) RefreshSkills(mgr *skills.Manager) {
    eligible := mgr.ListEligible()
    s.SkillsSnapshot = make([]*SkillSnapshot, len(eligible))
    for i, skill := range eligible {
        s.SkillsSnapshot[i] = &SkillSnapshot{
            Name:        skill.Name,
            Description: skill.Description,
            Path:        skill.Path,
        }
    }
}
```

---

## 6. System Prompt Integration

### 6.1 Skills in Prompt

```go
// internal/gateway/system_prompt.go

func (b *PromptBuilder) formatSkillsSection(skills []*SkillSnapshot) string {
    if len(skills) == 0 {
        return ""
    }

    var sb strings.Builder
    sb.WriteString("\n<skills>\n")
    sb.WriteString("Available skills provide specialized knowledge. ")
    sb.WriteString("Read SKILL.md for detailed instructions.\n\n")

    for _, skill := range skills {
        sb.WriteString(fmt.Sprintf("- **%s**: %s\n", skill.Name, skill.Description))
        sb.WriteString(fmt.Sprintf("  Location: `%s/SKILL.md`\n", skill.Path))
    }

    sb.WriteString("</skills>\n")
    return sb.String()
}
```

### 6.2 Token Budget

| Component | Characters | ~Tokens |
|-----------|------------|---------|
| Skills header | 100 | 25 |
| Per skill | 80 + len(name) + len(description) + len(path) | ~30-50 |

Example with 10 skills: ~500 tokens overhead

---

## 7. Configuration Schema

```yaml
# nexus.yaml
skills:
  # Discovery sources (in precedence order after workspace/local/bundled)
  sources:
    - type: local
      path: /shared/team-skills
    - type: git
      url: https://github.com/org/skills.git
      branch: main
      refresh: 24h
    - type: registry
      url: https://skills.example.com/api/v1

  # Loading options
  load:
    extraDirs:
      - /opt/nexus/skills
    watch: true
    watchDebounceMs: 250

  # Per-skill configuration
  entries:
    github:
      enabled: true
      apiKey: ${GITHUB_TOKEN}
      env:
        GITHUB_TOKEN: ${GITHUB_TOKEN}
      config:
        enterprise: true

    internal-tool:
      enabled: false  # Disable this skill
```

### 7.1 Config Types

```go
// internal/config/skills.go

type SkillsConfig struct {
    Sources []SourceConfig          `yaml:"sources"`
    Load    *SkillsLoadConfig       `yaml:"load"`
    Entries map[string]*SkillConfig `yaml:"entries"`
}

type SourceConfig struct {
    Type    string        `yaml:"type"`    // local, git, registry
    Path    string        `yaml:"path"`    // For local
    URL     string        `yaml:"url"`     // For git/registry
    Branch  string        `yaml:"branch"`  // For git
    Refresh time.Duration `yaml:"refresh"` // For git
    Auth    string        `yaml:"auth"`    // For registry
}

type SkillsLoadConfig struct {
    ExtraDirs       []string `yaml:"extraDirs"`
    Watch           bool     `yaml:"watch"`
    WatchDebounceMs int      `yaml:"watchDebounceMs"`
}

type SkillConfig struct {
    Enabled *bool             `yaml:"enabled"` // Pointer for tri-state
    APIKey  string            `yaml:"apiKey"`
    Env     map[string]string `yaml:"env"`
    Config  map[string]any    `yaml:"config"`
}
```

---

## 8. CLI Commands

```bash
# List all discovered skills
nexus skills list [--all] [--json]

# Show skill details
nexus skills show <name>

# Check skill eligibility
nexus skills check <name>

# Install skill from registry
nexus skills install <name|url>

# Update skills from git sources
nexus skills update [--all]

# Validate skill format
nexus skills validate <path>

# Create new skill from template
nexus skills init <name> [--path ./skills]
```

---

## 9. Implementation Phases

### Phase 1: Core (Week 1-2)
- [ ] SKILL.md parser (YAML frontmatter + markdown)
- [ ] Local directory discovery
- [ ] Basic gating (bins, env, enabled)
- [ ] Skill manager with caching
- [ ] System prompt integration
- [ ] `nexus skills list/show` commands

### Phase 2: Policy (Week 3)
- [ ] Tool policy resolution
- [ ] Profile-based defaults
- [ ] Group expansion
- [ ] Per-agent policy config
- [ ] Policy validation

### Phase 3: Advanced Discovery (Week 4)
- [ ] Git-based discovery
- [ ] HTTP registry client
- [ ] `nexus skills install/update` commands
- [ ] Auto-refresh background task

### Phase 4: Polish (Week 5)
- [ ] File watcher for hot reload
- [ ] Session snapshot persistence
- [ ] Skill validation command
- [ ] Skill init template
- [ ] Documentation

---

## 10. Open Questions

1. **Skill-provided tools**: Should skills be able to define JSON Schema tools that get registered with the LLM, or are skills purely instructional?

2. **Install automation**: Should `nexus skills install` actually run brew/apt/npm, or just tell the user what to install?

3. **Skill versioning**: Should we track skill versions for update detection?

4. **Cross-agent skills**: Should there be a way to share skills across multiple nexus instances (beyond git)?

---

## Appendix A: Example Skills

### A.1 Simple Skill (gh)

```markdown
---
name: github
description: "Interact with GitHub using the `gh` CLI for issues, PRs, and CI."
metadata:
  emoji: "🐙"
  requires:
    bins: ["gh"]
---

# GitHub Skill

Use `gh` CLI for GitHub operations.

## Issues
```bash
gh issue list --repo owner/repo
gh issue create --title "Bug" --body "Description"
```

## Pull Requests
```bash
gh pr list --repo owner/repo
gh pr checks <number> --repo owner/repo
```
```

### A.2 Complex Skill with Scripts

```markdown
---
name: image-gen
description: "Generate images using Gemini API. Use when asked to create, generate, or make images."
metadata:
  emoji: "🎨"
  requires:
    bins: ["uv"]
    env: ["GEMINI_API_KEY"]
  primaryEnv: "GEMINI_API_KEY"
  install:
    - id: uv
      kind: brew
      formula: uv
      bins: ["uv"]
      label: "Install uv (brew)"
---

# Image Generation

Generate images using the bundled script.

```bash
uv run {baseDir}/scripts/generate.py --prompt "description" --output image.png
```

Options:
- `--resolution`: 1K (default), 2K, 4K
- `--style`: realistic, artistic, cartoon
```

---

## Appendix B: Migration from Clawdbot

Skills using Clawdbot's single-line JSON metadata format can be migrated:

**Before (Clawdbot)**:
```markdown
---
name: foo
description: Do foo things
metadata: {"clawdbot":{"emoji":"🔧","requires":{"bins":["foo"]}}}
---
```

**After (Nexus)**:
```markdown
---
name: foo
description: Do foo things
metadata:
  emoji: "🔧"
  requires:
    bins: ["foo"]
---
```

A migration script can automate this:
```bash
nexus skills migrate <path> [--dry-run]
```
