package zalo

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleWebhookRejectsLargeBody(t *testing.T) {
	t.Parallel()

	a, err := NewZaloAdapter(ZaloConfig{Token: "token"})
	if err != nil {
		t.Fatalf("NewZaloAdapter: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webhook/zalo", bytes.NewReader(bytes.Repeat([]byte("a"), DefaultMaxWebhookBodyBytes+1)))
	rec := httptest.NewRecorder()

	a.HandleWebhook(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestHandleWebhookAcceptsValidPayload(t *testing.T) {
	t.Parallel()

	a, err := NewZaloAdapter(ZaloConfig{Token: "token"})
	if err != nil {
		t.Fatalf("NewZaloAdapter: %v", err)
	}

	update := ZaloUpdate{
		EventName: "message.text.received",
		Message: &ZaloMessage{
			MessageID: "msg-1",
			From: ZaloSender{
				ID:   "user-1",
				Name: "Alice",
			},
			Chat: ZaloChat{
				ID:       "chat-1",
				ChatType: "PRIVATE",
			},
			Date: 1,
			Text: "hello",
		},
	}
	body, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webhook/zalo", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	a.HandleWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	select {
	case msg := <-a.Messages():
		if msg == nil {
			t.Fatalf("message is nil")
		}
		if msg.Content != "hello" {
			t.Fatalf("content = %q, want %q", msg.Content, "hello")
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for message")
	}
}
