package bluebubbles

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

	a, err := NewBlueBubblesAdapter(BlueBubblesConfig{
		ServerURL: "http://example.com",
		Password:  "pw",
	})
	if err != nil {
		t.Fatalf("NewBlueBubblesAdapter: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webhook/bluebubbles", bytes.NewReader(bytes.Repeat([]byte("a"), DefaultMaxWebhookBodyBytes+1)))
	rec := httptest.NewRecorder()

	a.HandleWebhook(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestHandleWebhookAcceptsValidPayload(t *testing.T) {
	t.Parallel()

	a, err := NewBlueBubblesAdapter(BlueBubblesConfig{
		ServerURL: "http://example.com",
		Password:  "pw",
	})
	if err != nil {
		t.Fatalf("NewBlueBubblesAdapter: %v", err)
	}

	payload := WebhookPayload{
		Type: "new-message",
		Data: &WebhookMessage{
			GUID:     "guid-1",
			Text:     "hello",
			SenderID: "sender-1",
			ChatGUID: "iMessage;-;sender-1",
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webhook/bluebubbles", bytes.NewReader(body))
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
