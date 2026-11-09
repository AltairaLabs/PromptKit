package pipeline

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/composition"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	memorystore "github.com/AltairaLabs/PromptKit/runtime/v2/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/persistence/memory"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
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

// noopRetriever satisfies memorystore.Retriever for builder tests; it is
// never actually invoked because Build() only wires the stage.
type noopRetriever struct{}

func (noopRetriever) RetrieveContext(
	_ context.Context, _ map[string]string, _ []types.Message,
) ([]*memorystore.Memory, error) {
	return nil, nil
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

// VideoFrameResize puts an image-resize stage, carrying that config, right
// after the frame rate limiter; without it there is no resize stage.
func TestCollectPipelineStages_VideoFrameResize(t *testing.T) {
	newCfg := func(resize *stage.ImageResizeStageConfig) *Config {
		frlCfg := stage.DefaultFrameRateLimitConfig()
		return &Config{
			PromptRegistry:    createTestRegistry("chat"),
			TaskType:          "chat",
			Provider:          mock.NewProvider("test", "test-model", false),
			VideoStreamConfig: &frlCfg,
			VideoFrameResize:  resize,
		}
	}
	indexOf := func(stages []stage.Stage, name string) int {
		for i, s := range stages {
			if s.Name() == name {
				return i
			}
		}
		return -1
	}

	resizeCfg := stage.DefaultImageResizeStageConfig()
	resizeCfg.MaxWidth, resizeCfg.MaxHeight, resizeCfg.Quality = 640, 480, 70
	stages, err := collectPipelineStages(newCfg(&resizeCfg), nil, false)
	require.NoError(t, err)
	limitAt, resizeAt := indexOf(stages, "frame-rate-limit"), indexOf(stages, "image-resize")
	require.GreaterOrEqual(t, resizeAt, 0, "VideoFrameResize must add an image-resize stage")
	assert.Equal(t, limitAt+1, resizeAt, "resize runs on the frames the limiter keeps")
	resizeStage, ok := stages[resizeAt].(*stage.ImageResizeStage)
	require.True(t, ok)
	assert.Equal(t, resizeCfg, resizeStage.GetConfig())

	stages, err = collectPipelineStages(newCfg(nil), nil, false)
	require.NoError(t, err)
	assert.Equal(t, -1, indexOf(stages, "image-resize"))
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

// TestCollectPipelineStages_ReordersInputTranscript wires the transcript reorder
// stage right after the provider stage when ReorderInputTranscript is set, and
// omits it otherwise.
func TestCollectPipelineStages_ReordersInputTranscript(t *testing.T) {
	registry := createTestRegistry("chat")
	newCfg := func(reorder bool) *Config {
		return &Config{
			PromptRegistry:         registry,
			TaskType:               "chat",
			StreamInputProvider:    mock.NewStreamingProvider("test", "test-model", false),
			StreamInputConfig:      &providers.StreamingInputConfig{},
			ReorderInputTranscript: reorder,
		}
	}

	t.Run("reorder stage present after provider when enabled", func(t *testing.T) {
		stages, err := collectPipelineStages(newCfg(true), nil, false)
		require.NoError(t, err)
		provIdx, reorderIdx := -1, -1
		for i, s := range stages {
			switch s.Name() {
			case "duplex_provider":
				provIdx = i
			case "transcript-reorder":
				reorderIdx = i
			}
		}
		require.GreaterOrEqual(t, provIdx, 0)
		require.GreaterOrEqual(t, reorderIdx, 0, "transcript reorder stage must be wired when enabled")
		assert.Greater(t, reorderIdx, provIdx, "reorder must come after the provider stage")
	})

	t.Run("no reorder stage when disabled", func(t *testing.T) {
		stages, err := collectPipelineStages(newCfg(false), nil, false)
		require.NoError(t, err)
		for _, s := range stages {
			assert.NotEqual(t, "transcript-reorder", s.Name())
		}
	})
}

// TestBuild_RetrieverWithDuplexProviderIsRejected pins the duplex deadlock
// (#1962): MemoryRetrievalStage accumulates until its input channel closes, and
// a duplex session's input stays open for the session's lifetime, so the stage
// never forwards and the provider stage never receives a first element. The
// session hangs with no reply and no error — verified live against OpenAI
// Realtime — so the build must refuse instead.
func TestBuild_RetrieverWithDuplexProviderIsRejected(t *testing.T) {
	cfg := &Config{
		PromptRegistry:      createTestRegistry("chat"),
		TaskType:            "chat",
		MemoryRetriever:     &noopRetriever{},
		StreamInputProvider: mock.NewStreamingProvider("dup", "dup-model", false),
	}

	_, err := Build(cfg)
	require.ErrorIs(t, err, ErrRetrieverUnsupportedInDuplex)
}

// The control for the rejection above — that a retriever without a duplex
// provider still builds AND reaches the prompt — is
// TestWithRetriever_GroundsWithoutMemory in sdk/integration, which asserts the
// rendered system prompt rather than a non-nil pipeline.
