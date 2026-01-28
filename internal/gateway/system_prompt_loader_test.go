package gateway

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
	ragcontext "github.com/haasonsaas/nexus/internal/rag/context"
	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

func TestReadPromptFileMissing(t *testing.T) {
	content, err := readPromptFile(filepath.Join(t.TempDir(), "missing.md"))
	if err != nil {
		t.Fatalf("readPromptFile() error = %v", err)
	}
	if content != "" {
		t.Fatalf("expected empty content, got %q", content)
	}
}

func TestReadPromptFileTrimmed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("\nhello\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	content, err := readPromptFile(path)
	if err != nil {
		t.Fatalf("readPromptFile() error = %v", err)
	}
	if content != "hello" {
		t.Fatalf("expected trimmed content, got %q", content)
	}
}

func TestLoadToolNotesCombinesInlineAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("file notes"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Notes:     "inline notes",
			NotesFile: path,
		},
	}
	server := &Server{config: cfg, logger: slog.Default()}

	notes := server.loadToolNotes()
	if !strings.Contains(notes, "inline notes") || !strings.Contains(notes, "file notes") {
		t.Fatalf("expected merged notes, got %q", notes)
	}
}

func TestLoadToolNotesUsesWorkspaceFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "TOOLS.md")
	if err := os.WriteFile(path, []byte("workspace tools"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := &config.Config{
		Workspace: config.WorkspaceConfig{
			Enabled:      true,
			Path:         dir,
			MaxChars:     100,
			ToolsFile:    "TOOLS.md",
			AgentsFile:   "AGENTS.md",
			SoulFile:     "SOUL.md",
			UserFile:     "USER.md",
			IdentityFile: "IDENTITY.md",
			MemoryFile:   "MEMORY.md",
		},
	}
	server := &Server{config: cfg, logger: slog.Default()}

	notes := server.loadToolNotes()
	if !strings.Contains(notes, "workspace tools") {
		t.Fatalf("expected workspace tool notes, got %q", notes)
	}
}

func TestLoadHeartbeatOnDemand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "heartbeat.md")
	if err := os.WriteFile(path, []byte("check status"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := &config.Config{
		Session: config.SessionConfig{
			Heartbeat: config.HeartbeatConfig{
				Enabled: true,
				File:    path,
				Mode:    "on_demand",
			},
		},
	}
	server := &Server{config: cfg, logger: slog.Default()}

	msg := &models.Message{Content: "hello"}
	if heartbeat := server.loadHeartbeat(msg); heartbeat != "" {
		t.Fatalf("expected heartbeat to be empty, got %q", heartbeat)
	}

	msg = &models.Message{Content: "heartbeat"}
	if heartbeat := server.loadHeartbeat(msg); !strings.Contains(heartbeat, "check status") {
		t.Fatalf("expected heartbeat content, got %q", heartbeat)
	}
}

type stubSessionStore struct {
	history []*models.Message
	updated bool
}

func (s *stubSessionStore) Create(ctx context.Context, session *models.Session) error { return nil }
func (s *stubSessionStore) Get(ctx context.Context, id string) (*models.Session, error) {
	return nil, nil
}
func (s *stubSessionStore) Update(ctx context.Context, session *models.Session) error {
	s.updated = true
	return nil
}
func (s *stubSessionStore) Delete(ctx context.Context, id string) error { return nil }
func (s *stubSessionStore) GetByKey(ctx context.Context, key string) (*models.Session, error) {
	return nil, nil
}
func (s *stubSessionStore) GetOrCreate(ctx context.Context, key string, agentID string, channel models.ChannelType, channelID string) (*models.Session, error) {
	return nil, nil
}
func (s *stubSessionStore) List(ctx context.Context, agentID string, opts sessions.ListOptions) ([]*models.Session, error) {
	return nil, nil
}
func (s *stubSessionStore) AppendMessage(ctx context.Context, sessionID string, msg *models.Message) error {
	return nil
}
func (s *stubSessionStore) GetHistory(ctx context.Context, sessionID string, limit int) ([]*models.Message, error) {
	if limit <= 0 || len(s.history) <= limit {
		return s.history, nil
	}
	return s.history[:limit], nil
}

func TestMemoryFlushPromptTriggers(t *testing.T) {
	cfg := &config.Config{
		Session: config.SessionConfig{
			MemoryFlush: config.MemoryFlushConfig{
				Enabled:   true,
				Threshold: 2,
				Prompt:    "flush now",
			},
		},
	}
	store := &stubSessionStore{
		history: []*models.Message{{Content: "a"}, {Content: "b"}},
	}
	server := &Server{config: cfg, logger: slog.Default(), sessions: store}

	session := &models.Session{ID: "session-1"}
	prompt := server.memoryFlushPrompt(context.Background(), session)
	if prompt == "" {
		t.Fatalf("expected memory flush prompt")
	}
	if !store.updated {
		t.Fatalf("expected session metadata update")
	}
	if session.Metadata == nil || session.Metadata["memory_flush_date"] == "" {
		t.Fatalf("expected memory_flush_date to be set")
	}
	if pending, ok := session.Metadata["memory_flush_pending"].(bool); !ok || !pending {
		t.Fatalf("expected memory_flush_pending to be true")
	}
}

func TestReadPromptFileLimited(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.md")
	if err := os.WriteFile(path, []byte("0123456789"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	content, err := readPromptFileLimited(path, 5)
	if err != nil {
		t.Fatalf("readPromptFileLimited() error = %v", err)
	}
	if !strings.HasPrefix(content, "01234") {
		t.Fatalf("expected truncated content, got %q", content)
	}
	if !strings.Contains(content, "truncated") {
		t.Fatalf("expected truncation marker, got %q", content)
	}
}

type stubRAGSearcher struct {
	results []*models.DocumentSearchResult
}

func (s stubRAGSearcher) Search(ctx context.Context, req *models.DocumentSearchRequest) (*models.DocumentSearchResponse, error) {
	return &models.DocumentSearchResponse{Results: s.results}, nil
}

func TestSystemPromptIncludesRAGContext(t *testing.T) {
	chunk := &models.DocumentChunk{
		Content: "RAG context content",
		Metadata: models.ChunkMetadata{
			DocumentName: "TestDoc",
		},
	}
	searcher := stubRAGSearcher{
		results: []*models.DocumentSearchResult{
			{Chunk: chunk, Score: 0.9},
		},
	}
	injectorCfg := ragcontext.DefaultInjectorConfig()
	injectorCfg.Enabled = true
	injectorCfg.MaxChunks = 1
	injectorCfg.MinScore = 0.0
	injector := ragcontext.NewInjectorWithSearcher(searcher, injectorCfg)

	cfg := &config.Config{
		RAG: config.RAGConfig{
			ContextInjection: config.RAGContextInjectionConfig{
				Enabled: true,
			},
		},
	}
	server := &Server{config: cfg, logger: slog.Default(), ragInjector: injector}

	session := &models.Session{ID: "session-1", AgentID: "main"}
	msg := &models.Message{Content: "Find context"}

	prompt, _ := server.systemPromptForMessage(context.Background(), session, msg)
	if !strings.Contains(prompt, "RAG context content") {
		t.Fatalf("expected RAG context in prompt, got %q", prompt)
	}
}

func TestSystemPromptIncludesLinkContext(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			Links: config.LinksConfig{
				Enabled:  true,
				MaxLinks: 5,
			},
		},
	}
	server := &Server{config: cfg, logger: slog.Default()}

	session := &models.Session{ID: "session-1", AgentID: "main"}
	msg := &models.Message{Content: "Check https://example.com"}

	prompt, _ := server.systemPromptForMessage(context.Background(), session, msg)
	if !strings.Contains(prompt, "https://example.com") {
		t.Fatalf("expected link context in prompt, got %q", prompt)
	}
}

func TestLoadWorkspaceSectionsFromConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Do the thing"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SOUL.md"), []byte("Be kind"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("Remember this"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg := &config.Config{
		Workspace: config.WorkspaceConfig{
			Enabled:      true,
			Path:         dir,
			MaxChars:     100,
			AgentsFile:   "AGENTS.md",
			SoulFile:     "SOUL.md",
			UserFile:     "USER.md",
			IdentityFile: "IDENTITY.md",
			MemoryFile:   "MEMORY.md",
		},
	}

	sections, err := loadWorkspaceSectionsFromConfig(cfg)
	if err != nil {
		t.Fatalf("loadWorkspaceSectionsFromConfig() error = %v", err)
	}
	if len(sections) != 3 {
		t.Fatalf("expected 3 sections, got %d", len(sections))
	}
	if sections[0].Label != "Workspace instructions" {
		t.Fatalf("expected workspace instructions first, got %q", sections[0].Label)
	}
	if sections[1].Label != "Persona and boundaries" {
		t.Fatalf("expected persona second, got %q", sections[1].Label)
	}
	if sections[2].Label != "Workspace memory" {
		t.Fatalf("expected workspace memory third, got %q", sections[2].Label)
	}
}
