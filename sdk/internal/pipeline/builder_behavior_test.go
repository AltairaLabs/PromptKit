package pipeline

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	memorystore "github.com/AltairaLabs/PromptKit/runtime/v2/memory"
	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/runtime/v2/variables"
)

// runTurn sends one user message through a pipeline built from cfg and
// returns the result.
func runTurn(t *testing.T, cfg *Config, text string) *stage.ExecutionResult {
	t.Helper()
	pipe, err := Build(cfg)
	require.NoError(t, err)
	msg := types.Message{Role: "user"}
	msg.AddTextPart(text)
	result, err := pipe.ExecuteSync(context.Background(), stage.StreamElement{Message: &msg})
	require.NoError(t, err)
	return result
}

// The builder's job, seen from the caller: the pack's system template is
// rendered with the conversation's variables and reaches the provider with the
// prompt's sampling parameters, and the provider's reply comes back as the
// turn's assistant message. Build and its deprecated alias BuildStreamPipeline
// must produce the same pipeline.
func TestBuild_ProviderReceivesRenderedPromptAndSampling(t *testing.T) {
	builders := map[string]func(*Config) (*stage.StreamPipeline, error){
		"Build":               Build,
		"BuildStreamPipeline": BuildStreamPipeline,
	}
	for name, build := range builders {
		t.Run(name, func(t *testing.T) {
			p := newRecordingProvider()
			cfg := &Config{
				PromptRegistry: createTestRegistryWithTemplate("chat", "Hello {{user_name}}, you are in {{region}}!"),
				TaskType:       "chat",
				Provider:       p,
				MaxTokens:      2048,
				Temperature:    2.0,
				TopP:           0.9,
				Variables:      map[string]string{"user_name": "Alice", "region": "US-West"},
			}
			pipe, err := build(cfg)
			require.NoError(t, err)
			msg := types.Message{Role: "user"}
			msg.AddTextPart("Hi!")
			result, err := pipe.ExecuteSync(context.Background(), stage.StreamElement{Message: &msg})
			require.NoError(t, err)

			req := p.onlyRequest(t)
			assert.Equal(t, "Hello Alice, you are in US-West!", req.System)
			assert.Equal(t, 2048, req.MaxTokens)
			assert.Equal(t, float32(2.0), req.Temperature)
			assert.Equal(t, float32(0.9), req.TopP)
			require.Len(t, req.Messages, 1)
			assert.Equal(t, "user", req.Messages[0].Role)
			assert.Equal(t, "Hi!", req.Messages[0].GetContent())

			require.NotNil(t, result.Response)
			assert.Equal(t, "assistant", result.Response.Role)
			assert.Equal(t, recordedReply, result.Response.Content)
		})
	}
}

// An explicit zero temperature must reach the provider as a set zero, not as
// "unset" (which a provider replaces with its default).
func TestBuild_ExplicitZeroTemperatureReachesProvider(t *testing.T) {
	p := newRecordingProvider()
	runTurn(t, &Config{
		PromptRegistry: createTestRegistry("chat"),
		TaskType:       "chat",
		Provider:       p,
		Temperature:    0,
		TemperatureSet: true,
	}, "Hi!")

	req := p.onlyRequest(t)
	assert.Equal(t, float32(0), req.Temperature)
	assert.True(t, req.TemperatureSet, "an explicit zero temperature must be marked set")
}

// Dynamic variable providers contribute to the rendered prompt alongside the
// static variables.
func TestBuild_VariableProvidersReachRenderedPrompt(t *testing.T) {
	p := newRecordingProvider()
	runTurn(t, &Config{
		PromptRegistry:    createTestRegistryWithTemplate("chat", "{{name}} in {{env}}"),
		TaskType:          "chat",
		Provider:          p,
		Variables:         map[string]string{"name": "Alice"},
		VariableProviders: []variables.Provider{&testVariableProvider{vars: map[string]string{"env": "staging"}}},
	}, "Hi!")

	assert.Equal(t, "Alice in staging", p.onlyRequest(t).System)
}

// echoToolRegistry returns a registry holding one tool, get_weather, which the
// test prompt allows.
func echoToolRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	exec := &builderEchoExecutor{}
	reg.RegisterExecutor(exec)
	require.NoError(t, reg.Register(&tools.ToolDescriptor{
		Name:        "get_weather",
		Description: "weather",
		Mode:        exec.Name(),
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}))
	return reg
}

// The tool registry and the tool policy both reach the provider: the prompt's
// allowed tool is offered, with the policy's tool choice.
func TestBuild_ToolRegistryAndPolicyReachProvider(t *testing.T) {
	p := newRecordingProvider()
	runTurn(t, &Config{
		PromptRegistry: createTestRegistry("chat"),
		TaskType:       "chat",
		Provider:       p,
		ToolRegistry:   echoToolRegistry(t),
		ToolPolicy:     &rtpipeline.ToolPolicy{ToolChoice: "required", MaxRounds: 5},
	}, "Use a tool")

	p.mu.Lock()
	defer p.mu.Unlock()
	require.NotEmpty(t, p.tools, "tools were never built for the provider")
	assert.Equal(t, []string{"get_weather"}, p.tools[len(p.tools)-1])
	require.NotEmpty(t, p.choices)
	assert.Equal(t, "required", p.choices[0])
}

// With a state store, a turn's messages are persisted under the conversation
// ID and the next turn sends them back to the provider as history.
func TestBuild_StateStorePersistsAndReloadsHistory(t *testing.T) {
	store := statestore.NewMemoryStore()
	p := newRecordingProvider()
	cfg := &Config{
		PromptRegistry: createTestRegistry("chat"),
		TaskType:       "chat",
		Provider:       p,
		StateStore:     store,
		ConversationID: "conv-1",
	}
	runTurn(t, cfg, "first")
	runTurn(t, cfg, "second")

	req := p.lastRequest(t)
	var got []string
	for i := range req.Messages {
		got = append(got, req.Messages[i].Role+":"+req.Messages[i].GetContent())
	}
	assert.Equal(t, []string{"user:first", "assistant:" + recordedReply, "user:second"}, got)

	state, err := store.Load(context.Background(), "conv-1")
	require.NoError(t, err)
	require.Len(t, state.Messages, 4)
	assert.Equal(t, "second", state.Messages[2].GetContent())
	assert.Equal(t, recordedReply, state.Messages[3].GetContent())
}

// Without a provider there is no LLM stage: the user's message passes through
// the prompt stages and no assistant reply is produced.
func TestBuild_WithoutProviderProducesNoReply(t *testing.T) {
	result := runTurn(t, &Config{
		PromptRegistry: createTestRegistry("chat"),
		TaskType:       "chat",
	}, "Hi!")

	assert.Nil(t, result.Response, "no provider means no assistant reply")
	for i := range result.Messages {
		assert.NotEqual(t, "assistant", result.Messages[i].Role)
	}
}

// seedHistory stores n long user/assistant pairs under convID.
func seedHistory(t *testing.T, store *statestore.MemoryStore, convID string, n int) {
	t.Helper()
	long := strings.Repeat("lorem ipsum dolor sit amet ", 40)
	state := &statestore.ConversationState{ID: convID}
	for i := 0; i < n; i++ {
		state.Messages = append(state.Messages,
			types.Message{Role: "user", Content: long},
			types.Message{Role: "assistant", Content: long})
	}
	require.NoError(t, store.Save(context.Background(), state))
}

// A token budget truncates the history the provider sees ("sliding" drops the
// oldest), and the "fail" strategy refuses the turn instead.
func TestBuild_TokenBudgetTruncatesOrFails(t *testing.T) {
	newCfg := func(strategy string, p providers.Provider) *Config {
		store := statestore.NewMemoryStore()
		seedHistory(t, store, "conv-budget", 10)
		return &Config{
			PromptRegistry:     createTestRegistry("chat"),
			TaskType:           "chat",
			Provider:           p,
			StateStore:         store,
			ConversationID:     "conv-budget",
			TokenBudget:        800,
			TruncationStrategy: strategy,
		}
	}

	t.Run("sliding drops the oldest messages", func(t *testing.T) {
		p := newRecordingProvider()
		runTurn(t, newCfg("sliding", p), "latest")
		req := p.onlyRequest(t)
		assert.Less(t, len(req.Messages), 21, "history over budget must be truncated")
		require.NotEmpty(t, req.Messages)
		assert.Equal(t, "latest", req.Messages[len(req.Messages)-1].GetContent(),
			"truncation keeps the newest message")
	})

	t.Run("fail refuses the turn", func(t *testing.T) {
		p := newRecordingProvider()
		pipe, err := Build(newCfg("fail", p))
		require.NoError(t, err)
		msg := types.Message{Role: "user"}
		msg.AddTextPart("latest")
		_, err = pipe.ExecuteSync(context.Background(), stage.StreamElement{Message: &msg})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "budget")
		p.mu.Lock()
		defer p.mu.Unlock()
		assert.Empty(t, p.requests, "a refused turn must not reach the provider")
	})
}

// The truncation strategy names map onto the runtime strategies, and only the
// matching strategy carries its collaborator.
func TestBuildContextBuilderPolicy_StrategyMapping(t *testing.T) {
	rel := &stage.RelevanceConfig{}
	sum := &stubSummarizer{}
	cases := []struct {
		name     string
		want     stage.TruncationStrategy
		wantRel  bool
		wantSumm bool
	}{
		{"", stage.TruncateOldest, false, false},
		{"sliding", stage.TruncateOldest, false, false},
		{"oldest", stage.TruncateOldest, false, false},
		{"unknown", stage.TruncateOldest, false, false},
		{"summarize", stage.TruncateSummarize, false, true},
		{"relevance", stage.TruncateLeastRelevant, true, false},
		{"fail", stage.TruncateFail, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy := buildContextBuilderPolicy(&Config{
				TokenBudget: 1234, TruncationStrategy: tc.name, RelevanceConfig: rel, Summarizer: sum,
			})
			assert.Equal(t, 1234, policy.TokenBudget)
			assert.Equal(t, tc.want, policy.Strategy)
			assert.Equal(t, tc.wantRel, policy.RelevanceConfig != nil)
			assert.Equal(t, tc.wantSumm, policy.Summarizer != nil)
		})
	}
}

type stubSummarizer struct{}

func (stubSummarizer) Summarize(context.Context, []types.Message) (string, error) {
	return "summary", nil
}

// A provider hook in the registry sees the rendered request, and a denial
// stops the turn before the provider is called.
func TestBuild_HookRegistryGatesProviderCalls(t *testing.T) {
	t.Run("hook observes the rendered system prompt", func(t *testing.T) {
		hook := &observingHook{}
		p := newRecordingProvider()
		result := runTurn(t, &Config{
			PromptRegistry: createTestRegistryWithTemplate("chat", "Hooked {{x}}"),
			TaskType:       "chat",
			Provider:       p,
			Variables:      map[string]string{"x": "prompt"},
			HookRegistry:   hooks.NewRegistry(hooks.WithProviderHook(hook)),
		}, "Hi!")
		assert.Equal(t, []string{"Hooked prompt"}, hook.systemPrompts())
		require.NotNil(t, result.Response)
		assert.Equal(t, recordedReply, result.Response.Content)
	})

	t.Run("denying hook stops the call", func(t *testing.T) {
		p := newRecordingProvider()
		pipe, err := Build(&Config{
			PromptRegistry: createTestRegistry("chat"),
			TaskType:       "chat",
			Provider:       p,
			HookRegistry:   hooks.NewRegistry(hooks.WithProviderHook(&blockingVoiceHook{})),
		})
		require.NoError(t, err)
		msg := types.Message{Role: "user"}
		msg.AddTextPart("Hi!")
		_, err = pipe.ExecuteSync(context.Background(), stage.StreamElement{Message: &msg})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "blocked in voice")
		p.mu.Lock()
		defer p.mu.Unlock()
		assert.Empty(t, p.requests, "a denied call must not reach the provider")
	})
}

type observingHook struct {
	mu   sync.Mutex
	seen []string
}

func (*observingHook) Name() string { return "observing-hook" }

func (h *observingHook) BeforeCall(_ context.Context, req *hooks.ProviderRequest) hooks.Decision {
	h.mu.Lock()
	h.seen = append(h.seen, req.SystemPrompt)
	h.mu.Unlock()
	return hooks.Allow
}

func (*observingHook) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}

func (h *observingHook) systemPrompts() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.seen...)
}

// waitForEvents polls get until it returns at least n items or 2s pass. The
// bus delivers on a worker pool, so events arrive after ExecuteSync returns.
func waitForEvents[T any](get func() []T, n int) []T {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := get(); len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	return get()
}

// With an emitter, the template stage reports the prompt it rendered.
func TestBuild_EmitsTemplateRenderedWithTheRenderedPrompt(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	var mu sync.Mutex
	var rendered []*events.TemplateRenderedData
	bus.Subscribe(events.EventTemplateRendered, func(e *events.Event) {
		if d, ok := e.Data.(*events.TemplateRenderedData); ok {
			mu.Lock()
			rendered = append(rendered, d)
			mu.Unlock()
		}
	})

	runTurn(t, &Config{
		PromptRegistry: createTestRegistryWithTemplate("chat", "Hello {{user_name}}!"),
		TaskType:       "chat",
		Provider:       newRecordingProvider(),
		Variables:      map[string]string{"user_name": "Alice"},
		EventEmitter:   events.NewEmitter(bus, "", "session-1", "conv-1"),
	}, "Hi!")

	got := waitForEvents(func() []*events.TemplateRenderedData {
		mu.Lock()
		defer mu.Unlock()
		return append([]*events.TemplateRenderedData(nil), rendered...)
	}, 1)
	require.Len(t, got, 1)
	assert.Equal(t, "chat", got[0].TaskType)
	assert.Equal(t, "Hello Alice!", got[0].SystemPrompt)
	assert.Equal(t, "Alice", got[0].VariablesUsed["user_name"])
}

// staticRetriever returns one fixed memory.
type staticRetriever struct{}

func (staticRetriever) RetrieveContext(
	context.Context, map[string]string, []types.Message,
) ([]*memorystore.Memory, error) {
	return []*memorystore.Memory{{ID: "m1", Content: "user likes tea"}}, nil
}

// Retrieved memories reach the rendered prompt through memory_context, and a
// host formatter replaces the default rendering.
func TestBuild_MemoryRetrieverGroundsPrompt(t *testing.T) {
	t.Run("custom formatter", func(t *testing.T) {
		p := newRecordingProvider()
		runTurn(t, &Config{
			PromptRegistry:  createTestRegistryWithTemplate("chat", "ctx=[{{memory_context}}]"),
			TaskType:        "chat",
			Provider:        p,
			MemoryStore:     memorystore.NewInMemoryStore(),
			MemoryRetriever: staticRetriever{},
			MemoryContextFormatter: func(ms []*memorystore.Memory) string {
				return "host:" + ms[0].Content
			},
		}, "Hi!")
		assert.Equal(t, "ctx=[host:user likes tea]", p.onlyRequest(t).System)
	})

	t.Run("default formatter", func(t *testing.T) {
		p := newRecordingProvider()
		runTurn(t, &Config{
			PromptRegistry:  createTestRegistryWithTemplate("chat", "ctx=[{{memory_context}}]"),
			TaskType:        "chat",
			Provider:        p,
			MemoryRetriever: staticRetriever{},
		}, "Hi!")
		want := "ctx=[" + memorystore.DefaultContextFormatter(
			[]*memorystore.Memory{{ID: "m1", Content: "user likes tea"}}) + "]"
		assert.Equal(t, want, p.onlyRequest(t).System)
		assert.Contains(t, p.onlyRequest(t).System, "user likes tea")
	})
}

// With a recording config and store, both the user's input and the assistant's
// output are recorded as message events; without a store nothing is recorded.
func TestBuild_RecordingCapturesInputAndOutput(t *testing.T) {
	store, err := events.NewFileEventStore(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	runTurn(t, &Config{
		PromptRegistry: createTestRegistry("chat"),
		TaskType:       "chat",
		Provider:       newRecordingProvider(),
		RecordingConfig: &stage.RecordingStageConfig{
			SessionID: "rec-session", ConversationID: "rec-conv", IncludeAudio: true, IncludeImages: true,
		},
		RecordingStore: store,
	}, "Hello with recording!")

	evs, err := store.Query(context.Background(), &events.EventFilter{SessionID: "rec-session"})
	require.NoError(t, err)
	var got []string
	for _, e := range evs {
		if d, ok := e.Data.(*events.MessageCreatedData); ok {
			got = append(got, d.Role+":"+d.GetContent())
		}
	}
	// Each message is recorded exactly once, with its text where a reader of
	// the recording finds it (#2567).
	assert.ElementsMatch(t, []string{"user:Hello with recording!", "assistant:" + recordedReply}, got)
}

// stageNames lists the names of the stages a config builds, in order.
func stageNames(t *testing.T, cfg *Config) []string {
	t.Helper()
	stages, err := collectPipelineStages(cfg, nil, false)
	require.NoError(t, err)
	names := make([]string, 0, len(stages))
	for _, s := range stages {
		names = append(names, s.Name())
	}
	return names
}

// The optional pre-provider stages are gated exactly: recording needs both a
// config and a store; frame rate limiting needs a positive TargetFPS; image
// preprocessing needs its config. Each sits before the provider stage.
func TestCollectPipelineStages_OptionalStageGates(t *testing.T) {
	base := func() *Config {
		return &Config{
			PromptRegistry: createTestRegistry("chat"),
			TaskType:       "chat",
			Provider:       newRecordingProvider(),
		}
	}
	defaultNames := stageNames(t, base())
	assert.Equal(t, []string{"variable_provider", "prompt_assembly", "template", "provider"}, defaultNames)

	recNoStore := base()
	recNoStore.RecordingConfig = &stage.RecordingStageConfig{IncludeAudio: true}
	assert.Equal(t, defaultNames, stageNames(t, recNoStore), "recording without a store adds nothing")

	fps0 := base()
	fps0.VideoStreamConfig = &stage.FrameRateLimitConfig{TargetFPS: 0}
	assert.Equal(t, defaultNames, stageNames(t, fps0), "TargetFPS 0 adds no limiter")

	fps2 := base()
	frl := stage.DefaultFrameRateLimitConfig()
	frl.TargetFPS = 2
	fps2.VideoStreamConfig = &frl
	assert.Equal(t,
		[]string{"variable_provider", "prompt_assembly", "template", "frame-rate-limit", "provider"},
		stageNames(t, fps2))

	img := base()
	ipc := stage.DefaultImagePreprocessConfig()
	img.ImagePreprocessConfig = &ipc
	assert.Equal(t,
		[]string{"variable_provider", "prompt_assembly", "template", "image-preprocess", "provider"},
		stageNames(t, img))
}

// ExecutionTimeout bounds a turn: a provider that never answers is cut off at
// the configured deadline.
func TestBuild_ExecutionTimeoutCancelsAStuckTurn(t *testing.T) {
	p := newRecordingProvider()
	p.block = true
	timeout := 30 * time.Millisecond
	pipe, err := Build(&Config{
		PromptRegistry:   createTestRegistry("chat"),
		TaskType:         "chat",
		Provider:         p,
		ExecutionTimeout: &timeout,
	})
	require.NoError(t, err)

	msg := types.Message{Role: "user"}
	msg.AddTextPart("Hi!")
	start := time.Now()
	_, err = pipe.ExecuteSync(context.Background(), stage.StreamElement{Message: &msg})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 2*time.Second, "the turn must end at the execution timeout")
}

// pipelineConfigFor maps ExecutionTimeout: nil keeps the runtime default, a value
// overrides it, zero disables it, and a duplex provider always disables it.
func TestPipelineConfigFor_ExecutionTimeout(t *testing.T) {
	custom := 120 * time.Second
	zero := time.Duration(0)
	assert.Equal(t, stage.DefaultPipelineConfig().ExecutionTimeout, pipelineConfigFor(&Config{}).ExecutionTimeout)
	assert.Equal(t, custom, pipelineConfigFor(&Config{ExecutionTimeout: &custom}).ExecutionTimeout)
	assert.Equal(t, time.Duration(0), pipelineConfigFor(&Config{ExecutionTimeout: &zero}).ExecutionTimeout)
	assert.Equal(t, time.Duration(0), pipelineConfigFor(&Config{
		ExecutionTimeout:    &custom,
		StreamInputProvider: newRecordingProvider(),
	}).ExecutionTimeout)
}

// With a streaming-input provider the builder opens a duplex session carrying
// the rendered prompt and the prompt's sampling parameters, and forwards the
// user's text into it.
func TestBuild_DuplexSessionGetsRenderedPromptAndSampling(t *testing.T) {
	p := newRecordingProvider()
	p.WithAutoRespond("duplex reply")
	pipe, err := Build(&Config{
		PromptRegistry:      createTestRegistryWithTemplate("chat", "Voice for {{user_name}}"),
		TaskType:            "chat",
		StreamInputProvider: p,
		StreamInputConfig:   &providers.StreamingInputConfig{},
		Variables:           map[string]string{"user_name": "Alice"},
		MaxTokens:           321,
	})
	require.NoError(t, err)

	in := make(chan stage.StreamElement, 1)
	text := "hello duplex"
	in <- stage.StreamElement{Text: &text}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := pipe.Execute(ctx, in)
	require.NoError(t, err)

	var reply strings.Builder
	for elem := range out {
		if elem.Text != nil {
			reply.WriteString(*elem.Text)
		}
		if elem.EndOfTurn || strings.Contains(reply.String(), "duplex reply") {
			close(in)
			in = nil
			break
		}
	}
	cancel()
	for range out { //nolint:revive // drain
	}

	p.mu.Lock()
	sessions := append([]*providers.StreamingInputConfig(nil), p.sessions...)
	p.mu.Unlock()
	require.Len(t, sessions, 1)
	assert.Equal(t, "Voice for Alice", sessions[0].SystemInstruction)
	require.NotNil(t, sessions[0].Sampling)
	assert.Equal(t, 321, sessions[0].Sampling.MaxTokens)
	assert.Contains(t, p.GetSession().GetTexts(), "hello duplex")
	assert.Contains(t, reply.String(), "duplex reply")
}
