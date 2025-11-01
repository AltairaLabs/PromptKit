package middleware

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
)

// TestDynamicValidatorMiddleware_AlwaysCallsNext verifies that the validator middleware
// ALWAYS calls next(), even when validation fails. This is critical for the middleware
// chain to work correctly, especially for saving state after validation failures.
func TestDynamicValidatorMiddleware_AlwaysCallsNext(t *testing.T) {
	tests := []struct {
		name           string
		setupConfigs   func() []validators.ValidatorConfig
		content        string
		expectError    bool
		expectNextCall bool
	}{
		{
			name: "validation passes - next should be called",
			setupConfigs: func() []validators.ValidatorConfig {
				return []validators.ValidatorConfig{
					{Type: "mock", Params: map[string]interface{}{}},
				}
			},
			content:        "clean content",
			expectError:    false,
			expectNextCall: true,
		},
		{
			name: "validation fails - next should STILL be called",
			setupConfigs: func() []validators.ValidatorConfig {
				return []validators.ValidatorConfig{
					{Type: "mock", Params: map[string]interface{}{}},
				}
			},
			content:        "content with badword",
			expectError:    true,
			expectNextCall: true,
		},
		{
			name: "empty content - next should be called",
			setupConfigs: func() []validators.ValidatorConfig {
				return []validators.ValidatorConfig{
					{Type: "mock", Params: map[string]interface{}{}},
				}
			},
			content:        "",
			expectError:    false,
			expectNextCall: true,
		},
		{
			name: "no validator configs - next should STILL be called",
			setupConfigs: func() []validators.ValidatorConfig {
				return nil // No configs
			},
			content:        "any content",
			expectError:    false,
			expectNextCall: true,
		},
		{
			name: "empty validator config list - next should STILL be called",
			setupConfigs: func() []validators.ValidatorConfig {
				return []validators.ValidatorConfig{} // Empty list
			},
			content:        "any content",
			expectError:    false,
			expectNextCall: true,
		},
		{
			name: "all unknown validator types - next should STILL be called",
			setupConfigs: func() []validators.ValidatorConfig {
				return []validators.ValidatorConfig{
					{Type: "unknown1", Params: map[string]interface{}{}},
					{Type: "unknown2", Params: map[string]interface{}{}},
				}
			},
			content:        "any content",
			expectError:    false,
			expectNextCall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup registry with mock validator
			mockVal := &mockValidator{failOn: "badword"}
			registry := validators.NewRegistry()
			registry.Register("mock", func(params map[string]interface{}) validators.Validator { return mockVal })

			// Setup validator configs
			validatorConfigs := tt.setupConfigs()

			// Create execution context
			metadata := make(map[string]interface{})
			if validatorConfigs != nil {
				metadata["validator_configs"] = validatorConfigs
			}

			execCtx := &pipeline.ExecutionContext{
				Context:  context.Background(),
				Metadata: metadata,
				Messages: []types.Message{
					{
						Role:    "assistant",
						Content: tt.content,
					},
				},
			}

			// Track whether next() was called
			nextCalled := false
			next := func() error {
				nextCalled = true
				return nil
			}

			// Create validator middleware
			validatorMW := DynamicValidatorMiddleware(registry)

			// Call Process
			err := validatorMW.Process(execCtx, next)

			// Verify error expectation
			if tt.expectError && err == nil {
				t.Error("Expected validation error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected no error, got: %v", err)
			}

			// CRITICAL: Verify next() was called
			if tt.expectNextCall && !nextCalled {
				t.Errorf("CRITICAL BUG: next() was NOT called - middleware chain is broken!")
			}
			if !tt.expectNextCall && nextCalled {
				t.Error("next() was called unexpectedly")
			}
		})
	}
}

// TestDynamicValidatorMiddleware_NextErrorPropagation verifies that errors from
// downstream middleware are properly propagated, even when validation passes.
func TestDynamicValidatorMiddleware_NextErrorPropagation(t *testing.T) {
	// Setup registry with mock validator
	mockVal := &mockValidator{failOn: "badword"}
	registry := validators.NewRegistry()
	registry.Register("mock", func(params map[string]interface{}) validators.Validator { return mockVal })

	// Setup validator configs
	validatorConfigs := []validators.ValidatorConfig{
		{
			Type:   "mock",
			Params: map[string]interface{}{},
		},
	}

	// Test 1: Validation passes, but next() returns error
	t.Run("validation passes, next fails", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Context: context.Background(),
			Metadata: map[string]interface{}{
				"validator_configs": validatorConfigs,
			},
			Messages: []types.Message{
				{
					Role:    "assistant",
					Content: "clean content",
				},
			},
		}

		validatorMW := DynamicValidatorMiddleware(registry)

		// next() returns an error
		nextError := &pipeline.ValidationError{Type: "downstream", Details: "downstream middleware error"}
		err := validatorMW.Process(execCtx, func() error {
			return nextError
		})

		// The downstream error should be propagated
		if err == nil {
			t.Fatal("Expected downstream error to be propagated, got nil")
		}
		if err != nextError {
			t.Errorf("Expected downstream error to be propagated unchanged, got: %v", err)
		}
	})

	// Test 2: Validation fails, and next() also returns error
	t.Run("validation fails, next fails", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Context: context.Background(),
			Metadata: map[string]interface{}{
				"validator_configs": validatorConfigs,
			},
			Messages: []types.Message{
				{
					Role:    "assistant",
					Content: "content with badword",
				},
			},
		}

		validatorMW := DynamicValidatorMiddleware(registry)

		// next() returns an error
		nextError := &pipeline.ValidationError{Type: "downstream", Details: "downstream middleware error"}
		err := validatorMW.Process(execCtx, func() error {
			return nextError
		})

		// The validation error should be returned (takes precedence)
		if err == nil {
			t.Fatal("Expected error, got nil")
		}

		// The error should be a ValidationError (validation error takes precedence)
		if _, ok := err.(*pipeline.ValidationError); !ok {
			t.Errorf("Expected ValidationError, got: %T", err)
		}
	})
}

// TestDynamicValidatorMiddleware_ValidationsAttachedBeforeNextCall verifies that
// validation results are attached to the message BEFORE next() is called, so that
// downstream middleware (like StateStore) can see and persist them.
func TestDynamicValidatorMiddleware_ValidationsAttachedBeforeNextCall(t *testing.T) {
	// Setup registry with mock validator
	mockVal := &mockValidator{failOn: "badword"}
	registry := validators.NewRegistry()
	registry.Register("mock", func(params map[string]interface{}) validators.Validator { return mockVal })

	// Setup validator configs
	validatorConfigs := []validators.ValidatorConfig{
		{
			Type:   "mock",
			Params: map[string]interface{}{},
		},
	}

	// Test with failing validation
	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Metadata: map[string]interface{}{
			"validator_configs": validatorConfigs,
		},
		Messages: []types.Message{
			{
				Role:    "assistant",
				Content: "content with badword",
			},
		},
	}

	validatorMW := DynamicValidatorMiddleware(registry)

	// Track state when next() is called
	var validationsWhenNextCalled []types.ValidationResult

	err := validatorMW.Process(execCtx, func() error {
		// When next() is called, capture the validations
		if len(execCtx.Messages) > 0 {
			validationsWhenNextCalled = execCtx.Messages[0].Validations
		}
		return nil
	})

	// Should return validation error
	if err == nil {
		t.Fatal("Expected validation error, got nil")
	}

	// CRITICAL: Validations should be attached BEFORE next() was called
	if len(validationsWhenNextCalled) == 0 {
		t.Fatal("CRITICAL: Validations were NOT attached before next() was called - downstream middleware can't see them!")
	}

	// Verify the validation result indicates failure
	found := false
	for _, val := range validationsWhenNextCalled {
		if !val.Passed {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected to find failed validation in results")
	}

	// Verify validations are still on the message after everything completes
	if len(execCtx.Messages[0].Validations) == 0 {
		t.Error("Validations should still be attached to message after process completes")
	}
}
