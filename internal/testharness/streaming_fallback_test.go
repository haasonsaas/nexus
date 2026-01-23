package testharness_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haasonsaas/nexus/internal/testharness"
	"github.com/haasonsaas/nexus/pkg/models"
)

// MockStreamingOutput simulates streaming output handling.
type MockStreamingOutput struct {
	chunks           []string
	finalOutput      string
	streamingEnabled bool
	fallbackTriggered atomic.Bool
	errorOnChunk     int // -1 means no error
	updateCalls      int
	finalCalls       int
}

func NewMockStreamingOutput() *MockStreamingOutput {
	return &MockStreamingOutput{
		chunks:           make([]string, 0),
		streamingEnabled: true,
		errorOnChunk:     -1,
	}
}

func (m *MockStreamingOutput) OnChunk(chunk string) error {
	if m.errorOnChunk >= 0 && len(m.chunks) >= m.errorOnChunk {
		m.fallbackTriggered.Store(true)
		return errors.New("streaming error")
	}
	m.chunks = append(m.chunks, chunk)
	m.updateCalls++
	return nil
}

func (m *MockStreamingOutput) OnComplete(content string) {
	m.finalOutput = content
	m.finalCalls++
}

func (m *MockStreamingOutput) IsFallbackTriggered() bool {
	return m.fallbackTriggered.Load()
}

func (m *MockStreamingOutput) Reset() {
	m.chunks = make([]string, 0)
	m.finalOutput = ""
	m.fallbackTriggered.Store(false)
	m.updateCalls = 0
	m.finalCalls = 0
}

// TestStreamingFallback_NormalOperation tests normal streaming without errors.
func TestStreamingFallback_NormalOperation(t *testing.T) {
	output := NewMockStreamingOutput()

	chunks := []string{"Hello, ", "how ", "can I ", "help ", "you?"}
	for _, chunk := range chunks {
		if err := output.OnChunk(chunk); err != nil {
			t.Fatalf("OnChunk() error = %v", err)
		}
	}

	fullContent := strings.Join(chunks, "")
	output.OnComplete(fullContent)

	if output.IsFallbackTriggered() {
		t.Error("fallback should not be triggered in normal operation")
	}

	if output.updateCalls != len(chunks) {
		t.Errorf("expected %d update calls, got %d", len(chunks), output.updateCalls)
	}

	if output.finalCalls != 1 {
		t.Errorf("expected 1 final call, got %d", output.finalCalls)
	}

	if output.finalOutput != fullContent {
		t.Errorf("expected final output %q, got %q", fullContent, output.finalOutput)
	}
}

// TestStreamingFallback_ErrorTriggersFallback tests fallback on streaming error.
func TestStreamingFallback_ErrorTriggersFallback(t *testing.T) {
	output := NewMockStreamingOutput()
	output.errorOnChunk = 2 // Error on 3rd chunk

	chunks := []string{"Hello, ", "world", "!"}

	for i, chunk := range chunks {
		err := output.OnChunk(chunk)
		if i >= output.errorOnChunk {
			if err == nil {
				t.Fatalf("expected error on chunk %d", i)
			}
			break
		}
	}

	if !output.IsFallbackTriggered() {
		t.Error("expected fallback to be triggered on error")
	}

	// After fallback, should deliver content non-streaming
	fullContent := strings.Join(chunks, "")
	output.OnComplete(fullContent)

	if output.finalOutput != fullContent {
		t.Errorf("expected final output %q, got %q", fullContent, output.finalOutput)
	}
}

// StreamingCapabilities defines what streaming features a channel supports.
type StreamingCapabilities struct {
	SupportsStreaming     bool
	SupportsMessageEdit   bool
	MaxChunksBeforeEdit   int
	RecommendedChunkDelay time.Duration
}

// channelCapabilities maps channel types to their streaming capabilities.
var channelCapabilities = map[models.ChannelType]StreamingCapabilities{
	models.ChannelSlack: {
		SupportsStreaming:     true,
		SupportsMessageEdit:   true,
		MaxChunksBeforeEdit:   10,
		RecommendedChunkDelay: 500 * time.Millisecond,
	},
	models.ChannelDiscord: {
		SupportsStreaming:     true,
		SupportsMessageEdit:   true,
		MaxChunksBeforeEdit:   5,
		RecommendedChunkDelay: 1 * time.Second,
	},
	models.ChannelTelegram: {
		SupportsStreaming:     true,
		SupportsMessageEdit:   true,
		MaxChunksBeforeEdit:   20,
		RecommendedChunkDelay: 300 * time.Millisecond,
	},
	models.ChannelWhatsApp: {
		SupportsStreaming:     false, // WhatsApp doesn't support message editing
		SupportsMessageEdit:   false,
		MaxChunksBeforeEdit:   0,
		RecommendedChunkDelay: 0,
	},
	models.ChannelSignal: {
		SupportsStreaming:     false, // Signal doesn't support message editing
		SupportsMessageEdit:   false,
		MaxChunksBeforeEdit:   0,
		RecommendedChunkDelay: 0,
	},
	models.ChannelAPI: {
		SupportsStreaming:     true,
		SupportsMessageEdit:   true,
		MaxChunksBeforeEdit:   100,
		RecommendedChunkDelay: 50 * time.Millisecond,
	},
}

// TestStreamingFallback_ByChannelType tests streaming behavior per channel type.
func TestStreamingFallback_ByChannelType(t *testing.T) {
	g := testharness.NewGoldenAt(t, "testdata/golden/streaming")

	testCases := []struct {
		channel      models.ChannelType
		expectStream bool
	}{
		{models.ChannelSlack, true},
		{models.ChannelDiscord, true},
		{models.ChannelTelegram, true},
		{models.ChannelWhatsApp, false},
		{models.ChannelSignal, false},
		{models.ChannelAPI, true},
	}

	var results strings.Builder
	results.WriteString("Channel Streaming Capabilities:\n\n")

	for _, tc := range testCases {
		t.Run(string(tc.channel), func(t *testing.T) {
			caps, ok := channelCapabilities[tc.channel]
			if !ok {
				t.Skipf("no capabilities defined for %s", tc.channel)
				return
			}

			if caps.SupportsStreaming != tc.expectStream {
				t.Errorf("expected streaming=%v for %s, got %v", tc.expectStream, tc.channel, caps.SupportsStreaming)
			}

			results.WriteString(formatCapabilities(tc.channel, caps))
		})
	}

	g.AssertNamed("capabilities", results.String())
}

func formatCapabilities(channel models.ChannelType, caps StreamingCapabilities) string {
	var sb strings.Builder
	sb.WriteString(string(channel))
	sb.WriteString(":\n")
	sb.WriteString("  streaming: ")
	if caps.SupportsStreaming {
		sb.WriteString("yes\n")
		sb.WriteString("  message_edit: ")
		if caps.SupportsMessageEdit {
			sb.WriteString("yes\n")
		} else {
			sb.WriteString("no\n")
		}
		sb.WriteString("  max_chunks: ")
		sb.WriteString(itoa(caps.MaxChunksBeforeEdit))
		sb.WriteString("\n  chunk_delay: ")
		sb.WriteString(caps.RecommendedChunkDelay.String())
		sb.WriteString("\n")
	} else {
		sb.WriteString("no (immediate fallback to non-streaming)\n")
	}
	sb.WriteString("\n")
	return sb.String()
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

// TestStreamingFallback_RateLimiting tests streaming with rate limiting.
func TestStreamingFallback_RateLimiting(t *testing.T) {
	output := NewMockStreamingOutput()
	caps := channelCapabilities[models.ChannelSlack]

	chunks := []string{"One", " Two", " Three", " Four", " Five", " Six", " Seven", " Eight", " Nine", " Ten", " Eleven"}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	chunksDelivered := 0
	for _, chunk := range chunks {
		select {
		case <-ctx.Done():
			t.Fatal("timeout waiting for chunk delivery")
		default:
			// Check rate limit before delivering
			if chunksDelivered >= caps.MaxChunksBeforeEdit {
				// Should batch remaining into final
				break
			}

			if err := output.OnChunk(chunk); err != nil {
				t.Fatalf("OnChunk() error = %v", err)
			}
			chunksDelivered++
		}
	}

	// Verify we respect max chunks
	if chunksDelivered > caps.MaxChunksBeforeEdit {
		t.Errorf("delivered %d chunks, max should be %d", chunksDelivered, caps.MaxChunksBeforeEdit)
	}
}

// TestStreamingFallback_WhatsAppImmediateFallback tests immediate fallback for non-streaming channels.
func TestStreamingFallback_WhatsAppImmediateFallback(t *testing.T) {
	caps := channelCapabilities[models.ChannelWhatsApp]

	if caps.SupportsStreaming {
		t.Fatal("WhatsApp should not support streaming")
	}

	// For non-streaming channels, we should immediately deliver complete content
	// without any intermediate updates
	output := NewMockStreamingOutput()

	// Simulate what should happen for non-streaming channel
	fullContent := "Hello, this is a complete message that should be delivered at once."

	// No streaming chunks should be delivered
	// Just the final complete message
	output.OnComplete(fullContent)

	if output.updateCalls != 0 {
		t.Errorf("expected 0 update calls for non-streaming channel, got %d", output.updateCalls)
	}

	if output.finalOutput != fullContent {
		t.Errorf("expected final output %q, got %q", fullContent, output.finalOutput)
	}
}

// TestStreamingFallback_APIHighThroughput tests API streaming with high throughput.
func TestStreamingFallback_APIHighThroughput(t *testing.T) {
	caps := channelCapabilities[models.ChannelAPI]

	if !caps.SupportsStreaming {
		t.Fatal("API should support streaming")
	}

	output := NewMockStreamingOutput()

	// API channel can handle many more chunks
	chunks := make([]string, caps.MaxChunksBeforeEdit)
	for i := range chunks {
		chunks[i] = "chunk"
	}

	for _, chunk := range chunks {
		if err := output.OnChunk(chunk); err != nil {
			t.Fatalf("OnChunk() error = %v", err)
		}
	}

	if output.updateCalls != caps.MaxChunksBeforeEdit {
		t.Errorf("expected %d chunks for API, got %d", caps.MaxChunksBeforeEdit, output.updateCalls)
	}
}
