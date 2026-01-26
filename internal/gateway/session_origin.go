package gateway

import (
	"fmt"
	"strings"

	"github.com/haasonsaas/nexus/pkg/models"
)

const (
	sessionMetaOriginProvider  = "origin_provider"
	sessionMetaOriginFrom      = "origin_from"
	sessionMetaOriginTo        = "origin_to"
	sessionMetaOriginAccountID = "origin_account_id"
	sessionMetaOriginThreadID  = "origin_thread_id"
	sessionMetaOriginLabel     = "origin_label"
)

func ensureSessionOriginMetadata(session *models.Session, msg *models.Message) bool {
	if session == nil {
		return false
	}

	var changed bool
	set := func(key, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		if session.Metadata == nil {
			session.Metadata = map[string]any{}
		}
		if existing, ok := session.Metadata[key]; ok && strings.TrimSpace(fmt.Sprint(existing)) != "" {
			return
		}
		session.Metadata[key] = value
		changed = true
	}

	provider := string(session.Channel)
	if msg != nil && msg.Channel != "" {
		provider = string(msg.Channel)
	}
	threadID := strings.TrimSpace(session.ChannelID)
	if threadID == "" && msg != nil {
		threadID = strings.TrimSpace(msg.ChannelID)
	}

	set(sessionMetaOriginProvider, provider)
	set(sessionMetaOriginThreadID, threadID)

	// Message-derived fields (best-effort; leave unset if unknown).
	set(sessionMetaOriginFrom, findFirstMetaString(msg, "sender_id", "from", "from_id", "user_id"))
	set(sessionMetaOriginTo, findFirstMetaString(msg, "bot_id", "to", "to_id", "recipient_id"))
	set(sessionMetaOriginAccountID, findFirstMetaString(msg, "account_id", "connection_id", "adapter_id"))

	label := findFirstMetaString(msg, "label", "channel_name", "room_name", "group_name", "sender_name")
	if label == "" {
		label = threadID
	}
	set(sessionMetaOriginLabel, label)

	return changed
}

func findFirstMetaString(msg *models.Message, keys ...string) string {
	if msg == nil || msg.Metadata == nil {
		return ""
	}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if v, ok := msg.Metadata[key]; ok {
			out := strings.TrimSpace(fmt.Sprint(v))
			if out != "" && out != "<nil>" {
				return out
			}
		}
	}
	return ""
}
