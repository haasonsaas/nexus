package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/haasonsaas/nexus/internal/config"
	"github.com/haasonsaas/nexus/internal/provisioning"
	"github.com/haasonsaas/nexus/pkg/models"
	pb "github.com/haasonsaas/nexus/pkg/proto"
	"github.com/spf13/cobra"
	"golang.org/x/term"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gopkg.in/yaml.v3"
)

// =============================================================================
// Channel Command Helpers
// =============================================================================

// loadConfigForChannels loads the config for channel commands.
func loadConfigForChannels(configPath string) (*config.Config, error) {
	return config.Load(configPath)
}

// printChannelsList prints the list of configured channels.
func printChannelsList(out io.Writer, cfg *config.Config) {
	fmt.Fprintln(out, "Configured Channels")
	fmt.Fprintln(out, "===================")
	fmt.Fprintln(out)

	if cfg.Channels.Telegram.Enabled {
		fmt.Fprintln(out, "Telegram")
		fmt.Fprintln(out, "   Status: Enabled")
		if len(cfg.Channels.Telegram.BotToken) >= 10 {
			fmt.Fprintf(out, "   Bot Token: %s***\n", cfg.Channels.Telegram.BotToken[:10])
		}
		fmt.Fprintln(out)
	}

	if cfg.Channels.Discord.Enabled {
		fmt.Fprintln(out, "Discord")
		fmt.Fprintln(out, "   Status: Enabled")
		fmt.Fprintf(out, "   App ID: %s\n", cfg.Channels.Discord.AppID)
		fmt.Fprintln(out)
	}

	if cfg.Channels.Slack.Enabled {
		fmt.Fprintln(out, "Slack")
		fmt.Fprintln(out, "   Status: Enabled")
		if len(cfg.Channels.Slack.BotToken) >= 10 {
			fmt.Fprintf(out, "   Bot Token: %s***\n", cfg.Channels.Slack.BotToken[:10])
		}
		fmt.Fprintln(out)
	}
}

// printChannelsLogin prints channel login validation results.
func printChannelsLogin(out io.Writer, cfg *config.Config) {
	fmt.Fprintln(out, "Channel login checks:")

	if cfg.Channels.Telegram.Enabled {
		if cfg.Channels.Telegram.BotToken == "" {
			fmt.Fprintln(out, "  - Telegram: missing bot_token (use @BotFather)")
		} else {
			fmt.Fprintln(out, "  - Telegram: token set")
		}
	}

	if cfg.Channels.Discord.Enabled {
		if cfg.Channels.Discord.BotToken == "" || cfg.Channels.Discord.AppID == "" {
			fmt.Fprintln(out, "  - Discord: missing bot_token/app_id (create app + bot token)")
		} else {
			fmt.Fprintln(out, "  - Discord: token + app id set")
		}
	}

	if cfg.Channels.Slack.Enabled {
		if cfg.Channels.Slack.BotToken == "" || cfg.Channels.Slack.AppToken == "" || cfg.Channels.Slack.SigningSecret == "" {
			fmt.Fprintln(out, "  - Slack: missing bot_token/app_token/signing_secret")
		} else {
			fmt.Fprintln(out, "  - Slack: credentials set")
		}
	}

	fmt.Fprintln(out, "Run `nexus channels test <channel>` to send a test message.")
}

// printChannelsStatus prints the channel connection status.
func printChannelsStatus(ctx context.Context, out io.Writer, configPath, serverAddr, token, apiKey string) error {
	baseURL, err := resolveHTTPBaseURL(configPath, serverAddr)
	if err != nil {
		return err
	}
	client := newAPIClient(baseURL, token, apiKey)

	var status systemStatus
	if err := client.getJSON(ctx, "/api/status", &status); err != nil {
		return err
	}

	fmt.Fprintln(out, "Channel Connection Status")
	fmt.Fprintln(out, "========================")
	fmt.Fprintln(out)

	if len(status.Channels) == 0 {
		fmt.Fprintln(out, "No channel adapters reported by server.")
		return nil
	}

	for _, ch := range status.Channels {
		title := ch.Name
		if title == "" {
			title = ch.Type
		}
		fmt.Fprintln(out, cases.Title(language.English).String(title))
		fmt.Fprintf(out, "   Enabled: %t\n", ch.Enabled)
		fmt.Fprintf(out, "   Status: %s\n", ch.Status)
		if ch.Error != "" {
			fmt.Fprintf(out, "   Error: %s\n", ch.Error)
		}
		if ch.LastPing > 0 {
			fmt.Fprintf(out, "   Last Ping: %d\n", ch.LastPing)
		}
		if ch.HealthMessage != "" {
			fmt.Fprintf(out, "   Health: %s\n", ch.HealthMessage)
			if ch.HealthLatencyMs > 0 {
				fmt.Fprintf(out, "   Health Latency: %dms\n", ch.HealthLatencyMs)
			}
			if ch.HealthDegraded {
				fmt.Fprintln(out, "   Health Degraded: true")
			}
		}
		fmt.Fprintln(out)
	}

	return nil
}

// printChannelTest prints the channel test results.
func printChannelTest(ctx context.Context, out io.Writer, configPath, serverAddr, token, apiKey, channel, channelID, message string) error {
	slog.Info("testing channel connectivity", "channel", channel)

	baseURL, err := resolveHTTPBaseURL(configPath, serverAddr)
	if err != nil {
		return err
	}
	client := newAPIClient(baseURL, token, apiKey)

	if strings.TrimSpace(channelID) == "" {
		var status providerStatus
		if err := client.getJSON(ctx, fmt.Sprintf("/api/providers/%s", strings.ToLower(channel)), &status); err != nil {
			return err
		}

		fmt.Fprintf(out, "Channel %s status\n", channel)
		fmt.Fprintln(out, "======================")
		fmt.Fprintf(out, "Enabled: %t\n", status.Enabled)
		fmt.Fprintf(out, "Connected: %t\n", status.Connected)
		if status.Error != "" {
			fmt.Fprintf(out, "Error: %s\n", status.Error)
		}
		if status.HealthMessage != "" {
			fmt.Fprintf(out, "Health: %s\n", status.HealthMessage)
		}
		if status.HealthLatency > 0 {
			fmt.Fprintf(out, "Health Latency: %dms\n", status.HealthLatency)
		}
		if status.HealthDegraded {
			fmt.Fprintln(out, "Health Degraded: true")
		}
		if status.QRAvailable {
			fmt.Fprintf(out, "QR Updated At: %s\n", status.QRUpdatedAt)
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Provide --channel-id to send a live test message.")
		return nil
	}

	payload := map[string]string{
		"channel_id": channelID,
	}
	if strings.TrimSpace(message) != "" {
		payload["message"] = message
	}
	var response map[string]any
	if err := client.postJSON(ctx, fmt.Sprintf("/api/providers/%s/test", strings.ToLower(channel)), payload, &response); err != nil {
		return err
	}

	fmt.Fprintf(out, "Sent test message to %s\n", channel)
	fmt.Fprintf(out, "Channel ID: %s\n", channelID)
	if msg, ok := response["message"].(string); ok && msg != "" {
		fmt.Fprintf(out, "Message: %s\n", msg)
	}
	return nil
}

// runChannelsEnable enables a channel in the configuration.
func runChannelsEnable(cmd *cobra.Command, configPath, channel string) error {
	prov := provisioning.NewChannelProvisioner(configPath, nil)
	channelType := models.ChannelType(channel)

	if err := prov.EnableChannel(cmd.Context(), channelType); err != nil {
		return fmt.Errorf("enable channel: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Channel %s enabled\n", channel)
	return nil
}

// runChannelsDisable disables a channel in the configuration.
func runChannelsDisable(cmd *cobra.Command, configPath, channel string) error {
	prov := provisioning.NewChannelProvisioner(configPath, nil)
	channelType := models.ChannelType(channel)

	if err := prov.DisableChannel(cmd.Context(), channelType); err != nil {
		return fmt.Errorf("disable channel: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Channel %s disabled\n", channel)
	return nil
}

// runChannelsValidate validates channel configuration.
func runChannelsValidate(cmd *cobra.Command, configPath, channel string) error {
	prov := provisioning.NewChannelProvisioner(configPath, nil)
	out := cmd.OutOrStdout()

	if channel == "" {
		// Validate all channels
		channels, err := prov.ListChannels(cmd.Context())
		if err != nil {
			return fmt.Errorf("list channels: %w", err)
		}

		fmt.Fprintln(out, "Channel Validation")
		fmt.Fprintln(out, "==================")
		for _, ch := range channels {
			err := prov.ValidateChannel(cmd.Context(), ch.Type)
			if err != nil {
				fmt.Fprintf(out, "%-12s ✗ %v\n", ch.Type, err)
			} else {
				fmt.Fprintf(out, "%-12s ✓ Valid\n", ch.Type)
			}
		}
	} else {
		// Validate specific channel
		channelType := models.ChannelType(channel)
		if err := prov.ValidateChannel(cmd.Context(), channelType); err != nil {
			return fmt.Errorf("%s: %w", channel, err)
		}
		fmt.Fprintf(out, "%s: Valid\n", channel)
	}

	return nil
}

// runChannelsSetup runs the interactive channel setup wizard.
func runChannelsSetup(cmd *cobra.Command, configPath, serverAddr, channel, edgeID string, saveConfig bool) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	reader := bufio.NewReader(cmd.InOrStdin())

	// Connect to the provisioning service
	conn, err := grpc.NewClient(serverAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}
	defer conn.Close()

	client := pb.NewProvisioningServiceClient(conn)

	// Get available channels
	reqsResp, err := client.GetProvisioningRequirements(ctx, &pb.GetProvisioningRequirementsRequest{
		ChannelType: channel,
	})
	if err != nil {
		return fmt.Errorf("get provisioning requirements: %w", err)
	}

	// If no channel specified, list available options
	if channel == "" {
		fmt.Fprintln(out, "Available Channels for Setup")
		fmt.Fprintln(out, "============================")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Channel      Description                      Edge Required")
		fmt.Fprintln(out, "-----------  -------------------------------  -------------")
		for _, req := range reqsResp.Requirements {
			edgeReq := "No"
			if req.RequiresEdge {
				edgeReq = "Yes"
			}
			fmt.Fprintf(out, "%-12s %-32s %s\n", req.ChannelType, req.Description, edgeReq)
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Run `nexus channels setup <channel>` to begin setup.")
		return nil
	}

	// Find the requirements for the requested channel
	var requirements *pb.ProvisioningRequirements
	for _, req := range reqsResp.Requirements {
		if req.ChannelType == channel {
			requirements = req
			break
		}
	}
	if requirements == nil {
		return fmt.Errorf("unknown channel type: %s", channel)
	}

	// Check if edge is required
	if requirements.RequiresEdge && edgeID == "" {
		fmt.Fprintf(out, "\nWarning: %s requires an edge daemon for setup.\n", requirements.DisplayName)
		fmt.Fprintln(out, "Please specify --edge-id or ensure an edge daemon is connected.")
		edgeID = promptString(reader, "Edge ID (or press Enter to continue anyway)", "")
	}

	// Display channel info
	fmt.Fprintf(out, "\n%s Setup\n", requirements.DisplayName)
	fmt.Fprintln(out, strings.Repeat("=", len(requirements.DisplayName)+6))
	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s\n", requirements.Description)
	if requirements.DocsUrl != "" {
		fmt.Fprintf(out, "Documentation: %s\n", requirements.DocsUrl)
	}
	if requirements.EstimatedTime != "" {
		fmt.Fprintf(out, "Estimated time: %s\n", requirements.EstimatedTime)
	}
	fmt.Fprintln(out)

	// Confirm to proceed
	proceed := promptBool(reader, "Ready to begin setup?", true)
	if !proceed {
		fmt.Fprintln(out, "Setup cancelled.")
		return nil
	}

	// Start provisioning session
	startResp, err := client.StartProvisioning(ctx, &pb.StartProvisioningRequest{
		ChannelType: channel,
		EdgeId:      edgeID,
	})
	if err != nil {
		return fmt.Errorf("start provisioning: %w", err)
	}

	session := startResp.Session
	fmt.Fprintf(out, "\nSession started: %s\n", session.Id)

	// Process each step
	for session.Status == pb.ProvisioningStatus_PROVISIONING_STATUS_IN_PROGRESS {
		step := session.CurrentStep
		if step == nil {
			break
		}

		fmt.Fprintln(out)
		fmt.Fprintf(out, "Step %d/%d: %s\n", session.CurrentStepIndex+1, len(session.Steps), step.Title)
		fmt.Fprintln(out, strings.Repeat("-", 40))
		fmt.Fprintf(out, "%s\n", step.Description)
		fmt.Fprintln(out)

		// Handle different step types
		data := make(map[string]string)
		switch step.Type {
		case pb.ProvisioningStepType_PROVISIONING_STEP_TYPE_TOKEN_ENTRY,
			pb.ProvisioningStepType_PROVISIONING_STEP_TYPE_PHONE_NUMBER,
			pb.ProvisioningStepType_PROVISIONING_STEP_TYPE_VERIFICATION:
			// Prompt for input fields
			for _, field := range step.InputFields {
				label := field.Label
				if field.Required {
					label += " (required)"
				}
				if field.HelpText != "" {
					fmt.Fprintf(out, "  %s\n", field.HelpText)
				}

				var value string
				if field.Type == "password" {
					value = promptPassword(reader, "  "+label)
				} else {
					value = promptString(reader, "  "+label, "")
				}

				if field.Required && value == "" {
					return fmt.Errorf("field %s is required", field.Name)
				}
				data[field.Name] = value
			}

		case pb.ProvisioningStepType_PROVISIONING_STEP_TYPE_QR_CODE:
			// QR code step - requires edge daemon
			if step.RequiresEdge {
				if edgeID == "" {
					return fmt.Errorf("QR code step requires edge daemon (--edge-id)")
				}
				fmt.Fprintln(out, "A QR code will be displayed on your edge device.")
				if instructions, ok := step.Data["instructions"]; ok {
					fmt.Fprintf(out, "\n%s\n", instructions)
				}
				fmt.Fprintln(out)
				fmt.Fprintln(out, "Waiting for QR code scan...")
			} else {
				// Display QR code inline if we have the data
				if qrData, ok := step.Data["qr_data"]; ok {
					fmt.Fprintf(out, "QR Code data: %s\n", qrData)
				}
			}
			// Just prompt to continue - actual QR handling happens server-side
			promptString(reader, "Press Enter when QR code has been scanned", "")

		case pb.ProvisioningStepType_PROVISIONING_STEP_TYPE_OAUTH:
			// OAuth flow
			if authURL, ok := step.Data["auth_url"]; ok {
				fmt.Fprintf(out, "Please visit this URL to authorize:\n%s\n", authURL)
			}
			if callbackCode := promptString(reader, "Enter the authorization code (or press Enter if redirected)", ""); callbackCode != "" {
				data["code"] = callbackCode
			}

		case pb.ProvisioningStepType_PROVISIONING_STEP_TYPE_WAIT:
			// Wait step - poll for completion
			fmt.Fprintln(out, "Waiting for process to complete...")
			time.Sleep(2 * time.Second)
		}

		// Submit step data
		submitResp, err := client.SubmitProvisioningStep(ctx, &pb.SubmitProvisioningStepRequest{
			SessionId: session.Id,
			StepId:    step.Id,
			Data:      data,
		})
		if err != nil {
			return fmt.Errorf("submit step: %w", err)
		}

		session = submitResp.Session
		fmt.Fprintf(out, "Step completed.\n")
	}

	// Check final status
	switch session.Status {
	case pb.ProvisioningStatus_PROVISIONING_STATUS_COMPLETED:
		fmt.Fprintln(out)
		fmt.Fprintf(out, "%s setup completed successfully!\n", requirements.DisplayName)

		// Save to config if requested
		if saveConfig {
			if err := saveProvisioningResult(configPath, channel, session.Data); err != nil {
				return fmt.Errorf("save config: %w", err)
			}
			fmt.Fprintf(out, "Credentials saved to %s\n", configPath)
		}

		fmt.Fprintln(out)
		fmt.Fprintf(out, "Enable the channel with: nexus channels enable %s\n", channel)
		fmt.Fprintf(out, "Test the connection with: nexus channels test %s\n", channel)

	case pb.ProvisioningStatus_PROVISIONING_STATUS_FAILED:
		return fmt.Errorf("setup failed: %s", session.Error)

	case pb.ProvisioningStatus_PROVISIONING_STATUS_EXPIRED:
		return fmt.Errorf("setup session expired")

	case pb.ProvisioningStatus_PROVISIONING_STATUS_CANCELLED:
		fmt.Fprintln(out, "Setup cancelled.")
	}

	return nil
}

// promptPassword prompts for a password without showing input.
func promptPassword(reader *bufio.Reader, label string) string {
	fmt.Printf("%s: ", label)
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		text, err := term.ReadPassword(fd)
		fmt.Println()
		if err == nil {
			return strings.TrimSpace(string(text))
		}
	}
	text, err := reader.ReadString('\n')
	if err != nil {
		return ""
	}
	return strings.TrimSpace(text)
}

// saveProvisioningResult saves the provisioning result to the config file.
func saveProvisioningResult(configPath, channelType string, data map[string]string) error {
	// Load raw YAML to preserve formatting
	rawData, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	var node yaml.Node
	if err := yaml.Unmarshal(rawData, &node); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	// Map provisioning data to config fields
	channelKey := strings.ToLower(channelType)
	for key, value := range data {
		configKey := mapProvisioningKeyToConfig(key)
		if configKey == "" {
			continue
		}
		if err := setYAMLValue(&node, []string{"channels", channelKey, configKey}, value); err != nil {
			return fmt.Errorf("set %s: %w", configKey, err)
		}
	}

	// Write back
	output, err := yaml.Marshal(&node)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	return writeFilePreserveMode(configPath, output)
}

// mapProvisioningKeyToConfig maps provisioning data keys to config field names.
func mapProvisioningKeyToConfig(key string) string {
	mapping := map[string]string{
		"bot_token":      "bot_token",
		"app_token":      "app_token",
		"application_id": "app_id",
		"signing_secret": "signing_secret",
		"phone_number":   "phone_number",
	}
	if configKey, ok := mapping[key]; ok {
		return configKey
	}
	return key
}

// setYAMLValue sets a value at the given path in a YAML node.
// Duplicated from provisioning/channels.go to avoid import cycle.
func setYAMLValue(node *yaml.Node, path []string, value any) error {
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return fmt.Errorf("empty document")
		}
		return setYAMLValue(node.Content[0], path, value)
	}

	if len(path) == 0 {
		switch v := value.(type) {
		case bool:
			node.Kind = yaml.ScalarNode
			node.Tag = "!!bool"
			if v {
				node.Value = "true"
			} else {
				node.Value = "false"
			}
		case string:
			node.Kind = yaml.ScalarNode
			node.Tag = "!!str"
			node.Value = v
		default:
			return fmt.Errorf("unsupported value type: %T", value)
		}
		return nil
	}

	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("expected mapping at path %v", path)
	}

	key := path[0]
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return setYAMLValue(node.Content[i+1], path[1:], value)
		}
	}

	// Key not found, create it
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
	valueNode := &yaml.Node{}
	if len(path) > 1 {
		valueNode.Kind = yaml.MappingNode
	}
	node.Content = append(node.Content, keyNode, valueNode)
	return setYAMLValue(valueNode, path[1:], value)
}

// writeFilePreserveMode writes data to a file preserving its mode.
// Duplicated from provisioning/channels.go to avoid import cycle.
func writeFilePreserveMode(path string, data []byte) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, mode); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
