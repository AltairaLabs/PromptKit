package pipeline

import (
	"encoding/json"
	"errors"
	"testing"

	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuild(t *testing.T) {
	t.Run("minimal config", func(t *testing.T) {
		cfg := &Config{
			SystemPrompt: "You are helpful.",
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})

	t.Run("with token parameters", func(t *testing.T) {
		cfg := &Config{
			SystemPrompt: "You are helpful.",
			MaxTokens:    2048,
			Temperature:  0.5,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})

	t.Run("with tool registry", func(t *testing.T) {
		registry := tools.NewRegistry()

		cfg := &Config{
			SystemPrompt: "You are helpful.",
			ToolRegistry: registry,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})

	t.Run("with all options", func(t *testing.T) {
		registry := tools.NewRegistry()

		cfg := &Config{
			SystemPrompt: "You are a helpful assistant.",
			MaxTokens:    4096,
			Temperature:  0.7,
			ToolRegistry: registry,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})

	t.Run("with tool descriptors", func(t *testing.T) {
		schema := json.RawMessage(`{
			"type": "object",
			"properties": {
				"location": {
					"type": "string",
					"description": "The city name"
				}
			}
		}`)

		toolDesc := &tools.ToolDescriptor{
			Name:        "get_weather",
			Description: "Get weather for a location",
			InputSchema: schema,
		}

		cfg := &Config{
			SystemPrompt: "You are helpful.",
			Tools:        []*tools.ToolDescriptor{toolDesc},
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})
}

func TestSystemPromptMiddleware(t *testing.T) {
	t.Run("creates middleware with system prompt", func(t *testing.T) {
		m := &SystemPromptMiddleware{
			SystemPrompt: "You are a helpful assistant.",
		}
		assert.Equal(t, "You are a helpful assistant.", m.SystemPrompt)
	})

	t.Run("process sets system prompt on context", func(t *testing.T) {
		m := &SystemPromptMiddleware{
			SystemPrompt: "Test system prompt",
		}

		// Create mock execution context
		execCtx := &rtpipeline.ExecutionContext{}

		// Call Process
		nextCalled := false
		err := m.Process(execCtx, func() error {
			nextCalled = true
			// Verify system prompt was set
			assert.Equal(t, "Test system prompt", execCtx.SystemPrompt)
			return nil
		})

		require.NoError(t, err)
		assert.True(t, nextCalled)
	})

	t.Run("process propagates next error", func(t *testing.T) {
		m := &SystemPromptMiddleware{
			SystemPrompt: "Test",
		}

		execCtx := &rtpipeline.ExecutionContext{}
		expectedErr := errors.New("next error")

		err := m.Process(execCtx, func() error {
			return expectedErr
		})

		assert.Equal(t, expectedErr, err)
	})

	t.Run("stream chunk is no-op", func(t *testing.T) {
		m := &SystemPromptMiddleware{
			SystemPrompt: "Test",
		}

		err := m.StreamChunk(nil, nil)
		assert.NoError(t, err)
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
		// Temperature 0 is valid
		cfg := &Config{
			SystemPrompt: "Test",
			Temperature:  0.0,
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
		validatorRegistry := validators.NewRegistry()
		validatorConfigs := []validators.ValidatorConfig{
			{Type: "banned_words", Params: map[string]interface{}{"words": []string{"test"}}},
		}

		cfg := &Config{
			SystemPrompt:      "Test",
			ValidatorRegistry: validatorRegistry,
			ValidatorConfigs:  validatorConfigs,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})

	t.Run("with suppressed validation errors", func(t *testing.T) {
		validatorRegistry := validators.NewRegistry()
		validatorConfigs := []validators.ValidatorConfig{
			{Type: "banned_words", Params: map[string]interface{}{"words": []string{"test"}}},
		}

		cfg := &Config{
			SystemPrompt:             "Test",
			ValidatorRegistry:        validatorRegistry,
			ValidatorConfigs:         validatorConfigs,
			SuppressValidationErrors: true,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})
}
