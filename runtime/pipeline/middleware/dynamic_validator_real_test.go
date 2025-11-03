package middleware

import (
	"context"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
)

func TestDynamicValidatorMiddleware_BannedWords_Pass(t *testing.T) {
	registry := validators.DefaultRegistry

	// Setup validator config for banned words
	validatorConfigs := []validators.ValidatorConfig{
		{
			Type: "banned_words",
			Params: map[string]interface{}{
				"words": []interface{}{"guarantee", "promise", "definitely"},
			},
		},
	}

	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Metadata: map[string]interface{}{
			"validator_configs": validatorConfigs,
		},
		Messages: []types.Message{},
	}

	// Simulate provider adding assistant message BEFORE validator runs
	execCtx.Messages = append(execCtx.Messages, types.Message{
		Role:    "assistant",
		Content: "I can help you with your refund request. Let me check your account.",
	})

	validatorMW := DynamicValidatorMiddleware(registry)

	// Call Process - validator will validate the existing message
	err := validatorMW.Process(execCtx, func() error {
		// next() just continues to the next middleware
		return nil
	})

	if err != nil {
		t.Fatalf("Expected no error with clean content, got: %v", err)
	}
}

func TestDynamicValidatorMiddleware_BannedWords_Fail(t *testing.T) {
	registry := validators.DefaultRegistry

	// Setup validator config for banned words
	validatorConfigs := []validators.ValidatorConfig{
		{
			Type: "banned_words",
			Params: map[string]interface{}{
				"words": []interface{}{"guarantee", "promise", "definitely"},
			},
		},
	}

	testCases := []struct {
		name     string
		response string
		wantWord string
	}{
		{
			name:     "contains guarantee",
			response: "I can guarantee that your refund will be processed",
			wantWord: "guarantee",
		},
		{
			name:     "contains promise",
			response: "I promise this will work perfectly",
			wantWord: "promise",
		},
		{
			name:     "contains definitely",
			response: "This will definitely solve your problem",
			wantWord: "definitely",
		},
		{
			name:     "case insensitive - GUARANTEE",
			response: "I GUARANTEE this will work",
			wantWord: "guarantee",
		},
		{
			name:     "word boundary - guaranteed should not match guarantee",
			response: "Your satisfaction is guaranteed",
			wantWord: "", // should NOT match because "guaranteed" != "guarantee"
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			execCtx := &pipeline.ExecutionContext{
				Context: context.Background(),
				Metadata: map[string]interface{}{
					"validator_configs": validatorConfigs,
				},
				Messages: []types.Message{},
			}

			// Simulate provider adding assistant message BEFORE validator runs
			execCtx.Messages = append(execCtx.Messages, types.Message{
				Role:    "assistant",
				Content: tc.response,
			})

			validatorMW := DynamicValidatorMiddleware(registry)

			// Call Process - validator will validate the existing message
			err := validatorMW.Process(execCtx, func() error {
				// next() just continues to the next middleware
				return nil
			})

			if tc.wantWord == "" {
				// Should pass validation
				if err != nil {
					t.Fatalf("Expected no error (word boundary check), got: %v", err)
				}
			} else {
				// Should fail validation
				if err == nil {
					t.Fatalf("Expected validation error for banned word %q, got nil", tc.wantWord)
				}

				valErr, ok := err.(*pipeline.ValidationError)
				if !ok {
					t.Fatalf("Expected ValidationError, got: %T", err)
				}

				// Check that the Failures array contains information about the banned word
				found := false
				for _, failure := range valErr.Failures {
					// BannedWordsValidator returns Details as []string wrapped in map["value": violations]
					if value, ok := failure.Details["value"]; ok {
						// value could be []string or []interface{}
						switch v := value.(type) {
						case []string:
							for _, word := range v {
								if strings.EqualFold(word, tc.wantWord) {
									found = true
									break
								}
							}
						case []interface{}:
							for _, word := range v {
								if wordStr, ok := word.(string); ok {
									if strings.EqualFold(wordStr, tc.wantWord) {
										found = true
										break
									}
								}
							}
						}
					}
					if found {
						break
					}
				}

				if !found {
					t.Errorf("Expected failure details to mention banned word %q, got failures: %+v", tc.wantWord, valErr.Failures)
				}

				t.Logf("Correctly caught banned word %q in validation failures", tc.wantWord)
			}
		})
	}
}

func TestDynamicValidatorMiddleware_MaxLength_Pass(t *testing.T) {
	registry := validators.DefaultRegistry

	validatorConfigs := []validators.ValidatorConfig{
		{
			Type: "max_length",
			Params: map[string]interface{}{
				"max_characters": 100,
			},
		},
	}

	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Metadata: map[string]interface{}{
			"validator_configs": validatorConfigs,
		},
		Messages: []types.Message{},
	}

	// Simulate provider adding assistant message BEFORE validator runs
	execCtx.Messages = append(execCtx.Messages, types.Message{
		Role:    "assistant",
		Content: "Short response within limit",
	})

	validatorMW := DynamicValidatorMiddleware(registry)

	// Call Process - validator will validate the existing message
	err := validatorMW.Process(execCtx, func() error {
		// next() just continues to the next middleware
		return nil
	})

	if err != nil {
		t.Fatalf("Expected no error for short response, got: %v", err)
	}
}

func TestDynamicValidatorMiddleware_MaxLength_Fail(t *testing.T) {
	registry := validators.DefaultRegistry

	validatorConfigs := []validators.ValidatorConfig{
		{
			Type: "max_length",
			Params: map[string]interface{}{
				"max_characters": 50,
			},
		},
	}

	execCtx := &pipeline.ExecutionContext{
		Context: context.Background(),
		Metadata: map[string]interface{}{
			"validator_configs": validatorConfigs,
		},
		Messages: []types.Message{},
	}

	validatorMW := DynamicValidatorMiddleware(registry)

	// Create a response that's definitely over 50 characters
	longResponse := strings.Repeat("This is a very long response that exceeds the limit. ", 5)

	// Simulate provider adding assistant message BEFORE validator runs
	execCtx.Messages = append(execCtx.Messages, types.Message{
		Role:    "assistant",
		Content: longResponse,
	})

	// Call Process - validator will validate the existing message
	err := validatorMW.Process(execCtx, func() error {
		// next() just continues to the next middleware
		return nil
	})

	if err == nil {
		t.Fatal("Expected validation error for long response, got nil")
	}

	valErr, ok := err.(*pipeline.ValidationError)
	if !ok {
		t.Fatalf("Expected ValidationError, got: %T", err)
	}

	t.Logf("Correctly caught length violation: %s", valErr.Details)
}

func TestDynamicValidatorMiddleware_MultipleRealValidators(t *testing.T) {
	registry := validators.DefaultRegistry

	// Setup multiple validators
	validatorConfigs := []validators.ValidatorConfig{
		{
			Type: "banned_words",
			Params: map[string]interface{}{
				"words": []interface{}{"guarantee", "promise"},
			},
		},
		{
			Type: "max_length",
			Params: map[string]interface{}{
				"max_characters": 200,
			},
		},
	}

	// Test 1: Both pass
	t.Run("both_pass", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Context: context.Background(),
			Metadata: map[string]interface{}{
				"validator_configs": validatorConfigs,
			},
			Messages: []types.Message{},
		}

		// Simulate provider adding assistant message BEFORE validator runs
		execCtx.Messages = append(execCtx.Messages, types.Message{
			Role:    "assistant",
			Content: "I can help you with your refund request.",
		})

		validatorMW := DynamicValidatorMiddleware(registry)

		err := validatorMW.Process(execCtx, func() error {
			return nil
		})

		if err != nil {
			t.Fatalf("Expected no error, got: %v", err)
		}
	})

	// Test 2: Banned word violation
	t.Run("banned_word_violation", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Context: context.Background(),
			Metadata: map[string]interface{}{
				"validator_configs": validatorConfigs,
			},
			Messages: []types.Message{},
		}

		// Simulate provider adding assistant message BEFORE validator runs
		execCtx.Messages = append(execCtx.Messages, types.Message{
			Role:    "assistant",
			Content: "I guarantee this will work!",
		})

		validatorMW := DynamicValidatorMiddleware(registry)

		err := validatorMW.Process(execCtx, func() error {
			return nil
		})

		if err == nil {
			t.Fatal("Expected validation error for banned word, got nil")
		}
		t.Logf("Test passed: %v", err)
	})

	// Test 3: Length violation
	t.Run("length_violation", func(t *testing.T) {
		execCtx := &pipeline.ExecutionContext{
			Context: context.Background(),
			Metadata: map[string]interface{}{
				"validator_configs": validatorConfigs,
			},
			Messages: []types.Message{},
		}

		// Simulate provider adding assistant message BEFORE validator runs
		execCtx.Messages = append(execCtx.Messages, types.Message{
			Role:    "assistant",
			Content: strings.Repeat("This is a long response. ", 20),
		})

		validatorMW := DynamicValidatorMiddleware(registry)

		err := validatorMW.Process(execCtx, func() error {
			return nil
		})

		if err == nil {
			t.Fatal("Expected validation error for length, got nil")
		}
		t.Logf("Test passed: %v", err)
	})
}
