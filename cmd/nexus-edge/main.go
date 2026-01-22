// nexus-edge is the Nexus Edge Daemon that connects to a Nexus gateway
// and exposes local tools for remote execution.
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/haasonsaas/nexus/internal/edge"
	proto "github.com/haasonsaas/nexus/pkg/proto"
)

func main() {
	// Parse flags
	gatewayAddr := flag.String("gateway", "localhost:50051", "Gateway address")
	edgeID := flag.String("id", "", "Edge daemon ID (required)")
	edgeName := flag.String("name", "", "Edge daemon name")
	authMethod := flag.String("auth", "shared_secret", "Auth method: shared_secret or tofu")
	sharedSecret := flag.String("secret", "", "Shared secret for authentication")
	keyFile := flag.String("key", "", "Private key file for TOFU auth")
	generateKey := flag.Bool("generate-key", false, "Generate a new ed25519 key pair")
	verbose := flag.Bool("v", false, "Verbose logging")
	flag.Parse()

	// Set up logging
	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	// Handle key generation
	if *generateKey {
		if err := generateKeyPair(*keyFile); err != nil {
			logger.Error("failed to generate key", "error", err)
			os.Exit(1)
		}
		return
	}

	// Validate required flags
	if *edgeID == "" {
		logger.Error("--id is required")
		flag.Usage()
		os.Exit(1)
	}
	if *edgeName == "" {
		*edgeName = *edgeID
	}

	// Determine auth method
	var protoAuth proto.AuthMethod
	var privateKey ed25519.PrivateKey

	switch *authMethod {
	case "shared_secret":
		if *sharedSecret == "" {
			logger.Error("--secret is required for shared_secret auth")
			os.Exit(1)
		}
		protoAuth = proto.AuthMethod_AUTH_METHOD_SHARED_SECRET

	case "tofu":
		if *keyFile == "" {
			logger.Error("--key is required for tofu auth")
			os.Exit(1)
		}
		key, err := loadPrivateKey(*keyFile)
		if err != nil {
			logger.Error("failed to load private key", "error", err)
			os.Exit(1)
		}
		privateKey = key
		protoAuth = proto.AuthMethod_AUTH_METHOD_TOFU

	default:
		logger.Error("invalid auth method", "method", *authMethod)
		os.Exit(1)
	}

	// Create client
	client := edge.NewClient(edge.ClientConfig{
		GatewayAddr:             *gatewayAddr,
		EdgeID:                  *edgeID,
		EdgeName:                *edgeName,
		AuthMethod:              protoAuth,
		SharedSecret:            *sharedSecret,
		PrivateKey:              privateKey,
		HeartbeatInterval:       30 * time.Second,
		ReconnectDelay:          5 * time.Second,
		MaxConcurrentExecutions: 10,
	}, logger)

	// Register example tools
	registerExampleTools(client, logger)

	// Set up context with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Run the daemon
	logger.Info("starting nexus-edge daemon",
		"id", *edgeID,
		"gateway", *gatewayAddr,
	)

	if err := client.Run(ctx); err != nil && err != context.Canceled {
		logger.Error("daemon exited with error", "error", err)
		os.Exit(1)
	}

	logger.Info("daemon stopped")
}

func generateKeyPair(outputFile string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	if outputFile == "" {
		outputFile = "edge_key"
	}

	// Write private key
	privFile := outputFile
	if err := os.WriteFile(privFile, priv, 0600); err != nil {
		return fmt.Errorf("write private key: %w", err)
	}

	// Write public key
	pubFile := outputFile + ".pub"
	if err := os.WriteFile(pubFile, pub, 0644); err != nil {
		return fmt.Errorf("write public key: %w", err)
	}

	fmt.Printf("Generated key pair:\n")
	fmt.Printf("  Private key: %s\n", privFile)
	fmt.Printf("  Public key:  %s\n", pubFile)
	return nil
}

func loadPrivateKey(filename string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	if len(data) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid key size: expected %d, got %d", ed25519.PrivateKeySize, len(data))
	}
	return ed25519.PrivateKey(data), nil
}

// registerExampleTools registers some example tools for demonstration.
func registerExampleTools(client *edge.Client, logger *slog.Logger) {
	// Echo tool - simple echo for testing
	client.RegisterTool(&edge.Tool{
		Name:        "echo",
		Description: "Echo back the input message",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"message": {"type": "string", "description": "Message to echo"}
			},
			"required": ["message"]
		}`),
		Category:  proto.ToolCategory_TOOL_CATEGORY_CUSTOM,
		RiskLevel: proto.RiskLevel_RISK_LEVEL_LOW,
	}, func(ctx context.Context, req *edge.ToolExecutionRequest) (*edge.ToolExecutionResult, error) {
		var input struct {
			Message string `json:"message"`
		}
		if err := json.Unmarshal(req.Input, &input); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}
		logger.Debug("echo tool called", "message", input.Message)
		return &edge.ToolExecutionResult{
			Success: true,
			Output:  map[string]string{"echo": input.Message},
		}, nil
	})

	// System info tool - returns basic system information
	client.RegisterTool(&edge.Tool{
		Name:        "system_info",
		Description: "Get basic system information",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {}
		}`),
		Category:  proto.ToolCategory_TOOL_CATEGORY_SYSTEM,
		RiskLevel: proto.RiskLevel_RISK_LEVEL_LOW,
	}, func(ctx context.Context, req *edge.ToolExecutionRequest) (*edge.ToolExecutionResult, error) {
		hostname, _ := os.Hostname()
		return &edge.ToolExecutionResult{
			Success: true,
			Output: map[string]interface{}{
				"hostname": hostname,
				"os":       os.Getenv("GOOS"),
				"arch":     os.Getenv("GOARCH"),
				"pid":      os.Getpid(),
			},
		}, nil
	})

	// Current time tool
	client.RegisterTool(&edge.Tool{
		Name:        "current_time",
		Description: "Get the current time on this device",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"format": {"type": "string", "description": "Time format (optional, default RFC3339)"}
			}
		}`),
		Category:  proto.ToolCategory_TOOL_CATEGORY_DATA,
		RiskLevel: proto.RiskLevel_RISK_LEVEL_LOW,
	}, func(ctx context.Context, req *edge.ToolExecutionRequest) (*edge.ToolExecutionResult, error) {
		var input struct {
			Format string `json:"format"`
		}
		json.Unmarshal(req.Input, &input)

		format := time.RFC3339
		if input.Format != "" {
			format = input.Format
		}

		return &edge.ToolExecutionResult{
			Success: true,
			Output: map[string]string{
				"time":     time.Now().Format(format),
				"timezone": time.Now().Location().String(),
			},
		}, nil
	})

	// Environment variable tool (restricted)
	client.RegisterTool(&edge.Tool{
		Name:        "get_env",
		Description: "Get an environment variable value",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {"type": "string", "description": "Environment variable name"}
			},
			"required": ["name"]
		}`),
		Category:         proto.ToolCategory_TOOL_CATEGORY_SYSTEM,
		RiskLevel:        proto.RiskLevel_RISK_LEVEL_MEDIUM,
		RequiresApproval: true,
	}, func(ctx context.Context, req *edge.ToolExecutionRequest) (*edge.ToolExecutionResult, error) {
		var input struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(req.Input, &input); err != nil {
			return nil, fmt.Errorf("parse input: %w", err)
		}

		// Block sensitive env vars
		blocked := map[string]bool{
			"AWS_SECRET_ACCESS_KEY": true,
			"API_KEY":               true,
			"PASSWORD":              true,
			"TOKEN":                 true,
		}
		if blocked[input.Name] {
			return &edge.ToolExecutionResult{
				Success:      false,
				ErrorMessage: "access to this environment variable is blocked",
			}, nil
		}

		value := os.Getenv(input.Name)
		return &edge.ToolExecutionResult{
			Success: true,
			Output: map[string]string{
				"name":  input.Name,
				"value": value,
			},
		}, nil
	})

	logger.Info("registered example tools", "count", 4)
}
