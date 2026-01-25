package tailscale

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TailscaleStatus represents the status of Tailscale.
type TailscaleStatus struct {
	BackendState   string           `json:"BackendState"`
	Self           *TailscaleSelf   `json:"Self"`
	Peer           map[string]*Peer `json:"Peer"`
	CurrentTailnet *TailnetInfo     `json:"CurrentTailnet"`
}

// TailscaleSelf represents the local node.
type TailscaleSelf struct {
	ID           string   `json:"ID"`
	PublicKey    string   `json:"PublicKey"`
	HostName     string   `json:"HostName"`
	DNSName      string   `json:"DNSName"`
	OS           string   `json:"OS"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Online       bool     `json:"Online"`
}

// Peer represents a Tailscale peer.
type Peer struct {
	ID           string   `json:"ID"`
	PublicKey    string   `json:"PublicKey"`
	HostName     string   `json:"HostName"`
	DNSName      string   `json:"DNSName"`
	OS           string   `json:"OS"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Online       bool     `json:"Online"`
	LastSeen     string   `json:"LastSeen"`
}

// TailnetInfo contains tailnet information.
type TailnetInfo struct {
	Name            string `json:"Name"`
	MagicDNSSuffix  string `json:"MagicDNSSuffix"`
	MagicDNSEnabled bool   `json:"MagicDNSEnabled"`
}

// FunnelStatus represents Tailscale Funnel configuration.
type FunnelStatus struct {
	AllowFunnel bool              `json:"AllowFunnel"`
	Services    map[string]string `json:"Services"` // port -> handler
}

// ServeConfig represents Tailscale serve configuration.
type ServeConfig struct {
	TCP map[uint16]*TCPServe `json:"TCP,omitempty"`
	Web map[string]*WebServe `json:"Web,omitempty"`
}

// TCPServe represents TCP serve config.
type TCPServe struct {
	HTTPS bool `json:"HTTPS,omitempty"`
}

// WebServe represents web serve config.
type WebServe struct {
	Handlers map[string]*WebHandler `json:"Handlers,omitempty"`
}

// WebHandler represents a web handler.
type WebHandler struct {
	Proxy string `json:"Proxy,omitempty"`
	Path  string `json:"Path,omitempty"`
}

// DefaultTimeout is the default timeout for Tailscale commands.
const DefaultTimeout = 10 * time.Second

// Client wraps Tailscale CLI commands.
type Client struct {
	Path    string        // path to tailscale binary
	Timeout time.Duration // command timeout
}

// NewClient creates a new Tailscale client.
func NewClient() *Client {
	return &Client{
		Path:    FindTailscalePath(),
		Timeout: DefaultTimeout,
	}
}

// NewClientWithPath creates a new Tailscale client with a specific binary path.
func NewClientWithPath(path string) *Client {
	return &Client{
		Path:    path,
		Timeout: DefaultTimeout,
	}
}

// IsAvailable checks if Tailscale CLI is available.
func (c *Client) IsAvailable(ctx context.Context) bool {
	if c.Path == "" {
		return false
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.Path, "--version")
	return cmd.Run() == nil
}

// Status returns current Tailscale status.
func (c *Client) Status(ctx context.Context) (*TailscaleStatus, error) {
	output, err := c.runCommand(ctx, "status", "--json")
	if err != nil {
		return nil, fmt.Errorf("failed to get tailscale status: %w", err)
	}

	// Parse potentially noisy JSON output
	parsed, err := parsePossiblyNoisyJSON(output)
	if err != nil {
		return nil, fmt.Errorf("failed to parse tailscale status: %w", err)
	}

	var status TailscaleStatus
	if err := json.Unmarshal(parsed, &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal tailscale status: %w", err)
	}

	return &status, nil
}

// IsConnected checks if Tailscale is connected.
func (c *Client) IsConnected(ctx context.Context) bool {
	status, err := c.Status(ctx)
	if err != nil {
		return false
	}
	return status.BackendState == "Running"
}

// GetSelfDNSName returns the local node's DNS name.
func (c *Client) GetSelfDNSName(ctx context.Context) (string, error) {
	status, err := c.Status(ctx)
	if err != nil {
		return "", err
	}

	if status.Self == nil {
		return "", fmt.Errorf("no self information in status")
	}

	dnsName := strings.TrimSuffix(status.Self.DNSName, ".")
	if dnsName == "" {
		return "", fmt.Errorf("no DNS name available")
	}

	return dnsName, nil
}

// GetSelfIP returns the local node's Tailscale IP.
func (c *Client) GetSelfIP(ctx context.Context) (string, error) {
	status, err := c.Status(ctx)
	if err != nil {
		return "", err
	}

	if status.Self == nil {
		return "", fmt.Errorf("no self information in status")
	}

	if len(status.Self.TailscaleIPs) == 0 {
		return "", fmt.Errorf("no Tailscale IPs available")
	}

	return status.Self.TailscaleIPs[0], nil
}

// ServeStatus returns the current serve configuration.
func (c *Client) ServeStatus(ctx context.Context) (*ServeConfig, error) {
	output, err := c.runCommand(ctx, "serve", "status", "--json")
	if err != nil {
		// If serve is not configured, command may fail
		return &ServeConfig{}, nil
	}

	parsed, err := parsePossiblyNoisyJSON(output)
	if err != nil {
		return &ServeConfig{}, nil
	}

	var config ServeConfig
	if err := json.Unmarshal(parsed, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal serve status: %w", err)
	}

	return &config, nil
}

// FunnelStatus returns the current funnel configuration.
func (c *Client) FunnelStatus(ctx context.Context) (*FunnelStatus, error) {
	output, err := c.runCommand(ctx, "funnel", "status", "--json")
	if err != nil {
		// If funnel is not configured, command may fail
		return &FunnelStatus{}, nil
	}

	if len(output) == 0 {
		return &FunnelStatus{}, nil
	}

	parsed, err := parsePossiblyNoisyJSON(output)
	if err != nil {
		return &FunnelStatus{}, nil
	}

	var status FunnelStatus
	if err := json.Unmarshal(parsed, &status); err != nil {
		// Try parsing as a map
		var rawStatus map[string]interface{}
		if err := json.Unmarshal(parsed, &rawStatus); err != nil {
			return nil, fmt.Errorf("failed to unmarshal funnel status: %w", err)
		}

		// Extract information from raw status
		status.Services = make(map[string]string)
		status.AllowFunnel = len(rawStatus) > 0
	}

	return &status, nil
}

// Serve sets up Tailscale serve for a local port.
func (c *Client) Serve(ctx context.Context, port int, path string) error {
	args := []string{"serve", "--bg", "--yes"}
	if path != "" && path != "/" {
		args = append(args, path)
	}
	args = append(args, fmt.Sprintf("%d", port))

	_, err := c.runCommand(ctx, args...)
	if err != nil {
		return fmt.Errorf("failed to enable serve: %w", err)
	}
	return nil
}

// ServeHTTPS sets up HTTPS serve.
func (c *Client) ServeHTTPS(ctx context.Context, port int, localPort int) error {
	args := []string{"serve", "--bg", "--yes", "--https", fmt.Sprintf("%d", port)}
	if localPort > 0 {
		args = append(args, fmt.Sprintf("localhost:%d", localPort))
	}

	_, err := c.runCommand(ctx, args...)
	if err != nil {
		return fmt.Errorf("failed to enable HTTPS serve: %w", err)
	}
	return nil
}

// Funnel enables Tailscale Funnel for a port.
func (c *Client) Funnel(ctx context.Context, port int) error {
	_, err := c.runCommand(ctx, "funnel", "--bg", "--yes", fmt.Sprintf("%d", port))
	if err != nil {
		return fmt.Errorf("failed to enable funnel: %w", err)
	}
	return nil
}

// ServeOff disables serve for a path.
func (c *Client) ServeOff(ctx context.Context, path string) error {
	args := []string{"serve", "off"}
	if path != "" && path != "/" {
		args = append(args, path)
	}

	_, err := c.runCommand(ctx, args...)
	if err != nil {
		return fmt.Errorf("failed to disable serve: %w", err)
	}
	return nil
}

// ServeReset resets all serve configuration.
func (c *Client) ServeReset(ctx context.Context) error {
	_, err := c.runCommand(ctx, "serve", "reset")
	if err != nil {
		return fmt.Errorf("failed to reset serve: %w", err)
	}
	return nil
}

// FunnelOff disables funnel for a port.
func (c *Client) FunnelOff(ctx context.Context, port int) error {
	_, err := c.runCommand(ctx, "funnel", "off", fmt.Sprintf("%d", port))
	if err != nil {
		return fmt.Errorf("failed to disable funnel: %w", err)
	}
	return nil
}

// FunnelReset resets all funnel configuration.
func (c *Client) FunnelReset(ctx context.Context) error {
	_, err := c.runCommand(ctx, "funnel", "reset")
	if err != nil {
		return fmt.Errorf("failed to reset funnel: %w", err)
	}
	return nil
}

// FindPeerByHostname finds a peer by hostname.
func (c *Client) FindPeerByHostname(ctx context.Context, hostname string) (*Peer, error) {
	status, err := c.Status(ctx)
	if err != nil {
		return nil, err
	}

	hostname = strings.ToLower(hostname)

	for _, peer := range status.Peer {
		if strings.ToLower(peer.HostName) == hostname {
			return peer, nil
		}
		// Also check DNS name without trailing dot
		dnsName := strings.TrimSuffix(strings.ToLower(peer.DNSName), ".")
		if dnsName == hostname || strings.HasPrefix(dnsName, hostname+".") {
			return peer, nil
		}
	}

	return nil, fmt.Errorf("peer not found: %s", hostname)
}

// FindPeerByIP finds a peer by Tailscale IP.
func (c *Client) FindPeerByIP(ctx context.Context, ip string) (*Peer, error) {
	status, err := c.Status(ctx)
	if err != nil {
		return nil, err
	}

	for _, peer := range status.Peer {
		for _, peerIP := range peer.TailscaleIPs {
			if peerIP == ip {
				return peer, nil
			}
		}
	}

	return nil, fmt.Errorf("peer not found by IP: %s", ip)
}

// Ping pings a Tailscale peer.
func (c *Client) Ping(ctx context.Context, target string) (time.Duration, error) {
	// Use a shorter timeout for ping
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	output, err := c.runCommand(ctx, "ping", "-c", "1", target)
	if err != nil {
		return 0, fmt.Errorf("ping failed: %w", err)
	}

	// Parse latency from output
	// Example: "pong from hostname (100.x.y.z) via DERP(nyc) in 42ms"
	// Or: "pong from hostname (100.x.y.z) via direct in 1.234ms"
	latency := parsePingLatency(string(output))
	return latency, nil
}

// ListPeers returns all online peers.
func (c *Client) ListPeers(ctx context.Context) ([]*Peer, error) {
	status, err := c.Status(ctx)
	if err != nil {
		return nil, err
	}

	peers := make([]*Peer, 0, len(status.Peer))
	for _, peer := range status.Peer {
		if peer.Online {
			peers = append(peers, peer)
		}
	}
	return peers, nil
}

// GetTailnet returns the current tailnet info.
func (c *Client) GetTailnet(ctx context.Context) (*TailnetInfo, error) {
	status, err := c.Status(ctx)
	if err != nil {
		return nil, err
	}

	if status.CurrentTailnet == nil {
		return nil, fmt.Errorf("no tailnet information available")
	}

	return status.CurrentTailnet, nil
}

// runCommand executes a tailscale CLI command.
func (c *Client) runCommand(ctx context.Context, args ...string) ([]byte, error) {
	if c.Path == "" {
		return nil, fmt.Errorf("tailscale binary not found")
	}

	ctx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.Path, args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("command failed: %s: %s", err, string(exitErr.Stderr))
		}
		return nil, err
	}

	return output, nil
}

// FindTailscalePath finds the tailscale binary.
func FindTailscalePath() string {
	// Strategy 1: Check PATH
	if path, err := exec.LookPath("tailscale"); err == nil {
		return path
	}

	// Strategy 2: Known Linux paths
	linuxPaths := []string{
		"/usr/bin/tailscale",
		"/usr/local/bin/tailscale",
		"/snap/bin/tailscale",
	}

	for _, path := range linuxPaths {
		if fileExists(path) {
			return path
		}
	}

	// Strategy 3: Known macOS app path
	macPath := "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
	if fileExists(macPath) {
		return macPath
	}

	// Strategy 4: Check common macOS locations
	macPaths := []string{
		"/opt/homebrew/bin/tailscale",
		"/usr/local/Cellar/tailscale/*/bin/tailscale",
	}

	for _, pattern := range macPaths {
		matches, err := findGlobMatches(pattern)
		if err != nil {
			continue
		}
		if len(matches) > 0 {
			return matches[0]
		}
	}

	return ""
}

// parsePossiblyNoisyJSON extracts JSON from potentially noisy output.
func parsePossiblyNoisyJSON(data []byte) ([]byte, error) {
	trimmed := strings.TrimSpace(string(data))

	// Find JSON object boundaries
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")

	if start >= 0 && end > start {
		return []byte(trimmed[start : end+1]), nil
	}

	// Try array
	start = strings.Index(trimmed, "[")
	end = strings.LastIndex(trimmed, "]")

	if start >= 0 && end > start {
		return []byte(trimmed[start : end+1]), nil
	}

	return data, nil
}

// parsePingLatency parses latency from ping output.
func parsePingLatency(output string) time.Duration {
	// Match patterns like "in 42ms" or "in 1.234ms"
	re := regexp.MustCompile(`in (\d+(?:\.\d+)?)(ms|s)`)
	matches := re.FindStringSubmatch(output)
	if len(matches) < 3 {
		return 0
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0
	}

	unit := matches[2]
	switch unit {
	case "ms":
		return time.Duration(value * float64(time.Millisecond))
	case "s":
		return time.Duration(value * float64(time.Second))
	default:
		return 0
	}
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// findGlobMatches finds files matching a glob pattern.
func findGlobMatches(pattern string) ([]string, error) {
	return filepath.Glob(pattern)
}

// IsFunnelEnabled checks if funnel is enabled for the account.
func (c *Client) IsFunnelEnabled(ctx context.Context) (bool, error) {
	status, err := c.FunnelStatus(ctx)
	if err != nil {
		return false, err
	}
	return status.AllowFunnel, nil
}

// GetPublicURL returns the public URL for this node (if funnel is enabled).
func (c *Client) GetPublicURL(ctx context.Context, port int) (string, error) {
	dnsName, err := c.GetSelfDNSName(ctx)
	if err != nil {
		return "", err
	}

	// Standard HTTPS port doesn't need to be in URL
	if port == 443 {
		return fmt.Sprintf("https://%s", dnsName), nil
	}

	return fmt.Sprintf("https://%s:%d", dnsName, port), nil
}
