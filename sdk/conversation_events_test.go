package sdk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
)

func TestConversationEmitsPipelineEvents(t *testing.T) {
	t.Parallel()

	eventBus := events.NewEventBus()
	manager, err := NewConversationManager(
		WithProvider(mock.NewProvider("mock", "test-model", false)),
		WithEventBus(eventBus),
	)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	pack := minimalPack()

	conv, err := manager.CreateConversation(context.Background(), pack, ConversationConfig{
		UserID:     "user-1",
		PromptName: "chat",
	})
	if err != nil {
		t.Fatalf("failed to create conversation: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)

	conv.AddEventListener(func(e *events.Event) {
		if e.Type == events.EventPipelineCompleted {
			wg.Done()
		}
	})

	if _, err := conv.Send(context.Background(), "hi"); err != nil {
		t.Fatalf("send failed: %v", err)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for pipeline completion event")
	}
}

func minimalPack() *Pack {
	return &Pack{
		ID:          "event-demo",
		Name:        "event-demo",
		Version:     "1.0.0",
		Description: "Minimal pack for event listener demo",
		TemplateEngine: TemplateEngine{
			Version: "1.0.0",
			Syntax:  "go",
		},
		Prompts: map[string]*Prompt{
			"chat": {
				ID:             "chat",
				Name:           "chat",
				Description:    "Simple chat prompt",
				Version:        "1.0.0",
				SystemTemplate: "You are a helpful assistant.",
				Variables:      []*Variable{},
				Parameters: &Parameters{
					Temperature: 0.7,
					MaxTokens:   64,
				},
			},
		},
		Fragments: map[string]string{},
		Tools:     map[string]*Tool{},
	}
}
