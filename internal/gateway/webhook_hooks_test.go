package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebhookHooksRejectsLargeBody(t *testing.T) {
	t.Parallel()

	hooks, err := NewWebhookHooks(&WebhookConfig{
		Enabled:      true,
		Token:        "token",
		MaxBodyBytes: 10,
		Mappings: []WebhookMapping{
			{
				Path:    "foo",
				Handler: webhookHandlerCustom,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewWebhookHooks: %v", err)
	}

	hooks.RegisterHandler(webhookHandlerCustom, WebhookHandlerFunc(func(ctx context.Context, payload *WebhookPayload, mapping *WebhookMapping) (*WebhookResponse, error) {
		return &WebhookResponse{OK: true}, nil
	}))

	req := httptest.NewRequest(http.MethodPost, "/hooks/foo", bytes.NewReader(bytes.Repeat([]byte("a"), 11)))
	req.Header.Set("X-Webhook-Token", "token")
	rec := httptest.NewRecorder()

	hooks.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestWebhookHooksAcceptsValidPayload(t *testing.T) {
	t.Parallel()

	hooks, err := NewWebhookHooks(&WebhookConfig{
		Enabled: true,
		Token:   "token",
		Mappings: []WebhookMapping{
			{
				Path:    "foo",
				Handler: webhookHandlerCustom,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewWebhookHooks: %v", err)
	}

	var got *WebhookPayload
	hooks.RegisterHandler(webhookHandlerCustom, WebhookHandlerFunc(func(ctx context.Context, payload *WebhookPayload, mapping *WebhookMapping) (*WebhookResponse, error) {
		got = payload
		return &WebhookResponse{OK: true}, nil
	}))

	body, err := json.Marshal(&WebhookPayload{Message: "hi"})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/hooks/foo", bytes.NewReader(body))
	req.Header.Set("X-Webhook-Token", "token")
	rec := httptest.NewRecorder()

	hooks.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got == nil || got.Message != "hi" {
		t.Fatalf("payload.message = %#v, want %q", got, "hi")
	}
}
