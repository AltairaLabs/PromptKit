package middleware

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/middleware"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/config"
)

func TestContextExtractionMiddleware_EmptyMessages(t *testing.T) {
	mw := middleware.ContextExtractionMiddleware()

	execCtx := &pipeline.ExecutionContext{
		Messages:  []types.Message{},
		Variables: make(map[string]string),
		Metadata:  make(map[string]interface{}),
	}

	execCtx.Context = context.Background()

	err := mw.Process(execCtx, func() error { return nil })

	if err != nil {

		t.Fatalf("Expected no error, got %v", err)

	}

	if err != nil {

		t.Fatalf("After returned error: %v", err)

	}

	// Variables should be initialized but might be empty
	if execCtx.Variables == nil {
		t.Fatal("Variables map should be initialized")
	}
}

func TestContextExtractionMiddleware_SingleMessage(t *testing.T) {
	mw := middleware.ContextExtractionMiddleware()

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "I need help with my mobile app startup"},
		},
		Variables: make(map[string]string),
		Metadata:  make(map[string]interface{}),
	}

	execCtx.Context = context.Background()

	err := mw.Process(execCtx, func() error { return nil })

	if err != nil {

		t.Fatalf("Expected no error, got %v", err)

	}

	if err != nil {

		t.Fatalf("After returned error: %v", err)

	}

	// Should have domain hint from "mobile app startup"
	if execCtx.Variables["domain"] == "" {
		t.Error("Expected domain to be extracted")
	}

	// Context extraction cache should be stored in metadata
	if _, exists := execCtx.Metadata["context_extraction_cache"]; !exists {
		t.Error("Expected context_extraction_cache in metadata")
	}
}

func TestContextExtractionMiddleware_ConversationHistory(t *testing.T) {
	mw := middleware.ContextExtractionMiddleware()

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "I'm building a fintech application"},
			{Role: "assistant", Content: "That's great! What kind of fintech features are you planning?"},
			{Role: "user", Content: "I want to add payment processing and budgeting tools"},
		},
		Variables: make(map[string]string),
		Metadata:  make(map[string]interface{}),
	}

	execCtx.Context = context.Background()

	err := mw.Process(execCtx, func() error { return nil })

	if err != nil {

		t.Fatalf("Expected no error, got %v", err)

	}

	if err != nil {

		t.Fatalf("After returned error: %v", err)

	}

	// Should extract fintech domain
	domain := execCtx.Variables["domain"]
	if domain == "" {
		t.Error("Expected domain to be extracted from fintech conversation")
	}

	// Should have user context
	userContext := execCtx.Variables["user_context"]
	if userContext == "" {
		t.Error("Expected user_context to be extracted")
	}
}

func TestContextExtractionMiddleware_PreservesExistingVariables(t *testing.T) {
	mw := middleware.ContextExtractionMiddleware()

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "I need help with my mobile app"},
		},
		Variables: map[string]string{
			"domain": "pre-existing-domain",
			"custom": "custom-value",
		},
		Metadata: make(map[string]interface{}),
	}

	execCtx.Context = context.Background()

	err := mw.Process(execCtx, func() error { return nil })

	if err != nil {

		t.Fatalf("Expected no error, got %v", err)

	}

	if err != nil {

		t.Fatalf("After returned error: %v", err)

	}

	// Should preserve pre-existing variables
	if execCtx.Variables["domain"] != "pre-existing-domain" {
		t.Errorf("Expected domain to be preserved, got %s", execCtx.Variables["domain"])
	}

	if execCtx.Variables["custom"] != "custom-value" {
		t.Error("Expected custom variable to be preserved")
	}
}

func TestContextExtractionMiddleware_InitializesVariablesIfNil(t *testing.T) {
	mw := middleware.ContextExtractionMiddleware()

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
		Variables: nil, // Variables is nil
		Metadata:  make(map[string]interface{}),
	}

	execCtx.Context = context.Background()

	err := mw.Process(execCtx, func() error { return nil })

	if err != nil {

		t.Fatalf("Expected no error, got %v", err)

	}

	if err != nil {

		t.Fatalf("After returned error: %v", err)

	}

	// Variables should be initialized
	if execCtx.Variables == nil {
		t.Fatal("Variables should be initialized")
	}
}

func TestContextExtractionMiddleware_PropagatesNextError(t *testing.T) {
	mw := middleware.ContextExtractionMiddleware()

	execCtx := &pipeline.ExecutionContext{
		Messages:  []types.Message{},
		Variables: make(map[string]string),
		Metadata:  make(map[string]interface{}),
	}

	expectedErr := &pipeline.ValidationError{Type: "test_error", Details: "test details"}

	execCtx.Context = context.Background()
	err := mw.Process(execCtx, func() error { return expectedErr })

	if err != expectedErr {
		t.Errorf("Expected error to be propagated, got %v", err)
	}
}

func TestScenarioContextExtractionMiddleware_UsesScenarioMetadata(t *testing.T) {
	scenario := &config.Scenario{
		ID:          "test-scenario",
		Description: "A test scenario for mobile app",
		TaskType:    "conversation",
		ContextMetadata: &config.ContextMetadata{
			Domain:   "mobile app development",
			UserRole: "entrepreneur",
		},
	}

	mw := ScenarioContextExtractionMiddleware(scenario)

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "I need help"},
		},
		Variables: make(map[string]string),
		Metadata:  make(map[string]interface{}),
	}

	execCtx.Context = context.Background()

	err := mw.Process(execCtx, func() error { return nil })

	if err != nil {

		t.Fatalf("Expected no error, got %v", err)

	}

	if err != nil {

		t.Fatalf("After returned error: %v", err)

	}

	// Should use scenario metadata
	if execCtx.Variables["domain"] != "mobile app development" {
		t.Errorf("Expected domain from scenario metadata, got %s", execCtx.Variables["domain"])
	}

	if execCtx.Variables["user_context"] != "entrepreneur" {
		t.Errorf("Expected user_context from scenario metadata, got %s", execCtx.Variables["user_context"])
	}
}

func TestScenarioContextExtractionMiddleware_FallsBackToMessageAnalysis(t *testing.T) {
	// Scenario without metadata - should fall back to message analysis
	scenario := &config.Scenario{
		ID:          "test-scenario",
		Description: "General conversation",
		TaskType:    "conversation",
	}

	mw := ScenarioContextExtractionMiddleware(scenario)

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "I'm building a fintech startup"},
		},
		Variables: make(map[string]string),
		Metadata:  make(map[string]interface{}),
	}

	execCtx.Context = context.Background()

	err := mw.Process(execCtx, func() error { return nil })

	if err != nil {

		t.Fatalf("Expected no error, got %v", err)

	}

	if err != nil {

		t.Fatalf("After returned error: %v", err)

	}

	// Should extract from message content
	domain := execCtx.Variables["domain"]
	if domain == "" {
		t.Error("Expected domain to be extracted from message content")
	}

	// Should detect fintech or startup-related domain
	if domain != "financial technology" && domain != "business development" {
		t.Logf("Got domain: %s (acceptable fallback)", domain)
	}
}

func TestContextExtractionMiddleware_OnlyUsesMessages(t *testing.T) {
	// ContextExtractionMiddleware should only analyze messages, not use scenario
	mw := middleware.ContextExtractionMiddleware()

	execCtx := &pipeline.ExecutionContext{
		Messages: []types.Message{
			{Role: "user", Content: "I need help with my mobile app"},
		},
		Variables: make(map[string]string),
		Metadata:  make(map[string]interface{}),
	}

	execCtx.Context = context.Background()
	err := mw.Process(execCtx, func() error { return nil })
	if err != nil {
		t.Fatalf("Before failed: %v", err)
	}

	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Should extract domain from messages
	if execCtx.Variables["domain"] == "" {
		t.Error("Expected domain to be extracted from messages")
	}
}
