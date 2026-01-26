package agent

import (
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

func repairTranscript(history []*models.Message) []*models.Message {
	return sessions.SanitizeToolUseResultPairing(history)
}
