package provider

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/segment"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// pathsDeadline bounds every wait in this file.
const pathsDeadline = 3 * time.Second

func blockedFiring() BlockedFiring {
	return BlockedFiring{
		Decision:  &hooks.Decision{Reason: "banned", Metadata: map[string]any{"validator_type": "banned_words"}},
		Direction: "output", Text: "guardrail text", Turn: 2,
	}
}

func TestChooseBlockedText_PanickingFunctionKeepsTheGuardrailText(t *testing.T) {
	panics := func(context.Context, BlockedMessage) string { panic("host bug") }
	for _, timeout := range []time.Duration{0, time.Second} {
		f := blockedFiring()
		assert.Equal(t, "guardrail text", ChooseBlockedText(context.Background(), panics, timeout, f), "timeout %v", timeout)
		assert.NotContains(t, f.Decision.Metadata, BlockedMessageDetail, "timeout %v", timeout)
	}
}

func TestChooseBlockedText_FunctionPastTheTimeoutKeepsTheGuardrailText(t *testing.T) {
	sawCancel := make(chan bool, 1)
	slow := func(ctx context.Context, _ BlockedMessage) string {
		select {
		case <-ctx.Done():
			sawCancel <- true
		case <-time.After(pathsDeadline):
			sawCancel <- false
		}
		return "too late"
	}
	f := blockedFiring()
	assert.Equal(t, "guardrail text", ChooseBlockedText(context.Background(), slow, 5*time.Millisecond, f))
	assert.NotContains(t, f.Decision.Metadata, BlockedMessageDetail)
	select {
	case canceled := <-sawCancel:
		assert.True(t, canceled, "the function's context is canceled at the timeout")
	case <-time.After(pathsDeadline):
		t.Fatal("the function never returned")
	}
}

func TestChooseBlockedText_FunctionWithinTheTimeoutChoosesTheText(t *testing.T) {
	var got BlockedMessage
	fn := func(_ context.Context, b BlockedMessage) string { got = b; return "host text" }
	f := blockedFiring()
	assert.Equal(t, "host text", ChooseBlockedText(context.Background(), fn, time.Second, f))
	assert.Equal(t, "host text", f.Decision.Metadata[BlockedMessageDetail])
	assert.Equal(t, BlockedMessage{
		Direction: "output", Reason: "banned", Text: "guardrail text", TurnIndex: 2, ValidatorType: "banned_words",
	}, got)
}

func TestGuardrailOutcome_RecordWithoutValidationsStillEmits(t *testing.T) {
	bus := events.NewEventBus()
	got := make(chan *events.Event, 1)
	bus.Subscribe(events.EventValidationFailed, func(e *events.Event) { got <- e })
	g := GuardrailOutcome{Emitter: events.NewEmitter(bus, "run", "sess", "conv"), Turn: 3}
	g.Record(nil, hooks.Decision{Metadata: map[string]any{"validator_type": "length"}}, "output", time.Millisecond)
	select {
	case e := <-got:
		data, ok := e.Data.(*events.ValidationEventData)
		require.True(t, ok)
		assert.Equal(t, "length", data.ValidatorType)
		assert.Equal(t, "output", data.Direction)
		assert.Equal(t, 3, data.TurnIndex)
	case <-time.After(pathsDeadline):
		t.Fatal("no validation.failed event")
	}
}

func TestProviderConfig_ApplyCallParamsSetsEverySamplingField(t *testing.T) {
	freq, pres, topK := float32(0.1), float32(0.2), 40
	c := &ProviderConfig{}
	c.ApplyCallParams(prompt.CallParams{
		MaxTokens: 512, Temperature: 0.3, TemperatureSet: true, TopP: 0.9,
		FrequencyPenalty: &freq, PresencePenalty: &pres, TopK: &topK,
	})
	assert.Equal(t, 512, c.MaxTokens)
	assert.Equal(t, float32(0.3), c.Temperature)
	assert.True(t, c.TemperatureSet)
	assert.Equal(t, float32(0.9), c.TopP)
	assert.Same(t, &freq, c.FrequencyPenalty)
	assert.Same(t, &pres, c.PresencePenalty)
	assert.Same(t, &topK, c.TopK)
}

func TestProviderStage_RunsToolCallsOnlyWithARegistry(t *testing.T) {
	prov := mock.NewProvider("p", "m", false)
	assert.True(t, NewProviderStage(prov, tools.NewRegistry(), nil, nil).RunsToolCalls())
	assert.False(t, NewProviderStage(prov, nil, nil, nil).RunsToolCalls())
}

// A continuous-mode stage passes an element that is neither turn input nor a
// control element through unchanged.
func TestProviderStage_StreamingPassesStrayContentThrough(t *testing.T) {
	stage := NewProviderStage(mock.NewProvider("p", "m", false), nil, nil, &ProviderConfig{Streaming: true})
	in := make(chan core.StreamElement, 1)
	out := make(chan core.StreamElement, 4)
	stray := core.StreamElement{Audio: &core.AudioData{Samples: []byte{1, 2}, SampleRate: 16000, Channels: 1}}
	in <- stray
	close(in)
	require.NoError(t, stage.Process(context.Background(), in, out))
	var got []core.StreamElement
	for e := range out {
		got = append(got, e)
	}
	require.Len(t, got, 1)
	assert.Equal(t, []byte{1, 2}, got[0].Audio.Samples)
	assert.Nil(t, got[0].Message)
}

// heldGate holds every call of held pending and lets the others run.
func heldGate(held string) tools.ApprovalGate {
	return func(_ context.Context, _, name string, _ map[string]any) (*tools.PendingToolInfo, error) {
		if name != held {
			return nil, nil
		}
		return &tools.PendingToolInfo{Reason: "requires_approval"}, nil
	}
}

// A turn whose call is held pending ends with the turn's messages and then an
// element carrying the held call; the held call's tool name is filled in.
func TestProviderStage_HeldCallEndsTheTurnWithItsPendingElement(t *testing.T) {
	reg := registryWithTools(t, "probe")
	reg.RegisterExecutor(staticExecutor{})
	ts := core.NewTurnState()
	ts.AllowedTools = []string{"probe"}
	stage := NewProviderStageWithTurnState(&scriptedRoundProvider{toolRounds: 1}, reg, nil,
		&ProviderConfig{ApprovalGate: heldGate("probe")}, nil, nil, ts)

	in := make(chan core.StreamElement, 1)
	in <- core.NewMessageElement(&types.Message{Role: "user", Content: "go"})
	close(in)
	out := make(chan core.StreamElement, 16)
	require.NoError(t, stage.Process(context.Background(), in, out))

	var pending []tools.PendingToolExecution
	var roles []string
	for e := range out {
		require.Nil(t, e.Error)
		if e.Message != nil {
			roles = append(roles, e.Message.Role)
		}
		if len(e.Meta.PendingTools) > 0 {
			pending = e.Meta.PendingTools
		}
	}
	assert.Equal(t, []string{"user", "assistant"}, roles)
	require.Len(t, pending, 1)
	assert.Equal(t, "call_A", pending[0].CallID)
	assert.Equal(t, "probe", pending[0].ToolName)
	assert.Equal(t, "probe", pending[0].PendingInfo.ToolName)
}

// pastPendingTurn is a turn on a runner with probe (held) and echo (runs).
func pastPendingTurn(t *testing.T, maxRounds int) *ToolTurn {
	t.Helper()
	reg := registryWithTools(t, "probe", "echo")
	reg.RegisterExecutor(staticExecutor{})
	r := &ToolRunner{
		Registry: reg,
		Policy:   func() *pipeline.ToolPolicy { return nil },
		Config:   ToolRunnerConfig{ApprovalGate: heldGate("probe")},
	}
	return r.NewTurn(nil, maxRounds)
}

func TestToolTurn_RunRoundPastPendingRunsTheCallsNotHeld(t *testing.T) {
	out := pastPendingTurn(t, 1).RunRoundPastPending(context.Background(), []types.MessageToolCall{
		{ID: "h", Name: "probe", Args: json.RawMessage(`{}`)},
		{ID: "r", Name: "echo", Args: json.RawMessage(`{}`)},
	}, RoundRef{Round: 1})
	require.Len(t, out.Pending, 1)
	assert.Equal(t, "h", out.Pending[0].CallID)
	require.Len(t, out.Results, 1)
	assert.Equal(t, "r", out.Results[0].ToolResult.ID)
	// echo ran in the last round: the turn stops at its limit.
	assert.True(t, out.Stop)
	var maxRounds *core.MaxRoundsError
	require.ErrorAs(t, out.Err, &maxRounds)
	assert.Equal(t, 1, maxRounds.Rounds)
}

func TestToolTurn_RunRoundPastPendingWithEveryCallHeldDoesNotStop(t *testing.T) {
	out := pastPendingTurn(t, 1).RunRoundPastPending(context.Background(), []types.MessageToolCall{
		{ID: "h", Name: "probe", Args: json.RawMessage(`{}`)},
	}, RoundRef{Round: 1})
	require.Len(t, out.Pending, 1)
	assert.Empty(t, out.Results)
	assert.False(t, out.Stop, "nothing ran, so no limit is reached")
	assert.NoError(t, out.Err)
}

func TestCallsRun_LeavesOutTheHeldCalls(t *testing.T) {
	calls := []types.MessageToolCall{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	assert.Equal(t, calls, callsRun(calls, nil))
	assert.Equal(t, []types.MessageToolCall{{ID: "a"}, {ID: "c"}},
		callsRun(calls, []tools.PendingToolExecution{{CallID: "b"}}))
}

// A runner built without offer state (as the realtime stage builds one) keeps
// no offered set and treats every tools array as changed; its views are where
// DescribeTool's outcomes go.
func TestToolRunner_WithoutOfferState(t *testing.T) {
	r := &ToolRunner{Registry: tools.NewRegistry()}
	r.ResetOffered()
	r.RecordOffered("a")
	assert.Nil(t, r.OfferedToolNames())
	d := []*tools.ToolDescriptor{{Name: "a"}}
	r.MarkBuilt(d)
	assert.True(t, r.DescribedChanged(d))
	assert.Nil(t, r.viewState())

	views := &ToolOffer{}
	r.Views = views
	assert.Same(t, views, r.viewState())
}

func TestToolLoop_ApplyRoundEndsOnAnExecutionError(t *testing.T) {
	stage := NewProviderStage(mock.NewProvider("p", "m", false), tools.NewRegistry(), nil, nil)
	tl, err := stage.newToolLoop(&providerInput{messages: []types.Message{{Role: "user", Content: "go"}}})
	require.NoError(t, err)
	boom := errors.New("boom")
	done, msgs, gotErr := tl.applyRound(context.Background(), nil, RoundOutcome{
		Results: []types.Message{{Role: "tool"}}, Err: boom,
	}, 1)
	assert.True(t, done)
	require.ErrorIs(t, gotErr, boom)
	assert.Equal(t, []types.Message{{Role: "user", Content: "go"}}, msgs, "a failed round's results are not appended")
}

// failingRebuildProvider builds tools once and fails every rebuild after.
type failingRebuildProvider struct {
	*mock.ToolProvider
	builds int
}

func (p *failingRebuildProvider) BuildTooling(d []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	p.builds++
	if p.builds > 1 {
		return nil, errors.New("no tooling")
	}
	return p.ToolProvider.BuildTooling(d)
}

func TestToolLoop_ApplyRoundEndsWhenTheToolsCannotBeRebuilt(t *testing.T) {
	reg := registryWithTools(t, "probe")
	prov := &failingRebuildProvider{ToolProvider: mock.NewToolProvider("p", "m", false, nil)}
	ts := core.NewTurnState()
	ts.AllowedTools = []string{"probe"}
	stage := NewProviderStageWithTurnState(prov, reg, nil, nil, nil, nil, ts)
	tl, err := stage.newToolLoop(&providerInput{
		messages: []types.Message{{Role: "user", Content: "go"}}, allowedTools: []string{"probe"},
	})
	require.NoError(t, err)
	done, _, gotErr := tl.applyRound(context.Background(), []string{"probe"}, RoundOutcome{OfferChanged: true}, 1)
	assert.True(t, done)
	require.Error(t, gotErr)
	assert.Equal(t, "provider stage: rebuild tools: failed to build tools: no tooling", gotErr.Error())
}

func TestStreamMediaToElement_RoutesByMIMEType(t *testing.T) {
	video := StreamMediaToElement(&providers.StreamMediaData{
		MIMEType: "video/mp4", Data: []byte{1}, Width: 64, Height: 48, FrameRate: 30, IsKeyFrame: true,
	})
	require.NotNil(t, video.Video)
	assert.Equal(t, core.VideoData{
		Data: []byte{1}, MIMEType: "video/mp4", Width: 64, Height: 48, FrameRate: 30, IsKeyFrame: true,
	}, stripVideoTime(*video.Video))
	assert.Nil(t, video.Audio)

	image := StreamMediaToElement(&providers.StreamMediaData{MIMEType: "image/png", Data: []byte{2}, Width: 8, Height: 4})
	require.NotNil(t, image.Image)
	assert.Equal(t, []byte{2}, image.Image.Data)
	assert.Equal(t, "image/png", image.Image.MIMEType)
	assert.Equal(t, 8, image.Image.Width)
	assert.Equal(t, 4, image.Image.Height)

	other := StreamMediaToElement(&providers.StreamMediaData{MIMEType: "application/pdf", Data: []byte{3}})
	assert.Nil(t, other.Audio)
	assert.Nil(t, other.Video)
	assert.Nil(t, other.Image)
}

// stripVideoTime zeroes the fields StreamMediaToElement does not set from the
// media, so the rest compares exactly.
func stripVideoTime(v core.VideoData) core.VideoData {
	v.Timestamp = time.Time{}
	v.Duration = 0
	return v
}

// partialJudge reports its judgment's start and then returns allow.
type partialJudge struct{ judged chan struct{} }

func (partialJudge) Name() string { return "judge" }
func (partialJudge) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}
func (partialJudge) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}
func (partialJudge) StreamHold() hooks.StreamHoldMode { return hooks.StreamHoldSegment }
func (j partialJudge) JudgePartialReply(context.Context, *hooks.ProviderRequest, hooks.PartialReply) hooks.Decision {
	close(j.judged)
	return hooks.Allow
}

// askedContext is a canceled context that reports the first call of Done.
type askedContext struct {
	context.Context
	once  sync.Once
	asked chan struct{}
}

func (c *askedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.asked) })
	return c.Context.Done()
}

// A judgment that finishes after its round ended delivers no verdict: with
// nobody reading the results, it gives up on the round's context.
func TestStreamHold_JudgmentAfterTheRoundEndsDeliversNothing(t *testing.T) {
	judge := partialJudge{judged: make(chan struct{})}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	ctx := &askedContext{Context: canceled, asked: make(chan struct{})}
	results := make(chan PartialVerdict)
	h := &StreamHold{
		cfg: StreamHoldConfig{MaxSegmentJudges: 3}, Ctx: ctx, Results: results,
		seg: segment.New(segment.Config{MinChars: 1, FirstMinChars: 1}),
	}
	g := &HeldGuardrail{hook: judge, segment: true}
	h.seg.Feed("One sentence. Two.")
	require.NotEmpty(t, h.seg.Segments())
	h.Judge(g)
	for _, ch := range []chan struct{}{judge.judged, ctx.asked} {
		select {
		case <-ch:
		case <-time.After(pathsDeadline):
			t.Fatal("the judgment never finished")
		}
	}
	assert.True(t, g.Busy)
	assert.Equal(t, 1, g.calls)
}

// A held reply a chunk check truncates reaches the caller as the truncated
// reply, ending safety, both when the hold has released none of it and when
// the cut falls inside a segment it was still holding.
func TestStreamHold_TruncatedHeldReplyReleasesTheTruncation(t *testing.T) {
	for _, tc := range []struct {
		deltas []string
		want   string
	}{
		{[]string{"Sunny now. ", "Rain later today."}, "Sunny now."},
		{[]string{"Sun", "ny now and rain"}, "Sunny now "},
	} {
		limit, err := guardrails.NewGuardrailHook("length", map[string]any{"max_characters": 10})
		require.NoError(t, err)
		hold := &holdingHook{mode: hooks.StreamHoldSegment, decide: failOn("never")}
		stream := make(chan providers.StreamChunk, 8)
		sendScriptedDeltas(stream, tc.deltas...)
		stop := types.FinishReasonStop
		stream <- providers.StreamChunk{Content: strings.Join(tc.deltas, ""), FinishReason: &stop}
		close(stream)
		prov := &scriptedChannelProvider{Provider: mock.NewProvider("p", "m", false), stream: stream}
		ps := NewProviderStageWithHooks(prov, nil, nil, &ProviderConfig{MaxTokens: 100, StreamHold: smallHold}, nil,
			hooks.NewRegistry(hooks.WithProviderHook(hold), hooks.WithProviderHook(limit)))

		elems, err := runHeld(t, ps, "weather?")
		require.NoError(t, err)
		texts, finishes := textDeltas(elems)
		assert.Equal(t, []string{tc.want}, texts, "%q", tc.deltas)
		assert.Equal(t, []string{types.FinishReasonSafety}, finishes, "%q", tc.deltas)
		msg := lastAssistantOf(t, elems)
		assert.Equal(t, tc.want, msg.Content)
		assert.Equal(t, types.FinishReasonSafety, msg.FinishReason)
	}
}
