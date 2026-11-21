package sdk

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A judge guardrail declared with stream_hold holds a streamed reply back from
// the caller until it has judged it: per segment (stream_hold: segment) or
// whole (stream_hold: turn). These tests drive sdk.Open + Stream with the mock
// model, paced chunk by chunk, and a real llm_judge guardrail whose judge is
// the mock provider answering scripted verdicts, gated call by call.

const holdMessage = "I can only talk about the weather."

// holdLog is the order in which the judge and the caller saw things happen.
type holdLog struct {
	mu      sync.Mutex
	entries []string
}

func (l *holdLog) add(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, fmt.Sprintf(format, args...))
}

func (l *holdLog) list() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.entries...)
}

// holdJudge is the mock provider as a judge, answering each call with the
// next scripted verdict. It records the content each call judged, and when
// gated, answers a call only once the test lets it.
type holdJudge struct {
	providers.Provider
	log     *holdLog
	gate    chan struct{} // nil answers at once; else one receive per call
	started chan string   // the content of each call, as it starts
	fail    error         // answered instead of a verdict

	mu      sync.Mutex
	prompts []string
}

func newHoldJudge(log *holdLog, verdicts ...bool) *holdJudge {
	turns := make([]mock.Turn, len(verdicts))
	for i, pass := range verdicts {
		score := 0
		if pass {
			score = 1
		}
		turns[i] = mock.Turn{Type: "text",
			Content: fmt.Sprintf(`{"passed": %v, "score": %d, "reasoning": "scripted"}`, pass, score)}
	}
	return &holdJudge{
		Provider: mock.NewProviderWithRepository("judge", "judge-model", false, &sequenceRepo{turns: turns}),
		log:      log,
		started:  make(chan string, 16),
	}
}

func (j *holdJudge) Predict(ctx context.Context, req providers.PredictionRequest) (providers.PredictionResponse, error) {
	prompt := req.Messages[0].GetContent()
	content := strings.TrimPrefix(prompt, "Content to evaluate:\n")
	content, _, _ = strings.Cut(content, "\n\nCriteria: ")
	j.mu.Lock()
	j.prompts = append(j.prompts, prompt)
	n := len(j.prompts)
	j.mu.Unlock()
	j.log.add("judge %d: %s", n, content)
	j.started <- content
	if j.gate != nil {
		select {
		case <-j.gate:
		case <-ctx.Done():
			return providers.PredictionResponse{}, ctx.Err()
		}
	}
	if j.fail != nil {
		return providers.PredictionResponse{}, j.fail
	}
	resp, err := j.Provider.Predict(ctx, req)
	j.log.add("verdict %d", n)
	return resp, err
}

func (j *holdJudge) calls() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return append([]string(nil), j.prompts...)
}

// pacedModel is the mock model, its text chunks forwarded one per step the
// test sends. forwarded reports each chunk once the provider stage took it.
type pacedModel struct {
	*mock.ToolProvider
	step      chan struct{} // nil forwards every chunk at once
	forwarded chan string
}

func newPacedModel(paced bool, turns ...mock.Turn) *pacedModel {
	m := &pacedModel{
		ToolProvider: mock.NewToolProviderWithRepository("mock", "mock-model", false, &sequenceRepo{turns: turns}),
		forwarded:    make(chan string, 16),
	}
	if paced {
		m.step = make(chan struct{})
	}
	return m
}

func (m *pacedModel) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	in, err := m.ToolProvider.PredictStream(ctx, req)
	return m.pace(ctx, in), err
}

func (m *pacedModel) PredictStreamWithTools(
	ctx context.Context, req providers.PredictionRequest, tools interface{}, toolChoice string,
) (<-chan providers.StreamChunk, error) {
	in, err := m.ToolProvider.PredictStreamWithTools(ctx, req, tools, toolChoice)
	return m.pace(ctx, in), err
}

func (m *pacedModel) pace(ctx context.Context, in <-chan providers.StreamChunk) <-chan providers.StreamChunk {
	if in == nil {
		return nil
	}
	out := make(chan providers.StreamChunk)
	go func() {
		defer close(out)
		defer func() {
			for range in {
			}
		}()
		for c := range in {
			if c.Delta != "" && m.step != nil {
				select {
				case <-m.step:
				case <-ctx.Done():
					return
				}
			}
			select {
			case out <- c:
			case <-ctx.Done():
				return
			}
			if c.Delta != "" {
				m.forwarded <- c.Delta
			}
		}
	}()
	return out
}

// next lets the model send its next text chunk and waits until the provider
// stage has taken it.
func (m *pacedModel) next(t *testing.T) {
	t.Helper()
	letThrough(t, m.step)
	recvWithin(t, m.forwarded)
}

// recvWithin receives from ch, failing the test rather than hanging when
// nothing arrives. The bound is a hang guard; a passing test never waits on it.
func recvWithin[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("nothing arrived")
		var zero T
		return zero
	}
}

// letThrough sends one step on gate, failing the test rather than hanging
// when nothing is waiting on it.
func letThrough(t *testing.T, gate chan<- struct{}) {
	t.Helper()
	select {
	case gate <- struct{}{}:
	case <-time.After(10 * time.Second):
		t.Fatal("nothing waited on the gate")
	}
}

// recvChunk is the turn's next chunk, ok false once the turn's stream closed,
// failing the test rather than hanging when the turn neither sends nor ends.
func recvChunk(t *testing.T, ch <-chan StreamChunk) (c StreamChunk, ok bool) {
	t.Helper()
	select {
	case c, ok = <-ch:
		return c, ok
	case <-time.After(10 * time.Second):
		t.Fatal("the turn neither sent a chunk nor ended")
		return StreamChunk{}, false
	}
}

// holdSession is one conversation under a stream_hold judge guardrail.
type holdSession struct {
	conv   *Conversation
	model  *pacedModel
	judge  *holdJudge
	log    *holdLog
	mu     sync.Mutex
	events []*events.Event
	tools  atomic.Int32
}

func (s *holdSession) eventsOf(et events.EventType) []*events.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*events.Event
	for _, e := range s.events {
		if e.Type == et {
			out = append(out, e)
		}
	}
	return out
}

// openHold opens a conversation on model whose output is guarded by an
// llm_judge on judge, with params added to the guardrail's.
func openHold(
	t *testing.T, model *pacedModel, judge *holdJudge, params map[string]any, opts ...Option,
) *holdSession {
	t.Helper()
	s := &holdSession{model: model, judge: judge, log: judge.log}
	bus := events.NewEventBus()
	for _, et := range []events.EventType{
		events.EventStreamHoldReleased, events.EventStreamHoldCut, events.EventValidationFailed,
		events.EventValidationPassed,
	} {
		bus.Subscribe(et, func(e *events.Event) {
			s.mu.Lock()
			s.events = append(s.events, e)
			s.mu.Unlock()
		})
	}
	gp := map[string]any{"criteria": "The reply talks only about the weather."}
	for k, v := range params {
		gp[k] = v
	}
	conv, err := Open(writeGatePack(t), "assist", append([]Option{
		WithProvider(model),
		WithSkipSchemaValidation(),
		WithEventBus(bus),
		WithGuardrail(guardrails.Output("llm_judge", gp,
			guardrails.WithMessage(holdMessage), guardrails.WithJudge(handlers.NewProviderJudge(judge)))),
	}, opts...)...)
	require.NoError(t, err)
	conv.OnTool("send_message", func(map[string]any) (any, error) {
		s.tools.Add(1)
		return map[string]any{"sent": true}, nil
	})
	t.Cleanup(func() {
		if model.step != nil {
			close(model.step)
		}
		if judge.gate != nil {
			close(judge.gate)
		}
		_ = conv.Close()
		bus.Close()
	})
	s.conv = conv
	return s
}

// holdStream is a turn streamed through Stream, read by the test chunk by
// chunk.
type holdStream struct {
	s        *holdSession
	ch       <-chan StreamChunk
	deltas   []string
	replaces []bool
	resp     *Response
}

func (s *holdSession) stream(t *testing.T) *holdStream {
	t.Helper()
	return &holdStream{s: s, ch: s.conv.Stream(context.Background(), "what is the weather?")}
}

// delta waits for the caller's next text delta and returns it.
func (h *holdStream) delta(t *testing.T) string {
	t.Helper()
	for {
		c, ok := recvChunk(t, h.ch)
		require.True(t, ok, "the stream ended before another delta")
		require.NoError(t, c.Error)
		switch c.Type {
		case ChunkText:
			h.s.log.add("delta: %s", c.Text)
			h.deltas = append(h.deltas, c.Text)
			h.replaces = append(h.replaces, c.ReplacesText)
			return c.Text
		case ChunkDone:
			t.Fatalf("the stream ended before another delta; deltas so far %q", h.deltas)
		}
	}
}

// finish reads the rest of the turn.
func (h *holdStream) finish(t *testing.T) {
	t.Helper()
	for c, ok := recvChunk(t, h.ch); ok; c, ok = recvChunk(t, h.ch) {
		require.NoError(t, c.Error)
		switch c.Type {
		case ChunkText:
			h.s.log.add("delta: %s", c.Text)
			h.deltas = append(h.deltas, c.Text)
			h.replaces = append(h.replaces, c.ReplacesText)
		case ChunkDone:
			h.resp = c.Message
		}
	}
	require.NotNil(t, h.resp, "the stream ended without its response")
}

// lastAssistant is the conversation's stored last assistant message.
func lastAssistant(t *testing.T, conv *Conversation) types.Message {
	t.Helper()
	msgs := conv.Messages(context.Background())
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			return msgs[i]
		}
	}
	t.Fatal("no assistant message stored")
	return types.Message{}
}

var weatherChunks = []string{"The weather is mild today. ", "Expect light rain later. ", "Bring a coat."}

// smallSegments makes every sentence a segment of its own.
func smallSegments() Option { return WithStreamHold(StreamHoldConfig{MinChars: 1}) }

// Each segment reaches the caller only after the judge passed it, while the
// model goes on writing; the last is judged by the completed-reply check,
// which judges the whole reply as it does without a hold.
func TestStreamHold_SegmentReleasesEachSegmentAfterItsVerdict(t *testing.T) {
	log := &holdLog{}
	judge := newHoldJudge(log, true, true, true)
	judge.gate = make(chan struct{})
	model := newPacedModel(true, mock.Turn{Type: "text", StreamChunks: weatherChunks})
	s := openHold(t, model, judge, map[string]any{"stream_hold": "segment"}, smallSegments())
	turn := s.stream(t)

	model.next(t)
	assert.Equal(t, "The weather is mild today.", recvWithin(t, judge.started))
	letThrough(t, judge.gate)
	turn.delta(t)

	model.next(t)
	assert.Equal(t, "The weather is mild today. Expect light rain later.", recvWithin(t, judge.started))
	letThrough(t, judge.gate)
	turn.delta(t)

	model.next(t)
	assert.Equal(t, strings.Join(weatherChunks, ""), recvWithin(t, judge.started))
	letThrough(t, judge.gate)
	turn.finish(t)

	assert.Equal(t, []string{
		"judge 1: The weather is mild today.",
		"verdict 1",
		"delta: The weather is mild today.",
		"judge 2: The weather is mild today. Expect light rain later.",
		"verdict 2",
		"delta:  Expect light rain later.",
		"judge 3: The weather is mild today. Expect light rain later. Bring a coat.",
		"verdict 3",
		"delta:  Bring a coat.",
	}, log.list())
	assert.Equal(t, []bool{false, false, false}, turn.replaces)
	assert.Equal(t, strings.Join(weatherChunks, ""), turn.resp.Text())
	assert.Equal(t, strings.Join(weatherChunks, ""), lastAssistant(t, s.conv).Content)

	calls := judge.calls()
	require.Len(t, calls, 3)
	assert.Contains(t, calls[0], "may be incomplete", "a segment judge is told the reply is partial")
	assert.Contains(t, calls[1], "may be incomplete")
	assert.NotContains(t, calls[2], "may be incomplete", "the completed reply is judged as a whole")

	released := s.eventsOf(events.EventStreamHoldReleased)
	require.Len(t, released, 3)
	var spans []string
	for _, e := range released {
		d := e.Data.(*events.StreamHoldReleasedData)
		spans = append(spans, fmt.Sprintf("%d-%d", d.From, d.To))
	}
	assert.Equal(t, []string{"0-26", "26-51", "51-65"}, spans)

	var judged []string
	for _, e := range s.eventsOf(events.EventValidationPassed) {
		d := e.Data.(*events.ValidationEventData)
		judged = append(judged, fmt.Sprintf("%d@%d", d.SegmentIndex, d.SegmentEnd))
	}
	assert.Equal(t, []string{"1@26", "2@51", "0@0"}, judged,
		"one passed event per judgment: each segment's, then the completed reply's")
}

// A tail guardrail and a segment guardrail together release only what both
// have cleared: the segment's end, before the tail the banned-word check
// holds, then the rest once the reply passes.
func TestStreamHold_TailAndSegmentReleaseWhatBothCleared(t *testing.T) {
	log := &holdLog{}
	judge := newHoldJudge(log, true, true)
	judge.gate = make(chan struct{})
	model := newPacedModel(true, mock.Turn{Type: "text", StreamChunks: []string{"Sunny skies. Pine", " cones fall."}})
	s := openHold(t, model, judge, map[string]any{"stream_hold": "segment"}, smallSegments(), noPineapple())
	turn := s.stream(t)

	model.next(t)
	assert.Equal(t, "Sunny skies.", recvWithin(t, judge.started))
	letThrough(t, judge.gate)
	assert.Equal(t, "Sunny skies.", turn.delta(t))

	model.next(t)
	assert.Equal(t, "Sunny skies. Pine cones fall.", recvWithin(t, judge.started))
	letThrough(t, judge.gate)
	turn.finish(t)

	assert.Equal(t, []string{"Sunny skies.", " Pine cones fall."}, turn.deltas)
	assert.Equal(t, "Sunny skies. Pine cones fall.", turn.resp.Text())
}

// Segments that become ready while the judge is busy join its next call, so
// the number of judge calls follows the judge's latency, not the sentence
// count.
func TestStreamHold_SegmentCoalescesSegmentsReadyWhileTheJudgeRuns(t *testing.T) {
	log := &holdLog{}
	judge := newHoldJudge(log, true, true, true)
	judge.gate = make(chan struct{})
	chunks := []string{"Sun at first. ", "Then clouds. ", "Then wind. ", "Rain by night."}
	model := newPacedModel(true, mock.Turn{Type: "text", StreamChunks: chunks})
	s := openHold(t, model, judge, map[string]any{"stream_hold": "segment"}, smallSegments())
	turn := s.stream(t)

	model.next(t)
	assert.Equal(t, "Sun at first.", recvWithin(t, judge.started))
	model.next(t) // "Then clouds." and "Then wind." arrive while judge 1 runs
	model.next(t)
	letThrough(t, judge.gate)
	assert.Equal(t, "Sun at first.", turn.delta(t))

	assert.Equal(t, "Sun at first. Then clouds. Then wind.", recvWithin(t, judge.started),
		"the next call covers both segments that became ready")
	letThrough(t, judge.gate)
	assert.Equal(t, " Then clouds. Then wind.", turn.delta(t))

	model.next(t)
	recvWithin(t, judge.started)
	letThrough(t, judge.gate)
	turn.finish(t)

	assert.Equal(t, []string{"Sun at first.", " Then clouds. Then wind.", " Rain by night."}, turn.deltas)
	assert.Len(t, judge.calls(), 3, "four segments, three judge calls")
}

// A segment that fails after earlier ones were released ends the reply with
// the guardrail's message, marked as replacing what was streamed. The stored
// reply is the message alone, the judge is not called again, and the cut is
// reported.
func TestStreamHold_LaterFailingSegmentEndsWithTheMessage(t *testing.T) {
	log := &holdLog{}
	judge := newHoldJudge(log, true, false)
	judge.gate = make(chan struct{})
	model := newPacedModel(true, mock.Turn{Type: "text",
		StreamChunks: []string{"The weather is mild today. ", "Buy CompetitorCo shares now. ", "They are cheap."}})
	s := openHold(t, model, judge, map[string]any{"stream_hold": "segment"}, smallSegments())
	turn := s.stream(t)

	model.next(t)
	recvWithin(t, judge.started)
	letThrough(t, judge.gate)
	turn.delta(t)

	model.next(t)
	assert.Equal(t, "The weather is mild today. Buy CompetitorCo shares now.", recvWithin(t, judge.started))
	model.next(t) // the reply ends while the segment is judged
	letThrough(t, judge.gate)
	turn.finish(t)

	assert.Equal(t, []string{"The weather is mild today.", holdMessage}, turn.deltas)
	assert.Equal(t, []bool{false, true}, turn.replaces)
	assert.Equal(t, holdMessage, turn.resp.Text())
	assert.Equal(t, types.FinishReasonSafety, turn.resp.Message().FinishReason)

	stored := lastAssistant(t, s.conv)
	assert.Equal(t, holdMessage, stored.Content)
	assert.Equal(t, types.FinishReasonSafety, stored.FinishReason)
	require.Len(t, stored.Validations, 1)
	assert.Equal(t, "llm_judge", stored.Validations[0].ValidatorType)
	assert.False(t, stored.Validations[0].Passed)
	assert.Equal(t, 2, stored.Validations[0].Details["segment_index"])

	assert.Len(t, judge.calls(), 2, "a cut reply is not judged again")

	cuts := s.eventsOf(events.EventStreamHoldCut)
	require.Len(t, cuts, 1)
	cut := cuts[0].Data.(*events.StreamHoldCutData)
	assert.Equal(t, "llm_judge", cut.Guardrail)
	assert.Equal(t, 2, cut.SegmentIndex)
	assert.Equal(t, len("The weather is mild today."), cut.ReleasedBytes)

	failed := s.eventsOf(events.EventValidationFailed)
	require.Len(t, failed, 1)
	assert.Equal(t, 2, failed[0].Data.(*events.ValidationEventData).SegmentIndex)
	assert.Equal(t, len("The weather is mild today. Buy CompetitorCo shares now."),
		failed[0].Data.(*events.ValidationEventData).SegmentEnd)
}

// A free-form llm_judge holds the whole turn by default: nothing reaches the
// caller until the judge passed the completed reply, which then arrives as one
// delta.
func TestStreamHold_TurnReleasesNothingUntilTheVerdict(t *testing.T) {
	log := &holdLog{}
	judge := newHoldJudge(log, true)
	model := newPacedModel(false, mock.Turn{Type: "text", StreamChunks: weatherChunks})
	s := openHold(t, model, judge, nil)
	turn := s.stream(t)
	turn.finish(t)

	full := strings.Join(weatherChunks, "")
	assert.Equal(t, []string{"judge 1: " + full, "verdict 1", "delta: " + full}, log.list())
	assert.Equal(t, []string{full}, turn.deltas)
	assert.Equal(t, full, turn.resp.Text())
}

// A turn the judge fails reaches the caller as the guardrail's message only.
func TestStreamHold_TurnFailureEmitsOnlyTheMessage(t *testing.T) {
	log := &holdLog{}
	judge := newHoldJudge(log, false)
	model := newPacedModel(false, mock.Turn{Type: "text", StreamChunks: weatherChunks})
	s := openHold(t, model, judge, map[string]any{"stream_hold": "turn"})
	turn := s.stream(t)
	turn.finish(t)

	assert.Equal(t, []string{holdMessage}, turn.deltas)
	assert.Equal(t, []bool{true}, turn.replaces)
	assert.Equal(t, holdMessage, turn.resp.Text())
	assert.Equal(t, holdMessage, lastAssistant(t, s.conv).Content)
}

// A guardrail that holds nothing (stream_hold: none) streams the reply as it
// arrives; when the completed-reply check fails it, the message follows,
// marked as replacing what was streamed.
func TestStreamHold_NoneFailureEndsWithAReplacingMessage(t *testing.T) {
	log := &holdLog{}
	judge := newHoldJudge(log, false)
	model := newPacedModel(false, mock.Turn{Type: "text", StreamChunks: weatherChunks})
	s := openHold(t, model, judge, map[string]any{"stream_hold": "none"})
	turn := s.stream(t)
	turn.finish(t)

	assert.Equal(t, append(append([]string{}, weatherChunks...), holdMessage), turn.deltas)
	assert.Equal(t, []bool{false, false, false, true}, turn.replaces)
	assert.Equal(t, holdMessage, turn.resp.Text())
}

// A segment judge that errors fails closed: the caller gets the guardrail's
// message and none of the reply.
func TestStreamHold_SegmentJudgeErrorFailsClosed(t *testing.T) {
	log := &holdLog{}
	judge := newHoldJudge(log)
	judge.fail = errors.New("judge unavailable")
	model := newPacedModel(false, mock.Turn{Type: "text", StreamChunks: weatherChunks})
	s := openHold(t, model, judge, map[string]any{"stream_hold": "segment"}, smallSegments())
	turn := s.stream(t)
	turn.finish(t)

	assert.Equal(t, []string{holdMessage}, turn.deltas)
	assert.Equal(t, holdMessage, lastAssistant(t, s.conv).Content)
	assert.Len(t, judge.calls(), 1)
}

// A segment judge that does not answer within the stream-hold timeout fails
// closed, recorded as a timeout.
func TestStreamHold_SegmentJudgeTimeoutFailsClosed(t *testing.T) {
	log := &holdLog{}
	judge := newHoldJudge(log, true)
	judge.gate = make(chan struct{}) // never opened: the judge never answers
	model := newPacedModel(false, mock.Turn{Type: "text", StreamChunks: weatherChunks})
	s := openHold(t, model, judge, map[string]any{"stream_hold": "segment"},
		WithStreamHold(StreamHoldConfig{MinChars: 1, SegmentTimeout: 20 * time.Millisecond}))
	turn := s.stream(t)
	turn.finish(t)

	assert.Equal(t, []string{holdMessage}, turn.deltas)
	stored := lastAssistant(t, s.conv)
	assert.Equal(t, holdMessage, stored.Content)
	require.Len(t, stored.Validations, 1)
	assert.Equal(t, "timeout", stored.Validations[0].Details["reason"])
}

// Past max_segment_judges the rest of the round is held for the completed-reply
// check.
func TestStreamHold_MaxSegmentJudgesHandsTheRestToTheFinalCheck(t *testing.T) {
	log := &holdLog{}
	judge := newHoldJudge(log, true, true)
	judge.gate = make(chan struct{})
	model := newPacedModel(true, mock.Turn{Type: "text", StreamChunks: weatherChunks})
	s := openHold(t, model, judge, map[string]any{"stream_hold": "segment"},
		WithStreamHold(StreamHoldConfig{MinChars: 1, MaxSegmentJudges: 1}))
	turn := s.stream(t)

	model.next(t)
	recvWithin(t, judge.started)
	letThrough(t, judge.gate)
	turn.delta(t)
	model.next(t)
	model.next(t)
	assert.Equal(t, strings.Join(weatherChunks, ""), recvWithin(t, judge.started))
	letThrough(t, judge.gate)
	turn.finish(t)

	assert.Equal(t, []string{"The weather is mild today.", " Expect light rain later. Bring a coat."}, turn.deltas)
	assert.Len(t, judge.calls(), 2)
}

// Text a tool-calling round writes before its calls is held and judged like
// any other; a cut drops the round's tool calls, so they never run.
func TestStreamHold_ToolRoundCutDropsItsCalls(t *testing.T) {
	log := &holdLog{}
	judge := newHoldJudge(log, false)
	model := newPacedModel(false,
		mock.Turn{Type: "tool_calls", StreamChunks: []string{"Sending the competitor offer now. "},
			ToolCalls: []mock.ToolCall{{Name: "send_message", Arguments: map[string]any{"body": "hi"}}}},
		mock.Turn{Type: "text", Content: "Sent it."})
	s := openHold(t, model, judge, map[string]any{"stream_hold": "segment"}, smallSegments())
	turn := s.stream(t)
	turn.finish(t)

	assert.Equal(t, []string{holdMessage}, turn.deltas)
	assert.Equal(t, int32(0), s.tools.Load(), "the cut round's tool call never ran")
	stored := lastAssistant(t, s.conv)
	assert.Equal(t, holdMessage, stored.Content)
	assert.Empty(t, stored.ToolCalls)
}

// A tool-calling round's text that passes is released, its calls run, and the
// next round is held and judged in turn.
func TestStreamHold_ToolRoundTextIsReleasedAndItsCallsRun(t *testing.T) {
	log := &holdLog{}
	judge := newHoldJudge(log, true, true, true)
	model := newPacedModel(false,
		mock.Turn{Type: "tool_calls", StreamChunks: []string{"Sending the forecast now. "},
			ToolCalls: []mock.ToolCall{{Name: "send_message", Arguments: map[string]any{"body": "hi"}}}},
		mock.Turn{Type: "text", Content: "Sent it."})
	s := openHold(t, model, judge, map[string]any{"stream_hold": "segment"}, smallSegments())
	turn := s.stream(t)
	turn.finish(t)

	assert.Equal(t, "Sending the forecast now. Sent it.", strings.Join(turn.deltas, ""))
	assert.Equal(t, int32(1), s.tools.Load())
	assert.Equal(t, "Sent it.", turn.resp.Text())
}

// A session with no guardrail streams the model's chunks exactly as before.
func TestStreamHold_UnguardedStreamIsUnchanged(t *testing.T) {
	got := streamChunks(t, weatherChunks)
	assert.Equal(t, weatherChunks, got.deltas)
	assert.Equal(t, []bool{false, false, false}, got.replaces)
}

// stream_hold values the guardrail cannot honor are refused when the
// conversation is opened.
func TestStreamHold_InvalidModeIsAConstructionError(t *testing.T) {
	open := func(spec guardrails.Spec) error {
		_, err := Open(writeGatePack(t), "assist",
			WithProvider(mock.NewToolProviderWithRepository("mock", "mock-model", false, &sequenceRepo{})),
			WithSkipSchemaValidation(), WithGuardrail(spec))
		return err
	}
	err := open(guardrails.Output("content_excludes",
		map[string]any{"patterns": []any{"x"}, "stream_hold": "sometimes"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `stream_hold "sometimes"`)

	err = open(guardrails.Output("content_excludes",
		map[string]any{"patterns": []any{"x"}, "stream_hold": "segment"}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot judge a partial reply")

	err = open(guardrails.Output("llm_judge",
		map[string]any{"criteria": "c", "stream_hold": "tail"}, guardrails.WithJudge(handlers.NewProviderJudge(newHoldJudge(&holdLog{})))))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not judge chunks")
}

// Negative limits are refused when the conversation is opened.
func TestWithStreamHold_RejectsNegativeLimits(t *testing.T) {
	_, err := Open(writeGatePack(t), "assist",
		WithProvider(mock.NewToolProviderWithRepository("mock", "mock-model", false, &sequenceRepo{})),
		WithSkipSchemaValidation(), WithStreamHold(StreamHoldConfig{MinChars: -1}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "WithStreamHold: limits must be non-negative")
}

// A length check that truncates a reply a turn guardrail holds releases
// nothing the judge has not passed: the truncated reply waits for the
// completed-reply check, and a reply it fails reaches the caller as the
// message only.
func TestStreamHold_TruncationWaitsForTheHoldingJudge(t *testing.T) {
	maxLen := WithGuardrail(guardrails.Output("max_length", map[string]any{"max_characters": 20}))

	log := &holdLog{}
	judge := newHoldJudge(log, true)
	model := newPacedModel(false, mock.Turn{Type: "text", StreamChunks: weatherChunks})
	s := openHold(t, model, judge, map[string]any{"stream_hold": "turn"}, maxLen)
	turn := s.stream(t)
	turn.finish(t)
	assert.Equal(t, []string{"judge 1: The weather is mild ", "verdict 1", "delta: The weather is mild "}, log.list())

	failLog := &holdLog{}
	failing := newHoldJudge(failLog, false)
	model = newPacedModel(false, mock.Turn{Type: "text", StreamChunks: weatherChunks})
	s = openHold(t, model, failing, map[string]any{"stream_hold": "turn"}, maxLen)
	turn = s.stream(t)
	turn.finish(t)
	assert.Equal(t, []string{holdMessage}, turn.deltas, "none of the truncated reply reached the caller")
	assert.Equal(t, holdMessage, turn.resp.Text())
}

// Every delta a stream_hold releases, and the message that replaces a held
// reply, names the id the reply is stored under (StreamChunk.MessageID).
func TestStreamHold_DeltasCarryTheStoredMessageID(t *testing.T) {
	for name, pass := range map[string]bool{"released": true, "replaced": false} {
		t.Run(name, func(t *testing.T) {
			judge := newHoldJudge(&holdLog{}, pass)
			model := newPacedModel(false, mock.Turn{Type: "text", StreamChunks: weatherChunks})
			s := openHold(t, model, judge, map[string]any{"stream_hold": "turn"})
			var ids []string
			for c := range s.conv.Stream(context.Background(), "what is the weather?") {
				require.NoError(t, c.Error)
				if c.Type == ChunkText {
					ids = append(ids, c.MessageID)
				}
			}
			require.Len(t, ids, 1)
			stored := lastAssistant(t, s.conv)
			require.NotEmpty(t, stored.ID())
			assert.Equal(t, []string{stored.ID()}, ids)
		})
	}
}
