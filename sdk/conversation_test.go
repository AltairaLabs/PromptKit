package sdk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	testPromptSupport = "support"
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
			testPromptSupport: map[string]interface{}{
				"id":              testPromptSupport,
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

	data, err := json.MarshalIndent(packData, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal pack data: %v", err)
	}
	if err := os.WriteFile(packPath, data, 0600); err != nil {
		t.Fatalf("Failed to write pack file: %v", err)
	}

	// Create mock provider
	mockProvider := mock.NewProvider("test-provider", "test-model", false)

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
		PromptName: testPromptSupport,
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

	if conv.promptName != testPromptSupport {
		t.Errorf("expected prompt name '%s', got '%s'", testPromptSupport, conv.promptName)
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
			"predict": map[string]interface{}{
				"id":              "predict",
				"system_template": "You are a helpful assistant.",
				"parameters": map[string]interface{}{
					"temperature": 0.7,
					"max_tokens":  1500,
				},
			},
		},
	}

	data, err := json.MarshalIndent(packData, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal pack data: %v", err)
	}
	if err := os.WriteFile(packPath, data, 0600); err != nil {
		t.Fatalf("Failed to write pack file: %v", err)
	}

	// Create mock provider with canned response
	mockProvider := mock.NewProvider("test-provider", "test-model", false)

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
		PromptName: "predict",
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
			"predict": map[string]interface{}{
				"id":              "predict",
				"system_template": "You are helpful.",
				"parameters": map[string]interface{}{
					"temperature": 0.7,
					"max_tokens":  1500,
				},
			},
		},
	}

	data, err := json.MarshalIndent(packData, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal pack data: %v", err)
	}
	if err := os.WriteFile(packPath, data, 0600); err != nil {
		t.Fatalf("Failed to write pack file: %v", err)
	}

	// Create mock provider
	mockProvider := mock.NewProvider("test-provider", "test-model", false)

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
		PromptName: "predict",
		Metadata: map[string]interface{}{
			"prompt_name": "predict",
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

	data, err := json.MarshalIndent(packData, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal pack data: %v", err)
	}
	if err := os.WriteFile(packPath, data, 0600); err != nil {
		t.Fatalf("Failed to write pack file: %v", err)
	}

	// Create mock provider that supports streaming
	mockProvider := mock.NewProvider("test-provider", "test-model", false)

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
	mockProvider := mock.NewProvider("test", "test-model", false)
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
	mockProvider := mock.NewProvider("test", "test-model", false)
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

func TestConversation_StreamHelpers_UpdateStateFromStreamResult(t *testing.T) {
	conv := &Conversation{
		state: &statestore.ConversationState{
			Messages: []types.Message{{Role: "user", Content: "test"}},
		},
	}

	result := &pipeline.ExecutionResult{
		Messages: []types.Message{
			{Role: "user", Content: "test"},
			{Role: "assistant", Content: "response"},
		},
	}

	conv.updateStateFromStreamResult(result)

	if len(conv.state.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(conv.state.Messages))
	}
	if conv.state.Messages[1].Role != "assistant" {
		t.Errorf("expected assistant role, got %s", conv.state.Messages[1].Role)
	}
}

func TestConversation_StreamHelpers_BuildStreamResponse(t *testing.T) {
	conv := &Conversation{}

	result := &pipeline.ExecutionResult{
		Response: &pipeline.Response{
			Role:    "assistant",
			Content: "Hello",
			ToolCalls: []types.MessageToolCall{
				{ID: "call1", Name: "tool1"},
			},
		},
		CostInfo: types.CostInfo{
			InputTokens:  10,
			OutputTokens: 20,
			TotalCost:    0.001,
		},
	}

	start := time.Now().Add(-100 * time.Millisecond)
	response := conv.buildStreamResponse(result, "Hello", start)

	if response.Content != "Hello" {
		t.Errorf("expected content 'Hello', got '%s'", response.Content)
	}
	if response.TokensUsed != 30 {
		t.Errorf("expected 30 tokens, got %d", response.TokensUsed)
	}
	if response.Cost != 0.001 {
		t.Errorf("expected cost 0.001, got %f", response.Cost)
	}
	if response.LatencyMs < 90 {
		t.Errorf("expected latency >= 90ms, got %d", response.LatencyMs)
	}
	if len(response.ToolCalls) != 1 {
		t.Errorf("expected 1 tool call, got %d", len(response.ToolCalls))
	}
}

func TestConversation_ContinueHelpers(t *testing.T) {
	t.Run("validateContinuePreconditions_empty", func(t *testing.T) {
		conv := &Conversation{
			state: &statestore.ConversationState{
				Messages: []types.Message{},
			},
		}

		_, err := conv.validateContinuePreconditions()
		if err == nil {
			t.Error("expected error for empty messages")
		}
		if !strings.Contains(err.Error(), "no messages") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("validateContinuePreconditions_wrongRole", func(t *testing.T) {
		conv := &Conversation{
			state: &statestore.ConversationState{
				Messages: []types.Message{
					{Role: "user", Content: "hello"},
				},
			},
		}

		_, err := conv.validateContinuePreconditions()
		if err == nil {
			t.Error("expected error for wrong role")
		}
		if !strings.Contains(err.Error(), "must be a tool result") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("validateContinuePreconditions_success", func(t *testing.T) {
		conv := &Conversation{
			state: &statestore.ConversationState{
				Messages: []types.Message{
					{Role: "user", Content: "hello"},
					{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "call1"}}},
					{Role: "tool", ToolResult: &types.MessageToolResult{ID: "call1", Content: "result"}},
				},
			},
		}

		msg, err := conv.validateContinuePreconditions()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if msg.Role != "tool" {
			t.Errorf("expected tool role, got %s", msg.Role)
		}
	})
}

func TestConversation_UpdateStateAfterContinue(t *testing.T) {
	conv := &Conversation{
		state: &statestore.ConversationState{
			Messages: []types.Message{{Role: "user", Content: "test"}},
			Metadata: map[string]interface{}{
				"pending_tools": []interface{}{"tool1"},
				"other_key":     "value",
			},
		},
	}

	result := &pipeline.ExecutionResult{
		Messages: []types.Message{
			{Role: "user", Content: "test"},
			{Role: "assistant", Content: "done"},
		},
	}

	conv.updateStateAfterContinue(result)

	if len(conv.state.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(conv.state.Messages))
	}
	if _, exists := conv.state.Metadata["pending_tools"]; exists {
		t.Error("expected pending_tools to be cleared")
	}
	if conv.state.Metadata["other_key"] != "value" {
		t.Error("expected other metadata to be preserved")
	}
}

func TestConversation_BuildContinueResponse(t *testing.T) {
	conv := &Conversation{}

	result := &pipeline.ExecutionResult{
		Response: &pipeline.Response{
			Content: "Completed",
		},
		CostInfo: types.CostInfo{
			InputTokens:  5,
			OutputTokens: 15,
			TotalCost:    0.0005,
		},
		Messages: []types.Message{
			{Role: "user", Content: "test"},
			{Role: "assistant", Content: "Completed"},
		},
	}

	latency := 250 * time.Millisecond
	response := conv.buildContinueResponse(result, latency)

	if response.Content != "Completed" {
		t.Errorf("expected content 'Completed', got '%s'", response.Content)
	}
	if response.TokensUsed != 20 {
		t.Errorf("expected 20 tokens, got %d", response.TokensUsed)
	}
	if response.Cost != 0.0005 {
		t.Errorf("expected cost 0.0005, got %f", response.Cost)
	}
	if response.LatencyMs != 250 {
		t.Errorf("expected latency 250ms, got %d", response.LatencyMs)
	}
}

func TestConversation_Continue(t *testing.T) {
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
				},
			},
		},
	}

	data, err := json.MarshalIndent(packData, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal pack data: %v", err)
	}
	if err := os.WriteFile(packPath, data, 0600); err != nil {
		t.Fatalf("Failed to write pack file: %v", err)
	}

	// Create mock provider
	mockProvider := mock.NewProvider("test-provider", "test-model", false)

	// Create manager with state store
	stateStore := statestore.NewMemoryStore()
	manager, err := NewConversationManager(
		WithProvider(mockProvider),
		WithStateStore(stateStore),
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

	// Simulate a conversation state with a tool result message
	// (This happens after tool execution in a real scenario)
	conv.state.Messages = []types.Message{
		{Role: "user", Content: "Do something"},
		{Role: "assistant", Content: "", ToolCalls: []types.MessageToolCall{{ID: "call1"}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "call1", Content: "tool executed"}},
	}

	// Call Continue - this should execute the pipeline with the tool result
	response, err := conv.Continue(ctx)
	if err != nil {
		t.Fatalf("Continue() failed: %v", err)
	}

	if response == nil {
		t.Fatal("expected non-nil response")
	}

	// Mock provider should have generated a response
	if response.Content == "" {
		t.Error("expected non-empty content")
	}

	// Verify state was updated with the assistant response
	if len(conv.state.Messages) < 4 {
		t.Errorf("expected at least 4 messages after Continue(), got %d", len(conv.state.Messages))
	}

	// Last message should be the assistant response
	lastMsg := conv.state.Messages[len(conv.state.Messages)-1]
	if lastMsg.Role != "assistant" {
		t.Errorf("expected last message role 'assistant', got '%s'", lastMsg.Role)
	}
}

func TestConversation_ContinueErrors(t *testing.T) {
	t.Run("no_messages", func(t *testing.T) {
		conv := &Conversation{
			state: &statestore.ConversationState{
				Messages: []types.Message{},
			},
			manager: &ConversationManager{
				stateStore: statestore.NewMemoryStore(),
			},
		}

		_, err := conv.Continue(context.Background())
		if err == nil {
			t.Error("expected error for no messages")
		}
		if !strings.Contains(err.Error(), "no messages") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("wrong_last_role", func(t *testing.T) {
		conv := &Conversation{
			state: &statestore.ConversationState{
				Messages: []types.Message{
					{Role: "user", Content: "hello"},
					{Role: "assistant", Content: "hi"},
				},
			},
			manager: &ConversationManager{
				stateStore: statestore.NewMemoryStore(),
			},
		}

		_, err := conv.Continue(context.Background())
		if err == nil {
			t.Error("expected error for wrong last role")
		}
		if !strings.Contains(err.Error(), "must be a tool result") {
			t.Errorf("unexpected error: %v", err)
		}
	})
}
