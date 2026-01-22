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

// RegisterTools registers all available MCP tools with the runtime.
func RegisterTools(runtime *agent.Runtime, mgr *Manager) []string {
	if runtime == nil || mgr == nil {
		return nil
	}

	tools := listToolsSorted(mgr)
	used := make(map[string]struct{})
	registered := make([]string, 0, len(tools))
	for _, entry := range tools {
		name := safeToolName(entry.serverID, entry.tool.Name, used)
		runtime.RegisterTool(NewToolBridge(mgr, entry.serverID, entry.tool, name))
		registered = append(registered, name)
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
