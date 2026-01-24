package agent

import "github.com/haasonsaas/nexus/pkg/models"

func repairTranscript(history []*models.Message) []*models.Message {
	if len(history) == 0 {
		return history
	}

	pending := make(map[string]struct{})
	pendingOrder := make([]string, 0)
	repaired := make([]*models.Message, 0, len(history))

	clearPending := func() {
		for k := range pending {
			delete(pending, k)
		}
		pendingOrder = pendingOrder[:0]
	}

	for _, msg := range history {
		if msg == nil {
			continue
		}

		switch msg.Role {
		case models.RoleAssistant:
			clearPending()
			if len(msg.ToolCalls) > 0 {
				for _, call := range msg.ToolCalls {
					if call.ID == "" {
						continue
					}
					pending[call.ID] = struct{}{}
					pendingOrder = append(pendingOrder, call.ID)
				}
			}
			repaired = append(repaired, msg)
		case models.RoleTool:
			if len(msg.ToolResults) == 0 {
				continue
			}
			fixed := make([]models.ToolResult, 0, len(msg.ToolResults))
			for _, result := range msg.ToolResults {
				res := result
				if res.ToolCallID == "" && len(pendingOrder) > 0 {
					res.ToolCallID = pendingOrder[0]
				}
				if res.ToolCallID == "" {
					continue
				}
				if _, ok := pending[res.ToolCallID]; ok {
					delete(pending, res.ToolCallID)
					pendingOrder = removeID(pendingOrder, res.ToolCallID)
					fixed = append(fixed, res)
				}
			}
			if len(fixed) == 0 {
				continue
			}
			copied := *msg
			copied.ToolResults = fixed
			repaired = append(repaired, &copied)
		default:
			repaired = append(repaired, msg)
		}
	}

	return repaired
}

func removeID(ids []string, target string) []string {
	for i, id := range ids {
		if id == target {
			copy(ids[i:], ids[i+1:])
			return ids[:len(ids)-1]
		}
	}
	return ids
}
