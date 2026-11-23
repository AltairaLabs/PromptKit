package stage

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/composition"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/stagetest"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestContextCompactor_ForProvider(t *testing.T) {
	small := &windowedProvider{callRecordingProvider: newCallRecordingProvider("small", false), window: 8000}
	plain := newCallRecordingProvider("plain", false)

	fixed := &ContextCompactor{BudgetTokens: 1_000_000}
	assert.Same(t, fixed, fixed.ForProvider(small), "a fixed budget is the host's, and stays")

	following := &ContextCompactor{BudgetTokens: 1_000_000, BudgetFromProvider: true, PinRecentCount: 2}
	got, ok := following.ForProvider(small).(*ContextCompactor)
	require.True(t, ok)
	assert.Equal(t, 8000, got.BudgetTokens)
	assert.Equal(t, 2, got.PinRecentCount, "the rest of the configuration is kept")
	assert.Equal(t, 1_000_000, following.BudgetTokens, "the original is not modified")
	assert.Equal(t, DefaultBudgetTokens, following.ForProvider(plain).TokenBudget(),
		"a provider reporting no window gets the default")

	off := &ContextCompactor{BudgetTokens: 0, BudgetFromProvider: true}
	assert.Same(t, off, off.ForProvider(small), "a zero budget means compaction is off, and stays off")
}

// ContextCompactor reports the positions of the messages it removed.
func TestContextCompactor_ReportsRemovedIndices(t *testing.T) {
	c := &ContextCompactor{BudgetTokens: 100, Rules: []CompactionRule{CollapsePairs()}}
	cr := c.Compact(toolHistory(), 0)
	require.NotEmpty(t, cr.RemovedIndices)
	assert.Len(t, cr.Messages, len(toolHistory())-len(cr.RemovedIndices))
	for _, i := range cr.RemovedIndices {
		assert.Equal(t, "assistant", toolHistory()[i].Role, "CollapsePairs removes the assistant half of a pair")
	}
}

// Composition steps take the opt-out from their deps, and validate by default.
func TestCompositionStepProviderConfig_CarriesSkipContentValidation(t *testing.T) {
	reg := prompt.NewRegistryWithRepository(newMockRepo())
	registerSimplePrompt(t, reg, "p")
	step := &composition.Step{ID: "s", Kind: composition.KindPrompt, PromptTask: "p"}
	prov := mock.NewProvider("m", "m", false)

	for _, skip := range []bool{true, false} {
		deps := CompositionExecutorDeps{PromptRegistry: reg, SkipContentValidation: skip}
		cfg, err := deps.stepProviderConfig(step, prov)
		require.NoError(t, err)
		assert.Equal(t, skip, cfg.SkipContentValidation)
	}
}

// countingEcho is a local executor that counts its runs.
type countingEcho struct{ runs atomic.Int32 }

func (e *countingEcho) Name() string { return "counting-echo" }

// A composition step's calls count under the enclosing conversation's ID, so
// a window shared with the conversation's own turns spans both.
func TestCompositionExecutor_StepCallsCountUnderTheConversation(t *testing.T) {
	window := NewCallRateWindow()
	policy := &pipeline.ToolPolicy{MaxCallsPerMinute: 1}

	// A turn of conv-a's own ProviderStage calls echo once, filling the window.
	outerReg := tools.NewRegistry()
	outerEcho := &countingEcho{}
	outerReg.RegisterExecutor(outerEcho)
	require.NoError(t, outerReg.Register(&tools.ToolDescriptor{
		Name: "echo", Description: "echo", Mode: outerEcho.Name(), InputSchema: json.RawMessage(`{"type":"object"}`),
	}))
	outerTS := NewTurnState()
	outerTS.ConversationID = "conv-a"
	outerTS.AllowedTools = []string{"echo"}
	outerProv := &loopingToolProvider{ToolProvider: mock.NewToolProvider("outer", "m", false, nil)}
	outer := NewProviderStageWithTurnState(outerProv, outerReg,
		&pipeline.ToolPolicy{MaxCallsPerMinute: 1, StopOnTool: "echo"},
		&ProviderConfig{CallRate: window}, nil, nil, outerTS)
	in := make(chan StreamElement, 1)
	in <- NewMessageElement(&types.Message{Role: "user", Content: "go"})
	close(in)
	out := make(chan StreamElement, 16)
	require.NoError(t, outer.Process(context.Background(), in, out))
	require.Equal(t, int32(1), outerEcho.runs.Load(), "the outer turn ran echo once")

	// A step of the same conversation's composition calls echo in the same minute.
	reg := prompt.NewRegistryWithRepository(newMockRepo())
	require.NoError(t, reg.RegisterConfig("a", &prompt.Config{Spec: prompt.Spec{
		TaskType: "a", SystemTemplate: "You help.", AllowedTools: []string{"echo"},
	}}))
	stepReg := tools.NewRegistry()
	echo := &countingEcho{}
	stepReg.RegisterExecutor(echo)
	require.NoError(t, stepReg.Register(&tools.ToolDescriptor{
		Name: "echo", Description: "echo", Mode: echo.Name(), InputSchema: json.RawMessage(`{"type":"object"}`),
	}))
	parent := NewTurnState()
	parent.ConversationID = "conv-a"
	prov := &loopingToolProvider{ToolProvider: mock.NewToolProvider("loop", "m", false, nil)}
	exec := NewCompositionStepExecutor(CompositionExecutorDeps{
		PromptRegistry: reg, ToolRegistry: stepReg, ToolPolicy: policy, CallRate: window, ParentTurnState: parent,
		Provider: prov,
	})
	_, _ = exec(context.Background(), &composition.Step{
		ID: "s", Kind: composition.KindAgent, PromptTask: "a", Tools: []string{"echo"},
		Termination: &composition.Termination{ToolCalled: "echo"},
	}, json.RawMessage(`"go"`))
	require.Positive(t, prov.rounds.Load(), "the step's model called echo")
	assert.Zero(t, echo.runs.Load(), "the step's call is over the conversation's limit")
}

// passthroughRecorderStage records every element it sees and forwards it, so a
// test can prove a stage downstream of ProviderStage still ran.
type passthroughRecorderStage struct {
	mu   sync.Mutex
	seen []types.Message
}

func (s *passthroughRecorderStage) messages() []types.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]types.Message(nil), s.seen...)
}

// TestProviderStage_EnforcedPreCall_DownstreamStagesStillRun proves that an
// enforcing input guardrail skips only the provider work inside ProviderStage:
// the canned turn is emitted, the stage returns normally, and the next stage in
// the pipeline receives it. Guardrails belong to one stage — they must not
// abort the pipeline.
func TestProviderStage_EnforcedPreCall_DownstreamStagesStillRun(t *testing.T) {
	provider := &redactionRecordingProvider{Provider: mock.NewProvider("p", "m", false)}

	reg := hooks.NewRegistry(hooks.WithProviderHook(&enforceBeforeCallHook{
		reason:      "input blocked",
		replacement: "I can't help with that.",
		metadata:    map[string]any{"validator_type": "test_input"},
	}))

	provStage := NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, reg)
	recorder := &passthroughRecorderStage{}

	pipe, err := NewPipelineBuilder().Chain(provStage, recorder).Build()
	require.NoError(t, err)

	userMsg := types.Message{Role: "user", Content: "something disallowed"}
	_, err = pipe.ExecuteSync(context.Background(), NewMessageElement(&userMsg))
	require.NoError(t, err, "enforcement must not fail the pipeline")

	assert.Equal(t, 0, provider.callCount(), "provider must not be called")

	seen := recorder.messages()
	var blocked *types.Message
	for i := range seen {
		if seen[i].Role == "assistant" {
			blocked = &seen[i]
		}
	}
	require.NotNil(t, blocked, "downstream stage must receive the canned assistant turn")
	assert.Equal(t, "I can't help with that.", blocked.Content)
	assert.Equal(t, types.FinishReasonSafety, blocked.FinishReason,
		"blocked turn must be marked so downstream stages can detect the skip")
}

// TestExecute_StreamingProviderSkipsTurnAfterUpstreamFailure runs it through
// the pipeline: an upstream stage (an STT stage, say) that emits the user
// message and then fails must not get that message answered.
func TestExecute_StreamingProviderSkipsTurnAfterUpstreamFailure(t *testing.T) {
	for i := 0; i < 20; i++ {
		prov := &multiTurnRecordingProvider{}
		emitThenFail := NewStageFunc("stt", StageTypeTransform,
			func(_ context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
				defer close(out)
				for range in { //nolint:revive // drain
				}
				out <- userElement("transcribed")
				return errStageBoom
			})
		p, err := NewPipelineBuilder().Chain(
			emitThenFail,
			NewProviderStageWithTurnState(prov, nil, nil, &ProviderConfig{Streaming: true}, nil, nil, NewTurnState()),
		).Build()
		require.NoError(t, err)

		_, execErr := p.ExecuteSync(context.Background(), userElement("audio"))

		require.ErrorIs(t, execErr, errStageBoom, "run %d", i)
		require.Empty(t, prov.requests, "run %d: no model call after the upstream failed", i)
	}
}

// finishReasonProvider is a non-streaming fake that returns a fixed canonical
// finish reason, used to verify the provider stage copies it onto the message.
type finishReasonProvider struct {
	delayedStreamProvider
}

func TestProviderStage_CopiesFinishReasonToMessage(t *testing.T) {
	provider := &finishReasonProvider{}

	turnState := NewTurnState()
	turnState.SystemPrompt = "helper"
	providerStage := NewProviderStageWithTurnState(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, nil, turnState)

	pl, err := NewPipelineBuilderWithConfig(DefaultPipelineConfig()).
		Chain(providerStage).
		Build()
	require.NoError(t, err)

	userMsg := types.Message{Role: "user", Content: "hi"}
	result, err := pl.ExecuteSync(context.Background(), NewMessageElement(&userMsg))
	require.NoError(t, err)

	var assistant *types.Message
	for i := range result.Messages {
		if result.Messages[i].Role == "assistant" {
			assistant = &result.Messages[i]
		}
	}
	require.NotNil(t, assistant, "expected an assistant message")
	assert.Equal(t, types.FinishReasonMaxOutputTokens, assistant.FinishReason)
}

// delayedStreamProvider simulates a provider that waits before producing its
// first chunk — like Ollama queued behind other requests.
type delayedStreamProvider struct {
	providers.Provider
	delay    time.Duration
	response string

	// gate, when set, replaces delay: the reply waits until gate closes.
	// waiting, when set, is closed once the call has started waiting.
	gate    chan struct{}
	waiting chan struct{}
}

func TestIdleTimeout_SlowProviderExceedsTimeout(t *testing.T) {
	// The provider never answers. Once the fake idle clock passes the 200ms
	// window the pipeline must end the turn with ErrIdleTimeout, with no reply:
	// the provider was cancelled before producing one.
	const idle = 200 * time.Millisecond
	provider := &delayedStreamProvider{
		response: "late", gate: make(chan struct{}), waiting: make(chan struct{}),
	}

	turnState := NewTurnState()
	turnState.SystemPrompt = "You are a helper"
	providerStage := NewProviderStageWithTurnState(provider, nil, nil, &ProviderConfig{
		MaxTokens:   100,
		Temperature: 0.7,
	}, nil, nil, turnState)

	config := DefaultPipelineConfig()
	config.IdleTimeout = idle
	config.ExecutionTimeout = 0

	pl, err := NewPipelineBuilderWithConfig(config).
		Chain(providerStage).
		Build()
	require.NoError(t, err)
	clock := &stagetest.FakeIdleTimer{}
	pl.idleAfterFunc = clock.AfterFunc

	userMsg := types.Message{Role: "user", Content: "hello"}
	elem := NewMessageElement(&userMsg)

	type outcome struct {
		result *ExecutionResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, execErr := pl.ExecuteSync(context.Background(), elem)
		done <- outcome{result, execErr}
	}()

	<-provider.waiting
	clock.Advance(idle)
	var got outcome
	select {
	case got = <-done:
	case <-time.After(idleProbeBound):
		t.Fatal("the idle timeout did not end the turn")
	}

	assert.ErrorIs(t, got.err, ErrIdleTimeout)
	if got.result.Response != nil {
		assert.Empty(t, got.result.Response.Content,
			"response content should be empty when provider is cancelled by idle timeout")
	}
}

func TestIdleTimeout_SlowProviderWithinTimeout(t *testing.T) {
	// The provider holds its reply while the idle clock moves on by 150ms of a
	// 200ms window, then answers. The pipeline must succeed: the window never
	// ran out. The test moves a fake clock, so runner speed does not matter.
	const idle = 200 * time.Millisecond
	provider := &delayedStreamProvider{
		response: "hello", gate: make(chan struct{}), waiting: make(chan struct{}),
	}

	turnState := NewTurnState()
	turnState.SystemPrompt = "You are a helper"
	providerStage := NewProviderStageWithTurnState(provider, nil, nil, &ProviderConfig{
		MaxTokens:   100,
		Temperature: 0.7,
	}, nil, nil, turnState)

	config := DefaultPipelineConfig()
	config.IdleTimeout = idle
	config.ExecutionTimeout = 0

	pl, err := NewPipelineBuilderWithConfig(config).
		Chain(providerStage).
		Build()
	require.NoError(t, err)
	clock := &stagetest.FakeIdleTimer{}
	pl.idleAfterFunc = clock.AfterFunc

	userMsg := types.Message{Role: "user", Content: "hello"}
	elem := NewMessageElement(&userMsg)

	type outcome struct {
		result *ExecutionResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, execErr := pl.ExecuteSync(context.Background(), elem)
		done <- outcome{result, execErr}
	}()

	<-provider.waiting
	clock.Advance(idle * 3 / 4)
	close(provider.gate)
	got := <-done

	require.NoError(t, got.err)
	require.NotNil(t, got.result.Response)
	assert.Equal(t, "hello", got.result.Response.Content)
}

// reasoningEndToEndProvider returns a response carrying reasoning, and records the
// messages it was asked to predict on (to verify reasoning never re-enters context).
type reasoningEndToEndProvider struct {
	delayedStreamProvider
	gotMessages []types.Message
}

// TestProviderStage_ReasoningEndToEnd proves the wired seam: a provider's reasoning
// flows through the real pipeline onto Message.Reasoning, is excluded from the
// message's content projection, and is NOT echoed back into the provider request.
func TestProviderStage_ReasoningEndToEnd(t *testing.T) {
	provider := &reasoningEndToEndProvider{}
	turnState := NewTurnState()
	turnState.SystemPrompt = "helper"
	providerStage := NewProviderStageWithTurnState(provider, nil, nil, &ProviderConfig{MaxTokens: 100}, nil, nil, turnState)

	pl, err := NewPipelineBuilderWithConfig(DefaultPipelineConfig()).Chain(providerStage).Build()
	require.NoError(t, err)

	userMsg := types.Message{Role: "user", Content: "hi"}
	result, err := pl.ExecuteSync(context.Background(), NewMessageElement(&userMsg))
	require.NoError(t, err)

	var assistant *types.Message
	for i := range result.Messages {
		if result.Messages[i].Role == "assistant" {
			assistant = &result.Messages[i]
		}
	}
	require.NotNil(t, assistant, "expected an assistant message")
	require.NotNil(t, assistant.Reasoning, "reasoning must flow end-to-end onto Message.Reasoning")
	assert.Equal(t, "internal reasoning", assistant.Reasoning.Text)
	require.Len(t, assistant.Reasoning.Opaque, 1)
	assert.Equal(t, "the answer", assistant.GetContent(), "reasoning must be excluded from content")

	// The provider's request must not contain the reasoning text in any message.
	for _, m := range provider.gotMessages {
		assert.NotContains(t, m.GetContent(), "internal reasoning", "reasoning must not re-enter context")
		assert.Nil(t, m.Reasoning, "history messages into the provider carry no reasoning")
	}
}

// loopingToolProvider calls "echo" with fresh arguments on every round it is
// offered tools, so only a round limit ends its loop.
type loopingToolProvider struct {
	*mock.ToolProvider
	rounds atomic.Int64
}

// An agent step's termination.max_steps and its prompt's tool_policy.max_rounds
// bound the same loop, so the step runs to the lower of the two — in either
// direction. See pipeline.MergeToolPolicy.
func TestCompositionExecutor_AgentStepRunsToLowerOfMaxStepsAndMaxRounds(t *testing.T) {
	tests := []struct {
		name                string
		maxSteps, maxRounds int
		wantRounds          int64
	}{
		{"prompt max_rounds lower", 10, 4, 4},
		{"step max_steps lower", 3, 10, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := prompt.NewRegistryWithRepository(newMockRepo())
			require.NoError(t, reg.RegisterConfig("a", &prompt.Config{Spec: prompt.Spec{
				TaskType:       "a",
				SystemTemplate: "You are a helpful assistant.",
				AllowedTools:   []string{"echo"},
				ToolPolicy:     &prompt.ToolPolicyPack{MaxRounds: packspec.Ptr(tt.maxRounds)},
			}}))
			toolReg := tools.NewRegistry()
			registerEchoTool(t, toolReg, "echo")
			prov := &loopingToolProvider{ToolProvider: mock.NewToolProvider("loop", "m", false, nil)}

			exec := NewCompositionStepExecutor(CompositionExecutorDeps{
				PromptRegistry: reg, Provider: prov, ToolRegistry: toolReg,
			})
			_, err := exec(context.Background(), &composition.Step{
				ID: "s", Kind: composition.KindAgent, PromptTask: "a", Tools: []string{"echo"},
				Termination: &composition.Termination{MaxSteps: packspec.Ptr(tt.maxSteps)},
			}, json.RawMessage(`"go"`))
			require.Error(t, err)
			assert.Contains(t, err.Error(), fmt.Sprintf("max rounds (%d) exceeded", tt.wantRounds))
			assert.Equal(t, tt.wantRounds, prov.rounds.Load())
		})
	}
}

// liveTextOf is liveText counting the text an element that also completes the
// turn carries.
func liveTextOf(elems []StreamElement) []string {
	var out []string
	for _, e := range elems {
		if e.Text == nil || (e.Message != nil && e.Message.Role != core.RoleAssistant) {
			continue
		}
		if e.Meta.ReplacesText {
			out = append(out, "replace:"+*e.Text)
		} else {
			out = append(out, *e.Text)
		}
	}
	return out
}

// afterCallTexts is an output hook recording the text of each completed reply
// it judges.
type afterCallTexts struct{ seen []string }

// TestProviderStage_ReceivesTranscriptText covers a transcript being silently
// dropped between STT and the model.
//
// STTStage's entire output for a completed turn is StreamElement{Text: &text}.
// The shipped VAD topology wires it straight into ProviderStage — see
// sdk/internal/pipeline/builder_vad.go, "Audio -> VAD -> STT -> LLM -> TTS",
// with no stage in between. But accumulateInput collects only elem.Message and
// discards everything else, so the model is asked to answer an empty
// conversation. Nothing errors; the turn is simply empty.
func TestProviderStage_ReceivesTranscriptText(t *testing.T) {
	const transcript = "I'd like a home insurance quote"

	// Chain the two stages exactly as the shipped VAD pipeline does, rather than
	// feeding ProviderStage by hand: the contract under test is that a
	// transcribed turn reaches the model, and either stage may be the one to
	// close the gap.
	sttStage := NewSTTStage(&transcriptSTT{text: transcript}, DefaultSTTStageConfig())

	provider := newCapturingProvider()
	turnState := NewTurnState()
	turnState.SystemPrompt = "You are a helpful assistant"
	providerStage := NewProviderStageWithTurnState(provider, nil, nil, &ProviderConfig{
		MaxTokens: 100,
	}, nil, nil, turnState)

	ctx := context.Background()

	sttIn := make(chan StreamElement, 1)
	sttIn <- StreamElement{Audio: &AudioData{
		Samples:    make([]byte, 32000), // 1s @ 16kHz PCM16, over MinAudioBytes
		SampleRate: 16000,
		Channels:   1,
		Format:     AudioFormatPCM16,
	}}
	close(sttIn)

	sttOut := make(chan StreamElement, 8)
	require.NoError(t, sttStage.Process(ctx, sttIn, sttOut))

	provOut := make(chan StreamElement, 32)
	require.NoError(t, providerStage.Process(ctx, sttOut, provOut))
	for range provOut { //nolint:revive // drain
	}

	assert.True(t, provider.sawText(transcript),
		"the model never received the transcript. STTStage emits it on elem.Text, "+
			"and accumulateInput collects only elem.Message, so a VAD-mode turn asks "+
			"the model to answer an empty conversation")
}

// transcriptSTT is a base.STTProvider returning a fixed transcript.
type transcriptSTT struct{ text string }

func (e *countingEcho) Execute(_ context.Context, _ *tools.ToolDescriptor, args json.RawMessage) (json.RawMessage, error) {
	e.runs.Add(1)
	return args, nil
}

func (s *passthroughRecorderStage) Name() string { return "recorder" }

// Type is required by the Stage interface (stage.go:49-56) alongside Name and
// Process. Transform is the 1:1 forwarding shape.
func (s *passthroughRecorderStage) Type() core.StageType { return core.StageTypeTransform }

func (s *passthroughRecorderStage) Process(
	ctx context.Context, input <-chan core.StreamElement, output chan<- core.StreamElement,
) error {
	defer close(output)
	for elem := range input {
		if elem.Message != nil {
			s.mu.Lock()
			s.seen = append(s.seen, *elem.Message)
			s.mu.Unlock()
		}
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (p *finishReasonProvider) SupportsStreaming() bool { return false }

func (p *finishReasonProvider) Predict(
	_ context.Context, _ providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	return providers.PredictionResponse{
		Content:      "done",
		FinishReason: types.FinishReasonMaxOutputTokens,
	}, nil
}

// wait returns a channel that is ready when the provider should reply.
func (p *delayedStreamProvider) wait() <-chan struct{} {
	if p.waiting != nil {
		close(p.waiting)
	}
	if p.gate != nil {
		return p.gate
	}
	ready := make(chan struct{})
	time.AfterFunc(p.delay, func() { close(ready) })
	return ready
}

func (p *delayedStreamProvider) ID() string { return "delayed" }

func (p *delayedStreamProvider) Model() string { return "delayed-model" }

func (p *delayedStreamProvider) SupportsStreaming() bool { return true }

func (p *delayedStreamProvider) MaxContextTokens() int { return 4096 }

func (p *delayedStreamProvider) SupportsToolUse() bool { return false }

func (p *delayedStreamProvider) PredictStream(
	ctx context.Context, _ providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	ch := make(chan providers.StreamChunk, 1)
	go func() {
		defer close(ch)
		select {
		case <-p.wait():
			finish := "stop"
			ch <- providers.StreamChunk{
				Content:      p.response,
				Delta:        p.response,
				FinishReason: &finish,
			}
		case <-ctx.Done():
			return
		}
	}()
	return ch, nil
}

func (p *delayedStreamProvider) Predict(
	ctx context.Context, _ providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	select {
	case <-p.wait():
		return providers.PredictionResponse{Content: p.response}, nil
	case <-ctx.Done():
		return providers.PredictionResponse{}, ctx.Err()
	}
}

func (p *reasoningEndToEndProvider) SupportsStreaming() bool { return false }

func (p *reasoningEndToEndProvider) Predict(
	_ context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	p.gotMessages = req.Messages
	return providers.PredictionResponse{
		Content: "the answer",
		Reasoning: &types.ReasoningTrace{
			Text:   "internal reasoning",
			Opaque: []types.OpaqueReasoning{{Provider: "test", Kind: "thinking_signature", Data: "sig"}},
		},
	}, nil
}

func (p *loopingToolProvider) SupportsStreaming() bool { return false }

func (p *loopingToolProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, offered providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	if offered == nil {
		return providers.PredictionResponse{Content: "done"}, nil, nil
	}
	n := p.rounds.Add(1)
	calls := []types.MessageToolCall{{
		ID: fmt.Sprintf("c%d", n), Name: "echo", Args: json.RawMessage(fmt.Sprintf(`{"n":%d}`, n)),
	}}
	return providers.PredictionResponse{ToolCalls: calls}, calls, nil
}

func (*afterCallTexts) Name() string { return "after_call_texts" }

func (*afterCallTexts) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (h *afterCallTexts) AfterCall(_ context.Context, _ *hooks.ProviderRequest, r *hooks.ProviderResponse) hooks.Decision {
	h.seen = append(h.seen, r.Message.Content)
	return hooks.Allow
}

func (s *transcriptSTT) Name() string { return "transcript" }

func (s *transcriptSTT) Type() base.ProviderType { return base.ProviderTypeSTT }

func (s *transcriptSTT) Pricing() *base.PricingDescriptor { return nil }

func (s *transcriptSTT) Validate() error { return nil }

func (s *transcriptSTT) Init(context.Context) error { return nil }

func (s *transcriptSTT) HealthCheck(context.Context) error { return nil }

func (s *transcriptSTT) Close() error { return nil }

func (s *transcriptSTT) Transcribe(context.Context, base.STTRequest) (base.STTResponse, error) {
	return base.STTResponse{Text: s.text}, nil
}
