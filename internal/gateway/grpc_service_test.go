package gateway

import (
	"context"
	"testing"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/sessions"
	proto "github.com/haasonsaas/nexus/pkg/proto"
)

func TestSessionServiceLifecycle(t *testing.T) {
	server := &Server{config: &config.Config{Session: config.SessionConfig{DefaultAgentID: "main"}}}
	server.sessions = sessions.NewMemoryStore()

	service := newGRPCService(server)

	createResp, err := service.CreateSession(context.Background(), &proto.CreateSessionRequest{
		AgentId:   "main",
		Channel:   proto.ChannelType_CHANNEL_TYPE_API,
		ChannelId: "user-1",
		Title:     "hello",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if createResp.Session == nil || createResp.Session.Id == "" {
		t.Fatalf("expected session id")
	}

	getResp, err := service.GetSession(context.Background(), &proto.GetSessionRequest{Id: createResp.Session.Id})
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if getResp.Session.Title != "hello" {
		t.Fatalf("expected title")
	}

	updateResp, err := service.UpdateSession(context.Background(), &proto.UpdateSessionRequest{Id: createResp.Session.Id, Title: "updated"})
	if err != nil {
		t.Fatalf("UpdateSession() error = %v", err)
	}
	if updateResp.Session.Title != "updated" {
		t.Fatalf("expected updated title")
	}

	listResp, err := service.ListSessions(context.Background(), &proto.ListSessionsRequest{AgentId: "main", PageSize: 10})
	if err != nil {
		t.Fatalf("ListSessions() error = %v", err)
	}
	if len(listResp.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(listResp.Sessions))
	}

	deleteResp, err := service.DeleteSession(context.Background(), &proto.DeleteSessionRequest{Id: createResp.Session.Id})
	if err != nil {
		t.Fatalf("DeleteSession() error = %v", err)
	}
	if !deleteResp.Success {
		t.Fatalf("expected delete success")
	}
}
