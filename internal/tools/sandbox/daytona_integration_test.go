package sandbox

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDaytonaExecutor_Run(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Daytona integration test in short mode")
	}

	apiKey := strings.TrimSpace(os.Getenv("DAYTONA_API_KEY"))
	jwtToken := strings.TrimSpace(os.Getenv("DAYTONA_JWT_TOKEN"))
	if apiKey == "" && jwtToken == "" {
		t.Skip("DAYTONA_API_KEY or DAYTONA_JWT_TOKEN not set")
	}
	if jwtToken != "" && strings.TrimSpace(os.Getenv("DAYTONA_ORGANIZATION_ID")) == "" {
		t.Skip("DAYTONA_ORGANIZATION_ID required with DAYTONA_JWT_TOKEN")
	}

	executor, err := NewExecutor(WithBackend(BackendDaytona))
	if err != nil {
		t.Fatalf("NewExecutor: %v", err)
	}
	defer executor.Close()

	params := ExecuteParams{
		Language: "python",
		Code:     `print(open("input.txt", "r").read().strip())`,
		Files: map[string]string{
			"input.txt": "hello-daytona",
		},
		Timeout: 120,
	}

	payload, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	result, err := executor.Execute(ctx, payload)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.IsError {
		t.Fatalf("execution failed: %s", result.Content)
	}
	if !strings.Contains(result.Content, "hello-daytona") {
		t.Fatalf("unexpected output: %s", result.Content)
	}
}
