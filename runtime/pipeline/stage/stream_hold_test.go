package stage

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const holdBlocked = "Weather only."

// holdingHook is a stream_hold guardrail whose verdict on a reply, partial or
// complete, is decide's. It records the partial replies it judged.
type holdingHook struct {
	mode   hooks.StreamHoldMode
	decide func(ctx context.Context, text string) hooks.Decision

	mu      sync.Mutex
	partial []string
}

func (h *holdingHook) Name() string                     { return "holding" }
func (h *holdingHook) StreamHold() hooks.StreamHoldMode { return h.mode }
func (h *holdingHook) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (h *holdingHook) AfterCall(ctx context.Context, _ *hooks.ProviderRequest, resp *hooks.ProviderResponse) hooks.Decision {
	return h.decide(ctx, resp.Message.Content)
}

func (h *holdingHook) JudgePartialReply(
	ctx context.Context, _ *hooks.ProviderRequest, reply hooks.PartialReply,
) hooks.Decision {
	h.mu.Lock()
	h.partial = append(h.partial, reply.Text)
	h.mu.Unlock()
	return h.decide(ctx, reply.Text)
}

// failOn blocks a reply containing word, with holdBlocked.
func failOn(word string) func(context.Context, string) hooks.Decision {
	return func(_ context.Context, text string) hooks.Decision {
		if !strings.Contains(text, word) {
			return hooks.Allow
		}
		msg := holdBlocked
		d := hooks.Enforced("off topic", map[string]any{"validator_type": "holding"})
		d.Content = &msg
		return d
	}
}

// holdStage is a streaming provider stage reading deltas, guarded by hook.
func holdStage(t *testing.T, hook hooks.ProviderHook, cfg StreamHoldConfig, deltas ...string) *ProviderStage {
	t.Helper()
	stream := make(chan providers.StreamChunk, len(deltas)+1)
	sendScriptedDeltas(stream, deltas...)
	stop := types.FinishReasonStop
	stream <- providers.StreamChunk{Content: strings.Join(deltas, ""), FinishReason: &stop}
	close(stream)
	prov := &scriptedChannelProvider{Provider: mock.NewProvider("p", "m", false), stream: stream}
	return NewProviderStageWithHooks(prov, nil, nil, &ProviderConfig{MaxTokens: 100, StreamHold: cfg}, nil,
		hooks.NewRegistry(hooks.WithProviderHook(hook)))
}

var smallHold = StreamHoldConfig{MinChars: 1}

// A reply a segment fails ends with the guardrail's message, marked as
// replacing what was released; the stored reply is the message, its tool calls
// dropped, and the firing is recorded with the segment it fired on.
func TestStreamHold_SegmentFailureEndsWithTheMessage(t *testing.T) {
	hook := &holdingHook{mode: hooks.StreamHoldSegment, decide: failOn("stocks")}
	elems, err := runProviderStage(t, holdStage(t, hook, smallHold, "Sunny today. ", "Buy stocks now. ", "Really."),
		"weather?")
	require.NoError(t, err)

	assert.Equal(t, []string{"Sunny today.", "replace:" + holdBlocked}, liveText(elems))
	msg := lastAssistantOf(t, elems)
	assert.Equal(t, holdBlocked, msg.Content)
	assert.Equal(t, types.FinishReasonSafety, msg.FinishReason)
	require.Len(t, msg.Validations, 1)
	assert.Equal(t, "holding", msg.Validations[0].ValidatorType)
}

// A reply every segment passes reaches the caller whole, the last segment
// once the completed reply passed.
func TestStreamHold_SegmentPassReleasesTheReply(t *testing.T) {
	hook := &holdingHook{mode: hooks.StreamHoldSegment, decide: failOn("never")}
	elems, err := runProviderStage(t, holdStage(t, hook, smallHold, "Sunny today. ", "Rain later."), "weather?")
	require.NoError(t, err)

	assert.Equal(t, "Sunny today. Rain later.", strings.Join(liveText(elems), ""))
	assert.Equal(t, "Sunny today. Rain later.", lastAssistantOf(t, elems).Content)
	assert.Equal(t, []string{"Sunny today."}, hook.partial, "the last segment is judged with the reply")
}

// A turn guardrail releases nothing until the completed reply passes, then
// all of it at once; one it fails reaches the caller as the message only.
func TestStreamHold_TurnHoldsTheWholeReply(t *testing.T) {
	pass := &holdingHook{mode: hooks.StreamHoldTurn, decide: failOn("never")}
	elems, err := runProviderStage(t, holdStage(t, pass, smallHold, "Sunny today. ", "Rain later."), "weather?")
	require.NoError(t, err)
	assert.Equal(t, []string{"Sunny today. Rain later."}, liveText(elems))
	assert.Empty(t, pass.partial, "a turn guardrail judges no partial reply")

	fail := &holdingHook{mode: hooks.StreamHoldTurn, decide: failOn("Rain")}
	elems, err = runProviderStage(t, holdStage(t, fail, smallHold, "Sunny today. ", "Rain later."), "weather?")
	require.NoError(t, err)
	assert.Equal(t, []string{"replace:" + holdBlocked}, liveText(elems))
}

// A segment guardrail that denies without enforcing cuts the reply with the
// default blocked message.
func TestStreamHold_SegmentDenyCutsWithTheBlockedMessage(t *testing.T) {
	hook := &holdingHook{mode: hooks.StreamHoldSegment, decide: func(context.Context, string) hooks.Decision {
		return hooks.DenyWithMetadata("no", map[string]any{"validator_type": "holding"})
	}}
	elems, err := runProviderStage(t, holdStage(t, hook, smallHold, "Sunny today. ", "Rain."), "weather?")
	require.NoError(t, err)
	assert.Equal(t, []string{"replace:" + prompt.DefaultBlockedMessage}, liveText(elems))
}

// Past MaxSegmentJudges a guardrail judges no more partial replies that round.
func TestStreamHold_MaxSegmentJudges(t *testing.T) {
	hook := &holdingHook{mode: hooks.StreamHoldSegment, decide: failOn("never")}
	cfg := StreamHoldConfig{MinChars: 1, MaxSegmentJudges: 1}
	elems, err := runProviderStage(t, holdStage(t, hook, cfg, "One. ", "Two. ", "Three."), "weather?")
	require.NoError(t, err)
	assert.Equal(t, "One. Two. Three.", strings.Join(liveText(elems), ""))
	assert.Len(t, hook.partial, 1)
}

// A retry that restarts the reply abandons the judgments of the discarded
// attempt: only the retry's text is judged and released.
func TestStreamHold_RetryRestartsTheJudging(t *testing.T) {
	hook := &holdingHook{mode: hooks.StreamHoldSegment, decide: func(ctx context.Context, text string) hooks.Decision {
		if strings.Contains(text, "Draft") {
			<-ctx.Done() // the retry cancels this judgment
			return hooks.Deny("canceled")
		}
		return hooks.Allow
	}}
	stream := make(chan providers.StreamChunk, 8)
	sendScriptedDeltas(stream, "Draft one. ")
	stream <- providers.StreamChunk{Reset: true}
	sendScriptedDeltas(stream, "Sunny. ", "Warm.")
	stop := types.FinishReasonStop
	stream <- providers.StreamChunk{Content: "Sunny. Warm.", FinishReason: &stop}
	close(stream)
	prov := &scriptedChannelProvider{Provider: mock.NewProvider("p", "m", false), stream: stream}
	ps := NewProviderStageWithHooks(prov, nil, nil, &ProviderConfig{MaxTokens: 100, StreamHold: smallHold}, nil,
		hooks.NewRegistry(hooks.WithProviderHook(hook)))

	elems, err := runProviderStage(t, ps, "weather?")
	require.NoError(t, err)
	assert.Equal(t, "Sunny. Warm.", strings.Join(liveText(elems), ""))
	assert.Equal(t, "Sunny. Warm.", lastAssistantOf(t, elems).Content)
}

// A turn canceled while a judgment runs releases nothing it held.
func TestStreamHold_CancelReleasesNothingHeld(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 1)
	hook := &holdingHook{mode: hooks.StreamHoldSegment, decide: func(jctx context.Context, _ string) hooks.Decision {
		started <- struct{}{}
		<-jctx.Done()
		return hooks.Allow
	}}
	stream := make(chan providers.StreamChunk, 2)
	sendScriptedDeltas(stream, "Sunny today. ")
	prov := &scriptedChannelProvider{Provider: mock.NewProvider("p", "m", false), stream: stream}
	ps := NewProviderStageWithHooks(prov, nil, nil, &ProviderConfig{MaxTokens: 100, StreamHold: smallHold}, nil,
		hooks.NewRegistry(hooks.WithProviderHook(hook)))
	go func() {
		<-started
		cancel()
	}()

	elems, _ := runProviderStageWithCtx(t, ctx, ps, "weather?")
	assert.Empty(t, liveText(elems))
}

// Released text and a cut are reported.
func TestStreamHold_ReportsReleasesAndCuts(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Close()
	var mu sync.Mutex
	var got []string
	for _, et := range []events.EventType{events.EventStreamHoldReleased, events.EventStreamHoldCut} {
		bus.Subscribe(et, func(e *events.Event) {
			mu.Lock()
			defer mu.Unlock()
			switch d := e.Data.(type) {
			case *events.StreamHoldReleasedData:
				got = append(got, "released")
				assert.GreaterOrEqual(t, d.Held, time.Duration(0))
			case *events.StreamHoldCutData:
				got = append(got, "cut:"+d.Guardrail)
			}
		})
	}
	hook := &holdingHook{mode: hooks.StreamHoldTurn, decide: failOn("never")}
	ps := holdStage(t, hook, smallHold, "Sunny.")
	ps.emitter = events.NewEmitter(bus, "", "", "")
	_, err := runProviderStage(t, ps, "weather?")
	require.NoError(t, err)

	cutter := &holdingHook{mode: hooks.StreamHoldSegment, decide: failOn("Sunny")}
	ps = holdStage(t, cutter, smallHold, "Sunny today. ", "Rain.")
	ps.emitter = events.NewEmitter(bus, "", "", "")
	_, err = runProviderStage(t, ps, "weather?")
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 2
	}, time.Second, time.Millisecond)
	// The bus delivers asynchronously, so the two runs' events may arrive in
	// either order.
	assert.ElementsMatch(t, []string{"released", "cut:holding"}, got)
}

// Unset limits take the defaults; set ones stand.
func TestStreamHoldConfig_WithDefaults(t *testing.T) {
	assert.Equal(t, StreamHoldConfig{
		MinChars: 60, firstMinChars: 24, maxChars: 600, MaxSegmentJudges: 8, SegmentTimeout: 10 * time.Second,
	}, StreamHoldConfig{}.withDefaults())
	assert.Equal(t, StreamHoldConfig{
		MinChars: 10, firstMinChars: 10, maxChars: 600, MaxSegmentJudges: 2, SegmentTimeout: time.Second,
	}, StreamHoldConfig{MinChars: 10, MaxSegmentJudges: 2, SegmentTimeout: time.Second}.withDefaults())
	assert.Equal(t, segmentConfig{minChars: 100, firstMinChars: 24, maxChars: 1000},
		StreamHoldConfig{MinChars: 100}.segmentConfig(), "the cap grows with a large minimum")
}

// liveHoldRun is a stage reading a stream the test feeds chunk by chunk, its
// output read as it comes.
type liveHoldRun struct {
	stream chan providers.StreamChunk
	output chan StreamElement
	sofar  string
	done   chan error
}

func startHoldRun(t *testing.T, hook hooks.ProviderHook, cfg StreamHoldConfig) *liveHoldRun {
	t.Helper()
	r := &liveHoldRun{
		stream: make(chan providers.StreamChunk), output: make(chan StreamElement, 50), done: make(chan error, 1),
	}
	prov := &scriptedChannelProvider{Provider: mock.NewProvider("p", "m", false), stream: r.stream}
	ps := NewProviderStageWithHooks(prov, nil, nil, &ProviderConfig{MaxTokens: 100, StreamHold: cfg}, nil,
		hooks.NewRegistry(hooks.WithProviderHook(hook)))
	input := make(chan StreamElement, 1)
	msg := types.Message{Role: "user", Content: "weather?"}
	input <- NewMessageElement(&msg)
	close(input)
	go func() { r.done <- ps.Process(context.Background(), input, r.output) }()
	return r
}

func (r *liveHoldRun) send(delta string) {
	r.sofar += delta
	r.stream <- providers.StreamChunk{Delta: delta, Content: r.sofar}
}

func (r *liveHoldRun) end() {
	stop := types.FinishReasonStop
	r.stream <- providers.StreamChunk{Content: r.sofar, FinishReason: &stop}
	close(r.stream)
}

// nextText waits for the next text element the caller receives.
func (r *liveHoldRun) nextText(t *testing.T) StreamElement {
	t.Helper()
	for {
		select {
		case e := <-r.output:
			if e.Text != nil && e.Message == nil {
				return e
			}
		case <-time.After(10 * time.Second):
			t.Fatal("no text arrived")
		}
	}
}

func (r *liveHoldRun) rest(t *testing.T) []StreamElement {
	t.Helper()
	require.NoError(t, <-r.done)
	var out []StreamElement
	for e := range r.output {
		out = append(out, e)
	}
	return out
}

// Once a guardrail made its round's last partial-reply judgment, the segments
// that follow wait for the completed reply's check.
func TestStreamHold_MaxSegmentJudgesStopsJudging(t *testing.T) {
	hook := &holdingHook{mode: hooks.StreamHoldSegment, decide: failOn("never")}
	r := startHoldRun(t, hook, StreamHoldConfig{MinChars: 1, MaxSegmentJudges: 1})
	r.send("One. ")
	assert.Equal(t, "One.", *r.nextText(t).Text)
	r.send("Two. ")
	r.send("Three.")
	r.end()
	assert.Equal(t, []string{" Two. Three."}, liveText(r.rest(t)))
	assert.Equal(t, []string{"One."}, hook.partial)
}

// A guardrail with judgments left judges the segments that follow.
func TestStreamHold_JudgesEachSegmentWhileTheModelWrites(t *testing.T) {
	hook := &holdingHook{mode: hooks.StreamHoldSegment, decide: failOn("never")}
	r := startHoldRun(t, hook, StreamHoldConfig{MinChars: 1, MaxSegmentJudges: 2})
	r.send("One. ")
	assert.Equal(t, "One.", *r.nextText(t).Text)
	r.send("Two. ")
	assert.Equal(t, " Two.", *r.nextText(t).Text)
	r.send("Three.")
	r.end()
	last := r.rest(t)
	assert.Equal(t, []string{" Three."}, liveText(last))
	assert.Equal(t, []string{"One.", "One. Two."}, hook.partial)
}

// The first segment ends at the smaller first-segment minimum; later ones
// wait for the full minimum.
func TestStreamHold_FirstSegmentUsesTheSmallerMinimum(t *testing.T) {
	hook := &holdingHook{mode: hooks.StreamHoldSegment, decide: failOn("never")}
	r := startHoldRun(t, hook, StreamHoldConfig{MinChars: 40, firstMinChars: 5})
	r.send("Hi there. ")
	assert.Equal(t, "Hi there.", *r.nextText(t).Text)
	r.send("And more. ")
	r.end()
	assert.Equal(t, []string{" And more. "}, liveText(r.rest(t)))
	assert.Equal(t, []string{"Hi there."}, hook.partial)
}

// Media behind held text follows the text when it is released, and the
// released end of the reply carries its finish reason.
func TestStreamHold_ReleasedTextLeadsItsMediaAndEndsTheReply(t *testing.T) {
	hook := &holdingHook{mode: hooks.StreamHoldTurn, decide: failOn("never")}
	stream := make(chan providers.StreamChunk, 4)
	sendScriptedDeltas(stream, "Hi.")
	stream <- providers.StreamChunk{Content: "Hi.",
		MediaData: &providers.StreamMediaData{Data: []byte{1, 2}, MIMEType: "audio/pcm"}}
	stop := types.FinishReasonStop
	stream <- providers.StreamChunk{Content: "Hi.", FinishReason: &stop}
	close(stream)
	prov := &scriptedChannelProvider{Provider: mock.NewProvider("p", "m", false), stream: stream}
	ps := NewProviderStageWithHooks(prov, nil, nil, &ProviderConfig{MaxTokens: 100}, nil,
		hooks.NewRegistry(hooks.WithProviderHook(hook)))

	elems, err := runProviderStage(t, ps, "weather?")
	require.NoError(t, err)
	var order []string
	for _, e := range elems {
		switch {
		case e.Text != nil && e.Message == nil:
			require.NotNil(t, e.Meta.FinishReason)
			order = append(order, "text:"+*e.Text+":"+*e.Meta.FinishReason)
		case e.Audio != nil:
			order = append(order, "media")
		}
	}
	assert.Equal(t, []string{"text:Hi.:" + types.FinishReasonStop, "media"}, order)
}

// A cut cancels the other guardrails' judgments still running, and reports
// how many.
func TestStreamHold_CutCancelsTheOtherJudgments(t *testing.T) {
	bus := events.NewEventBus()
	defer bus.Close()
	cuts := make(chan *events.StreamHoldCutData, 1)
	bus.Subscribe(events.EventStreamHoldCut, func(e *events.Event) { cuts <- e.Data.(*events.StreamHoldCutData) })

	started := make(chan struct{})
	slow := &holdingHook{mode: hooks.StreamHoldSegment, decide: func(ctx context.Context, _ string) hooks.Decision {
		close(started)
		<-ctx.Done()
		return hooks.Allow
	}}
	fast := &holdingHook{mode: hooks.StreamHoldSegment, decide: func(ctx context.Context, text string) hooks.Decision {
		<-started
		return failOn("Sunny")(ctx, text)
	}}
	ps := holdStage(t, slow, smallHold, "Sunny today. ", "Rain.")
	ps.hookRegistry = hooks.NewRegistry(hooks.WithProviderHook(slow), hooks.WithProviderHook(fast))
	ps.emitter = events.NewEmitter(bus, "", "", "")

	elems, err := runProviderStage(t, ps, "weather?")
	require.NoError(t, err)
	assert.Equal(t, []string{"replace:" + holdBlocked}, liveText(elems))
	select {
	case cut := <-cuts:
		assert.Equal(t, 1, cut.CanceledJudges)
		assert.Equal(t, 1, cut.SegmentIndex)
		assert.Equal(t, 0, cut.ReleasedBytes)
	case <-time.After(10 * time.Second):
		t.Fatal("no cut reported")
	}
}

// A round whose text is suppressed, or with no stream_hold guardrail, reads
// its stream without a hold.
func TestStreamHold_NoHoldWithoutHoldersOrText(t *testing.T) {
	hook := &holdingHook{mode: hooks.StreamHoldTurn, decide: failOn("never")}
	ps := holdStage(t, hook, smallHold)
	var text holdController
	assert.Nil(t, ps.newStreamHold(context.Background(), &hooks.ProviderRequest{}, roundRef{}, &text, true))
	h := ps.newStreamHold(context.Background(), &hooks.ProviderRequest{}, roundRef{}, &text, false)
	require.NotNil(t, h)
	assert.Len(t, h.holders, 1)
	assert.False(t, h.judging, "a turn guardrail holds the round: no segment is judged")
	assert.Equal(t, []int{0}, text.held, "the guardrail has cleared none of the reply")

	plain := NewProviderStageWithHooks(mock.NewProvider("p", "m", false), nil, nil, &ProviderConfig{}, nil,
		hooks.NewRegistry(hooks.WithProviderHook(&enforceAfterCallHook{replacement: "x"})))
	assert.Nil(t, plain.newStreamHold(context.Background(), &hooks.ProviderRequest{}, roundRef{}, &text, false))
}

// A released byte was held from the arrival of the chunk that carried it.
func TestStreamHold_HeldSinceTheChunkArrived(t *testing.T) {
	h := &streamHold{}
	h.received(5)
	first := h.arrivals[0].at
	h.received(5) // nothing new arrived
	h.received(10)
	require.Len(t, h.arrivals, 2)
	second := h.arrivals[1].at
	assert.Equal(t, first, h.heldSince(4))
	assert.Equal(t, second, h.heldSince(5))
	assert.Equal(t, second, h.heldSince(9))
}

// A released delta that ends a segment, or the reply, is marked SegmentEnd,
// so a consumer that works a segment at a time can take it at once, without
// waiting for the text after it.
func TestStreamHold_ReleasedSegmentsAreMarkedAtTheirEnd(t *testing.T) {
	hook := &holdingHook{mode: hooks.StreamHoldSegment, decide: failOn("never")}
	r := startHoldRun(t, hook, StreamHoldConfig{MinChars: 1})
	r.send("One. ")
	first := r.nextText(t)
	assert.Equal(t, "One.", *first.Text)
	assert.True(t, first.Meta.SegmentEnd)
	r.send("Two")
	r.end()
	var marks []string
	for _, e := range r.rest(t) {
		if e.Text != nil && e.Message == nil {
			marks = append(marks, fmt.Sprintf("%s|%v", *e.Text, e.Meta.SegmentEnd))
		}
	}
	assert.Equal(t, []string{" Two|true"}, marks, "the reply's end ends its last segment")
}
