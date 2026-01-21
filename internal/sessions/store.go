package sessions

import (
	"context"

	"github.com/yourorg/nexus/pkg/models"
)

// Store is the interface for session persistence.
type Store interface {
	// Session CRUD
	Create(ctx context.Context, session *models.Session) error
	Get(ctx context.Context, id string) (*models.Session, error)
	Update(ctx context.Context, session *models.Session) error
	Delete(ctx context.Context, id string) error

	// Session lookup
	GetByKey(ctx context.Context, key string) (*models.Session, error)
	GetOrCreate(ctx context.Context, key string, agentID string, channel models.ChannelType, channelID string) (*models.Session, error)
	List(ctx context.Context, agentID string, opts ListOptions) ([]*models.Session, error)

	// Message history
	AppendMessage(ctx context.Context, sessionID string, msg *models.Message) error
	GetHistory(ctx context.Context, sessionID string, limit int) ([]*models.Message, error)
}

// ListOptions configures session listing.
type ListOptions struct {
	Channel models.ChannelType
	Limit   int
	Offset  int
}

// SessionKey builds a unique session key.
func SessionKey(agentID string, channel models.ChannelType, channelID string) string {
	return agentID + ":" + string(channel) + ":" + channelID
}
