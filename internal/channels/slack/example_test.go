package slack_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/haasonsaas/nexus/internal/channels/slack"
	"github.com/haasonsaas/nexus/pkg/models"
)

// Example demonstrates how to use the Slack adapter
func Example() {
	// Configure the adapter with bot and app tokens
	cfg := slack.Config{
		BotToken: "xoxb-your-bot-token",       // Bot token for API calls
		AppToken: "xapp-your-app-level-token", // App token for Socket Mode
	}

	// Create the adapter
	adapter, err := slack.NewAdapter(cfg)
	if err != nil {
		log.Fatalf("Failed to create Slack adapter: %v", err)
	}

	// Create a context for lifecycle management
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Start the adapter (connects to Slack via Socket Mode)
	if err := adapter.Start(ctx); err != nil {
		log.Fatalf("Failed to start Slack adapter: %v", err)
	}

	// Listen for incoming messages
	go func() {
		for msg := range adapter.Messages() {
			fmt.Printf("Received message: %s\n", msg.Content)
			fmt.Printf("From channel: %s\n", msg.Metadata["slack_channel"])
			fmt.Printf("From user: %s\n", msg.Metadata["slack_user_id"])

			// Reply to the message
			reply := &models.Message{
				Content: "Hello! I received your message.",
				Metadata: map[string]any{
					"slack_channel":   msg.Metadata["slack_channel"],
					"slack_thread_ts": msg.Metadata["slack_ts"], // Reply in thread
				},
			}

			if err := adapter.Send(ctx, reply); err != nil {
				log.Printf("Failed to send reply: %v", err)
			}
		}
	}()

	// Check connection status
	status := adapter.Status()
	fmt.Printf("Connected: %v\n", status.Connected)

	// Gracefully stop the adapter
	if err := adapter.Stop(ctx); err != nil {
		log.Printf("Error stopping adapter: %v", err)
	}
}

// ExampleAdapter_Send demonstrates sending messages with different features
func ExampleAdapter_Send() {
	cfg := slack.Config{
		BotToken: "xoxb-your-bot-token",
		AppToken: "xapp-your-app-level-token",
	}

	adapter, err := slack.NewAdapter(cfg)
	if err != nil {
		log.Fatalf("Failed to create Slack adapter: %v", err)
	}
	ctx := context.Background()

	// Simple text message
	simpleMsg := &models.Message{
		Content: "Hello, Slack!",
		Metadata: map[string]any{
			"slack_channel": "C123456789", // Channel ID
		},
	}
	_ = adapter.Send(ctx, simpleMsg)

	// Message with attachments (images will be displayed inline)
	msgWithImage := &models.Message{
		Content: "Check out this image:",
		Attachments: []models.Attachment{
			{
				Type:     "image",
				URL:      "https://example.com/image.png",
				Filename: "image.png",
			},
		},
		Metadata: map[string]any{
			"slack_channel": "C123456789",
		},
	}
	_ = adapter.Send(ctx, msgWithImage)

	// Thread reply
	threadReply := &models.Message{
		Content: "This is a reply in a thread",
		Metadata: map[string]any{
			"slack_channel":   "C123456789",
			"slack_thread_ts": "1234567890.123456", // Original message timestamp
		},
	}
	_ = adapter.Send(ctx, threadReply)

	// Message with reaction
	msgWithReaction := &models.Message{
		Content: "React to this!",
		Metadata: map[string]any{
			"slack_channel":  "C123456789",
			"slack_reaction": "thumbsup", // Adds :thumbsup: reaction
		},
	}
	_ = adapter.Send(ctx, msgWithReaction)
}
