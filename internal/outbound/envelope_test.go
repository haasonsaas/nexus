package outbound

import (
	"encoding/json"
	"testing"
)

func TestBuildResultEnvelope(t *testing.T) {
	delivery := &OutboundDeliveryJSON{
		Channel:   "slack",
		Via:       DeliveryViaDirect,
		To:        "user@example.com",
		MessageID: "msg-123",
		MediaURL:  nil,
	}

	tests := []struct {
		name          string
		params        BuildResultEnvelopeParams
		wantFlattened bool
		check         func(t *testing.T, got BuildResultEnvelopeResult)
	}{
		{
			name: "flattens when only delivery and flatten is default",
			params: BuildResultEnvelopeParams{
				Delivery: delivery,
			},
			wantFlattened: true,
			check: func(t *testing.T, got BuildResultEnvelopeResult) {
				if got.Delivery == nil {
					t.Error("Delivery is nil, want non-nil")
				}
				if got.Envelope != nil {
					t.Error("Envelope is non-nil, want nil for flattened")
				}
			},
		},
		{
			name: "flattens when only delivery and flatten is true",
			params: BuildResultEnvelopeParams{
				Delivery:        delivery,
				FlattenDelivery: boolPtr(true),
			},
			wantFlattened: true,
			check: func(t *testing.T, got BuildResultEnvelopeResult) {
				if got.Delivery == nil {
					t.Error("Delivery is nil, want non-nil")
				}
			},
		},
		{
			name: "does not flatten when flatten is false",
			params: BuildResultEnvelopeParams{
				Delivery:        delivery,
				FlattenDelivery: boolPtr(false),
			},
			wantFlattened: false,
			check: func(t *testing.T, got BuildResultEnvelopeResult) {
				if got.Envelope == nil {
					t.Error("Envelope is nil, want non-nil")
				}
				if got.Envelope.Delivery == nil {
					t.Error("Envelope.Delivery is nil, want non-nil")
				}
			},
		},
		{
			name: "does not flatten when meta is present",
			params: BuildResultEnvelopeParams{
				Delivery: delivery,
				Meta:     map[string]any{"key": "value"},
			},
			wantFlattened: false,
			check: func(t *testing.T, got BuildResultEnvelopeResult) {
				if got.Envelope == nil {
					t.Error("Envelope is nil, want non-nil")
				}
				if got.Envelope.Meta == nil {
					t.Error("Envelope.Meta is nil, want non-nil")
				}
			},
		},
		{
			name: "does not flatten when payloads are present",
			params: BuildResultEnvelopeParams{
				Delivery: delivery,
				Payloads: []OutboundPayloadJSON{
					{Text: "Hello", MediaURL: nil},
				},
			},
			wantFlattened: false,
			check: func(t *testing.T, got BuildResultEnvelopeResult) {
				if got.Envelope == nil {
					t.Error("Envelope is nil, want non-nil")
				}
				if len(got.Envelope.Payloads) != 1 {
					t.Errorf("Payloads length = %d, want 1", len(got.Envelope.Payloads))
				}
			},
		},
		{
			name: "does not flatten when empty payloads slice is provided",
			params: BuildResultEnvelopeParams{
				Delivery: delivery,
				Payloads: []OutboundPayloadJSON{},
			},
			wantFlattened: false,
			check: func(t *testing.T, got BuildResultEnvelopeResult) {
				if got.Envelope == nil {
					t.Error("Envelope is nil, want non-nil")
				}
				if got.Envelope.Payloads == nil {
					t.Error("Envelope.Payloads is nil, want empty slice")
				}
			},
		},
		{
			name: "envelope with all fields",
			params: BuildResultEnvelopeParams{
				Delivery: delivery,
				Meta:     map[string]any{"version": 1},
				Payloads: []OutboundPayloadJSON{
					{Text: "First message", MediaURL: strPtr("https://example.com/1.png")},
					{Text: "Second message", MediaURL: nil, MediaURLs: []string{"https://example.com/2.png"}},
				},
				FlattenDelivery: boolPtr(false),
			},
			wantFlattened: false,
			check: func(t *testing.T, got BuildResultEnvelopeResult) {
				if got.Envelope == nil {
					t.Fatal("Envelope is nil, want non-nil")
				}
				if len(got.Envelope.Payloads) != 2 {
					t.Errorf("Payloads length = %d, want 2", len(got.Envelope.Payloads))
				}
				if got.Envelope.Meta == nil {
					t.Error("Meta is nil, want non-nil")
				}
				if got.Envelope.Delivery == nil {
					t.Error("Delivery is nil, want non-nil")
				}
			},
		},
		{
			name:          "empty params returns empty envelope",
			params:        BuildResultEnvelopeParams{},
			wantFlattened: false,
			check: func(t *testing.T, got BuildResultEnvelopeResult) {
				if got.Envelope == nil {
					t.Error("Envelope is nil, want non-nil")
				}
				if got.Envelope.Payloads != nil {
					t.Error("Envelope.Payloads is non-nil, want nil")
				}
				if got.Envelope.Meta != nil {
					t.Error("Envelope.Meta is non-nil, want nil")
				}
				if got.Envelope.Delivery != nil {
					t.Error("Envelope.Delivery is non-nil, want nil")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildResultEnvelope(tt.params)
			if got.IsFlattened() != tt.wantFlattened {
				t.Errorf("IsFlattened() = %v, want %v", got.IsFlattened(), tt.wantFlattened)
			}
			tt.check(t, got)
		})
	}
}

func TestBuildResultEnvelopeResult_IsFlattened(t *testing.T) {
	tests := []struct {
		name   string
		result BuildResultEnvelopeResult
		want   bool
	}{
		{
			name: "flattened - only delivery",
			result: BuildResultEnvelopeResult{
				Delivery: &OutboundDeliveryJSON{MessageID: "123"},
			},
			want: true,
		},
		{
			name: "not flattened - only envelope",
			result: BuildResultEnvelopeResult{
				Envelope: &OutboundResultEnvelope{},
			},
			want: false,
		},
		{
			name: "not flattened - both set (invalid state but should return false)",
			result: BuildResultEnvelopeResult{
				Envelope: &OutboundResultEnvelope{},
				Delivery: &OutboundDeliveryJSON{MessageID: "123"},
			},
			want: false,
		},
		{
			name:   "not flattened - both nil",
			result: BuildResultEnvelopeResult{},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.result.IsFlattened(); got != tt.want {
				t.Errorf("IsFlattened() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildResultEnvelopeResult_ToAny(t *testing.T) {
	delivery := &OutboundDeliveryJSON{
		Channel:   "test",
		Via:       DeliveryViaDirect,
		To:        "user",
		MessageID: "msg-1",
	}

	envelope := &OutboundResultEnvelope{
		Payloads: []OutboundPayloadJSON{{Text: "hello"}},
		Delivery: delivery,
	}

	t.Run("returns delivery when flattened", func(t *testing.T) {
		result := BuildResultEnvelopeResult{Delivery: delivery}
		got := result.ToAny()
		if got != delivery {
			t.Error("ToAny() did not return delivery")
		}
	})

	t.Run("returns envelope when not flattened", func(t *testing.T) {
		result := BuildResultEnvelopeResult{Envelope: envelope}
		got := result.ToAny()
		if got != envelope {
			t.Error("ToAny() did not return envelope")
		}
	})
}

func TestOutboundPayloadJSON_JSONMarshaling(t *testing.T) {
	tests := []struct {
		name    string
		payload OutboundPayloadJSON
		check   func(t *testing.T, data map[string]any)
	}{
		{
			name: "minimal payload",
			payload: OutboundPayloadJSON{
				Text:     "Hello world",
				MediaURL: nil,
			},
			check: func(t *testing.T, data map[string]any) {
				if data["text"] != "Hello world" {
					t.Errorf("text = %v, want %q", data["text"], "Hello world")
				}
				if data["mediaUrl"] != nil {
					t.Errorf("mediaUrl = %v, want nil", data["mediaUrl"])
				}
				if _, ok := data["mediaUrls"]; ok {
					t.Error("mediaUrls should be omitted when empty")
				}
			},
		},
		{
			name: "payload with media URL",
			payload: OutboundPayloadJSON{
				Text:     "Check this out",
				MediaURL: strPtr("https://example.com/image.png"),
			},
			check: func(t *testing.T, data map[string]any) {
				if data["mediaUrl"] != "https://example.com/image.png" {
					t.Errorf("mediaUrl = %v, want %q", data["mediaUrl"], "https://example.com/image.png")
				}
			},
		},
		{
			name: "payload with media URLs",
			payload: OutboundPayloadJSON{
				Text:      "Multiple images",
				MediaURL:  nil,
				MediaURLs: []string{"https://example.com/1.png", "https://example.com/2.png"},
			},
			check: func(t *testing.T, data map[string]any) {
				urls, ok := data["mediaUrls"].([]any)
				if !ok {
					t.Fatalf("mediaUrls is not an array: %T", data["mediaUrls"])
				}
				if len(urls) != 2 {
					t.Errorf("mediaUrls length = %d, want 2", len(urls))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonBytes, err := json.Marshal(tt.payload)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			var data map[string]any
			if err := json.Unmarshal(jsonBytes, &data); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}

			tt.check(t, data)
		})
	}
}

func TestOutboundResultEnvelope_JSONMarshaling(t *testing.T) {
	t.Run("envelope with all fields", func(t *testing.T) {
		envelope := OutboundResultEnvelope{
			Payloads: []OutboundPayloadJSON{
				{Text: "Hello", MediaURL: nil},
			},
			Meta: map[string]any{"key": "value"},
			Delivery: &OutboundDeliveryJSON{
				Channel:   "slack",
				Via:       DeliveryViaDirect,
				To:        "user",
				MessageID: "msg-1",
			},
		}

		jsonBytes, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}

		var data map[string]any
		if err := json.Unmarshal(jsonBytes, &data); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}

		if _, ok := data["payloads"]; !ok {
			t.Error("payloads field missing")
		}
		if _, ok := data["meta"]; !ok {
			t.Error("meta field missing")
		}
		if _, ok := data["delivery"]; !ok {
			t.Error("delivery field missing")
		}
	})

	t.Run("empty envelope omits empty fields", func(t *testing.T) {
		envelope := OutboundResultEnvelope{}

		jsonBytes, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}

		var data map[string]any
		if err := json.Unmarshal(jsonBytes, &data); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}

		if _, ok := data["payloads"]; ok {
			t.Error("payloads should be omitted when nil")
		}
		if _, ok := data["meta"]; ok {
			t.Error("meta should be omitted when nil")
		}
		if _, ok := data["delivery"]; ok {
			t.Error("delivery should be omitted when nil")
		}
	})
}

// Helper function
func boolPtr(b bool) *bool {
	return &b
}
