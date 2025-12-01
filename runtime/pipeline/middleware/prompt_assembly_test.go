package middleware

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptAssemblyMiddleware_NilRegistry(t *testing.T) {
	// Test with nil registry - should use default prompt
	middleware := PromptAssemblyMiddleware(nil, "test-task", nil)

	execCtx := &pipeline.ExecutionContext{
		Metadata: make(map[string]interface{}),
	}

	called := false
	err := middleware.Process(execCtx, func() error {
		called = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, "You are a helpful AI assistant.", execCtx.SystemPrompt)
	assert.Nil(t, execCtx.AllowedTools)
}

func TestPromptAssemblyMiddleware_TaskNotFound(t *testing.T) {
	// Test with registry but task not found
	repo := memory.NewPromptRepository()
	registry := prompt.NewRegistryWithRepository(repo)

	middleware := PromptAssemblyMiddleware(registry, "nonexistent-task", nil)

	execCtx := &pipeline.ExecutionContext{
		Metadata: make(map[string]interface{}),
	}

	called := false
	err := middleware.Process(execCtx, func() error {
		called = true
		return nil
	})

	require.NoError(t, err)
	assert.True(t, called)
	assert.Equal(t, "You are a helpful AI assistant.", execCtx.SystemPrompt)
	assert.Nil(t, execCtx.AllowedTools)
}

func TestPromptAssemblyMiddleware_WithToolsAndValidators(t *testing.T) {
	repo := memory.NewPromptRepository()

	enabled := true
	disabled := false

	repo.RegisterPrompt("test-task", &prompt.Config{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Config",
		Spec: prompt.Spec{
			TaskType:       "test-task",
			SystemTemplate: "You are a helpful assistant for {{domain}}",
			AllowedTools:   []string{"search", "calculator"},
			Variables: []prompt.VariableMetadata{
				{Name: "domain", Required: true, Type: "string"},
			},
			Validators: []prompt.ValidatorConfig{
				{
					ValidatorConfig: validators.ValidatorConfig{
						Type: "length",
						Params: map[string]interface{}{
							"max_characters": 1000,
						},
					},
					Enabled: &enabled,
				},
				{
					ValidatorConfig: validators.ValidatorConfig{
						Type: "banned_words",
					},
					Enabled: &disabled,
				},
			},
		},
	})

	registry := prompt.NewRegistryWithRepository(repo)

	baseVars := map[string]string{"domain": "customer support"}
	middleware := PromptAssemblyMiddleware(registry, "test-task", baseVars)

	execCtx := &pipeline.ExecutionContext{
		Metadata: make(map[string]interface{}),
	}

	err := middleware.Process(execCtx, func() error {
		return nil
	})

	require.NoError(t, err)
	assert.Contains(t, execCtx.SystemPrompt, "customer support")
	assert.Equal(t, []string{"search", "calculator"}, execCtx.AllowedTools)

	// Check validator configs in metadata - only enabled ones
	validatorConfigs, ok := execCtx.Metadata["validator_configs"].([]validators.ValidatorConfig)
	require.True(t, ok, "validator_configs should be in metadata")
	assert.Len(t, validatorConfigs, 1, "only enabled validators should be included")
	assert.Equal(t, "length", validatorConfigs[0].Type)

	// Check base variables merged into Variables
	assert.Equal(t, "customer support", execCtx.Variables["domain"])
}

func TestPromptAssemblyMiddleware_VariablesMerging(t *testing.T) {
	repo := memory.NewPromptRepository()

	repo.RegisterPrompt("var-task", &prompt.Config{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Config",
		Spec: prompt.Spec{
			TaskType:       "var-task",
			SystemTemplate: "Test",
		},
	})

	registry := prompt.NewRegistryWithRepository(repo)

	baseVars := map[string]string{"var1": "value1", "var2": "value2"}
	middleware := PromptAssemblyMiddleware(registry, "var-task", baseVars)

	execCtx := &pipeline.ExecutionContext{
		Metadata:  make(map[string]interface{}),
		Variables: map[string]string{"var2": "existing_value", "var3": "value3"},
	}

	err := middleware.Process(execCtx, func() error {
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, "value1", execCtx.Variables["var1"], "new var should be added")
	assert.Equal(t, "existing_value", execCtx.Variables["var2"], "existing var should not be overwritten")
	assert.Equal(t, "value3", execCtx.Variables["var3"], "existing var should be preserved")
}

func TestPromptAssemblyMiddleware_NoValidators(t *testing.T) {
	repo := memory.NewPromptRepository()

	repo.RegisterPrompt("no-validators", &prompt.Config{
		APIVersion: "promptkit.altairalabs.ai/v1alpha1",
		Kind:       "Config",
		Spec: prompt.Spec{
			TaskType:       "no-validators",
			SystemTemplate: "Test",
			Validators:     []prompt.ValidatorConfig{},
		},
	})

	registry := prompt.NewRegistryWithRepository(repo)
	middleware := PromptAssemblyMiddleware(registry, "no-validators", nil)

	execCtx := &pipeline.ExecutionContext{
		Metadata: make(map[string]interface{}),
	}

	err := middleware.Process(execCtx, func() error {
		return nil
	})

	require.NoError(t, err)
	_, exists := execCtx.Metadata["validator_configs"]
	assert.False(t, exists, "validator_configs should not be in metadata when no validators")
}
