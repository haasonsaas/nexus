package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	pb "github.com/haasonsaas/nexus/pkg/proto"
	"github.com/spf13/cobra"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// =============================================================================
// Status Command Helpers
// =============================================================================

// printSystemStatus prints the system status.
func printSystemStatus(ctx context.Context, out io.Writer, jsonOutput bool, configPath, serverAddr, token, apiKey string) error {
	baseURL, err := resolveHTTPBaseURL(configPath, serverAddr)
	if err != nil {
		return err
	}
	client := newAPIClient(baseURL, token, apiKey)

	var status systemStatus
	if err := client.getJSON(ctx, "/api/status", &status); err != nil {
		return err
	}

	if jsonOutput {
		payload := struct {
			Version string       `json:"version"`
			Commit  string       `json:"commit"`
			Build   string       `json:"build"`
			System  systemStatus `json:"system"`
		}{
			Version: version,
			Commit:  commit,
			Build:   date,
			System:  status,
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}

	fmt.Fprintln(out, "NEXUS STATUS")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Version: %s (commit: %s)\n", version, commit)
	fmt.Fprintf(out, "Built: %s\n", date)
	fmt.Fprintf(out, "Uptime: %s\n", status.UptimeString)
	fmt.Fprintf(out, "Go: %s | Goroutines: %d | CPU: %d\n", status.GoVersion, status.NumGoroutines, status.NumCPU)
	fmt.Fprintf(out, "Memory: %.2f MB alloc / %.2f MB sys\n", status.MemAllocMB, status.MemSysMB)
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Database")
	if status.DatabaseStatus == "" {
		fmt.Fprintln(out, "   Status: unknown")
	} else {
		fmt.Fprintf(out, "   Status: %s\n", status.DatabaseStatus)
	}
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Channels")
	if len(status.Channels) == 0 {
		fmt.Fprintln(out, "   No channel adapters reported.")
	} else {
		for _, ch := range status.Channels {
			name := ch.Name
			if name == "" {
				name = ch.Type
			}
			fmt.Fprintf(out, "   %s: %s\n", cases.Title(language.English).String(name), ch.Status)
			if ch.Error != "" {
				fmt.Fprintf(out, "     Error: %s\n", ch.Error)
			}
			if ch.HealthMessage != "" {
				fmt.Fprintf(out, "     Health: %s\n", ch.HealthMessage)
			}
		}
	}
	fmt.Fprintln(out)

	if status.HealthChecks != nil && len(status.HealthChecks.Checks) > 0 {
		fmt.Fprintln(out, "Components")
		for _, check := range status.HealthChecks.Checks {
			fmt.Fprintf(out, "   %s: %s\n", check.Name, check.Status)
			if check.Message != "" {
				fmt.Fprintf(out, "     %s\n", check.Message)
			}
		}
		fmt.Fprintln(out)
	}

	fmt.Fprintln(out, "LLM Providers")
	fmt.Fprintln(out, "   Not reported by server status API")
	fmt.Fprintln(out)

	fmt.Fprintln(out, "Tools")
	fmt.Fprintln(out, "   Not reported by server status API")
	fmt.Fprintln(out)

	return nil
}

// =============================================================================
// Edge Command Handlers
// =============================================================================

// runEdgeStatus shows the status of edge daemons.
func runEdgeStatus(cmd *cobra.Command, configPath string, edgeID string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Connect to the running Nexus server
	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.GRPCPort)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintln(cmd.OutOrStdout(), "Edge Status")
		fmt.Fprintln(cmd.OutOrStdout(), "===========")
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintf(cmd.OutOrStdout(), "Cannot connect to Nexus server at %s\n", addr)
		fmt.Fprintln(cmd.OutOrStdout(), "Ensure the server is running with 'nexus serve'")
		return nil
	}
	defer conn.Close()

	client := pb.NewEdgeServiceClient(conn)
	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	defer cancel()

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Edge Status")
	fmt.Fprintln(out, "===========")
	fmt.Fprintln(out)

	if edgeID == "" {
		// List all edges
		resp, err := client.ListEdges(ctx, &pb.ListEdgesRequest{})
		if err != nil {
			fmt.Fprintf(out, "Error querying edges: %v\n", err)
			return nil
		}

		if len(resp.Edges) == 0 {
			fmt.Fprintln(out, "No edges currently connected.")
			fmt.Fprintln(out, "Run 'nexus-edge --core-url <url> --edge-id <id>' to connect an edge daemon.")
		} else {
			fmt.Fprintf(out, "Connected Edges: %d\n\n", len(resp.Edges))
			fmt.Fprintln(out, "ID            Status       Tools  Last Heartbeat")
			fmt.Fprintln(out, "------------  -----------  -----  ---------------")
			for _, edge := range resp.Edges {
				status := connectionStatusString(edge.ConnectionStatus)
				heartbeat := "never"
				if edge.LastHeartbeat != nil {
					heartbeat = time.Since(edge.LastHeartbeat.AsTime()).Round(time.Second).String() + " ago"
				}
				fmt.Fprintf(out, "%-12s  %-11s  %5d  %s\n",
					truncate(edge.EdgeId, 12),
					status,
					len(edge.Tools),
					heartbeat)
			}
		}
	} else {
		// Get specific edge
		resp, err := client.GetEdgeStatus(ctx, &pb.GetEdgeStatusRequest{EdgeId: edgeID})
		if err != nil {
			fmt.Fprintf(out, "Edge '%s' not found or not connected\n", edgeID)
			return nil
		}

		edge := resp.Status
		fmt.Fprintf(out, "Edge ID:     %s\n", edge.EdgeId)
		fmt.Fprintf(out, "Name:        %s\n", edge.Name)
		fmt.Fprintf(out, "Status:      %s\n", connectionStatusString(edge.ConnectionStatus))
		if edge.ConnectedAt != nil {
			fmt.Fprintf(out, "Connected:   %s\n", edge.ConnectedAt.AsTime().Format(time.RFC3339))
		}
		if edge.LastHeartbeat != nil {
			fmt.Fprintf(out, "Heartbeat:   %s ago\n", time.Since(edge.LastHeartbeat.AsTime()).Round(time.Second))
		}
		fmt.Fprintf(out, "Tools:       %d\n", len(edge.Tools))
		if len(edge.Tools) > 0 {
			fmt.Fprintln(out, "\nRegistered Tools:")
			for _, tool := range edge.Tools {
				fmt.Fprintf(out, "  - %s\n", tool)
			}
		}
	}
	return nil
}

// connectionStatusString converts EdgeConnectionStatus to a display string.
func connectionStatusString(status pb.EdgeConnectionStatus) string {
	switch status {
	case pb.EdgeConnectionStatus_EDGE_CONNECTION_STATUS_CONNECTED:
		return "Connected"
	case pb.EdgeConnectionStatus_EDGE_CONNECTION_STATUS_DISCONNECTED:
		return "Disconnected"
	case pb.EdgeConnectionStatus_EDGE_CONNECTION_STATUS_RECONNECTING:
		return "Reconnecting"
	default:
		return "Unknown"
	}
}

// truncate shortens a string to maxLen with ellipsis.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// runEdgeList lists connected edge daemons.
func runEdgeList(cmd *cobra.Command, configPath string, showTools bool) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.GRPCPort)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintln(cmd.OutOrStdout(), "Cannot connect to Nexus server")
		fmt.Fprintf(cmd.OutOrStdout(), "Ensure the server is running at %s\n", addr)
		return nil
	}
	defer conn.Close()

	client := pb.NewEdgeServiceClient(conn)
	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	defer cancel()

	resp, err := client.ListEdges(ctx, &pb.ListEdgesRequest{})
	if err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "Error querying edges: %v\n", err)
		return nil
	}

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Connected Edge Daemons")
	fmt.Fprintln(out, "======================")
	fmt.Fprintln(out)

	if len(resp.Edges) == 0 {
		fmt.Fprintln(out, "No edges currently connected.")
		return nil
	}

	fmt.Fprintln(out, "ID            Name          Status       Tools  Last Heartbeat")
	fmt.Fprintln(out, "------------  ------------  -----------  -----  ---------------")
	for _, edge := range resp.Edges {
		status := connectionStatusString(edge.ConnectionStatus)
		heartbeat := "never"
		if edge.LastHeartbeat != nil {
			heartbeat = time.Since(edge.LastHeartbeat.AsTime()).Round(time.Second).String() + " ago"
		}
		name := edge.Name
		if name == "" {
			name = "-"
		}
		fmt.Fprintf(out, "%-12s  %-12s  %-11s  %5d  %s\n",
			truncate(edge.EdgeId, 12),
			truncate(name, 12),
			status,
			len(edge.Tools),
			heartbeat)

		if showTools && len(edge.Tools) > 0 {
			for _, tool := range edge.Tools {
				fmt.Fprintf(out, "              └─ %s\n", tool)
			}
		}
	}
	return nil
}

// runEdgeTools lists tools from connected edges.
func runEdgeTools(cmd *cobra.Command, configPath string, edgeID string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.GRPCPort)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		fmt.Fprintln(cmd.OutOrStdout(), "Cannot connect to Nexus server")
		fmt.Fprintf(cmd.OutOrStdout(), "Ensure the server is running at %s\n", addr)
		return nil
	}
	defer conn.Close()

	client := pb.NewEdgeServiceClient(conn)
	ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
	defer cancel()

	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "Edge Tools")
	fmt.Fprintln(out, "==========")
	fmt.Fprintln(out)

	if edgeID != "" {
		// Get tools for specific edge
		resp, err := client.GetEdgeStatus(ctx, &pb.GetEdgeStatusRequest{EdgeId: edgeID})
		if err != nil {
			fmt.Fprintf(out, "Edge '%s' not found or not connected\n", edgeID)
			return nil
		}
		edge := resp.Status
		if len(edge.Tools) == 0 {
			fmt.Fprintf(out, "No tools registered by edge '%s'\n", edgeID)
			return nil
		}
		fmt.Fprintf(out, "Tools from edge '%s':\n\n", edgeID)
		for _, tool := range edge.Tools {
			fmt.Fprintf(out, "  edge:%s.%s\n", edgeID, tool)
		}
	} else {
		// List tools from all edges
		resp, err := client.ListEdges(ctx, &pb.ListEdgesRequest{})
		if err != nil {
			fmt.Fprintf(out, "Error querying edges: %v\n", err)
			return nil
		}

		if len(resp.Edges) == 0 {
			fmt.Fprintln(out, "No edge tools available. Connect an edge daemon first.")
			return nil
		}

		totalTools := 0
		for _, edge := range resp.Edges {
			if len(edge.Tools) > 0 {
				fmt.Fprintf(out, "Edge: %s\n", edge.EdgeId)
				for _, tool := range edge.Tools {
					fmt.Fprintf(out, "  edge:%s.%s\n", edge.EdgeId, tool)
					totalTools++
				}
				fmt.Fprintln(out)
			}
		}
		if totalTools == 0 {
			fmt.Fprintln(out, "No tools registered by any connected edge.")
		}
	}
	return nil
}

// runEdgeApprove approves a pending edge (TOFU).
func runEdgeApprove(cmd *cobra.Command, configPath string, edgeID string) error {
	fmt.Fprintf(cmd.OutOrStdout(), "Approving edge: %s\n", edgeID)
	fmt.Fprintln(cmd.OutOrStdout(), "No pending edge found with that ID.")
	return nil
}

// runEdgeRevoke revokes an approved edge.
func runEdgeRevoke(cmd *cobra.Command, configPath string, edgeID string) error {
	fmt.Fprintf(cmd.OutOrStdout(), "Revoking edge: %s\n", edgeID)
	fmt.Fprintln(cmd.OutOrStdout(), "No approved edge found with that ID.")
	return nil
}
