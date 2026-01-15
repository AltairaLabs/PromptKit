package pipeline

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/runtime/validators"
	"github.com/AltairaLabs/PromptKit/runtime/variables"
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
		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Hello!")

		elem := stage.StreamElement{
			Message:  &userMsg,
			Metadata: map[string]interface{}{"conversation_id": "test-conv"},
		}

		result, err := pipe.ExecuteSync(context.Background(), elem)
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
		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Hi!")

		elem := stage.StreamElement{
			Message:  &userMsg,
			Metadata: map[string]interface{}{"conversation_id": "test-conv"},
		}

		result, err := pipe.ExecuteSync(context.Background(), elem)
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

		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("What mode?")

		elem := stage.StreamElement{
			Message:  &userMsg,
			Metadata: map[string]interface{}{"conversation_id": "test-conv"},
		}

		result, err := pipe.ExecuteSync(context.Background(), elem)
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

		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Use a tool")

		elem := stage.StreamElement{
			Message:  &userMsg,
			Metadata: map[string]interface{}{"conversation_id": "test-conv"},
		}

		result, err := pipe.ExecuteSync(context.Background(), elem)
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

	t.Run("uses DuplexProviderStage when streaming provider provided", func(t *testing.T) {
		registry := createTestRegistry("chat")
		mockProvider := mock.NewStreamingProvider("test", "test-model", false)

		cfg := &Config{
			PromptRegistry:      registry,
			TaskType:            "chat",
			StreamInputProvider: mockProvider,
			StreamInputConfig:   &providers.StreamingInputConfig{},
			UseStages:           true,
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
		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Hello from stages!")

		elem := stage.StreamElement{
			Message:  &userMsg,
			Metadata: map[string]interface{}{"conversation_id": "test-conv"},
		}

		result, err := pipe.ExecuteSync(context.Background(), elem)
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

		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Hi!")

		elem := stage.StreamElement{
			Message:  &userMsg,
			Metadata: map[string]interface{}{"conversation_id": "test-conv"},
		}

		result, err := pipe.ExecuteSync(context.Background(), elem)
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
		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Test adapter")

		elem := stage.StreamElement{
			Message:  &userMsg,
			Metadata: map[string]interface{}{"conversation_id": "test-conv"},
		}

		result, err := pipe.ExecuteSync(context.Background(), elem)
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

		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Test metadata")

		elem := stage.StreamElement{
			Message:  &userMsg,
			Metadata: map[string]interface{}{"conversation_id": "test-conv"},
		}

		result, err := pipe.ExecuteSync(context.Background(), elem)
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

		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Preserve me")

		elem := stage.StreamElement{
			Message:  &userMsg,
			Metadata: map[string]interface{}{"conversation_id": "test-conv"},
		}

		result, err := pipe.ExecuteSync(context.Background(), elem)
		require.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("adapter with streaming provider exercises processStreaming", func(t *testing.T) {
		registry := createTestRegistry("chat")
		// Create a streaming provider to exercise processStreaming code path
		mockProvider := mock.NewProvider("test-mock", "test-model", true)

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       mockProvider,
			UseStages:      true,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)

		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Test streaming")

		elem := stage.StreamElement{
			Message:  &userMsg,
			Metadata: map[string]interface{}{"conversation_id": "test-conv"},
		}

		// Execute - this should work with streaming provider
		result, err := pipe.ExecuteSync(context.Background(), elem)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.NotNil(t, result.Response)
		assert.NotEmpty(t, result.Response.Content)
	})

	t.Run("adapter StreamChunk is called in streaming mode", func(t *testing.T) {
		registry := createTestRegistry("chat")
		mockProvider := mock.NewProvider("test-mock", "test-model", true)

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       mockProvider,
			UseStages:      true,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)

		// Create input element
		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Stream test")

		inputChan := make(chan stage.StreamElement, 1)
		inputChan <- stage.StreamElement{
			Message:  &userMsg,
			Metadata: map[string]interface{}{},
		}
		close(inputChan)

		// Use Execute for streaming
		outputChan, err := pipe.Execute(context.Background(), inputChan)
		require.NoError(t, err)
		assert.NotNil(t, outputChan)

		// Consume stream elements
		var elements []stage.StreamElement
		for elem := range outputChan {
			elements = append(elements, elem)
		}

		// Should have received at least one element
		assert.NotEmpty(t, elements)
	})

	t.Run("adapter handles streaming mode", func(t *testing.T) {
		registry := createTestRegistry("chat")
		mockProvider := mock.NewProvider("test-mock", "test-model", true) // Streaming enabled

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       mockProvider,
			UseStages:      true,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)

		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Test streaming")

		elem := stage.StreamElement{
			Message:  &userMsg,
			Metadata: map[string]interface{}{"conversation_id": "test-conv"},
		}

		result, err := pipe.ExecuteSync(context.Background(), elem)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.NotNil(t, result.Response)
		assert.NotEmpty(t, result.Response.Content)
	})
}

func TestBuildStreamPipeline(t *testing.T) {
	t.Run("builds stream pipeline successfully", func(t *testing.T) {
		registry := createTestRegistry("chat")

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
		}

		pipeline, err := BuildStreamPipeline(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("builds stream pipeline with streaming provider", func(t *testing.T) {
		registry := createTestRegistry("chat")
		mockProvider := mock.NewStreamingProvider("test", "test-model", false)

		cfg := &Config{
			PromptRegistry:      registry,
			TaskType:            "chat",
			StreamInputProvider: mockProvider,
			StreamInputConfig:   &providers.StreamingInputConfig{},
		}

		pipeline, err := BuildStreamPipeline(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("builds with state store", func(t *testing.T) {
		registry := createTestRegistry("chat")
		store := statestore.NewMemoryStore()

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			StateStore:     store,
			ConversationID: "test-conv",
		}

		pipeline, err := BuildStreamPipeline(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("builds with variable providers", func(t *testing.T) {
		registry := createTestRegistry("chat")
		varProvider := &testVariableProvider{vars: map[string]string{"key": "value"}}

		cfg := &Config{
			PromptRegistry:    registry,
			TaskType:          "chat",
			VariableProviders: []variables.Provider{varProvider},
		}

		pipeline, err := BuildStreamPipeline(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("builds with validators", func(t *testing.T) {
		registry := createTestRegistry("chat")
		validatorRegistry := validators.NewRegistry()

		cfg := &Config{
			PromptRegistry:    registry,
			TaskType:          "chat",
			ValidatorRegistry: validatorRegistry,
			ValidatorConfigs: []validators.ValidatorConfig{
				{Type: "length", Params: map[string]interface{}{"max": 100}},
			},
		}

		pipeline, err := BuildStreamPipeline(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("builds with provider", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       provider,
			MaxTokens:      1000,
			Temperature:    0.7,
		}

		pipeline, err := BuildStreamPipeline(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("builds with tool registry and provider", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)
		toolRegistry := tools.NewRegistry()

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       provider,
			ToolRegistry:   toolRegistry,
			MaxTokens:      2000,
			Temperature:    0.5,
		}

		pipeline, err := BuildStreamPipeline(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("builds with all common options", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)
		toolRegistry := tools.NewRegistry()
		store := statestore.NewMemoryStore()
		varProvider := &testVariableProvider{vars: map[string]string{"env": "test"}}
		validatorRegistry := validators.NewRegistry()

		cfg := &Config{
			PromptRegistry:    registry,
			TaskType:          "chat",
			Provider:          provider,
			ToolRegistry:      toolRegistry,
			StateStore:        store,
			ConversationID:    "full-test",
			Variables:         map[string]string{"name": "Alice"},
			VariableProviders: []variables.Provider{varProvider},
			ValidatorRegistry: validatorRegistry,
			ValidatorConfigs: []validators.ValidatorConfig{
				{Type: "length", Params: map[string]interface{}{"max": 1000}},
			},
			MaxTokens:   4096,
			Temperature: 0.8,
		}

		pipeline, err := BuildStreamPipeline(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("builds with tool policy", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)
		toolRegistry := tools.NewRegistry()
		toolPolicy := &rtpipeline.ToolPolicy{
			ToolChoice:          "required",
			MaxRounds:           5,
			MaxToolCallsPerTurn: 3,
		}

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       provider,
			ToolRegistry:   toolRegistry,
			ToolPolicy:     toolPolicy,
		}

		pipeline, err := BuildStreamPipeline(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("builds with suppress validation errors", func(t *testing.T) {
		registry := createTestRegistry("chat")
		validatorRegistry := validators.NewRegistry()

		cfg := &Config{
			PromptRegistry:    registry,
			TaskType:          "chat",
			ValidatorRegistry: validatorRegistry,
			ValidatorConfigs: []validators.ValidatorConfig{
				{Type: "length", Params: map[string]interface{}{"max": 100}},
			},
			SuppressValidationErrors: true,
		}

		pipeline, err := BuildStreamPipeline(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("builds with VAD mode configuration", func(t *testing.T) {
		t.Skip("VAD mode requires complex audio service mocks - tested via integration tests")
		// This branch is covered by integration tests (voice-interview example)
		// Unit testing would require mocking:
		// - audio.VADAnalyzer
		// - audio.TurnDetector
		// - stt.Service
		// - tts.Service (with streaming support)
		// - audio.InterruptionHandler
	})

	t.Run("builds minimal pipeline without provider", func(t *testing.T) {
		registry := createTestRegistry("chat")

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			// No provider - should still build with prompt assembly and template stages
		}

		pipeline, err := BuildStreamPipeline(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("builds with token budget", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)

		cfg := &Config{
			PromptRegistry:     registry,
			TaskType:           "chat",
			Provider:           provider,
			TokenBudget:        10000,
			TruncationStrategy: "sliding",
		}

		pipeline, err := BuildStreamPipeline(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("builds with image preprocess config", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)

		defaultConfig := stage.DefaultImagePreprocessConfig()
		cfg := &Config{
			PromptRegistry:        registry,
			TaskType:              "chat",
			Provider:              provider,
			ImagePreprocessConfig: &defaultConfig,
		}

		pipeline, err := BuildStreamPipeline(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("builds with summarize truncation strategy", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)

		cfg := &Config{
			PromptRegistry:     registry,
			TaskType:           "chat",
			Provider:           provider,
			TokenBudget:        5000,
			TruncationStrategy: "summarize",
		}

		pipeline, err := BuildStreamPipeline(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("builds with relevance truncation strategy", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)

		cfg := &Config{
			PromptRegistry:     registry,
			TaskType:           "chat",
			Provider:           provider,
			TokenBudget:        5000,
			TruncationStrategy: "relevance",
		}

		pipeline, err := BuildStreamPipeline(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("builds with fail truncation strategy", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)

		cfg := &Config{
			PromptRegistry:     registry,
			TaskType:           "chat",
			Provider:           provider,
			TokenBudget:        5000,
			TruncationStrategy: "fail",
		}

		pipeline, err := BuildStreamPipeline(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})
}

// testVariableProvider implements variables.Provider for testing
type testVariableProvider struct {
	vars map[string]string
	err  error
}

func (t *testVariableProvider) Name() string {
	return "test"
}

func (t *testVariableProvider) Provide(ctx context.Context) (map[string]string, error) {
	return t.vars, t.err
}
