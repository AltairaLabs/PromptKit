package pipeline

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestRegistry creates a prompt registry with a test prompt.
func createTestRegistry(taskType string) *prompt.Registry {
	repo := memory.NewPromptRepository()
	repo.RegisterPrompt(taskType, &prompt.Config{
		APIVersion: "promptkit.io/v1alpha1",
		Kind:       "Prompt",
		Spec: prompt.Spec{
			TaskType:       taskType,
			SystemTemplate: "You are a helpful assistant.",
			AllowedTools:   []string{"get_weather"},
		},
	})
	return prompt.NewRegistryWithRepository(repo)
}

func TestBuild(t *testing.T) {
	t.Run("minimal config with prompt registry", func(t *testing.T) {
		registry := createTestRegistry("chat")

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})

	t.Run("with token parameters", func(t *testing.T) {
		registry := createTestRegistry("chat")

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			MaxTokens:      2048,
			Temperature:    0.5,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})

	t.Run("with tool registry", func(t *testing.T) {
		promptRegistry := createTestRegistry("chat")
		toolRegistry := tools.NewRegistry()

		cfg := &Config{
			PromptRegistry: promptRegistry,
			TaskType:       "chat",
			ToolRegistry:   toolRegistry,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})

	t.Run("with variables", func(t *testing.T) {
		registry := createTestRegistry("chat")

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Variables: map[string]string{
				"user_name": "Alice",
			},
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})

	t.Run("with all options", func(t *testing.T) {
		promptRegistry := createTestRegistry("chat")
		toolRegistry := tools.NewRegistry()

		cfg := &Config{
			PromptRegistry: promptRegistry,
			TaskType:       "chat",
			MaxTokens:      4096,
			Temperature:    0.7,
			ToolRegistry:   toolRegistry,
			Variables: map[string]string{
				"user_name": "Alice",
			},
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})
}

func TestConfig(t *testing.T) {
	t.Run("zero values are valid", func(t *testing.T) {
		cfg := &Config{}
		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})

	t.Run("temperature boundaries", func(t *testing.T) {
		registry := createTestRegistry("chat")

		// Temperature 0 is valid
		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Temperature:    0.0,
		}
		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)

		// Temperature 2.0 is valid (max for some providers)
		cfg.Temperature = 2.0
		pipe, err = Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})

	t.Run("with validator registry and configs", func(t *testing.T) {
		promptRegistry := createTestRegistry("chat")
		validatorRegistry := validators.NewRegistry()
		validatorConfigs := []validators.ValidatorConfig{
			{Type: "banned_words", Params: map[string]interface{}{"words": []string{"test"}}},
		}

		cfg := &Config{
			PromptRegistry:    promptRegistry,
			TaskType:          "chat",
			ValidatorRegistry: validatorRegistry,
			ValidatorConfigs:  validatorConfigs,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})

	t.Run("with suppressed validation errors", func(t *testing.T) {
		promptRegistry := createTestRegistry("chat")
		validatorRegistry := validators.NewRegistry()
		validatorConfigs := []validators.ValidatorConfig{
			{Type: "banned_words", Params: map[string]interface{}{"words": []string{"test"}}},
		}

		cfg := &Config{
			PromptRegistry:           promptRegistry,
			TaskType:                 "chat",
			ValidatorRegistry:        validatorRegistry,
			ValidatorConfigs:         validatorConfigs,
			SuppressValidationErrors: true,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})
}
