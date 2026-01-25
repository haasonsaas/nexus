// Package infra provides exec approval infrastructure for command execution control.
package infra

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ExecSecurity defines the security mode for exec commands.
type ExecSecurity string

const (
	// ExecSecurityDeny blocks all commands not on the allowlist.
	ExecSecurityDeny ExecSecurity = "deny"
	// ExecSecurityAllowlist allows commands matching the allowlist.
	ExecSecurityAllowlist ExecSecurity = "allowlist"
	// ExecSecurityFull allows all commands without restriction.
	ExecSecurityFull ExecSecurity = "full"
)

// ExecAsk defines when to ask for approval.
type ExecAsk string

const (
	// ExecAskOff never asks for approval.
	ExecAskOff ExecAsk = "off"
	// ExecAskOnMiss asks for approval when command isn't on allowlist.
	ExecAskOnMiss ExecAsk = "on-miss"
	// ExecAskAlways always asks for approval.
	ExecAskAlways ExecAsk = "always"
)

// ApprovalDecision represents the user's decision on an exec request.
type ApprovalDecision string

const (
	// ApprovalAllowOnce allows the command for this request only.
	ApprovalAllowOnce ApprovalDecision = "allow-once"
	// ApprovalAllowAlways allows the command and adds it to the allowlist.
	ApprovalAllowAlways ApprovalDecision = "allow-always"
	// ApprovalDeny denies the command.
	ApprovalDeny ApprovalDecision = "deny"
)

// AllowlistEntry represents a permitted command pattern.
type AllowlistEntry struct {
	ID               string `json:"id,omitempty"`
	Pattern          string `json:"pattern"`
	LastUsedAt       int64  `json:"last_used_at,omitempty"`
	LastUsedCommand  string `json:"last_used_command,omitempty"`
	LastResolvedPath string `json:"last_resolved_path,omitempty"`
}

// ExecApprovalsDefaults contains default settings for exec approvals.
type ExecApprovalsDefaults struct {
	Security        ExecSecurity `json:"security,omitempty"`
	Ask             ExecAsk      `json:"ask,omitempty"`
	AskFallback     ExecSecurity `json:"ask_fallback,omitempty"`
	AutoAllowSkills bool         `json:"auto_allow_skills,omitempty"`
}

// ExecApprovalsAgent contains per-agent exec settings.
type ExecApprovalsAgent struct {
	ExecApprovalsDefaults
	Allowlist []AllowlistEntry `json:"allowlist,omitempty"`
}

// ExecApprovalsFile is the persisted exec approvals configuration.
type ExecApprovalsFile struct {
	Version  int                            `json:"version"`
	Socket   *ExecApprovalsSocket           `json:"socket,omitempty"`
	Defaults *ExecApprovalsDefaults         `json:"defaults,omitempty"`
	Agents   map[string]*ExecApprovalsAgent `json:"agents,omitempty"`
}

// ExecApprovalsSocket contains socket communication settings.
type ExecApprovalsSocket struct {
	Path  string `json:"path,omitempty"`
	Token string `json:"token,omitempty"`
}

// ExecApprovalsResolved contains the resolved approval settings for a request.
type ExecApprovalsResolved struct {
	Path       string
	SocketPath string
	Token      string
	Defaults   ExecApprovalsDefaults
	Agent      ExecApprovalsDefaults
	Allowlist  []AllowlistEntry
	File       *ExecApprovalsFile
}

// Default safe binaries that can run without allowlist when stdin-only.
var DefaultSafeBins = []string{"jq", "grep", "cut", "sort", "uniq", "head", "tail", "tr", "wc"}

const (
	defaultExecApprovalsPath   = "~/.nexus/exec-approvals.json"
	defaultExecApprovalsSocket = "~/.nexus/exec-approvals.sock"
)

// expandHome expands ~ to the user's home directory.
func expandHome(path string) string {
	if path == "" {
		return path
	}
	if path == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

// ResolveExecApprovalsPath returns the path to the exec approvals file.
func ResolveExecApprovalsPath() string {
	return expandHome(defaultExecApprovalsPath)
}

// ResolveExecApprovalsSocketPath returns the path to the approval socket.
func ResolveExecApprovalsSocketPath() string {
	return expandHome(defaultExecApprovalsSocket)
}

// generateToken creates a secure random token.
func generateToken() string {
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

// LoadExecApprovals loads the exec approvals file.
func LoadExecApprovals() (*ExecApprovalsFile, error) {
	path := ResolveExecApprovalsPath()
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &ExecApprovalsFile{
			Version: 1,
			Agents:  make(map[string]*ExecApprovalsAgent),
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read exec approvals: %w", err)
	}

	var file ExecApprovalsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse exec approvals: %w", err)
	}

	if file.Version != 1 {
		return &ExecApprovalsFile{
			Version: 1,
			Agents:  make(map[string]*ExecApprovalsAgent),
		}, nil
	}

	if file.Agents == nil {
		file.Agents = make(map[string]*ExecApprovalsAgent)
	}

	return &file, nil
}

// SaveExecApprovals persists the exec approvals file.
func SaveExecApprovals(file *ExecApprovalsFile) error {
	path := ResolveExecApprovalsPath()
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create exec approvals dir: %w", err)
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal exec approvals: %w", err)
	}

	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write exec approvals: %w", err)
	}

	return nil
}

// EnsureExecApprovals loads or creates the exec approvals file with defaults.
func EnsureExecApprovals() (*ExecApprovalsFile, error) {
	file, err := LoadExecApprovals()
	if err != nil {
		file = &ExecApprovalsFile{
			Version: 1,
			Agents:  make(map[string]*ExecApprovalsAgent),
		}
	}

	// Ensure socket config
	if file.Socket == nil {
		file.Socket = &ExecApprovalsSocket{}
	}
	if file.Socket.Path == "" {
		file.Socket.Path = ResolveExecApprovalsSocketPath()
	}
	if file.Socket.Token == "" {
		file.Socket.Token = generateToken()
	}

	if err := SaveExecApprovals(file); err != nil {
		return file, err
	}

	return file, nil
}

// ResolveExecApprovals resolves the approval settings for an agent.
func ResolveExecApprovals(agentID string) (*ExecApprovalsResolved, error) {
	file, err := EnsureExecApprovals()
	if err != nil {
		return nil, err
	}

	if agentID == "" {
		agentID = "default"
	}

	defaults := ExecApprovalsDefaults{
		Security:    ExecSecurityDeny,
		Ask:         ExecAskOnMiss,
		AskFallback: ExecSecurityDeny,
	}
	if file.Defaults != nil {
		if file.Defaults.Security != "" {
			defaults.Security = file.Defaults.Security
		}
		if file.Defaults.Ask != "" {
			defaults.Ask = file.Defaults.Ask
		}
		if file.Defaults.AskFallback != "" {
			defaults.AskFallback = file.Defaults.AskFallback
		}
		defaults.AutoAllowSkills = file.Defaults.AutoAllowSkills
	}

	agent := defaults
	var allowlist []AllowlistEntry

	// Check wildcard agent first
	if wildcard, ok := file.Agents["*"]; ok && wildcard != nil {
		if wildcard.Security != "" {
			agent.Security = wildcard.Security
		}
		if wildcard.Ask != "" {
			agent.Ask = wildcard.Ask
		}
		if wildcard.AskFallback != "" {
			agent.AskFallback = wildcard.AskFallback
		}
		if wildcard.AutoAllowSkills {
			agent.AutoAllowSkills = true
		}
		allowlist = append(allowlist, wildcard.Allowlist...)
	}

	// Then check specific agent
	if specific, ok := file.Agents[agentID]; ok && specific != nil {
		if specific.Security != "" {
			agent.Security = specific.Security
		}
		if specific.Ask != "" {
			agent.Ask = specific.Ask
		}
		if specific.AskFallback != "" {
			agent.AskFallback = specific.AskFallback
		}
		if specific.AutoAllowSkills {
			agent.AutoAllowSkills = true
		}
		allowlist = append(allowlist, specific.Allowlist...)
	}

	return &ExecApprovalsResolved{
		Path:       ResolveExecApprovalsPath(),
		SocketPath: expandHome(file.Socket.Path),
		Token:      file.Socket.Token,
		Defaults:   defaults,
		Agent:      agent,
		Allowlist:  allowlist,
		File:       file,
	}, nil
}

// CommandResolution contains resolved executable information.
type CommandResolution struct {
	RawExecutable  string
	ResolvedPath   string
	ExecutableName string
}

// ExecCommandSegment represents a parsed command segment.
type ExecCommandSegment struct {
	Raw        string
	Argv       []string
	Resolution *CommandResolution
}

// ExecCommandAnalysis contains the analysis of a shell command.
type ExecCommandAnalysis struct {
	OK       bool
	Reason   string
	Segments []ExecCommandSegment
}

// AnalyzeShellCommand parses and analyzes a shell command.
func AnalyzeShellCommand(command, cwd string) *ExecCommandAnalysis {
	if strings.TrimSpace(command) == "" {
		return &ExecCommandAnalysis{OK: false, Reason: "empty command"}
	}

	// Split by pipe
	segments, err := splitShellPipeline(command)
	if err != nil {
		return &ExecCommandAnalysis{OK: false, Reason: err.Error()}
	}

	result := &ExecCommandAnalysis{OK: true}
	for _, seg := range segments {
		argv := tokenizeSegment(seg)
		if len(argv) == 0 {
			return &ExecCommandAnalysis{OK: false, Reason: "empty pipeline segment"}
		}

		resolution := resolveCommandExecutable(argv[0], cwd)
		result.Segments = append(result.Segments, ExecCommandSegment{
			Raw:        seg,
			Argv:       argv,
			Resolution: resolution,
		})
	}

	return result
}

// splitShellPipeline splits a command by pipe operators.
func splitShellPipeline(command string) ([]string, error) {
	var segments []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	for i := 0; i < len(command); i++ {
		ch := command[i]

		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}

		if ch == '\\' && !inSingle {
			escaped = true
			current.WriteByte(ch)
			continue
		}

		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			current.WriteByte(ch)
			continue
		}

		if ch == '"' && !inSingle {
			inDouble = !inDouble
			current.WriteByte(ch)
			continue
		}

		if !inSingle && !inDouble {
			if ch == '|' {
				if i+1 < len(command) && command[i+1] == '|' {
					return nil, errors.New("unsupported shell token: ||")
				}
				if i+1 < len(command) && command[i+1] == '&' {
					return nil, errors.New("unsupported shell token: |&")
				}
				seg := strings.TrimSpace(current.String())
				if seg != "" {
					segments = append(segments, seg)
				}
				current.Reset()
				continue
			}

			if ch == '&' || ch == ';' {
				return nil, fmt.Errorf("unsupported shell token: %c", ch)
			}

			if ch == '>' || ch == '<' || ch == '`' || ch == '\n' || ch == '(' || ch == ')' {
				return nil, fmt.Errorf("unsupported shell token: %c", ch)
			}

			if ch == '$' && i+1 < len(command) && command[i+1] == '(' {
				return nil, errors.New("unsupported shell token: $()")
			}
		}

		current.WriteByte(ch)
	}

	if escaped || inSingle || inDouble {
		return nil, errors.New("unterminated shell quote/escape")
	}

	seg := strings.TrimSpace(current.String())
	if seg != "" {
		segments = append(segments, seg)
	}

	if len(segments) == 0 {
		return nil, errors.New("empty command")
	}

	return segments, nil
}

// tokenizeSegment splits a command segment into argv.
func tokenizeSegment(segment string) []string {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false
	escaped := false

	for i := 0; i < len(segment); i++ {
		ch := segment[i]

		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}

		if ch == '\\' && !inSingle {
			escaped = true
			continue
		}

		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}

		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}

		if !inSingle && !inDouble && (ch == ' ' || ch == '\t') {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteByte(ch)
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// resolveCommandExecutable resolves the executable path for a command.
func resolveCommandExecutable(rawExec, cwd string) *CommandResolution {
	if rawExec == "" {
		return nil
	}

	expanded := expandHome(rawExec)

	// If it contains a path separator, resolve it
	if strings.Contains(expanded, "/") {
		var resolved string
		if filepath.IsAbs(expanded) {
			resolved = expanded
		} else {
			base := cwd
			if base == "" {
				base, _ = os.Getwd()
			}
			resolved = filepath.Join(base, expanded)
		}

		if isExecutableFile(resolved) {
			return &CommandResolution{
				RawExecutable:  rawExec,
				ResolvedPath:   resolved,
				ExecutableName: filepath.Base(resolved),
			}
		}

		return &CommandResolution{
			RawExecutable:  rawExec,
			ExecutableName: filepath.Base(expanded),
		}
	}

	// Search PATH
	pathEnv := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(pathEnv) {
		candidate := filepath.Join(dir, expanded)
		if isExecutableFile(candidate) {
			return &CommandResolution{
				RawExecutable:  rawExec,
				ResolvedPath:   candidate,
				ExecutableName: expanded,
			}
		}
	}

	return &CommandResolution{
		RawExecutable:  rawExec,
		ExecutableName: expanded,
	}
}

// isExecutableFile checks if a path is an executable file.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	// On Unix, check execute permission
	return info.Mode()&0o111 != 0
}

// MatchAllowlist checks if a resolution matches any allowlist entry.
func MatchAllowlist(entries []AllowlistEntry, resolution *CommandResolution) *AllowlistEntry {
	if resolution == nil || resolution.ResolvedPath == "" {
		return nil
	}

	for i := range entries {
		entry := &entries[i]
		pattern := strings.TrimSpace(entry.Pattern)
		if pattern == "" {
			continue
		}

		// Pattern must contain path separator to match resolved paths
		if !strings.Contains(pattern, "/") && !strings.Contains(pattern, "~") {
			continue
		}

		if matchesPattern(pattern, resolution.ResolvedPath) {
			return entry
		}
	}

	return nil
}

// matchesPattern checks if a target matches a glob pattern.
func matchesPattern(pattern, target string) bool {
	expanded := expandHome(pattern)
	normalizedPattern := strings.ToLower(expanded)
	normalizedTarget := strings.ToLower(target)

	regex := globToRegexp(normalizedPattern)
	return regex.MatchString(normalizedTarget)
}

// globToRegexp converts a glob pattern to a regexp.
func globToRegexp(pattern string) *regexp.Regexp {
	var result strings.Builder
	result.WriteString("^")

	for i := 0; i < len(pattern); i++ {
		ch := pattern[i]
		switch ch {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				result.WriteString(".*")
				i++
			} else {
				result.WriteString("[^/]*")
			}
		case '?':
			result.WriteString(".")
		case '.', '+', '^', '$', '{', '}', '(', ')', '[', ']', '|', '\\':
			result.WriteString("\\")
			result.WriteByte(ch)
		default:
			result.WriteByte(ch)
		}
	}

	result.WriteString("$")
	re, _ := regexp.Compile(result.String())
	return re
}

// NormalizeSafeBins returns a normalized set of safe binary names.
func NormalizeSafeBins(entries []string) map[string]bool {
	result := make(map[string]bool)
	for _, entry := range entries {
		name := strings.ToLower(strings.TrimSpace(entry))
		if name != "" {
			result[name] = true
		}
	}
	return result
}

// IsSafeBinUsage checks if a command uses a safe binary with stdin-only arguments.
func IsSafeBinUsage(argv []string, resolution *CommandResolution, safeBins map[string]bool, cwd string) bool {
	if len(safeBins) == 0 || resolution == nil {
		return false
	}

	execName := strings.ToLower(resolution.ExecutableName)
	if !safeBins[execName] {
		return false
	}

	if resolution.ResolvedPath == "" {
		return false
	}

	// Check that no arguments look like file paths
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	for _, arg := range argv[1:] {
		if arg == "-" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			// Check for -f=path style
			if idx := strings.Index(arg, "="); idx > 0 {
				value := arg[idx+1:]
				if isPathLike(value) || fileExists(filepath.Join(cwd, value)) {
					return false
				}
			}
			continue
		}
		if isPathLike(arg) {
			return false
		}
		if fileExists(filepath.Join(cwd, arg)) {
			return false
		}
	}

	return true
}

// isPathLike checks if a string looks like a file path.
func isPathLike(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return false
	}
	if strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") || strings.HasPrefix(s, "~") {
		return true
	}
	return strings.HasPrefix(s, "/")
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// AllowlistEvaluation contains the result of evaluating an allowlist.
type AllowlistEvaluation struct {
	Satisfied bool
	Matches   []*AllowlistEntry
}

// EvaluateExecAllowlist evaluates an analysis against the allowlist.
func EvaluateExecAllowlist(analysis *ExecCommandAnalysis, allowlist []AllowlistEntry, safeBins map[string]bool, cwd string) *AllowlistEvaluation {
	result := &AllowlistEvaluation{}

	if !analysis.OK || len(analysis.Segments) == 0 {
		return result
	}

	for _, seg := range analysis.Segments {
		match := MatchAllowlist(allowlist, seg.Resolution)
		if match != nil {
			result.Matches = append(result.Matches, match)
			continue
		}

		if IsSafeBinUsage(seg.Argv, seg.Resolution, safeBins, cwd) {
			continue
		}

		// Not allowed
		return result
	}

	result.Satisfied = true
	return result
}

// RequiresApproval checks if a command requires user approval.
func RequiresApproval(ask ExecAsk, security ExecSecurity, analysisOK, allowlistSatisfied bool) bool {
	if ask == ExecAskAlways {
		return true
	}
	if ask == ExecAskOnMiss && security == ExecSecurityAllowlist {
		return !analysisOK || !allowlistSatisfied
	}
	return false
}

// AddAllowlistEntry adds a new entry to an agent's allowlist.
func AddAllowlistEntry(file *ExecApprovalsFile, agentID, pattern string) error {
	if agentID == "" {
		agentID = "default"
	}
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return errors.New("empty pattern")
	}

	if file.Agents == nil {
		file.Agents = make(map[string]*ExecApprovalsAgent)
	}

	agent := file.Agents[agentID]
	if agent == nil {
		agent = &ExecApprovalsAgent{}
		file.Agents[agentID] = agent
	}

	// Check for duplicates
	for _, entry := range agent.Allowlist {
		if entry.Pattern == pattern {
			return nil
		}
	}

	agent.Allowlist = append(agent.Allowlist, AllowlistEntry{
		ID:         generateToken()[:16],
		Pattern:    pattern,
		LastUsedAt: time.Now().UnixMilli(),
	})

	return SaveExecApprovals(file)
}

// RecordAllowlistUse updates the last used timestamp for an entry.
func RecordAllowlistUse(file *ExecApprovalsFile, agentID string, entry *AllowlistEntry, command, resolvedPath string) {
	if agentID == "" {
		agentID = "default"
	}

	agent := file.Agents[agentID]
	if agent == nil {
		return
	}

	for i := range agent.Allowlist {
		if agent.Allowlist[i].Pattern == entry.Pattern {
			agent.Allowlist[i].LastUsedAt = time.Now().UnixMilli()
			agent.Allowlist[i].LastUsedCommand = command
			agent.Allowlist[i].LastResolvedPath = resolvedPath
			_ = SaveExecApprovals(file)
			return
		}
	}
}

// ApprovalRequest represents a pending exec approval request.
type ApprovalRequest struct {
	ID           string            `json:"id"`
	Command      string            `json:"command"`
	Cwd          string            `json:"cwd,omitempty"`
	AgentID      string            `json:"agent_id,omitempty"`
	SessionKey   string            `json:"session_key,omitempty"`
	ResolvedPath string            `json:"resolved_path,omitempty"`
	Security     ExecSecurity      `json:"security,omitempty"`
	Ask          ExecAsk           `json:"ask,omitempty"`
	CreatedAtMs  int64             `json:"created_at_ms"`
	ExpiresAtMs  int64             `json:"expires_at_ms"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// ApprovalResponse contains the decision for an approval request.
type ApprovalResponse struct {
	ID         string           `json:"id"`
	Decision   ApprovalDecision `json:"decision"`
	ResolvedBy string           `json:"resolved_by,omitempty"`
	Timestamp  int64            `json:"timestamp"`
}

// ApprovalManager manages pending exec approval requests.
type ApprovalManager struct {
	mu       sync.RWMutex
	pending  map[string]*pendingApproval
	handlers []ApprovalHandler
	config   *ApprovalManagerConfig
}

type pendingApproval struct {
	request  *ApprovalRequest
	resultCh chan ApprovalDecision
	timer    *time.Timer
}

// ApprovalHandler receives approval requests and provides decisions.
type ApprovalHandler interface {
	// HandleRequest is called when a new approval request is created.
	HandleRequest(ctx context.Context, request *ApprovalRequest)
	// HandleResolved is called when a request is resolved.
	HandleResolved(ctx context.Context, request *ApprovalRequest, decision ApprovalDecision, resolvedBy string)
}

// ApprovalHandlerFunc is a function that implements ApprovalHandler.
type ApprovalHandlerFunc func(ctx context.Context, request *ApprovalRequest)

// HandleRequest implements ApprovalHandler.
func (f ApprovalHandlerFunc) HandleRequest(ctx context.Context, request *ApprovalRequest) {
	f(ctx, request)
}

// HandleResolved implements ApprovalHandler.
func (f ApprovalHandlerFunc) HandleResolved(ctx context.Context, request *ApprovalRequest, decision ApprovalDecision, resolvedBy string) {
	// No-op by default
}

// ApprovalManagerConfig configures the approval manager.
type ApprovalManagerConfig struct {
	DefaultTimeoutMs int64
}

// NewApprovalManager creates a new approval manager.
func NewApprovalManager(config *ApprovalManagerConfig) *ApprovalManager {
	if config == nil {
		config = &ApprovalManagerConfig{
			DefaultTimeoutMs: 30000,
		}
	}
	return &ApprovalManager{
		pending: make(map[string]*pendingApproval),
		config:  config,
	}
}

// RegisterHandler adds an approval handler.
func (m *ApprovalManager) RegisterHandler(handler ApprovalHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers = append(m.handlers, handler)
}

// Request creates a new approval request and waits for a decision.
func (m *ApprovalManager) Request(ctx context.Context, req *ApprovalRequest) (ApprovalDecision, error) {
	if req.ID == "" {
		req.ID = generateToken()[:16]
	}
	if req.CreatedAtMs == 0 {
		req.CreatedAtMs = time.Now().UnixMilli()
	}
	if req.ExpiresAtMs == 0 {
		req.ExpiresAtMs = req.CreatedAtMs + m.config.DefaultTimeoutMs
	}

	resultCh := make(chan ApprovalDecision, 1)
	timeout := time.Duration(req.ExpiresAtMs-req.CreatedAtMs) * time.Millisecond
	timer := time.NewTimer(timeout)

	pending := &pendingApproval{
		request:  req,
		resultCh: resultCh,
		timer:    timer,
	}

	m.mu.Lock()
	m.pending[req.ID] = pending
	handlers := make([]ApprovalHandler, len(m.handlers))
	copy(handlers, m.handlers)
	m.mu.Unlock()

	// Notify handlers
	for _, h := range handlers {
		go h.HandleRequest(ctx, req)
	}

	// Wait for decision or timeout
	select {
	case decision := <-resultCh:
		timer.Stop()
		return decision, nil
	case <-timer.C:
		m.mu.Lock()
		delete(m.pending, req.ID)
		m.mu.Unlock()
		return ApprovalDeny, errors.New("approval request timed out")
	case <-ctx.Done():
		timer.Stop()
		m.mu.Lock()
		delete(m.pending, req.ID)
		m.mu.Unlock()
		return ApprovalDeny, ctx.Err()
	}
}

// Resolve provides a decision for a pending request.
func (m *ApprovalManager) Resolve(requestID string, decision ApprovalDecision, resolvedBy string) error {
	m.mu.Lock()
	pending, ok := m.pending[requestID]
	if !ok {
		m.mu.Unlock()
		return errors.New("request not found or already resolved")
	}
	delete(m.pending, requestID)
	handlers := make([]ApprovalHandler, len(m.handlers))
	copy(handlers, m.handlers)
	m.mu.Unlock()

	pending.timer.Stop()

	// Send decision
	select {
	case pending.resultCh <- decision:
	default:
	}

	// Notify handlers
	for _, h := range handlers {
		go h.HandleResolved(context.Background(), pending.request, decision, resolvedBy)
	}

	return nil
}

// GetPending returns a copy of a pending request.
func (m *ApprovalManager) GetPending(requestID string) *ApprovalRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	pending, ok := m.pending[requestID]
	if !ok {
		return nil
	}

	// Return a copy
	req := *pending.request
	return &req
}

// ListPending returns all pending requests.
func (m *ApprovalManager) ListPending() []*ApprovalRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*ApprovalRequest, 0, len(m.pending))
	for _, p := range m.pending {
		req := *p.request
		result = append(result, &req)
	}
	return result
}
