package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/variables"
)

// mockProvider is a test helper that returns predefined values
type mockVariableProvider struct {
	name   string
	vars   map[string]string
	err    error
	called bool
}

func (m *mockVariableProvider) Name() string {
	return m.name
}

func (m *mockVariableProvider) Provide(ctx context.Context) (map[string]string, error) {
	m.called = true
	return m.vars, m.err
}

// mockStateStore is a test helper for state store
type mockStateStore struct {
	state *statestore.ConversationState
	err   error
}

func (m *mockStateStore) Load(ctx context.Context, id string) (*statestore.ConversationState, error) {
	return m.state, m.err
}

func (m *mockStateStore) Save(ctx context.Context, state *statestore.ConversationState) error {
	return nil
}

func TestVariableProviderMiddleware_NoProviders(t *testing.T) {
	middleware := VariableProviderMiddleware()

	execCtx := &pipeline.ExecutionContext{
		Context:   context.Background(),
		Variables: map[string]string{"existing": "value"},
	}

	nextCalled := false
	err := middleware.Process(execCtx, func() error {
		nextCalled = true
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !nextCalled {
		t.Error("next() should be called")
	}
	if execCtx.Variables["existing"] != "value" {
		t.Error("existing variables should be preserved")
	}
}

func TestVariableProviderMiddleware_SingleProvider(t *testing.T) {
	provider := &mockVariableProvider{
		name: "test",
		vars: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}

	middleware := VariableProviderMiddleware(provider)

	execCtx := &pipeline.ExecutionContext{
		Context:   context.Background(),
		Variables: nil,
	}

	err := middleware.Process(execCtx, func() error { return nil })

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !provider.called {
		t.Error("provider should be called")
	}
	if execCtx.Variables["key1"] != "value1" {
		t.Errorf("Variables[key1] = %v, want value1", execCtx.Variables["key1"])
	}
	if execCtx.Variables["key2"] != "value2" {
		t.Errorf("Variables[key2] = %v, want value2", execCtx.Variables["key2"])
	}
}

func TestVariableProviderMiddleware_MultipleProviders(t *testing.T) {
	provider1 := &mockVariableProvider{
		name: "first",
		vars: map[string]string{"key1": "first_value"},
	}
	provider2 := &mockVariableProvider{
		name: "second",
		vars: map[string]string{"key2": "second_value"},
	}

	middleware := VariableProviderMiddleware(provider1, provider2)

	execCtx := &pipeline.ExecutionContext{
		Context:   context.Background(),
		Variables: nil,
	}

	err := middleware.Process(execCtx, func() error { return nil })

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if execCtx.Variables["key1"] != "first_value" {
		t.Error("first provider values should be present")
	}
	if execCtx.Variables["key2"] != "second_value" {
		t.Error("second provider values should be present")
	}
}

func TestVariableProviderMiddleware_ProviderOverridesExisting(t *testing.T) {
	provider := &mockVariableProvider{
		name: "override",
		vars: map[string]string{"key": "provider_value"},
	}

	middleware := VariableProviderMiddleware(provider)

	execCtx := &pipeline.ExecutionContext{
		Context:   context.Background(),
		Variables: map[string]string{"key": "original_value"},
	}

	err := middleware.Process(execCtx, func() error { return nil })

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if execCtx.Variables["key"] != "provider_value" {
		t.Errorf("provider should override existing, got %v", execCtx.Variables["key"])
	}
}

func TestVariableProviderMiddleware_LaterProviderOverridesEarlier(t *testing.T) {
	provider1 := &mockVariableProvider{
		name: "first",
		vars: map[string]string{"key": "first_value"},
	}
	provider2 := &mockVariableProvider{
		name: "second",
		vars: map[string]string{"key": "second_value"},
	}

	middleware := VariableProviderMiddleware(provider1, provider2)

	execCtx := &pipeline.ExecutionContext{
		Context:   context.Background(),
		Variables: nil,
	}

	err := middleware.Process(execCtx, func() error { return nil })

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if execCtx.Variables["key"] != "second_value" {
		t.Errorf("later provider should override, got %v", execCtx.Variables["key"])
	}
}

func TestVariableProviderMiddleware_ProviderError(t *testing.T) {
	provider := &mockVariableProvider{
		name: "failing",
		err:  errors.New("provider error"),
	}

	middleware := VariableProviderMiddleware(provider)

	execCtx := &pipeline.ExecutionContext{
		Context:   context.Background(),
		Variables: nil,
	}

	nextCalled := false
	err := middleware.Process(execCtx, func() error {
		nextCalled = true
		return nil
	})

	if err == nil {
		t.Error("expected error from failing provider")
	}
	if nextCalled {
		t.Error("next() should not be called on provider error")
	}
}

func TestVariableProviderMiddleware_WithStateStore(t *testing.T) {
	store := &mockStateStore{
		state: &statestore.ConversationState{
			ID: "conv-123",
			Metadata: map[string]interface{}{
				"user_name": "Alice",
			},
		},
	}

	// With the new design, state store is injected via constructor
	provider := variables.NewStateProvider(store, "conv-123")

	middleware := VariableProviderMiddleware(provider)

	execCtx := &pipeline.ExecutionContext{
		Context:   context.Background(),
		Variables: nil,
	}

	err := middleware.Process(execCtx, func() error { return nil })

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if execCtx.Variables["user_name"] != "Alice" {
		t.Errorf("Variables[user_name] = %v, want Alice", execCtx.Variables["user_name"])
	}
}

func TestVariableProviderMiddleware_StateStoreError(t *testing.T) {
	store := &mockStateStore{
		err: errors.New("store error"),
	}

	// StateProvider with a failing store handles errors gracefully (returns nil, nil)
	stateProvider := variables.NewStateProvider(store, "conv-123")

	// Also add a regular provider to verify it still works
	regularProvider := &mockVariableProvider{
		name: "test",
		vars: map[string]string{"key": "value"},
	}

	middleware := VariableProviderMiddleware(stateProvider, regularProvider)

	execCtx := &pipeline.ExecutionContext{
		Context:   context.Background(),
		Variables: nil,
	}

	// State store errors are handled gracefully by StateProvider (returns nil, nil)
	// so the middleware continues to process other providers
	err := middleware.Process(execCtx, func() error { return nil })

	if err != nil {
		t.Errorf("state store error should be handled gracefully: %v", err)
	}
	if !regularProvider.called {
		t.Error("regular provider should still be called")
	}
	if execCtx.Variables["key"] != "value" {
		t.Error("regular provider values should still be set")
	}
}

func TestVariableProviderMiddleware_StreamChunk(t *testing.T) {
	middleware := VariableProviderMiddleware()

	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
	}

	// StreamChunk should be a no-op
	err := middleware.StreamChunk(execCtx, nil)
	if err != nil {
		t.Errorf("StreamChunk should return nil: %v", err)
	}
}
