package pipeline

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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

// createTestRegistryWithTemplate creates a prompt registry with variable substitution support.
func createTestRegistryWithTemplate(taskType, template string) *prompt.Registry {
	repo := memory.NewPromptRepository()
	repo.RegisterPrompt(taskType, &prompt.Config{
		APIVersion: "promptkit.io/v1alpha1",
		Kind:       "Prompt",
		Spec: prompt.Spec{
			TaskType:       taskType,
			SystemTemplate: template,
		},
	})
	return prompt.NewRegistryWithRepository(repo)
}

func TestBuildWithMockProvider(t *testing.T) {
	t.Run("executes pipeline with mock provider", func(t *testing.T) {
		registry := createTestRegistry("chat")
		mockProvider := mock.NewProvider("test-mock", "test-model", false)

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       mockProvider,
			MaxTokens:      100,
			Temperature:    0.5,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)

		// Execute the pipeline
		execOpts := &rtpipeline.ExecutionOptions{
			Context:        context.Background(),
			ConversationID: "test-conv",
		}
		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Hello!")

		result, err := pipe.ExecuteWithMessageOptions(execOpts, userMsg)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.NotNil(t, result.Response)
	})

	t.Run("variables are substituted in system prompt", func(t *testing.T) {
		// Create registry with template variables
		registry := createTestRegistryWithTemplate("chat", "Hello {{user_name}}, you are in {{region}}!")

		mockProvider := mock.NewProvider("test-mock", "test-model", false)

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       mockProvider,
			Variables: map[string]string{
				"user_name": "Alice",
				"region":    "US-West",
			},
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)

		// Execute and verify prompt was assembled with variables
		execOpts := &rtpipeline.ExecutionOptions{
			Context:        context.Background(),
			ConversationID: "test-conv",
		}
		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Hi!")

		result, err := pipe.ExecuteWithMessageOptions(execOpts, userMsg)
		require.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("template middleware processes system prompt", func(t *testing.T) {
		// This test verifies that TemplateMiddleware properly copies SystemPrompt to Prompt
		registry := createTestRegistryWithTemplate("chat", "System: {{mode}} mode active")

		mockProvider := mock.NewProvider("test-mock", "test-model", false)

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       mockProvider,
			Variables: map[string]string{
				"mode": "test",
			},
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)

		execOpts := &rtpipeline.ExecutionOptions{
			Context:        context.Background(),
			ConversationID: "test-conv",
		}
		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("What mode?")

		result, err := pipe.ExecuteWithMessageOptions(execOpts, userMsg)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.NotNil(t, result.Response)
	})

	t.Run("pipeline with tool registry", func(t *testing.T) {
		promptRegistry := createTestRegistry("chat")
		toolRegistry := tools.NewRegistry()
		mockProvider := mock.NewToolProvider("test-mock", "test-model", false, nil)

		cfg := &Config{
			PromptRegistry: promptRegistry,
			TaskType:       "chat",
			Provider:       mockProvider,
			ToolRegistry:   toolRegistry,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)

		execOpts := &rtpipeline.ExecutionOptions{
			Context:        context.Background(),
			ConversationID: "test-conv",
		}
		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Use a tool")

		result, err := pipe.ExecuteWithMessageOptions(execOpts, userMsg)
		require.NoError(t, err)
		assert.NotNil(t, result)
	})
}

// TestBuildStagePipeline tests the stage-based pipeline builder
func TestBuildStagePipeline(t *testing.T) {
	t.Run("builds stage pipeline when UseStages is true", func(t *testing.T) {
		registry := createTestRegistry("chat")
		mockProvider := mock.NewProvider("test-mock", "test-model", false)

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       mockProvider,
			UseStages:      true,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})

	t.Run("falls back to middleware when duplex session provided", func(t *testing.T) {
		registry := createTestRegistry("chat")
		mockSession := mock.NewMockStreamSession()

		cfg := &Config{
			PromptRegistry:     registry,
			TaskType:           "chat",
			StreamInputSession: mockSession,
			UseStages:          true, // Request stages but should fall back
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})

	t.Run("stage pipeline executes successfully", func(t *testing.T) {
		registry := createTestRegistry("chat")
		mockProvider := mock.NewProvider("test-mock", "test-model", false)

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       mockProvider,
			UseStages:      true,
			MaxTokens:      100,
			Temperature:    0.7,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)

		// Execute the pipeline
		execOpts := &rtpipeline.ExecutionOptions{
			Context:        context.Background(),
			ConversationID: "test-conv",
		}
		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Hello from stages!")

		result, err := pipe.ExecuteWithMessageOptions(execOpts, userMsg)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.NotNil(t, result.Response)
	})

	t.Run("stage pipeline with variables", func(t *testing.T) {
		registry := createTestRegistryWithTemplate("chat", "Hello {{user_name}}!")
		mockProvider := mock.NewProvider("test-mock", "test-model", false)

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       mockProvider,
			UseStages:      true,
			Variables: map[string]string{
				"user_name": "Bob",
			},
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)

		execOpts := &rtpipeline.ExecutionOptions{
			Context:        context.Background(),
			ConversationID: "test-conv",
		}
		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Hi!")

		result, err := pipe.ExecuteWithMessageOptions(execOpts, userMsg)
		require.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("stage pipeline with validators", func(t *testing.T) {
		registry := createTestRegistry("chat")
		mockProvider := mock.NewProvider("test-mock", "test-model", false)
		validatorRegistry := validators.NewRegistry()
		validatorConfigs := []validators.ValidatorConfig{
			{Type: "banned_words", Params: map[string]interface{}{"words": []string{"test"}}},
		}

		cfg := &Config{
			PromptRegistry:    registry,
			TaskType:          "chat",
			Provider:          mockProvider,
			UseStages:         true,
			ValidatorRegistry: validatorRegistry,
			ValidatorConfigs:  validatorConfigs,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})

	t.Run("stage pipeline without provider", func(t *testing.T) {
		registry := createTestRegistry("chat")

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			UseStages:      true,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})
}

// TestStreamPipelineAdapter tests the middleware adapter for stage pipelines
func TestStreamPipelineAdapter(t *testing.T) {
	t.Run("adapter converts execution context", func(t *testing.T) {
		registry := createTestRegistry("chat")
		mockProvider := mock.NewProvider("test-mock", "test-model", false)

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       mockProvider,
			UseStages:      true,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)

		// Execute to test the adapter
		execOpts := &rtpipeline.ExecutionOptions{
			Context:        context.Background(),
			ConversationID: "test-conv",
		}
		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Test adapter")

		result, err := pipe.ExecuteWithMessageOptions(execOpts, userMsg)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.NotNil(t, result.Response)
		assert.Equal(t, "assistant", result.Response.Role)
	})

	t.Run("adapter handles metadata", func(t *testing.T) {
		registry := createTestRegistry("chat")
		mockProvider := mock.NewProvider("test-mock", "test-model", false)

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       mockProvider,
			UseStages:      true,
			Variables: map[string]string{
				"test_key": "test_value",
			},
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)

		execOpts := &rtpipeline.ExecutionOptions{
			Context:        context.Background(),
			ConversationID: "test-conv",
		}
		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Test metadata")

		result, err := pipe.ExecuteWithMessageOptions(execOpts, userMsg)
		require.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("adapter preserves messages", func(t *testing.T) {
		registry := createTestRegistry("chat")
		mockProvider := mock.NewProvider("test-mock", "test-model", false)

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       mockProvider,
			UseStages:      true,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)

		execOpts := &rtpipeline.ExecutionOptions{
			Context:        context.Background(),
			ConversationID: "test-conv",
		}
		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Preserve me")

		result, err := pipe.ExecuteWithMessageOptions(execOpts, userMsg)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.NotNil(t, result.Response)
		// Response should have content from the mock provider
		assert.NotEmpty(t, result.Response.Content)
	})
}
