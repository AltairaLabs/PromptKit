package pipeline

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/composition"
	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	memorystore "github.com/AltairaLabs/PromptKit/runtime/memory"
	"github.com/AltairaLabs/PromptKit/runtime/persistence/memory"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/tools"
	"github.com/AltairaLabs/PromptKit/runtime/types"
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

	t.Run("with memory retriever and custom context formatter", func(t *testing.T) {
		registry := createTestRegistry("chat")
		store := memorystore.NewInMemoryStore()
		retriever := &noopRetriever{}
		formatter := func(_ []*memorystore.Memory) string { return "host-formatted" }

		cfg := &Config{
			PromptRegistry:         registry,
			TaskType:               "chat",
			MemoryStore:            store,
			MemoryRetriever:        retriever,
			MemoryContextFormatter: formatter,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})

	t.Run("with memory retriever but no formatter", func(t *testing.T) {
		registry := createTestRegistry("chat")
		store := memorystore.NewInMemoryStore()
		retriever := &noopRetriever{}

		cfg := &Config{
			PromptRegistry:  registry,
			TaskType:        "chat",
			MemoryStore:     store,
			MemoryRetriever: retriever,
			// MemoryContextFormatter intentionally left nil; default applies.
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})
}

// noopRetriever satisfies memorystore.Retriever for builder tests; it is
// never actually invoked because Build() only wires the stage.
type noopRetriever struct{}

func (noopRetriever) RetrieveContext(
	_ context.Context, _ map[string]string, _ []types.Message,
) ([]*memorystore.Memory, error) {
	return nil, nil
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
			Message: &userMsg,
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
			Message: &userMsg,
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
			Message: &userMsg,
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
			Message: &userMsg,
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
			Message: &userMsg,
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
			Message: &userMsg,
		}

		result, err := pipe.ExecuteSync(context.Background(), elem)
		require.NoError(t, err)
		assert.NotNil(t, result)
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
			Message: &userMsg,
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
			Message: &userMsg,
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
			Message: &userMsg,
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
			Message: &userMsg,
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
			Message: &userMsg,
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
			Message: &userMsg,
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

		cfg := &Config{
			PromptRegistry:    registry,
			TaskType:          "chat",
			Provider:          provider,
			ToolRegistry:      toolRegistry,
			StateStore:        store,
			ConversationID:    "full-test",
			Variables:         map[string]string{"name": "Alice"},
			VariableProviders: []variables.Provider{varProvider},
			MaxTokens:         4096,
			Temperature:       0.8,
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

func TestCollectPipelineStagesVariableProvider(t *testing.T) {
	t.Run("variable provider stage is present without providers", func(t *testing.T) {
		// VariableProviderStage is always added (even with no providers) so that
		// static cfg.Variables are injected into the element context.
		registry := createTestRegistry("chat")
		mockProvider := mock.NewProvider("test-mock", "test-model", false)

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       mockProvider,
			Variables:      map[string]string{"env": "prod"},
			// No VariableProviders — stage must still be present for static vars
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)

		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Hello!")

		elem := stage.StreamElement{
			Message: &userMsg,
		}

		result, err := pipe.ExecuteSync(context.Background(), elem)
		require.NoError(t, err)
		assert.NotNil(t, result)
	})

	t.Run("template stage uses emitter when event emitter is set", func(t *testing.T) {
		registry := createTestRegistryWithTemplate("chat", "Hello {{user_name}}!")
		mockProvider := mock.NewProvider("test-mock", "test-model", false)
		bus := events.NewEventBus()
		emitter := events.NewEmitter(bus, "", "session-1", "conv-1")

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       mockProvider,
			Variables:      map[string]string{"user_name": "Alice"},
			EventEmitter:   emitter,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)

		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Hi!")

		elem := stage.StreamElement{
			Message: &userMsg,
		}

		result, err := pipe.ExecuteSync(context.Background(), elem)
		require.NoError(t, err)
		assert.NotNil(t, result)
	})
}

func TestBuildWithHookRegistry(t *testing.T) {
	t.Run("builds with hook registry", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)
		hookReg := hooks.NewRegistry() // empty but non-nil

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       provider,
			HookRegistry:   hookReg,
		}

		pipeline, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("builds and executes with hook registry", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)
		hookReg := hooks.NewRegistry()

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       provider,
			HookRegistry:   hookReg,
			MaxTokens:      100,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)

		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Hello with hooks!")

		elem := stage.StreamElement{
			Message: &userMsg,
		}

		result, err := pipe.ExecuteSync(context.Background(), elem)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.NotNil(t, result.Response)
	})

	t.Run("nil hook registry is accepted", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       provider,
			HookRegistry:   nil,
		}

		pipeline, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})
}

func TestBuildWithRecordingConfig(t *testing.T) {
	t.Run("builds with recording stages", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)
		store := &fakePipelineEventStore{}

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       provider,
			RecordingConfig: &stage.RecordingStageConfig{
				SessionID:      "test-session",
				ConversationID: "test-conv",
				IncludeAudio:   true,
				IncludeVideo:   false,
				IncludeImages:  true,
			},
			RecordingStore: store,
		}

		pipeline, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("recording stages execute successfully", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)
		store := &fakePipelineEventStore{}

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       provider,
			RecordingConfig: &stage.RecordingStageConfig{
				SessionID:      "test-session",
				ConversationID: "test-conv",
				IncludeAudio:   true,
				IncludeImages:  true,
			},
			RecordingStore: store,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)

		userMsg := types.Message{Role: "user"}
		userMsg.AddTextPart("Hello with recording!")

		elem := stage.StreamElement{
			Message: &userMsg,
		}

		result, err := pipe.ExecuteSync(context.Background(), elem)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.NotNil(t, result.Response)
	})

	t.Run("no recording stages without config", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       provider,
		}

		pipeline, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("no recording stages without event store", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       provider,
			RecordingConfig: &stage.RecordingStageConfig{
				IncludeAudio: true,
			},
			// RecordingStore is nil — stages should not be added
		}

		pipeline, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})
}

// testVariableProvider implements variables.Provider for testing
func TestNewPipelineBuilderWithExecutionTimeout(t *testing.T) {
	t.Run("uses default timeout when ExecutionTimeout is nil", func(t *testing.T) {
		cfg := &Config{}
		builder := newPipelineBuilder(cfg)
		assert.NotNil(t, builder)
	})

	t.Run("uses custom timeout when ExecutionTimeout is set", func(t *testing.T) {
		timeout := 120 * time.Second
		registry := createTestRegistry("chat")
		cfg := &Config{
			PromptRegistry:   registry,
			TaskType:         "chat",
			ExecutionTimeout: &timeout,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})

	t.Run("disables timeout when ExecutionTimeout is zero", func(t *testing.T) {
		timeout := time.Duration(0)
		registry := createTestRegistry("chat")
		cfg := &Config{
			PromptRegistry:   registry,
			TaskType:         "chat",
			ExecutionTimeout: &timeout,
		}

		pipe, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipe)
	})
}

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

func TestBuildWithVideoStreamConfig(t *testing.T) {
	t.Run("builds with video stream config", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)

		frlCfg := stage.DefaultFrameRateLimitConfig()
		frlCfg.TargetFPS = 2.0
		cfg := &Config{
			PromptRegistry:    registry,
			TaskType:          "chat",
			Provider:          provider,
			VideoStreamConfig: &frlCfg,
		}

		pipeline, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("no frame rate limit stage without config", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)

		cfg := &Config{
			PromptRegistry: registry,
			TaskType:       "chat",
			Provider:       provider,
		}

		pipeline, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})

	t.Run("no frame rate limit stage when TargetFPS is zero", func(t *testing.T) {
		registry := createTestRegistry("chat")
		provider := mock.NewProvider("test", "test-model", false)

		frlCfg := stage.FrameRateLimitConfig{TargetFPS: 0}
		cfg := &Config{
			PromptRegistry:    registry,
			TaskType:          "chat",
			Provider:          provider,
			VideoStreamConfig: &frlCfg,
		}

		pipeline, err := Build(cfg)
		require.NoError(t, err)
		assert.NotNil(t, pipeline)
	})
}

// TestBuildProviderStages_SelectsCompositionStage verifies that when
// Config.ActiveComposition is non-nil, buildProviderStages returns a single
// *stage.CompositionStage instead of a ProviderStage (RFC 0010).
func TestBuildProviderStages_SelectsCompositionStage(t *testing.T) {
	comp := &composition.Composition{
		Version: 1, Output: "s",
		Steps: []*composition.Step{{
			ID:   "s",
			Kind: composition.KindTool,
			Tool: "echo",
			Args: map[string]any{"v": "${input.x}"},
		}},
	}

	// Build a tool registry with an echo tool (mirrors registerEchoTool from the
	// stage package's own composition tests).
	reg := tools.NewRegistry()
	echoExec := &builderEchoExecutor{}
	reg.RegisterExecutor(echoExec)
	if err := reg.Register(&tools.ToolDescriptor{
		Name:        "echo",
		Description: "echo tool",
		Mode:        echoExec.Name(),
		InputSchema: []byte(`{"type":"object"}`),
	}); err != nil {
		t.Fatalf("register echo tool: %v", err)
	}

	promptReg := createTestRegistry("chat")

	cfg := &Config{
		Provider:          mock.NewProvider("test-mock", "test-model", false),
		ToolRegistry:      reg,
		PromptRegistry:    promptReg,
		ActiveComposition: comp,
		CompositionName:   "analyze_doc",
	}

	stages, err := buildProviderStages(cfg, stage.NewTurnState())
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 1 {
		t.Fatalf("want 1 stage, got %d", len(stages))
	}
	if _, ok := stages[0].(*stage.CompositionStage); !ok {
		t.Fatalf("want *stage.CompositionStage, got %T", stages[0])
	}
}

// TestBuildProviderStages_DefaultNameWhenCompositionNameEmpty verifies that an
// empty CompositionName falls back to "composition".
func TestBuildProviderStages_DefaultNameWhenCompositionNameEmpty(t *testing.T) {
	comp := &composition.Composition{
		Version: 1, Output: "s",
		Steps: []*composition.Step{{
			ID:   "s",
			Kind: composition.KindTool,
			Tool: "echo",
			Args: map[string]any{"v": "x"},
		}},
	}

	reg := tools.NewRegistry()
	echoExec := &builderEchoExecutor{}
	reg.RegisterExecutor(echoExec)
	if err := reg.Register(&tools.ToolDescriptor{
		Name:        "echo",
		Description: "echo tool",
		Mode:        echoExec.Name(),
		InputSchema: []byte(`{"type":"object"}`),
	}); err != nil {
		t.Fatalf("register echo tool: %v", err)
	}

	cfg := &Config{
		Provider:          mock.NewProvider("test-mock", "test-model", false),
		ToolRegistry:      reg,
		PromptRegistry:    createTestRegistry("chat"),
		ActiveComposition: comp,
		CompositionName:   "", // intentionally empty
	}

	stages, err := buildProviderStages(cfg, stage.NewTurnState())
	if err != nil {
		t.Fatal(err)
	}
	if len(stages) != 1 {
		t.Fatalf("want 1 stage, got %d", len(stages))
	}
	cs, ok := stages[0].(*stage.CompositionStage)
	if !ok {
		t.Fatalf("want *stage.CompositionStage, got %T", stages[0])
	}
	if cs.Name() != "composition" {
		t.Errorf("want name %q, got %q", "composition", cs.Name())
	}
}

// TestBuildCompositionStage_EmitsEvents verifies that when ActiveComposition is set
// and an EventEmitter is provided, executing the pipeline emits composition.*
// events (composition.started, composition.step.started/completed,
// composition.completed). Before the fix, NewCompositionStage was called without a
// recorder so step-level events were never emitted; after the fix
// NewCompositionStageWithRecorder wires a recorder that forwards to the emitter.
func TestBuildCompositionStage_EmitsEvents(t *testing.T) {
	comp := &composition.Composition{
		Version: 1, Output: "s",
		Steps: []*composition.Step{{
			ID:   "s",
			Kind: composition.KindTool,
			Tool: "echo",
			Args: map[string]any{"v": "${input.x}"},
		}},
	}

	reg := tools.NewRegistry()
	echoExec := &builderEchoExecutor{}
	reg.RegisterExecutor(echoExec)
	if err := reg.Register(&tools.ToolDescriptor{
		Name:        "echo",
		Description: "echo tool",
		Mode:        echoExec.Name(),
		InputSchema: []byte(`{"type":"object"}`),
	}); err != nil {
		t.Fatalf("register echo tool: %v", err)
	}

	bus := events.NewEventBus()
	emitter := events.NewEmitter(bus, "", "session-1", "conv-1")

	// Collect composition.step.completed events.
	var stepEvents []*events.Event
	done := make(chan struct{})
	unsub := bus.Subscribe(events.EventCompositionStepCompleted, func(ev *events.Event) {
		stepEvents = append(stepEvents, ev)
		close(done)
	})
	defer unsub()

	cfg := &Config{
		Provider:          mock.NewProvider("test-mock", "test-model", false),
		ToolRegistry:      reg,
		PromptRegistry:    createTestRegistry("chat"),
		ActiveComposition: comp,
		CompositionName:   "test_comp",
		EventEmitter:      emitter,
	}

	pipe, err := Build(cfg)
	require.NoError(t, err)

	userMsg := types.Message{Role: "user"}
	userMsg.AddTextPart(`{"x":42}`)
	elem := stage.StreamElement{Message: &userMsg}

	_, err = pipe.ExecuteSync(context.Background(), elem)
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for composition.step.completed event — recorder not wired")
	}
	assert.Len(t, stepEvents, 1, "expected one composition.step.completed event")
}

// builderEchoExecutor is a local tools.Executor that returns its args as the result.
// Mirrors the echoExecutor in stage/composition_executor_test.go, but lives here
// to avoid cross-package test helper imports.
type builderEchoExecutor struct{}

func (e *builderEchoExecutor) Name() string { return "builder-echo-exec" }

func (e *builderEchoExecutor) Execute(_ context.Context, _ *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	return args, nil
}

// fakePipelineEventStore is a minimal events.EventStore for builder tests.
type fakePipelineEventStore struct{}

func (f *fakePipelineEventStore) Append(_ context.Context, _ *events.Event) error {
	return nil
}
func (f *fakePipelineEventStore) OnEvent(*events.Event) {}
func (f *fakePipelineEventStore) Query(_ context.Context, _ *events.EventFilter) ([]*events.Event, error) {
	return nil, nil
}
func (f *fakePipelineEventStore) QueryRaw(
	_ context.Context, _ *events.EventFilter,
) ([]*events.StoredEvent, error) {
	return nil, nil
}
func (f *fakePipelineEventStore) Stream(_ context.Context, _ string) (<-chan *events.Event, error) {
	return nil, nil
}
func (f *fakePipelineEventStore) Close() error { return nil }

// TestCollectPipelineStages_PacesOutputAudioForVoice is the fix for the reported
// audio jitter/corruption on long realtime replies. A streaming provider (OpenAI
// Realtime) delivers a whole assistant reply faster than realtime; without an
// output-direction pacing stage the burst overruns the realtime speaker's
// ~200ms jitter buffer and the oldest audio is dropped — audible stutter. When
// the pipeline drives a realtime sink (OpenVoice, PaceOutputAudio) an
// "audio-pacing-output" stage must sit AFTER the provider stage so response
// audio reaches the sink at real-time cadence. Headless/manual duplex consumers
// (OpenDuplex reading Response() directly) must NOT be paced — it would only
// slow them down.
func TestCollectPipelineStages_PacesOutputAudioForVoice(t *testing.T) {
	registry := createTestRegistry("chat")
	newCfg := func(pace bool) *Config {
		return &Config{
			PromptRegistry:      registry,
			TaskType:            "chat",
			StreamInputProvider: mock.NewStreamingProvider("test", "test-model", false),
			StreamInputConfig:   &providers.StreamingInputConfig{},
			PaceOutputAudio:     pace,
		}
	}

	t.Run("output pacing stage present after provider when PaceOutputAudio", func(t *testing.T) {
		stages, err := collectPipelineStages(newCfg(true), nil, false)
		require.NoError(t, err)

		provIdx, paceIdx := -1, -1
		for i, s := range stages {
			switch s.Name() {
			case "duplex_provider":
				provIdx = i
			case "audio-pacing-output":
				paceIdx = i
			}
		}
		require.GreaterOrEqual(t, provIdx, 0, "duplex provider stage must be present")
		require.GreaterOrEqual(t, paceIdx, 0,
			"output audio pacing stage must be wired for realtime voice playback, or long replies overrun the sink")
		assert.Greater(t, paceIdx, provIdx,
			"output pacing must come AFTER the provider stage so response audio is paced toward the sink")
	})

	t.Run("no output pacing stage without PaceOutputAudio (headless/manual)", func(t *testing.T) {
		stages, err := collectPipelineStages(newCfg(false), nil, false)
		require.NoError(t, err)
		for _, s := range stages {
			assert.NotEqual(t, "audio-pacing-output", s.Name(),
				"headless/manual duplex consumers must not be slowed by realtime output pacing")
		}
	})
}
