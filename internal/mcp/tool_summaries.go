package mcp

import (
	"encoding/json"

	"github.com/haasonsaas/nexus/pkg/models"
)

// ToolSummaries returns tool metadata for all MCP tools with safe names.
func ToolSummaries(mgr *Manager) []models.ToolSummary {
	if mgr == nil {
		return nil
	}

	tools := listToolsSorted(mgr)
	used := make(map[string]struct{})
	summaries := make([]models.ToolSummary, 0, len(tools))

	for _, entry := range tools {
		name := safeToolName(entry.serverID, entry.tool.Name, used)
		summaries = append(summaries, models.ToolSummary{
			Name:        name,
			Description: entry.tool.Description,
			Schema:      entry.tool.InputSchema,
			Source:      "mcp",
			Namespace:   entry.serverID,
			Canonical:   canonicalToolName(entry.serverID, entry.tool.Name),
		})
	}

	for _, serverID := range listServerIDs(mgr) {
		resListName := safeToolName(serverID, "resources_list", used)
		resReadName := safeToolName(serverID, "resource_read", used)
		promptListName := safeToolName(serverID, "prompts_list", used)
		promptGetName := safeToolName(serverID, "prompt_get", used)

		resList := NewResourceListBridge(mgr, serverID, resListName)
		resRead := NewResourceReadBridge(mgr, serverID, resReadName)
		promptList := NewPromptListBridge(mgr, serverID, promptListName)
		promptGet := NewPromptGetBridge(mgr, serverID, promptGetName)

		summaries = append(summaries,
			toolSummaryFromTool(resList, "mcp", serverID, canonicalResourceList(serverID)),
			toolSummaryFromTool(resRead, "mcp", serverID, canonicalResourceRead(serverID)),
			toolSummaryFromTool(promptList, "mcp", serverID, canonicalPromptList(serverID)),
			toolSummaryFromTool(promptGet, "mcp", serverID, canonicalPromptGet(serverID)),
		)
	}

	return summaries
}

type summaryTool interface {
	Name() string
	Description() string
	Schema() json.RawMessage
}

func toolSummaryFromTool(tool summaryTool, source, namespace, canonical string) models.ToolSummary {
	if tool == nil {
		return models.ToolSummary{}
	}
	return models.ToolSummary{
		Name:        tool.Name(),
		Description: tool.Description(),
		Schema:      tool.Schema(),
		Source:      source,
		Namespace:   namespace,
		Canonical:   canonical,
	}
}
