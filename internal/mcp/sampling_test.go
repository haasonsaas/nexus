package mcp

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"
)

type fakeTransport struct {
	requests  chan *JSONRPCRequest
	events    chan *JSONRPCNotification
	responses chan *JSONRPCResponse
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{
		requests:  make(chan *JSONRPCRequest, 1),
		events:    make(chan *JSONRPCNotification, 1),
		responses: make(chan *JSONRPCResponse, 1),
	}
}

func (f *fakeTransport) Connect(ctx context.Context) error { return nil }

func (f *fakeTransport) Close() error { return nil }

func (f *fakeTransport) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return nil, nil
}

func (f *fakeTransport) Notify(ctx context.Context, method string, params any) error { return nil }

func (f *fakeTransport) Events() <-chan *JSONRPCNotification { return f.events }

func (f *fakeTransport) Requests() <-chan *JSONRPCRequest { return f.requests }

func (f *fakeTransport) Respond(ctx context.Context, id any, result any, rpcErr *JSONRPCError) error {
	resp := &JSONRPCResponse{JSONRPC: "2.0", ID: id, Error: rpcErr}
	if result != nil {
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		resp.Result = data
	}
	f.responses <- resp
	return nil
}

func (f *fakeTransport) Connected() bool { return true }

func TestClientHandleSamplingResponds(t *testing.T) {
	transport := newFakeTransport()
	client := &Client{
		config:    &ServerConfig{ID: "server"},
		transport: transport,
		logger:    slog.Default(),
	}

	handler := func(ctx context.Context, req *SamplingRequest) (*SamplingResponse, error) {
		if len(req.Messages) != 1 {
			t.Fatalf("expected 1 message, got %d", len(req.Messages))
		}
		return &SamplingResponse{
			Role: "assistant",
			Content: MessageContent{
				Type: "text",
				Text: "ok",
			},
			Model: "test-model",
		}, nil
	}

	client.HandleSampling(handler)

	params := json.RawMessage(`{"messages":[{"role":"user","content":{"type":"text","text":"hello"}}],"maxTokens":5}`)
	transport.requests <- &JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "sampling/createMessage",
		Params:  params,
	}

	select {
	case resp := <-transport.responses:
		if resp.Error != nil {
			t.Fatalf("unexpected error response: %+v", resp.Error)
		}
		var payload SamplingResponse
		if err := json.Unmarshal(resp.Result, &payload); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if payload.Content.Text != "ok" {
			t.Fatalf("expected response text %q, got %q", "ok", payload.Content.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for sampling response")
	}
}
