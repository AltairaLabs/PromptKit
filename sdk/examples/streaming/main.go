package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	// Check for API key
	apiKey := os.Getenv("OPENAI_API_KEY") // NOSONAR: Example code showing environment variable usage
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required") // NOSONAR: Example code
	}

	// Create provider
	provider := openai.NewProvider(
		"openai",
		"gpt-4",
		"", // Use default base URL
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   500,
		},
		false, // Don't include raw output
	)

	// Create conversation manager
	manager, err := sdk.NewConversationManager(
		sdk.WithProvider(provider),
	)
	if err != nil {
		log.Fatalf("Failed to create conversation manager: %v", err)
	}

	// Create a simple test pack in memory
	pack := createTestPack()

	// Create conversation
	ctx := context.Background()
	conv, err := manager.CreateConversation(ctx, pack, sdk.ConversationConfig{
		UserID:     "user123",
		PromptName: "assistant",
		Variables: map[string]interface{}{
			"name": "Alice",
		},
	})
	if err != nil {
		log.Fatalf("Failed to create conversation: %v", err)
	}

	fmt.Printf("🤖 Streaming conversation started (ID: %s)\n\n", conv.GetID())

	// First message - non-streaming
	fmt.Println("User: Tell me a short story about a robot.")
	fmt.Print("Assistant: ")

	resp, err := conv.Send(ctx, "Tell me a short story about a robot.")
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}
	fmt.Printf("%s\n\n", resp.Content)
	fmt.Printf("💰 Cost: $%.4f | ⏱️  Latency: %dms | 🎫 Tokens: %d\n\n",
		resp.Cost, resp.LatencyMs, resp.TokensUsed)

	// Second message - with streaming
	time.Sleep(1 * time.Second)
	fmt.Println("User: Now tell me another one, but shorter.")
	fmt.Print("Assistant: ")

	streamChan, err := conv.SendStream(ctx, "Now tell me another one, but shorter.")
	if err != nil {
		log.Fatalf("Failed to start streaming: %v", err)
	}

	// Process stream events
	var finalResp *sdk.Response
	for event := range streamChan {
		switch event.Type {
		case "content":
			// Print content deltas as they arrive
			fmt.Print(event.Content)

		case "tool_call":
			// Handle tool calls
			fmt.Printf("\n[Tool Call: %s]\n", event.ToolCall.Name)

		case "error":
			// Handle errors
			fmt.Printf("\n❌ Error: %v\n", event.Error)
			return

		case "done":
			// Stream completed
			fmt.Println()
			finalResp = event.Final
		}
	}

	if finalResp != nil {
		fmt.Printf("\n💰 Cost: $%.4f | ⏱️  Latency: %dms | 🎫 Tokens: %d\n",
			finalResp.Cost, finalResp.LatencyMs, finalResp.TokensUsed)
	}

	// Show conversation history
	fmt.Println("\n📜 Conversation History:")
	history := conv.GetHistory()
	for i, msg := range history {
		preview := msg.Content
		if len(preview) > 60 {
			preview = preview[:60] + "..."
		}
		fmt.Printf("  %d. [%s] %s\n", i+1, msg.Role, preview)
	}
}

// createTestPack creates a simple test pack for demonstration
func createTestPack() *sdk.Pack {
	return &sdk.Pack{
		ID:      "streaming-demo",
		Name:    "Streaming Demo Pack",
		Version: "1.0.0",
		TemplateEngine: sdk.TemplateEngine{
			Version:  "v1",
			Syntax:   "{{variable}}",
			Features: []string{"basic_substitution"},
		},
		Prompts: map[string]*sdk.Prompt{
			"assistant": {
				ID:             "assistant",
				Name:           "Assistant",
				SystemTemplate: "You are a helpful assistant named {{name}}. Be concise and friendly.",
				Variables: []*sdk.Variable{
					{
						Name:     "name",
						Type:     "string",
						Required: true,
					},
				},
				Parameters: &sdk.Parameters{
					Temperature: 0.7,
					MaxTokens:   500,
				},
			},
		},
	}
}
