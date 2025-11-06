package mock

import (

"github.com/AltairaLabs/PromptKit/runtime/providers"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestInMemoryMockRepository(t *testing.T) {
	repo := NewInMemoryMockRepository("default response")

	ctx := context.Background()

	// Test default response
	response, err := repo.GetResponse(ctx, MockResponseParams{
		ProviderID: "test-provider",
		ModelName:  "test-model",
	})
	if err != nil {
		t.Fatalf("GetResponse failed: %v", err)
	}
	if response != "default response" {
		t.Errorf("Expected 'default response', got %q", response)
	}

	// Set scenario-specific response
	repo.SetResponse("scenario1", 0, "scenario1 default")
	response, err = repo.GetResponse(ctx, MockResponseParams{
		ScenarioID: "scenario1",
		ProviderID: "test-provider",
	})
	if err != nil {
		t.Fatalf("GetResponse failed: %v", err)
	}
	if response != "scenario1 default" {
		t.Errorf("Expected 'scenario1 default', got %q", response)
	}

	// Set turn-specific response
	repo.SetResponse("scenario1", 1, "turn 1 response")
	response, err = repo.GetResponse(ctx, MockResponseParams{
		ScenarioID: "scenario1",
		TurnNumber: 1,
		ProviderID: "test-provider",
	})
	if err != nil {
		t.Fatalf("GetResponse failed: %v", err)
	}
	if response != "turn 1 response" {
		t.Errorf("Expected 'turn 1 response', got %q", response)
	}

	// Test fallback to scenario default when turn not found
	response, err = repo.GetResponse(ctx, MockResponseParams{
		ScenarioID: "scenario1",
		TurnNumber: 99,
		ProviderID: "test-provider",
	})
	if err != nil {
		t.Fatalf("GetResponse failed: %v", err)
	}
	if response != "scenario1 default" {
		t.Errorf("Expected 'scenario1 default', got %q", response)
	}
}

func TestFileMockRepository(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a mock config file
	configPath := filepath.Join(tmpDir, "mock-config.yaml")
	configContent := `defaultResponse: "global default"
scenarios:
  scenario1:
    defaultResponse: "scenario1 default"
    turns:
      1: "scenario1 turn1"
      2: "scenario1 turn2"
  scenario2:
    turns:
      1: "scenario2 turn1"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}

	// Create repository
	repo, err := NewFileMockRepository(configPath)
	if err != nil {
		t.Fatalf("NewFileMockRepository failed: %v", err)
	}

	ctx := context.Background()

	// Test global default
	response, err := repo.GetResponse(ctx, MockResponseParams{
		ProviderID: "test-provider",
		ModelName:  "test-model",
	})
	if err != nil {
		t.Fatalf("GetResponse failed: %v", err)
	}
	if response != "global default" {
		t.Errorf("Expected 'global default', got %q", response)
	}

	// Test scenario default
	response, err = repo.GetResponse(ctx, MockResponseParams{
		ScenarioID: "scenario1",
		TurnNumber: 99, // Non-existent turn should fall back to scenario default
		ProviderID: "test-provider",
	})
	if err != nil {
		t.Fatalf("GetResponse failed: %v", err)
	}
	if response != "scenario1 default" {
		t.Errorf("Expected 'scenario1 default', got %q", response)
	}

	// Test turn-specific response
	response, err = repo.GetResponse(ctx, MockResponseParams{
		ScenarioID: "scenario1",
		TurnNumber: 1,
		ProviderID: "test-provider",
	})
	if err != nil {
		t.Fatalf("GetResponse failed: %v", err)
	}
	if response != "scenario1 turn1" {
		t.Errorf("Expected 'scenario1 turn1', got %q", response)
	}

	// Test scenario without default falls back to global
	response, err = repo.GetResponse(ctx, MockResponseParams{
		ScenarioID: "scenario2",
		TurnNumber: 99, // Non-existent turn
		ProviderID: "test-provider",
	})
	if err != nil {
		t.Fatalf("GetResponse failed: %v", err)
	}
	if response != "global default" {
		t.Errorf("Expected 'global default', got %q", response)
	}

	// Test scenario2 turn1
	response, err = repo.GetResponse(ctx, MockResponseParams{
		ScenarioID: "scenario2",
		TurnNumber: 1,
		ProviderID: "test-provider",
	})
	if err != nil {
		t.Fatalf("GetResponse failed: %v", err)
	}
	if response != "scenario2 turn1" {
		t.Errorf("Expected 'scenario2 turn1', got %q", response)
	}
}

func TestFileMockRepository_InvalidFile(t *testing.T) {
	// Test non-existent file
	_, err := NewFileMockRepository("/path/to/nonexistent/file.yaml")
	if err == nil {
		t.Fatal("Expected error for non-existent file, got nil")
	}

	// Test invalid YAML
	tmpDir := t.TempDir()
	invalidPath := filepath.Join(tmpDir, "invalid.yaml")
	if err := os.WriteFile(invalidPath, []byte("invalid: yaml: content:"), 0644); err != nil {
		t.Fatalf("Failed to write invalid file: %v", err)
	}

	_, err = NewFileMockRepository(invalidPath)
	if err == nil {
		t.Fatal("Expected error for invalid YAML, got nil")
	}
}

func TestMockProviderWithRepository(t *testing.T) {
	// Create in-memory repository with custom responses
	repo := NewInMemoryMockRepository("default")
	repo.SetResponse("test-scenario", 1, "custom turn 1")

	// Create mock provider with repository
	provider := NewMockProviderWithRepository("test-provider", "test-model", false, repo)

	if provider.ID() != "test-provider" {
		t.Errorf("Expected provider ID 'test-provider', got %q", provider.ID())
	}

	ctx := context.Background()

	// Test Chat method (repository is used internally, but we can't easily inject scenario/turn)
	// This mainly tests that the provider was created successfully
	req := providers.ChatRequest{
		Messages: []types.Message{
			{Role: "user", Content: "test message"},
		},
	}

	resp, err := provider.Chat(ctx, req)
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// Response should be from repository (default since we can't set scenario context)
	if resp.Content == "" {
		t.Error("Expected non-empty response content")
	}
}
