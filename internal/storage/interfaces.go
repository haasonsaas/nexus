package storage

import (
	"context"
	"errors"

	"github.com/haasonsaas/nexus/internal/auth"
	"github.com/haasonsaas/nexus/pkg/models"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
)

// AgentStore persists agent configurations.
type AgentStore interface {
	Create(ctx context.Context, agent *models.Agent) error
	Get(ctx context.Context, id string) (*models.Agent, error)
	List(ctx context.Context, userID string, limit, offset int) ([]*models.Agent, int, error)
	Update(ctx context.Context, agent *models.Agent) error
	Delete(ctx context.Context, id string) error
}

// ChannelConnectionStore persists channel connection records.
type ChannelConnectionStore interface {
	Create(ctx context.Context, conn *models.ChannelConnection) error
	Get(ctx context.Context, id string) (*models.ChannelConnection, error)
	List(ctx context.Context, userID string, limit, offset int) ([]*models.ChannelConnection, int, error)
	Update(ctx context.Context, conn *models.ChannelConnection) error
	Delete(ctx context.Context, id string) error
}

// UserStore persists user identities (OAuth and API users).
type UserStore interface {
	FindOrCreate(ctx context.Context, info *auth.UserInfo) (*models.User, error)
	Get(ctx context.Context, id string) (*models.User, error)
}

// StoreSet groups storage dependencies.
type StoreSet struct {
	Agents   AgentStore
	Channels ChannelConnectionStore
	Users    UserStore
	closer   func() error
}

// Close closes any underlying resources.
func (s StoreSet) Close() error {
	if s.closer == nil {
		return nil
	}
	return s.closer()
}
