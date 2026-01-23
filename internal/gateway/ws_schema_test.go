package gateway

import (
	"encoding/json"
	"testing"
)

func TestInitWSSchemas(t *testing.T) {
	// Should not error on init
	err := initWSSchemas()
	if err != nil {
		t.Errorf("initWSSchemas() error = %v", err)
	}

	// Should be idempotent
	err = initWSSchemas()
	if err != nil {
		t.Errorf("initWSSchemas() second call error = %v", err)
	}
}

func TestValidateWSRequestFrame(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		frame     *wsFrame
		wantError bool
	}{
		{
			name: "valid connect request",
			raw: `{
				"type": "req",
				"id": "1",
				"method": "connect",
				"params": {
					"minProtocol": 1,
					"maxProtocol": 1,
					"client": {
						"id": "test-client",
						"version": "1.0.0",
						"platform": "linux"
					}
				}
			}`,
			frame: &wsFrame{
				Type:   "req",
				ID:     "1",
				Method: "connect",
				Params: json.RawMessage(`{"minProtocol": 1, "maxProtocol": 1, "client": {"id": "test-client", "version": "1.0.0", "platform": "linux"}}`),
			},
			wantError: false,
		},
		{
			name: "valid ping request",
			raw: `{
				"type": "req",
				"id": "2",
				"method": "ping",
				"params": {}
			}`,
			frame: &wsFrame{
				Type:   "req",
				ID:     "2",
				Method: "ping",
				Params: json.RawMessage(`{}`),
			},
			wantError: false,
		},
		{
			name: "valid health request",
			raw: `{
				"type": "req",
				"id": "3",
				"method": "health"
			}`,
			frame: &wsFrame{
				Type:   "req",
				ID:     "3",
				Method: "health",
			},
			wantError: false,
		},
		{
			name:      "invalid JSON",
			raw:       `{invalid}`,
			frame:     nil,
			wantError: true,
		},
		{
			name: "missing type",
			raw: `{
				"id": "1",
				"method": "ping"
			}`,
			frame:     nil,
			wantError: true,
		},
		{
			name: "missing id",
			raw: `{
				"type": "req",
				"method": "ping"
			}`,
			frame:     nil,
			wantError: true,
		},
		{
			name: "missing method",
			raw: `{
				"type": "req",
				"id": "1"
			}`,
			frame:     nil,
			wantError: true,
		},
		{
			name: "nil frame",
			raw: `{
				"type": "req",
				"id": "1",
				"method": "ping"
			}`,
			frame:     nil,
			wantError: true,
		},
		{
			name: "chat.send missing content",
			raw: `{
				"type": "req",
				"id": "1",
				"method": "chat.send",
				"params": {}
			}`,
			frame: &wsFrame{
				Type:   "req",
				ID:     "1",
				Method: "chat.send",
				Params: json.RawMessage(`{}`),
			},
			wantError: true,
		},
		{
			name: "valid chat.send",
			raw: `{
				"type": "req",
				"id": "1",
				"method": "chat.send",
				"params": {"content": "hello"}
			}`,
			frame: &wsFrame{
				Type:   "req",
				ID:     "1",
				Method: "chat.send",
				Params: json.RawMessage(`{"content": "hello"}`),
			},
			wantError: false,
		},
		{
			name: "chat.history missing sessionId",
			raw: `{
				"type": "req",
				"id": "1",
				"method": "chat.history",
				"params": {}
			}`,
			frame: &wsFrame{
				Type:   "req",
				ID:     "1",
				Method: "chat.history",
				Params: json.RawMessage(`{}`),
			},
			wantError: true,
		},
		{
			name: "valid chat.history",
			raw: `{
				"type": "req",
				"id": "1",
				"method": "chat.history",
				"params": {"sessionId": "session-123"}
			}`,
			frame: &wsFrame{
				Type:   "req",
				ID:     "1",
				Method: "chat.history",
				Params: json.RawMessage(`{"sessionId": "session-123"}`),
			},
			wantError: false,
		},
		{
			name: "valid sessions.list",
			raw: `{
				"type": "req",
				"id": "1",
				"method": "sessions.list",
				"params": {}
			}`,
			frame: &wsFrame{
				Type:   "req",
				ID:     "1",
				Method: "sessions.list",
				Params: json.RawMessage(`{}`),
			},
			wantError: false,
		},
		{
			name: "unknown method with valid base schema",
			raw: `{
				"type": "req",
				"id": "1",
				"method": "unknown.method",
				"params": {"anything": "goes"}
			}`,
			frame: &wsFrame{
				Type:   "req",
				ID:     "1",
				Method: "unknown.method",
				Params: json.RawMessage(`{"anything": "goes"}`),
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWSRequestFrame([]byte(tt.raw), tt.frame)
			if (err != nil) != tt.wantError {
				t.Errorf("validateWSRequestFrame() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestWSSchemaConstants(t *testing.T) {
	// Verify schema constants are valid JSON
	schemas := []struct {
		name   string
		schema string
	}{
		{"wsRequestSchema", wsRequestSchema},
		{"wsConnectParamsSchema", wsConnectParamsSchema},
		{"wsHealthParamsSchema", wsHealthParamsSchema},
		{"wsPingParamsSchema", wsPingParamsSchema},
		{"wsChatSendParamsSchema", wsChatSendParamsSchema},
		{"wsChatHistoryParamsSchema", wsChatHistoryParamsSchema},
		{"wsChatAbortParamsSchema", wsChatAbortParamsSchema},
		{"wsSessionsListParamsSchema", wsSessionsListParamsSchema},
		{"wsSessionsPatchParamsSchema", wsSessionsPatchParamsSchema},
	}

	for _, tt := range schemas {
		t.Run(tt.name, func(t *testing.T) {
			var v any
			if err := json.Unmarshal([]byte(tt.schema), &v); err != nil {
				t.Errorf("%s is not valid JSON: %v", tt.name, err)
			}
		})
	}
}
