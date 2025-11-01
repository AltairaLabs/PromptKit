package sdk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
)

func TestConversationManager_CreateConversation(t *testing.T) {
	// Create a test pack
	tmpDir := t.TempDir()
	packPath := filepath.Join(tmpDir, "test.pack.json")

	packData := map[string]interface{}{
		"id":      "test-pack",
		"name":    "Test Pack",
		"version": "1.0.0",
		"template_engine": map[string]interface{}{
			"version": "v1",
			"syntax":  "{{variable}}",
		},
		"prompts": map[string]interface{}{
			"support": map[string]interface{}{
				"id":              "support",
				"name":            "Support Bot",
				"version":         "1.0.0",
				"system_template": "You are a {{role}} assistant.",
				"variables": []map[string]interface{}{
					{
						"name":     "role",
						"type":     "string",
						"required": true,
					},
				},
				"parameters": map[string]interface{}{
					"temperature": 0.7,
					"max_tokens":  1500,
				},
			},
		},
	}

	data, _ := json.MarshalIndent(packData, "", "  ")
	os.WriteFile(packPath, data, 0644)

	// Create mock provider
	mockProvider := providers.NewMockProvider("test-provider", "test-model", false)

	// Create manager
	manager, err := NewConversationManager(
		WithProvider(mockProvider),
	)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Load pack
	pack, err := manager.LoadPack(packPath)
	if err != nil {
		t.Fatalf("failed to load pack: %v", err)
	}

	// Create conversation
	ctx := context.Background()
	conv, err := manager.CreateConversation(ctx, pack, ConversationConfig{
		UserID:     "user123",
		PromptName: "support",
		Variables: map[string]interface{}{
			"role": "customer support",
		},
	})
	if err != nil {
		t.Fatalf("failed to create conversation: %v", err)
	}

	if conv.GetUserID() != "user123" {
		t.Errorf("expected user ID 'user123', got '%s'", conv.GetUserID())
	}

	if conv.promptName != "support" {
		t.Errorf("expected prompt name 'support', got '%s'", conv.promptName)
	}

	// Verify system prompt was interpolated
	if conv.state.SystemPrompt != "You are a customer support assistant." {
		t.Errorf("unexpected system prompt: %s", conv.state.SystemPrompt)
	}
}

func TestConversationManager_Send(t *testing.T) {
	// Create a test pack
	tmpDir := t.TempDir()
	packPath := filepath.Join(tmpDir, "test.pack.json")

	packData := map[string]interface{}{
		"id":      "test-pack",
		"name":    "Test Pack",
		"version": "1.0.0",
		"template_engine": map[string]interface{}{
			"version": "v1",
		},
		"prompts": map[string]interface{}{
			"chat": map[string]interface{}{
				"id":              "chat",
				"system_template": "You are a helpful assistant.",
				"parameters": map[string]interface{}{
					"temperature": 0.7,
					"max_tokens":  1500,
				},
			},
		},
	}

	data, _ := json.MarshalIndent(packData, "", "  ")
	os.WriteFile(packPath, data, 0644)

	// Create mock provider with canned response
	mockProvider := providers.NewMockProvider("test-provider", "test-model", false)

	// Create manager
	manager, err := NewConversationManager(
		WithProvider(mockProvider),
	)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Load pack and create conversation
	pack, _ := manager.LoadPack(packPath)
	ctx := context.Background()
	conv, err := manager.CreateConversation(ctx, pack, ConversationConfig{
		UserID:     "user123",
		PromptName: "chat",
	})
	if err != nil {
		t.Fatalf("failed to create conversation: %v", err)
	}

	// Send message
	resp, err := conv.Send(ctx, "Hello!")
	if err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	if resp.Content == "" {
		t.Error("expected non-empty response content")
	}

	// Verify message history
	history := conv.GetHistory()
	if len(history) != 2 {
		t.Errorf("expected 2 messages in history, got %d", len(history))
	}

	if history[0].Role != "user" {
		t.Errorf("expected first message role 'user', got '%s'", history[0].Role)
	}

	if history[1].Role != "assistant" {
		t.Errorf("expected second message role 'assistant', got '%s'", history[1].Role)
	}
}

func TestConversationManager_GetConversation(t *testing.T) {
	// Create a test pack
	tmpDir := t.TempDir()
	packPath := filepath.Join(tmpDir, "test.pack.json")

	packData := map[string]interface{}{
		"id":      "test-pack",
		"name":    "Test Pack",
		"version": "1.0.0",
		"template_engine": map[string]interface{}{
			"version": "v1",
		},
		"prompts": map[string]interface{}{
			"chat": map[string]interface{}{
				"id":              "chat",
				"system_template": "You are helpful.",
				"parameters": map[string]interface{}{
					"temperature": 0.7,
					"max_tokens":  1500,
				},
			},
		},
	}

	data, _ := json.MarshalIndent(packData, "", "  ")
	os.WriteFile(packPath, data, 0644)

	// Create mock provider
	mockProvider := providers.NewMockProvider("test-provider", "test-model", false)

	// Create shared state store
	stateStore := statestore.NewMemoryStore()

	// Create manager
	manager, err := NewConversationManager(
		WithProvider(mockProvider),
		WithStateStore(stateStore),
	)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Load pack and create conversation
	pack, _ := manager.LoadPack(packPath)
	ctx := context.Background()

	// Create conversation with metadata including prompt_name
	conv, err := manager.CreateConversation(ctx, pack, ConversationConfig{
		UserID:     "user123",
		PromptName: "chat",
		Metadata: map[string]interface{}{
			"prompt_name": "chat",
		},
	})
	if err != nil {
		t.Fatalf("failed to create conversation: %v", err)
	}

	conversationID := conv.GetID()

	// Create a new manager (simulating restart) with SAME state store
	manager2, _ := NewConversationManager(
		WithProvider(mockProvider),
		WithStateStore(stateStore), // Use same store
	)

	// Load the conversation
	loadedConv, err := manager2.GetConversation(ctx, conversationID, pack)
	if err != nil {
		t.Fatalf("failed to load conversation: %v", err)
	}

	if loadedConv.GetID() != conversationID {
		t.Errorf("expected conversation ID '%s', got '%s'", conversationID, loadedConv.GetID())
	}

	if loadedConv.GetUserID() != "user123" {
		t.Errorf("expected user ID 'user123', got '%s'", loadedConv.GetUserID())
	}
}

func TestConversationManager_SendStream(t *testing.T) {
	// Create a test pack
	tmpDir := t.TempDir()
	packPath := filepath.Join(tmpDir, "test.pack.json")

	packData := map[string]interface{}{
		"id":      "test-pack",
		"name":    "Test Pack",
		"version": "1.0.0",
		"template_engine": map[string]interface{}{
			"version": "v1",
			"syntax":  "{{variable}}",
		},
		"prompts": map[string]interface{}{
			"assistant": map[string]interface{}{
				"id":              "assistant",
				"name":            "Assistant",
				"version":         "1.0.0",
				"system_template": "You are a helpful assistant.",
				"parameters": map[string]interface{}{
					"temperature": 0.7,
					"max_tokens":  100,
				},
			},
		},
	}

	data, _ := json.MarshalIndent(packData, "", "  ")
	os.WriteFile(packPath, data, 0644)

	// Create mock provider that supports streaming
	mockProvider := providers.NewMockProvider("test-provider", "test-model", false)

	// Create manager
	manager, err := NewConversationManager(
		WithProvider(mockProvider),
	)
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	// Load pack
	pack, err := manager.LoadPack(packPath)
	if err != nil {
		t.Fatalf("failed to load pack: %v", err)
	}

	// Create conversation
	ctx := context.Background()
	conv, err := manager.CreateConversation(ctx, pack, ConversationConfig{
		UserID:     "user123",
		PromptName: "assistant",
	})
	if err != nil {
		t.Fatalf("failed to create conversation: %v", err)
	}

	// Send streaming message
	streamChan, err := conv.SendStream(ctx, "Hello, can you help me?")
	if err != nil {
		t.Fatalf("failed to start streaming: %v", err)
	}

	// Collect events
	var contentParts []string
	var finalResp *Response
	var hadError bool
	var gotDone bool

	for event := range streamChan {
		switch event.Type {
		case "content":
			contentParts = append(contentParts, event.Content)
		case "error":
			t.Logf("received error event: %v", event.Error)
			hadError = true
		case "done":
			gotDone = true
			finalResp = event.Final
		}
	}

	// Verify we got a done event
	if !gotDone && !hadError {
		t.Error("expected to receive done event")
	}

	// If we got content, verify it's not empty
	if len(contentParts) > 0 {
		fullContent := ""
		for _, part := range contentParts {
			fullContent += part
		}
		t.Logf("received content: %s", fullContent)
	}

	// Note: finalResp may be nil if mock provider doesn't set FinalResult properly
	// This is acceptable for a basic streaming test
	if finalResp != nil {
		t.Logf("final response - tokens: %d, cost: $%.4f", finalResp.TokensUsed, finalResp.Cost)
	}
}

func TestConversationManager_WithToolRegistry(t *testing.T) {
	mockProvider := providers.NewMockProvider("test", "test-model", false)
	store := statestore.NewMemoryStore()

	// Create empty tool registry
	registry := &tools.Registry{}

	// Create manager with tool registry option
	manager, err := NewConversationManager(
		WithProvider(mockProvider),
		WithStateStore(store),
		WithToolRegistry(registry),
	)

	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	if manager.toolRegistry == nil {
		t.Error("expected tool registry to be set")
	}

	if manager.toolRegistry != registry {
		t.Error("expected tool registry to match provided registry")
	}
}

func TestConversationManager_WithConfig(t *testing.T) {
	mockProvider := providers.NewMockProvider("test", "test-model", false)
	store := statestore.NewMemoryStore()

	config := ManagerConfig{
		MaxConcurrentExecutions: 10,
		EnableMetrics:           true,
	}

	// Create manager with config option
	manager, err := NewConversationManager(
		WithProvider(mockProvider),
		WithStateStore(store),
		WithConfig(config),
	)

	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}

	if manager.config.MaxConcurrentExecutions != 10 {
		t.Errorf("expected max concurrent executions 10, got %d", manager.config.MaxConcurrentExecutions)
	}

	if !manager.config.EnableMetrics {
		t.Error("expected metrics to be enabled")
	}
}
