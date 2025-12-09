// Package main demonstrates the Variable Providers feature in the PromptKit SDK.
//
// This example shows:
//   - Using the built-in TimeProvider for dynamic time variables
//   - Creating a custom provider for application-specific variables
//   - How providers inject variables automatically before each Send()
//
// Run with:
//
//	export OPENAI_API_KEY=your-key
//	go run .
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/variables"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// UserContextProvider demonstrates a custom variable provider.
// In real applications, this might fetch user preferences from a database,
// load session data, or integrate with external services.
type UserContextProvider struct {
	userID      string
	preferences map[string]string
}

// NewUserContextProvider creates a provider with user-specific context.
func NewUserContextProvider(userID string) *UserContextProvider {
	return &UserContextProvider{
		userID: userID,
		preferences: map[string]string{
			"language":       "English",
			"response_style": "friendly and conversational",
			"expertise":      "beginner",
		},
	}
}

// Name returns the provider identifier.
func (p *UserContextProvider) Name() string {
	return "user_context"
}

// Provide returns user-specific variables.
// In production, this might query a database or cache.
func (p *UserContextProvider) Provide(ctx context.Context) (map[string]string, error) {
	return map[string]string{
		"user_id":        p.userID,
		"user_language":  p.preferences["language"],
		"response_style": p.preferences["response_style"],
		"user_expertise": p.preferences["expertise"],
	}, nil
}

func main() {
	// Check for API key first
	if os.Getenv("OPENAI_API_KEY") == "" {
		fmt.Println("⚠️  Set OPENAI_API_KEY environment variable to run this example")
		fmt.Println("   export OPENAI_API_KEY=your-key")
		os.Exit(1)
	}

	// Create providers
	timeProvider := variables.NewTimeProviderWithLocation(time.Local)
	userProvider := NewUserContextProvider("user-12345")

	// Open conversation with variable providers
	// Variables from providers are automatically resolved before each Send()
	conv, err := sdk.Open("./assistant.pack.json", "assistant",
		sdk.WithVariableProvider(timeProvider),
		sdk.WithVariableProvider(userProvider),
	)
	if err != nil {
		log.Fatalf("Failed to open pack: %v", err)
	}
	defer conv.Close()

	ctx := context.Background()

	fmt.Println("=== Variable Providers Demo ===")
	fmt.Println()

	// First message - the assistant will use injected time and user context
	fmt.Println("User: What time is it and how should you respond to me?")
	resp, err := conv.Send(ctx, "What time is it and how should you respond to me?")
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}
	fmt.Println("Assistant:", resp.Text())
	fmt.Println()

	// Second message - demonstrate that time updates dynamically
	fmt.Println("--- Waiting 2 seconds ---")
	time.Sleep(2 * time.Second)

	fmt.Println("User: What's the current time now?")
	resp, err = conv.Send(ctx, "What's the current time now?")
	if err != nil {
		log.Fatalf("Failed to send message: %v", err)
	}
	fmt.Println("Assistant:", resp.Text())
	fmt.Println()

	// Show the variables that were provided
	fmt.Println("=== Variables Provided ===")
	vars, _ := timeProvider.Provide(ctx)
	for k, v := range vars {
		fmt.Printf("  %s: %s\n", k, v)
	}
	vars, _ = userProvider.Provide(ctx)
	for k, v := range vars {
		fmt.Printf("  %s: %s\n", k, v)
	}
}
