package storage

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/haasonsaas/nexus/internal/auth"
	"github.com/haasonsaas/nexus/pkg/models"
)

// MemoryAgentStore provides an in-memory AgentStore.
type MemoryAgentStore struct {
	mu     sync.RWMutex
	agents map[string]*models.Agent
}

// NewMemoryAgentStore creates an in-memory agent store.
func NewMemoryAgentStore() *MemoryAgentStore {
	return &MemoryAgentStore{agents: make(map[string]*models.Agent)}
}

func (s *MemoryAgentStore) Create(ctx context.Context, agent *models.Agent) error {
	if agent == nil || agent.ID == "" {
		return fmt.Errorf("agent is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.agents[agent.ID]; exists {
		return ErrAlreadyExists
	}
	s.agents[agent.ID] = agent
	return nil
}

func (s *MemoryAgentStore) Get(ctx context.Context, id string) (*models.Agent, error) {
	if id == "" {
		return nil, ErrNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	agent, ok := s.agents[id]
	if !ok {
		return nil, ErrNotFound
	}
	return agent, nil
}

func (s *MemoryAgentStore) List(ctx context.Context, userID string, limit, offset int) ([]*models.Agent, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	agents := make([]*models.Agent, 0, len(s.agents))
	for _, agent := range s.agents {
		if userID != "" && agent.UserID != userID {
			continue
		}
		agents = append(agents, agent)
	}
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].CreatedAt.After(agents[j].CreatedAt)
	})
	return paginateAgents(agents, limit, offset), len(agents), nil
}

func paginateAgents(agents []*models.Agent, limit, offset int) []*models.Agent {
	if offset < 0 {
		offset = 0
	}
	if offset > len(agents) {
		offset = len(agents)
	}
	end := len(agents)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return agents[offset:end]
}

func (s *MemoryAgentStore) Update(ctx context.Context, agent *models.Agent) error {
	if agent == nil || agent.ID == "" {
		return fmt.Errorf("agent is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.agents[agent.ID]; !exists {
		return ErrNotFound
	}
	s.agents[agent.ID] = agent
	return nil
}

func (s *MemoryAgentStore) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.agents[id]; !exists {
		return ErrNotFound
	}
	delete(s.agents, id)
	return nil
}

// MemoryChannelConnectionStore provides an in-memory ChannelConnectionStore.
type MemoryChannelConnectionStore struct {
	mu          sync.RWMutex
	connections map[string]*models.ChannelConnection
}

// NewMemoryChannelConnectionStore creates an in-memory channel connection store.
func NewMemoryChannelConnectionStore() *MemoryChannelConnectionStore {
	return &MemoryChannelConnectionStore{connections: make(map[string]*models.ChannelConnection)}
}

func (s *MemoryChannelConnectionStore) Create(ctx context.Context, conn *models.ChannelConnection) error {
	if conn == nil || conn.ID == "" {
		return fmt.Errorf("connection is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.connections[conn.ID]; exists {
		return ErrAlreadyExists
	}
	s.connections[conn.ID] = conn
	return nil
}

func (s *MemoryChannelConnectionStore) Get(ctx context.Context, id string) (*models.ChannelConnection, error) {
	if id == "" {
		return nil, ErrNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	conn, ok := s.connections[id]
	if !ok {
		return nil, ErrNotFound
	}
	return conn, nil
}

func (s *MemoryChannelConnectionStore) List(ctx context.Context, userID string, limit, offset int) ([]*models.ChannelConnection, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	connections := make([]*models.ChannelConnection, 0, len(s.connections))
	for _, conn := range s.connections {
		if userID != "" && conn.UserID != userID {
			continue
		}
		connections = append(connections, conn)
	}
	sort.Slice(connections, func(i, j int) bool {
		return connections[i].ConnectedAt.After(connections[j].ConnectedAt)
	})
	return paginateConnections(connections, limit, offset), len(connections), nil
}

func paginateConnections(connections []*models.ChannelConnection, limit, offset int) []*models.ChannelConnection {
	if offset < 0 {
		offset = 0
	}
	if offset > len(connections) {
		offset = len(connections)
	}
	end := len(connections)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return connections[offset:end]
}

func (s *MemoryChannelConnectionStore) Update(ctx context.Context, conn *models.ChannelConnection) error {
	if conn == nil || conn.ID == "" {
		return fmt.Errorf("connection is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.connections[conn.ID]; !exists {
		return ErrNotFound
	}
	s.connections[conn.ID] = conn
	return nil
}

func (s *MemoryChannelConnectionStore) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.connections[id]; !exists {
		return ErrNotFound
	}
	delete(s.connections, id)
	return nil
}

// MemoryUserStore provides an in-memory UserStore.
type MemoryUserStore struct {
	mu              sync.RWMutex
	users           map[string]*models.User
	usersByEmail    map[string]string
	usersByProvider map[string]string
}

// NewMemoryUserStore creates an in-memory user store.
func NewMemoryUserStore() *MemoryUserStore {
	return &MemoryUserStore{
		users:           make(map[string]*models.User),
		usersByEmail:    make(map[string]string),
		usersByProvider: make(map[string]string),
	}
}

func (s *MemoryUserStore) FindOrCreate(ctx context.Context, info *auth.UserInfo) (*models.User, error) {
	if info == nil {
		return nil, fmt.Errorf("user info is required")
	}
	provider := strings.ToLower(strings.TrimSpace(info.Provider))
	providerKey := ""
	if provider != "" && info.ID != "" {
		providerKey = provider + "|" + info.ID
	}
	email := strings.ToLower(strings.TrimSpace(info.Email))

	s.mu.Lock()
	defer s.mu.Unlock()
	if providerKey != "" {
		if id, ok := s.usersByProvider[providerKey]; ok {
			user := s.users[id]
			if info.Name != "" {
				user.Name = info.Name
			}
			if info.AvatarURL != "" {
				user.AvatarURL = info.AvatarURL
			}
			user.Provider = provider
			user.ProviderID = info.ID
			user.UpdatedAt = time.Now()
			return user, nil
		}
	}
	if email != "" {
		if id, ok := s.usersByEmail[email]; ok {
			user := s.users[id]
			if info.Name != "" {
				user.Name = info.Name
			}
			if info.AvatarURL != "" {
				user.AvatarURL = info.AvatarURL
			}
			if provider != "" && info.ID != "" {
				user.Provider = provider
				user.ProviderID = info.ID
			}
			user.UpdatedAt = time.Now()
			if providerKey != "" {
				s.usersByProvider[providerKey] = user.ID
			}
			return user, nil
		}
	}

	user := &models.User{
		ID:         uuid.NewString(),
		Email:      email,
		Name:       info.Name,
		AvatarURL:  info.AvatarURL,
		Provider:   provider,
		ProviderID: info.ID,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	s.users[user.ID] = user
	if email != "" {
		s.usersByEmail[email] = user.ID
	}
	if providerKey != "" {
		s.usersByProvider[providerKey] = user.ID
	}

	return user, nil
}

func (s *MemoryUserStore) Get(ctx context.Context, id string) (*models.User, error) {
	if id == "" {
		return nil, ErrNotFound
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.users[id]
	if !ok {
		return nil, ErrNotFound
	}
	return user, nil
}

// NewMemoryStores constructs a StoreSet backed by memory.
func NewMemoryStores() StoreSet {
	return StoreSet{
		Agents:   NewMemoryAgentStore(),
		Channels: NewMemoryChannelConnectionStore(),
		Users:    NewMemoryUserStore(),
	}
}
