package gateway

import (
	"strings"

	"github.com/haasonsaas/nexus/internal/experiments"
	"github.com/haasonsaas/nexus/pkg/models"
)

func (s *Server) experimentOverrides(session *models.Session, msg *models.Message) experiments.Overrides {
	if s == nil || s.experimentsMgr == nil {
		return experiments.Overrides{}
	}
	subject := ""
	if session != nil && session.Metadata != nil {
		if value, ok := session.Metadata["user_id"].(string); ok {
			subject = strings.TrimSpace(value)
		}
	}
	if subject == "" && session != nil {
		subject = strings.TrimSpace(session.ID)
	}
	if subject == "" && msg != nil {
		subject = strings.TrimSpace(msg.ChannelID)
	}
	return s.experimentsMgr.Resolve(subject)
}
