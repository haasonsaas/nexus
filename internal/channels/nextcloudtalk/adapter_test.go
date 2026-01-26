package nextcloudtalk

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleWebhookRejectsLargeBody(t *testing.T) {
	t.Parallel()

	a, err := NewAdapter(Config{
		BaseURL:   "http://example.com",
		BotSecret: "secret",
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	a.ctx = context.Background()

	req := httptest.NewRequest(http.MethodPost, "/nextcloud-talk-webhook", bytes.NewReader(bytes.Repeat([]byte("a"), maxWebhookBodyBytes+1)))
	rec := httptest.NewRecorder()

	a.handleWebhook(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestHandleWebhookAcceptsValidSignature(t *testing.T) {
	t.Parallel()

	cfg := Config{
		BaseURL:   "http://example.com",
		BotSecret: "secret",
	}
	a, err := NewAdapter(cfg)
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	a.ctx = context.Background()

	payload := WebhookPayload{
		Type: "Create",
		Actor: PayloadActor{
			Type: "users",
			ID:   "user-1",
			Name: "Alice",
		},
		Object: PayloadObject{
			Type:    "message",
			ID:      "msg-1",
			Content: "hello",
		},
		Target: PayloadTarget{
			Type: "room",
			ID:   "room-1",
			Name: "Room",
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	random := "random"
	mac := hmac.New(sha256.New, []byte(cfg.BotSecret))
	mac.Write([]byte(random))
	mac.Write(body)
	signature := hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest(http.MethodPost, "/nextcloud-talk-webhook", bytes.NewReader(body))
	req.Header.Set("X-Nextcloud-Talk-Signature", signature)
	req.Header.Set("X-Nextcloud-Talk-Random", random)
	rec := httptest.NewRecorder()

	a.handleWebhook(rec, req)

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
