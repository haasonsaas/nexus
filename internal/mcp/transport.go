package mcp

import (
	"context"
	"encoding/json"
)

// Transport defines the interface for MCP transports.
type Transport interface {
	// Connect establishes the transport connection.
	Connect(ctx context.Context) error

	// Close closes the transport connection.
	Close() error

	// Call sends a request and waits for a response.
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)

	// Notify sends a notification (no response expected).
	Notify(ctx context.Context, method string, params any) error

	// Events returns a channel for receiving notifications from the server.
	Events() <-chan *JSONRPCNotification

	// Requests returns a channel for receiving server-initiated requests.
	Requests() <-chan *JSONRPCRequest

	// Respond sends a response to a server-initiated request.
	Respond(ctx context.Context, id any, result any, rpcErr *JSONRPCError) error

	// Connected returns whether the transport is connected.
	Connected() bool
}

// NewTransport creates a new transport based on the server configuration.
func NewTransport(cfg *ServerConfig) Transport {
	switch cfg.Transport {
	case TransportHTTP:
		return NewHTTPTransport(cfg)
	default:
		return NewStdioTransport(cfg)
	}
}
