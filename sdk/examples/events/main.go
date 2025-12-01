// Package main demonstrates listening to runtime events from the SDK.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func main() {
	ctx := context.Background()

	eventBus := events.NewEventBus()
	manager, pack := setupManager(eventBus)

	conversation, err := manager.CreateConversation(ctx, pack, sdk.ConversationConfig{
		UserID:     "user-123",
		PromptName: "chat",
	})
	if err != nil {
		log.Fatalf("failed to create conversation: %v", err)
	}

	conversation.AddEventListener(func(e *events.Event) {
		fmt.Printf("[%s] %s\n", e.Timestamp.Format(time.RFC3339), e.Type)
	})

	response, err := conversation.Send(ctx, "Hello!")
	if err != nil {
		log.Fatalf("send failed: %v", err)
	}

	fmt.Printf("\nAssistant: %s\n", response.Content)
}

func setupManager(bus *events.EventBus) (*sdk.ConversationManager, *sdk.Pack) {
	manager, err := sdk.NewConversationManager(
		sdk.WithProvider(mock.NewProvider("mock", "test-model", false)),
		sdk.WithEventBus(bus),
	)
	if err != nil {
		log.Fatalf("failed to create manager: %v", err)
	}

	pack := &sdk.Pack{
		ID:          "event-demo",
		Name:        "event-demo",
		Version:     "1.0.0",
		Description: "Minimal pack for event listener demo",
		TemplateEngine: sdk.TemplateEngine{
			Version: "1.0.0",
			Syntax:  "go",
		},
		Prompts: map[string]*sdk.Prompt{
			"chat": {
				ID:             "chat",
				Name:           "chat",
				Description:    "Simple chat prompt",
				Version:        "1.0.0",
				SystemTemplate: "You are a helpful assistant.",
				Variables:      []*sdk.Variable{},
				Parameters: &sdk.Parameters{
					Temperature: 0.7,
					MaxTokens:   64,
				},
			},
		},
		Fragments: map[string]string{},
		Tools:     map[string]*sdk.Tool{},
	}

	return manager, pack
}
