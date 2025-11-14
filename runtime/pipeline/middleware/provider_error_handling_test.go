package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/mock"
)

// TestProviderMiddleware_ErrorHandling tests that state is saved even when provider fails
func TestProviderMiddleware_ErrorHandling(t *testing.T) {
	// Create a mock provider that returns an error
	mockProvider := new(MockProvider)
	mockProvider.On("Predict", context.Background(), mock.Anything).Return(
		providers.PredictionResponse{},
		errors.New("simulated provider error"),
	)

	// Track if next() was called
	nextCalled := false
	nextFunc := func() error {
		nextCalled = true
		return nil
	}

	// Create middleware
	middleware := ProviderMiddleware(mockProvider, nil, nil, nil)

	// Create execution context
	execCtx := &pipeline.ExecutionContext{
		Context:  context.Background(),
		Messages: []types.Message{},
	}

	// Execute middleware
	err := middleware.Process(execCtx, nextFunc)

	// Verify next() was called even though provider errored
	if !nextCalled {
		t.Error("next() should be called even when provider errors, to allow state save")
	}

	// Verify the error is still returned
	if err == nil {
		t.Error("Expected error to be returned")
	}

	if !errors.Is(err, errors.New("simulated provider error")) && !contains(err.Error(), "simulated provider error") {
		t.Errorf("Expected error message to contain 'simulated provider error', got: %s", err.Error())
	}

	t.Logf("✅ next() called even with provider error, allowing state to be saved")
	mockProvider.AssertExpectations(t)
}

// Helper function to check if error message contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestProviderMiddleware_NextError tests that errors from next() are handled correctly
func TestProviderMiddleware_NextError(t *testing.T) {
	// Create a mock provider that succeeds
	mockProvider := new(MockProvider)
	mockProvider.On("Predict", context.Background(), mock.Anything).Return(
		providers.PredictionResponse{
			Content: "Success response",
			CostInfo: &types.CostInfo{
				InputTokens:  10,
				OutputTokens: 5,
				TotalCost:    0.0001,
			},
		},
		nil,
	)

	// next() returns an error
	nextFunc := func() error {
		return errors.New("state save error")
	}

	// Create middleware
	middleware := ProviderMiddleware(mockProvider, nil, nil, nil)

	// Create execution context
	execCtx := &pipeline.ExecutionContext{
		Context:  context.Background(),
		Messages: []types.Message{},
	}

	// Execute middleware
	err := middleware.Process(execCtx, nextFunc)

	// Verify the next() error is returned when provider succeeds
	if err == nil {
		t.Error("Expected error from next() to be returned")
	}

	if err.Error() != "state save error" {
		t.Errorf("Expected error message 'state save error', got: %s", err.Error())
	}

	t.Logf("✅ next() error correctly propagated when provider succeeds")
	mockProvider.AssertExpectations(t)
}
