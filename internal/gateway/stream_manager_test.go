package gateway

import (
	"context"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/pkg/models"
)

func TestStreamManager_OnTextThrottlesUpdates(t *testing.T) {
	streaming := &mockStreamingAdapter{}
	outbound := &mockOutboundAdapter{}
	manager := NewStreamManager(StreamingBehavior{
		Mode:           StreamingRealTime,
		UpdateInterval: 10 * time.Millisecond,
	}, streaming, outbound)

	msg := &models.Message{}
	ctx := context.Background()

	handled, err := manager.OnText(ctx, msg, "Hello")
	if err != nil || !handled {
		t.Fatalf("expected first chunk handled, err=%v handled=%v", err, handled)
	}

	handled, err = manager.OnText(ctx, msg, " world")
	if err != nil || !handled {
		t.Fatalf("expected second chunk handled, err=%v handled=%v", err, handled)
	}
	if streaming.updateCalls != 0 {
		t.Fatalf("expected throttled update, got %d updates", streaming.updateCalls)
	}

	time.Sleep(15 * time.Millisecond)

	handled, err = manager.OnText(ctx, msg, "!")
	if err != nil || !handled {
		t.Fatalf("expected third chunk handled, err=%v handled=%v", err, handled)
	}
	if streaming.updateCalls == 0 {
		t.Fatalf("expected update after throttle window")
	}
	if streaming.lastContent != "Hello world!" {
		t.Fatalf("unexpected streamed content: %q", streaming.lastContent)
	}
}

func TestStreamManager_FinalizeFallsBackOnUpdateError(t *testing.T) {
	streaming := &mockStreamingAdapter{updateErr: errTest}
	outbound := &mockOutboundAdapter{}
	manager := NewStreamManager(StreamingBehavior{Mode: StreamingRealTime}, streaming, outbound)

	msg := &models.Message{}
	ctx := context.Background()

	if _, err := manager.OnText(ctx, msg, "Hello"); err != nil {
		t.Fatalf("OnText error: %v", err)
	}

	if err := manager.Finalize(ctx, msg, "Hello"); err != nil {
		t.Fatalf("Finalize error: %v", err)
	}
	if outbound.sendCalls != 1 {
		t.Fatalf("expected fallback send, got %d", outbound.sendCalls)
	}
}

func TestStreamManager_FinalizeAfterStartFailure(t *testing.T) {
	streaming := &mockStreamingAdapter{startErr: errTest}
	outbound := &mockOutboundAdapter{}
	manager := NewStreamManager(StreamingBehavior{Mode: StreamingRealTime}, streaming, outbound)

	msg := &models.Message{}
	ctx := context.Background()

	handled, err := manager.OnText(ctx, msg, "Hello")
	if err != nil {
		t.Fatalf("OnText error: %v", err)
	}
	if handled {
		t.Fatalf("expected fallback when start fails")
	}

	if err := manager.Finalize(ctx, msg, "Hello"); err != nil {
		t.Fatalf("Finalize error: %v", err)
	}
	if outbound.sendCalls != 1 {
		t.Fatalf("expected outbound send after start failure, got %d", outbound.sendCalls)
	}
}

var errTest = &testError{}

type testError struct{}

func (e *testError) Error() string { return "test error" }
