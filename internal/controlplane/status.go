package controlplane

import "context"

// GatewayStatus provides a summary of gateway runtime state.
type GatewayStatus struct {
	UptimeSeconds int64  `json:"uptime_seconds"`
	Uptime        string `json:"uptime"`
	StartTime     string `json:"start_time,omitempty"`
	GRPCAddress   string `json:"grpc_address,omitempty"`
	HTTPAddress   string `json:"http_address,omitempty"`
	Version       string `json:"version,omitempty"`
	ConfigPath    string `json:"config_path,omitempty"`
}

// GatewayManager exposes runtime status.
type GatewayManager interface {
	GatewayStatus(ctx context.Context) (GatewayStatus, error)
}
