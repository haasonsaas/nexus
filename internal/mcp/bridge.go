package mcp

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/haasonsaas/nexus/internal/agent"
)

const maxToolNameLen = 64

// ToolCaller defines the MCP tool execution contract used by the bridge.
type ToolCaller interface {
	CallTool(ctx context.Context, serverID, toolName string, arguments map[string]any) (*ToolCallResult, error)
}

// ResourceReader defines the MCP resource read contract used by the bridge.
type ResourceReader interface {
	ReadResource(ctx context.Context, serverID, uri string) ([]*ResourceContent, error)
}

// PromptGetter defines the MCP prompt get contract used by the bridge.
type PromptGetter interface {
	GetPrompt(ctx context.Context, serverID, name string, arguments map[string]string) (*GetPromptResult, error)
}

// ToolPolicyRegistrar allows MCP tools to be mapped into policy systems.
type ToolPolicyRegistrar interface {
	RegisterAlias(alias string, canonical string)
	RegisterMCPServer(serverID string, tools []string)
}

// ToolBridge wraps an MCP tool and exposes it as a Nexus agent tool.
type ToolBridge struct {
	caller   ToolCaller
	serverID string
	tool     *MCPTool
	name     string
}

// NewToolBridge creates a bridge tool with a precomputed safe name.
func NewToolBridge(caller ToolCaller, serverID string, tool *MCPTool, safeName string) *ToolBridge {
	return &ToolBridge{
		caller:   caller,
		serverID: serverID,
		tool:     tool,
		name:     safeName,
	}
}

// Name returns the safe tool name registered with the LLM provider.
func (b *ToolBridge) Name() string {
	return b.name
}

// Description returns the MCP tool description, prefixed with MCP metadata.
func (b *ToolBridge) Description() string {
	desc := strings.TrimSpace(b.tool.Description)
	if desc == "" {
		return fmt.Sprintf("MCP tool %s.%s", b.serverID, b.tool.Name)
	}
	return fmt.Sprintf("MCP tool %s.%s: %s", b.serverID, b.tool.Name, desc)
}

// Schema returns the MCP tool input schema.
func (b *ToolBridge) Schema() json.RawMessage {
	if len(b.tool.InputSchema) == 0 {
		return json.RawMessage(`{"type":"object"}`)
	}
	return b.tool.InputSchema
}

// Execute invokes the MCP tool via the manager.
func (b *ToolBridge) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var arguments map[string]any
	if len(params) > 0 {
		if err := json.Unmarshal(params, &arguments); err != nil {
			return nil, err
		}
	}

	result, err := b.caller.CallTool(ctx, b.serverID, b.tool.Name, arguments)
	if err != nil {
		return nil, err
	}

	content, isError := formatToolCallResult(result)
	return &agent.ToolResult{
		Content: content,
		IsError: isError,
	}, nil
}

// ResourceListBridge exposes MCP resources/list as a tool.
type ResourceListBridge struct {
	lister   *Manager
	serverID string
	name     string
}

// NewResourceListBridge creates a resource list tool.
func NewResourceListBridge(mgr *Manager, serverID, safeName string) *ResourceListBridge {
	return &ResourceListBridge{lister: mgr, serverID: serverID, name: safeName}
}

func (b *ResourceListBridge) Name() string { return b.name }

func (b *ResourceListBridge) Description() string {
	return fmt.Sprintf("List MCP resources for %s", b.serverID)
}

func (b *ResourceListBridge) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}

func (b *ResourceListBridge) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	resources := b.lister.AllResources()[b.serverID]
	payload, err := json.Marshal(resources)
	if err != nil {
		return nil, err
	}
	return &agent.ToolResult{Content: string(payload)}, nil
}

// ResourceReadBridge exposes MCP resources/read as a tool.
type ResourceReadBridge struct {
	reader   ResourceReader
	serverID string
	name     string
}

// NewResourceReadBridge creates a resource read tool.
func NewResourceReadBridge(reader ResourceReader, serverID, safeName string) *ResourceReadBridge {
	return &ResourceReadBridge{reader: reader, serverID: serverID, name: safeName}
}

func (b *ResourceReadBridge) Name() string { return b.name }

func (b *ResourceReadBridge) Description() string {
	return fmt.Sprintf("Read an MCP resource from %s (provide uri)", b.serverID)
}

func (b *ResourceReadBridge) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"uri":{"type":"string"}},"required":["uri"]}`)
}

func (b *ResourceReadBridge) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.URI) == "" {
		return nil, fmt.Errorf("uri is required")
	}
	contents, err := b.reader.ReadResource(ctx, b.serverID, input.URI)
	if err != nil {
		return nil, err
	}
	content, isError := formatResourceContents(contents)
	return &agent.ToolResult{Content: content, IsError: isError}, nil
}

// PromptListBridge exposes MCP prompts/list as a tool.
type PromptListBridge struct {
	lister   *Manager
	serverID string
	name     string
}

// NewPromptListBridge creates a prompt list tool.
func NewPromptListBridge(mgr *Manager, serverID, safeName string) *PromptListBridge {
	return &PromptListBridge{lister: mgr, serverID: serverID, name: safeName}
}

func (b *PromptListBridge) Name() string { return b.name }

func (b *PromptListBridge) Description() string {
	return fmt.Sprintf("List MCP prompts for %s", b.serverID)
}

func (b *PromptListBridge) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}

func (b *PromptListBridge) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	prompts := b.lister.AllPrompts()[b.serverID]
	payload, err := json.Marshal(prompts)
	if err != nil {
		return nil, err
	}
	return &agent.ToolResult{Content: string(payload)}, nil
}

// PromptGetBridge exposes MCP prompts/get as a tool.
type PromptGetBridge struct {
	getter   PromptGetter
	serverID string
	name     string
}

// NewPromptGetBridge creates a prompt get tool.
func NewPromptGetBridge(getter PromptGetter, serverID, safeName string) *PromptGetBridge {
	return &PromptGetBridge{getter: getter, serverID: serverID, name: safeName}
}

func (b *PromptGetBridge) Name() string { return b.name }

func (b *PromptGetBridge) Description() string {
	return fmt.Sprintf("Fetch an MCP prompt from %s (provide name, arguments)", b.serverID)
}

func (b *PromptGetBridge) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"},"arguments":{"type":"object"}},"required":["name"]}`)
}

func (b *PromptGetBridge) Execute(ctx context.Context, params json.RawMessage) (*agent.ToolResult, error) {
	var input struct {
		Name      string            `json:"name"`
		Arguments map[string]string `json:"arguments,omitempty"`
	}
	if err := json.Unmarshal(params, &input); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Name) == "" {
		return nil, fmt.Errorf("name is required")
	}
	result, err := b.getter.GetPrompt(ctx, b.serverID, input.Name, input.Arguments)
	if err != nil {
		return nil, err
	}
	content, isError := formatPromptResult(result)
	return &agent.ToolResult{Content: content, IsError: isError}, nil
}

// RegisterTools registers all available MCP tools with the runtime.
func RegisterTools(runtime *agent.Runtime, mgr *Manager) []string {
	return RegisterToolsWithRegistrar(runtime, mgr, nil)
}

// RegisterToolsWithRegistrar registers MCP tools and optionally registers policy aliases.
func RegisterToolsWithRegistrar(runtime *agent.Runtime, mgr *Manager, registrar ToolPolicyRegistrar) []string {
	if runtime == nil || mgr == nil {
		return nil
	}

	tools := listToolsSorted(mgr)
	used := make(map[string]struct{})
	registered := make([]string, 0, len(tools))
	serverTools := make(map[string][]string)
	for _, entry := range tools {
		name := safeToolName(entry.serverID, entry.tool.Name, used)
		runtime.RegisterTool(NewToolBridge(mgr, entry.serverID, entry.tool, name))
		registered = append(registered, name)
		serverTools[entry.serverID] = append(serverTools[entry.serverID], entry.tool.Name)
		if registrar != nil {
			registrar.RegisterAlias(name, canonicalToolName(entry.serverID, entry.tool.Name))
		}
	}

	serverIDs := listServerIDs(mgr)
	for _, serverID := range serverIDs {
		resListName := safeToolName(serverID, "resources_list", used)
		resReadName := safeToolName(serverID, "resource_read", used)
		promptListName := safeToolName(serverID, "prompts_list", used)
		promptGetName := safeToolName(serverID, "prompt_get", used)

		runtime.RegisterTool(NewResourceListBridge(mgr, serverID, resListName))
		runtime.RegisterTool(NewResourceReadBridge(mgr, serverID, resReadName))
		runtime.RegisterTool(NewPromptListBridge(mgr, serverID, promptListName))
		runtime.RegisterTool(NewPromptGetBridge(mgr, serverID, promptGetName))

		registered = append(registered, resListName, resReadName, promptListName, promptGetName)

		if registrar != nil {
			registrar.RegisterAlias(resListName, canonicalResourceList(serverID))
			registrar.RegisterAlias(resReadName, canonicalResourceRead(serverID))
			registrar.RegisterAlias(promptListName, canonicalPromptList(serverID))
			registrar.RegisterAlias(promptGetName, canonicalPromptGet(serverID))
		}

		serverTools[serverID] = append(serverTools[serverID],
			"resources.list",
			"resources.read",
			"prompts.list",
			"prompts.get",
		)
	}

	if registrar != nil {
		for serverID, names := range serverTools {
			registrar.RegisterMCPServer(serverID, names)
		}
	}

	return registered
}

type toolEntry struct {
	serverID string
	tool     *MCPTool
}

func listToolsSorted(mgr *Manager) []toolEntry {
	all := mgr.AllTools()
	if len(all) == 0 {
		return nil
	}

	serverIDs := make([]string, 0, len(all))
	for id := range all {
		serverIDs = append(serverIDs, id)
	}
	sort.Strings(serverIDs)

	var entries []toolEntry
	for _, serverID := range serverIDs {
		tools := all[serverID]
		sort.Slice(tools, func(i, j int) bool {
			return tools[i].Name < tools[j].Name
		})
		for _, tool := range tools {
			entries = append(entries, toolEntry{serverID: serverID, tool: tool})
		}
	}
	return entries
}

func listServerIDs(mgr *Manager) []string {
	seen := make(map[string]struct{})
	for id := range mgr.AllTools() {
		seen[id] = struct{}{}
	}
	for id := range mgr.AllResources() {
		seen[id] = struct{}{}
	}
	for id := range mgr.AllPrompts() {
		seen[id] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func safeToolName(serverID, toolName string, used map[string]struct{}) string {
	base := "mcp_" + sanitizeToolPart(serverID) + "_" + sanitizeToolPart(toolName)
	name := base
	if len(name) > maxToolNameLen {
		name = truncateWithHash(base, serverID, toolName)
	}

	if _, exists := used[name]; exists {
		name = dedupeWithHash(name, serverID, toolName)
	}

	used[name] = struct{}{}
	return name
}

func sanitizeToolPart(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	underscore := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			underscore = false
		default:
			if !underscore {
				b.WriteByte('_')
				underscore = true
			}
		}
	}
	clean := strings.Trim(b.String(), "_")
	if clean == "" {
		return "tool"
	}
	return clean
}

func toolNameHash(serverID, toolName string) string {
	sum := sha1.Sum([]byte(serverID + ":" + toolName))
	return hex.EncodeToString(sum[:])[:8]
}

func truncateWithHash(base, serverID, toolName string) string {
	suffix := "_" + toolNameHash(serverID, toolName)
	if maxToolNameLen <= len(suffix) {
		return suffix[len(suffix)-maxToolNameLen:]
	}
	trimLen := maxToolNameLen - len(suffix)
	if trimLen > len(base) {
		trimLen = len(base)
	}
	return base[:trimLen] + suffix
}

func dedupeWithHash(base, serverID, toolName string) string {
	suffix := "_" + toolNameHash(serverID, toolName)
	name := base + suffix
	if len(name) <= maxToolNameLen {
		return name
	}
	return truncateWithHash(base, serverID, toolName)
}

func formatToolCallResult(result *ToolCallResult) (string, bool) {
	if result == nil {
		return "", false
	}
	if len(result.Content) == 0 {
		return "", result.IsError
	}

	allText := true
	var combined strings.Builder
	for _, item := range result.Content {
		if item.Type != "text" {
			allText = false
			break
		}
		if item.Text == "" {
			continue
		}
		if combined.Len() > 0 {
			combined.WriteString("\n")
		}
		combined.WriteString(item.Text)
	}

	if allText && combined.Len() > 0 {
		return combined.String(), result.IsError
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return "", result.IsError
	}
	return string(payload), result.IsError
}

func formatResourceContents(contents []*ResourceContent) (string, bool) {
	if len(contents) == 0 {
		return "", false
	}
	if len(contents) == 1 && contents[0].Text != "" {
		return contents[0].Text, false
	}
	payload, err := json.Marshal(contents)
	if err != nil {
		return "", false
	}
	return string(payload), false
}

func formatPromptResult(result *GetPromptResult) (string, bool) {
	if result == nil || len(result.Messages) == 0 {
		return "", false
	}
	if len(result.Messages) == 1 && result.Messages[0].Content.Type == "text" {
		return result.Messages[0].Content.Text, false
	}
	payload, err := json.Marshal(result.Messages)
	if err != nil {
		return "", false
	}
	return string(payload), false
}

func canonicalToolName(serverID, toolName string) string {
	return fmt.Sprintf("mcp:%s.%s", serverID, toolName)
}

func canonicalResourceList(serverID string) string {
	return fmt.Sprintf("mcp:%s.resources.list", serverID)
}

func canonicalResourceRead(serverID string) string {
	return fmt.Sprintf("mcp:%s.resources.read", serverID)
}

func canonicalPromptList(serverID string) string {
	return fmt.Sprintf("mcp:%s.prompts.list", serverID)
}

func canonicalPromptGet(serverID string) string {
	return fmt.Sprintf("mcp:%s.prompts.get", serverID)
}
