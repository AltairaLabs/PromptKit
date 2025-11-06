package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/openai"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// This example demonstrates context window management with token budgets and truncation strategies.
// When a conversation exceeds the configured token budget, the SDK automatically applies the
// configured truncation strategy to keep the conversation within limits.

func main() {
	ctx := context.Background()

	// Get API key from environment
	apiKey := os.Getenv("OPENAI_API_KEY") // NOSONAR: Example code showing environment variable usage
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable not set") // NOSONAR: Example code
	}

	// Create an OpenAI provider
	provider := openai.NewOpenAIProvider(
		"openai",
		"gpt-4o-mini",
		"",
		providers.ProviderDefaults{
			Temperature: 0.7,
			MaxTokens:   500,
		},
		false,
	)

	// Example 1: Conversation with "oldest" truncation strategy
	fmt.Println("=== Example 1: Oldest-first truncation ===")
	fmt.Println("This strategy removes the oldest messages when the token budget is exceeded.")
	fmt.Println()

	contextPolicy1 := &middleware.ContextBuilderPolicy{
		TokenBudget:      2000, // Small budget to trigger truncation quickly
		ReserveForOutput: 500,  // Reserve tokens for the response
		Strategy:         middleware.TruncateOldest,
		CacheBreakpoints: false, // No cache breakpoints for this example
	}

	manager1, err := sdk.NewConversationManager(
		sdk.WithProvider(provider),
	)
	if err != nil {
		log.Fatalf("Failed to create conversation manager: %v", err)
	}

	pack1 := createTestPack()

	config1 := sdk.ConversationConfig{
		UserID:        "user-123",
		PromptName:    "assistant",
		ContextPolicy: contextPolicy1,
		Variables: map[string]interface{}{
			"name": "Context Manager",
		},
	}

	conv1, err := manager1.CreateConversation(ctx, pack1, config1)
	if err != nil {
		log.Fatalf("Failed to create conversation: %v", err)
	}

	// Have a longer conversation that will exceed the token budget
	messages1 := []string{
		"What is context window management?",
		"Can you explain token budgets in more detail?",
		"How does truncation work?",
		"What happens to the oldest messages?",
		"Tell me about different truncation strategies.",
	}

	for i, msg := range messages1 {
		fmt.Printf("\n[Turn %d] User: %s\n", i+1, msg)
		result, err := conv1.Send(ctx, msg)
		if err != nil {
			log.Fatalf("Failed to send message: %v", err)
		}
		fmt.Printf("[Turn %d] Assistant: %s\n", i+1, result.Content)
		fmt.Printf("[Turn %d] Tokens used: %d | Cost: $%.4f\n", i+1, result.TokensUsed, result.Cost)
	}

	// Example 2: Conversation with "fail" strategy
	fmt.Println("\n\n=== Example 2: Fail-on-overflow strategy ===")
	fmt.Println("This strategy returns an error when the token budget is exceeded.")
	fmt.Println()

	contextPolicy2 := &middleware.ContextBuilderPolicy{
		TokenBudget:      1000, // Very small budget to trigger failure
		ReserveForOutput: 500,
		Strategy:         middleware.TruncateFail,
		CacheBreakpoints: false,
	}

	manager2, err := sdk.NewConversationManager(
		sdk.WithProvider(provider),
	)
	if err != nil {
		log.Fatalf("Failed to create conversation manager: %v", err)
	}

	pack2 := createTestPack()

	config2 := sdk.ConversationConfig{
		UserID:        "user-456",
		PromptName:    "assistant",
		ContextPolicy: contextPolicy2,
		Variables: map[string]interface{}{
			"name": "Strict Budget Manager",
		},
	}

	conv2, err := manager2.CreateConversation(ctx, pack2, config2)
	if err != nil {
		log.Fatalf("Failed to create conversation: %v", err)
	}

	// Send messages until we hit the budget limit
	messages2 := []string{
		"Hello, how are you?",
		"Can you tell me a story?",
		"Make it a long story about adventure.",
		"Add more details about the characters.",
	}

	for i, msg := range messages2 {
		fmt.Printf("\n[Turn %d] User: %s\n", i+1, msg)
		result, err := conv2.Send(ctx, msg)
		if err != nil {
			// Check if it's a token budget error
			if strings.Contains(err.Error(), "token budget") || strings.Contains(err.Error(), "context length") {
				fmt.Printf("[Turn %d] ⚠️  Token budget exceeded! Error: %v\n", i+1, err)
				break
			}
			log.Fatalf("Failed to send message: %v", err)
		}
		fmt.Printf("[Turn %d] Assistant: %s\n", i+1, result.Content)
		fmt.Printf("[Turn %d] Tokens used: %d | Cost: $%.4f\n", i+1, result.TokensUsed, result.Cost)
	}

	fmt.Println("\n\n=== Context Management Example Complete ===")
	fmt.Println("Key takeaways:")
	fmt.Println("- Token budgets help control costs and stay within model limits")
	fmt.Println("- Different truncation strategies suit different use cases")
	fmt.Println("- 'oldest' is good for long conversations where early context is less important")
	fmt.Println("- 'fail' is good when you want explicit control and error handling")
	fmt.Println("- 'summarize' (not shown) creates summaries of truncated messages")
	fmt.Println("- 'relevance' (not shown) uses semantic relevance to keep important messages")
}

// createTestPack creates a simple test pack for demonstration
func createTestPack() *sdk.Pack {
	return &sdk.Pack{
		ID:      "context-management-demo",
		Name:    "Context Management Demo Pack",
		Version: "1.0.0",
		TemplateEngine: sdk.TemplateEngine{
			Version:  "v1",
			Syntax:   "{{variable}}",
			Features: []string{"basic_substitution"},
		},
		Prompts: map[string]*sdk.Prompt{
			"assistant": {
				ID:   "assistant",
				Name: "Assistant",
				SystemTemplate: `You are a helpful assistant named {{name}}. When asked about context management, 
explain that you're operating under token budget constraints. Try to be concise 
but informative in your responses.`,
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
