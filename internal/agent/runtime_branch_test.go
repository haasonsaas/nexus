package agent

import (
	"context"
	"testing"

	"github.com/haasonsaas/nexus/internal/sessions"
	"github.com/haasonsaas/nexus/pkg/models"
)

func TestRuntimePersistsToBranchStore(t *testing.T) {
	sessionStore := sessions.NewMemoryStore()
	branchStore := sessions.NewMemoryBranchStore()

	runtime := NewRuntime(stubProvider{}, sessionStore)
	runtime.SetBranchStore(branchStore)

	session := &models.Session{
		ID:        "session-1",
		AgentID:   "agent-1",
		Channel:   models.ChannelAPI,
		ChannelID: "channel-1",
	}
	msg := &models.Message{Role: models.RoleUser, Content: "hello"}

	ctx := context.Background()
	chunks, err := runtime.Process(ctx, session, msg)
	if err != nil {
		t.Fatalf("Process error: %v", err)
	}
	for range chunks {
	}

	branch, err := branchStore.GetPrimaryBranch(ctx, session.ID)
	if err != nil {
		t.Fatalf("expected primary branch: %v", err)
	}

	history, err := branchStore.GetBranchHistory(ctx, branch.ID, 10)
	if err != nil {
		t.Fatalf("GetBranchHistory error: %v", err)
	}
	if len(history) == 0 {
		t.Fatal("expected branch history to include messages")
	}
	if history[0].BranchID != branch.ID {
		t.Fatalf("expected BranchID %q, got %q", branch.ID, history[0].BranchID)
	}
}
