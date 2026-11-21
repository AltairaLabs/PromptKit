package stage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// streamedReplyRate is the PCM rate segmentRecordingTTS synthesizes at.
const streamedReplyRate = 16000

// segmentRecordingTTS is a priced PCM service that records the text of every
// synthesis call. Each call returns audio, or what synth returns for it.
type segmentRecordingTTS struct {
	pricedStreamingTTS
	synth func(ctx context.Context, n int, text string) (io.ReadCloser, error)

	mu    sync.Mutex
	texts []string
}

func newSegmentRecordingTTS() *segmentRecordingTTS {
	svc := &segmentRecordingTTS{pricedStreamingTTS: pricedStreamingTTS{pcmRate: streamedReplyRate}}
	svc.synthesizeFunc = func(ctx context.Context, text string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
		svc.mu.Lock()
		svc.texts = append(svc.texts, text)
		n := len(svc.texts)
		svc.mu.Unlock()
		if svc.synth != nil {
			return svc.synth(ctx, n, text)
		}
		return io.NopCloser(bytes.NewReader(make([]byte, 320))), nil
	}
	return svc
}

func (s *segmentRecordingTTS) spoken() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.texts...)
}

// delta is a streaming text delta, as the provider stage releases it.
func delta(text string) stage.StreamElement {
	e := stage.NewTextElement(text)
	e.Meta.StreamingDelta = true
	return e
}

// replacement is the delta a guardrail's mid-stream replacement arrives as.
func replacement(text string) stage.StreamElement {
	e := delta(text)
	e.Meta.ReplacesText = true
	fr := types.FinishReasonSafety
	e.Meta.FinishReason = &fr
	return e
}

func assistant(text string) stage.StreamElement {
	return stage.NewMessageElement(&types.Message{Role: "assistant", Content: text})
}

func newStreamedReplyStage(svc tts.Service) *stage.TTSStageWithInterruption {
	cfg := stage.DefaultTTSStageWithInterruptionConfig()
	cfg.Retry = tts.RetryConfig{MaxAttempts: 1}
	return stage.NewTTSStageWithInterruption(svc, cfg)
}

// ttsCostUSD is the total of the tts_cost stamped on msg, or -1 when none is.
func ttsCostUSD(msg *types.Message) float64 {
	costMap, ok := msg.Meta["tts_cost"].(map[string]any)
	if !ok {
		return -1
	}
	usd, _ := costMap["total_cost_usd"].(float64)
	return usd
}

// kinds summarizes elements: "delta:<text>", "audio:<segment text or ->",
// "message:<content>", "end", "interrupt", "error".
func kinds(elems []stage.StreamElement) []string {
	var out []string
	for _, e := range elems {
		switch {
		case e.Interrupt:
			out = append(out, "interrupt")
		case e.EndOfTurn:
			out = append(out, "end")
		case e.Audio != nil:
			label := "-"
			if e.Text != nil {
				label = *e.Text
			}
			out = append(out, "audio:"+label)
		case e.Message != nil:
			out = append(out, "message:"+e.Message.Content)
		case e.Error != nil:
			out = append(out, "error")
		case e.Text != nil:
			out = append(out, "delta:"+*e.Text)
		}
	}
	return out
}

// replyTestBound bounds every wait in these tests. A reply over a few scripted
// elements takes milliseconds; a broken stage may stop reading its input, never
// emit, or block in a synthesis that waits on a context nothing cancels, and
// must fail here, not at the test binary's timeout.
const replyTestBound = 5 * time.Second

// replyRun is a TTSStageWithInterruption's Process running on its own
// goroutine, fed and read one element at a time, every step bounded by
// replyTestBound. Cleanup cancels Process's context, which releases a
// synthesis still waiting on it.
type replyRun struct {
	cancel context.CancelFunc
	input  chan stage.StreamElement
	output chan stage.StreamElement
	done   chan error
	got    []stage.StreamElement
}

func startReplyRun(t *testing.T, s stage.Stage) *replyRun {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	r := &replyRun{
		cancel: cancel,
		input:  make(chan stage.StreamElement),
		output: make(chan stage.StreamElement, 32),
		done:   make(chan error, 1),
	}
	go func() { r.done <- s.Process(ctx, r.input, r.output) }()
	return r
}

// send hands the stage its next input element.
func (r *replyRun) send(t *testing.T, elems ...stage.StreamElement) {
	t.Helper()
	for _, e := range elems {
		select {
		case r.input <- e:
		case <-time.After(replyTestBound):
			t.Fatalf("the stage stopped reading its input (sending %v)", kinds([]stage.StreamElement{e}))
		}
	}
}

// next is the stage's next output element.
func (r *replyRun) next(t *testing.T) stage.StreamElement {
	t.Helper()
	select {
	case e, ok := <-r.output:
		if !ok {
			t.Fatal("the stage closed its output early")
		}
		r.got = append(r.got, e)
		return e
	case <-time.After(replyTestBound):
		t.Fatalf("the stage emitted nothing more after %v", kinds(r.got))
		return stage.StreamElement{}
	}
}

// settleQuiet is how long the stage's output stays quiet before settle takes
// the reply as settled.
const settleQuiet = 100 * time.Millisecond

// settle reads the stage's output until it has been quiet for settleQuiet.
// A reply's Message waits until its audio would have played, and an input
// that closes first is a session closing, which cuts the reply short; so the
// input closes only once what was sent has settled.
func (r *replyRun) settle(t *testing.T) {
	t.Helper()
	deadline := time.After(replyTestBound)
	for {
		select {
		case e, ok := <-r.output:
			if !ok {
				return
			}
			r.got = append(r.got, e)
		case <-time.After(settleQuiet):
			return
		case <-deadline:
			t.Fatal("the stage's output never settled")
		}
	}
}

// runReply runs a streamed-reply stage over inputs, as a live session does:
// the input closes once the reply has settled. It returns what it emitted.
func runReply(t *testing.T, s stage.Stage, inputs []stage.StreamElement) []stage.StreamElement {
	t.Helper()
	r := startReplyRun(t, s)
	r.send(t, inputs...)
	return r.finish(t)
}

// finish lets the reply settle, closes the input, waits for Process to return
// without error, and returns everything the stage emitted, in order.
func (r *replyRun) finish(t *testing.T) []stage.StreamElement {
	t.Helper()
	r.settle(t)
	close(r.input)
	select {
	case err := <-r.done:
		require.NoError(t, err)
	case <-time.After(replyTestBound):
		t.Fatalf("Process did not return after its input closed; emitted %v", kinds(r.got))
	}
	for {
		select {
		case e, ok := <-r.output:
			if !ok {
				return r.got
			}
			r.got = append(r.got, e)
		case <-time.After(replyTestBound):
			t.Fatal("the stage did not close its output")
			return nil
		}
	}
}

// awaitSignal waits for ch to close or receive, failing the test if it has not
// by replyTestBound.
func awaitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(replyTestBound):
		t.Fatal(what)
	}
}

// A sentence is synthesized as soon as its delta completes it, before the rest
// of the reply has arrived; the completed Message is not spoken again, and it
// carries the cost of every segment.
func TestTTSStageWithInterruption_SpeaksStreamedSentencesAsTheyComplete(t *testing.T) {
	svc := newSegmentRecordingTTS()
	r := startReplyRun(t, newStreamedReplyStage(svc))

	r.send(t, delta("One. "))
	for len(audioElements(r.got)) == 0 {
		r.next(t) // fails if the first sentence waits for the rest of the reply
	}
	r.send(t, delta("Two"), delta(" three."), assistant("One. Two three."), stage.NewEndOfTurnElement())
	got := r.finish(t)

	assert.Equal(t, []string{"One.", "Two three."}, svc.spoken())
	assert.Equal(t, []string{
		"delta:One. ", "audio:One.", "delta:Two", "delta: three.", "audio:Two three.",
		"message:One. Two three.", "end",
	}, kinds(got))
	msg := got[len(got)-2].Message
	assert.InDelta(t, float64(len("One.")+len("Two three."))*0.0001, ttsCostUSD(msg), 1e-12)
	assert.True(t, got[1].Meta.SynthesizedSpeech, "segment audio is marked as synthesized speech")
}

// A segment's audio is handed on in pieces as it is read, not after the whole
// segment is synthesized; only the first piece carries the segment's text.
func TestTTSStageWithInterruption_StreamsASegmentsAudioInPieces(t *testing.T) {
	svc := newSegmentRecordingTTS()
	pcm := generateTestPCMAudio(8000) // 250ms at 16kHz
	svc.synth = func(context.Context, int, string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(pcm)), nil
	}
	s := newStreamedReplyStage(svc)

	results := runReply(t, s, []stage.StreamElement{delta("Hello."), assistant("Hello.")})

	pieces := audioElements(results)
	require.Len(t, pieces, 3, "100ms pieces of a 250ms segment")
	var joined []byte
	for i, p := range pieces {
		joined = append(joined, p.Audio.Samples...)
		assert.Equal(t, streamedReplyRate, p.Audio.SampleRate)
		if i == 0 {
			require.NotNil(t, p.Text)
			assert.Equal(t, "Hello.", *p.Text)
		} else {
			assert.Nil(t, p.Text, "only the first piece carries the segment's text")
		}
	}
	assert.Equal(t, pcm, joined, "the pieces are the segment's audio, in order")
}

// A segment whose audio ends on a piece boundary is handed on as exactly
// those pieces: the empty read at the end of the audio is not a piece.
func TestTTSStageWithInterruption_AudioEndingOnAPieceBoundaryAddsNoEmptyPiece(t *testing.T) {
	svc := newSegmentRecordingTTS()
	pcm := generateTestPCMAudio(6400) // 200ms at 16kHz: two 100ms pieces
	svc.synth = func(context.Context, int, string) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(pcm)), nil
	}
	r := startReplyRun(t, newStreamedReplyStage(svc))
	r.send(t, delta("Hello."), assistant("Hello."))
	pieces := audioElements(r.finish(t))

	require.Len(t, pieces, 2)
	for _, p := range pieces {
		assert.Len(t, p.Audio.Samples, 3200)
	}
}

// A streaming delta with no text (a final chunk carrying only its finish
// reason) passes through and speaks nothing.
func TestTTSStageWithInterruption_TextlessDeltaPassesThrough(t *testing.T) {
	svc := newSegmentRecordingTTS()
	r := startReplyRun(t, newStreamedReplyStage(svc))
	bare := stage.StreamElement{}
	bare.Meta.StreamingDelta = true
	r.send(t, delta("Hi there. "), bare, assistant("Hi there."))
	got := r.finish(t)

	assert.Equal(t, []string{"Hi there."}, svc.spoken())
	require.Len(t, got, 4, "the textless delta is passed on too")
	assert.True(t, got[2].Meta.StreamingDelta && got[2].Text == nil, "the textless delta, in order")
	assert.Equal(t, []string{"delta:Hi there. ", "audio:Hi there.", "message:Hi there."}, kinds(got))
}

// A synthesis a barge-in cut short is not a failure, whatever error the
// service returns once its context is canceled: the reply's Message carries
// no error.
func TestTTSStageWithInterruption_SynthesisErrorAfterBargeInIsNotAFailure(t *testing.T) {
	svc := newSegmentRecordingTTS()
	started := make(chan struct{})
	svc.synth = func(ctx context.Context, n int, _ string) (io.ReadCloser, error) {
		if n == 1 {
			close(started)
			<-ctx.Done()
			return nil, errors.New("connection reset by peer")
		}
		return io.NopCloser(bytes.NewReader(make([]byte, 320))), nil
	}
	r := startReplyRun(t, newStreamedReplyStage(svc))

	r.send(t, delta("One. "))
	awaitSignal(t, started, "the first sentence was never synthesized")
	r.send(t, assistant("One."), stage.NewInterruptElement())
	got := r.finish(t)

	require.Equal(t, []string{"delta:One. ", "message:", "interrupt"}, kinds(got),
		"the caller heard none of the reply")
	assert.NoError(t, got[1].Error, "a canceled synthesis does not fail the reply")
}

// Once the stage's context ends, it keeps reading its input to the end, so
// the stage feeding it is never blocked.
func TestTTSStageWithInterruption_KeepsReadingInputAfterItsContextEnds(t *testing.T) {
	svc := newSegmentRecordingTTS()
	r := startReplyRun(t, newStreamedReplyStage(svc))
	r.send(t, delta("One. "))
	r.cancel()
	for i := 0; i < 50; i++ {
		r.send(t, delta("More. ")) // fails if nothing reads the input
	}
	close(r.input)
	select {
	case <-r.done:
	case <-time.After(replyTestBound):
		t.Fatal("Process did not return after its context ended")
	}
}

// A guardrail's mid-stream replacement drops what was released but not yet
// spoken and speaks the guardrail's message in its place. The reply is
// recorded as the caller heard it: the spoken segment, then the message.
func TestTTSStageWithInterruption_ReplacementDropsUnspokenText(t *testing.T) {
	svc := newSegmentRecordingTTS()
	s := newStreamedReplyStage(svc)

	results := runReply(t, s, []stage.StreamElement{
		delta("Fine. Then a dra"), replacement("Blocked."), assistant("Blocked."), stage.NewEndOfTurnElement(),
	})

	assert.Equal(t, []string{"Fine.", "Blocked."}, svc.spoken())
	assert.Contains(t, kinds(results), "message:Fine. Blocked.")
}

// A completed reply whose text is not what streamed (a guardrail replaced it
// after the fact) is spoken in place of what was not yet spoken.
func TestTTSStageWithInterruption_ReplacedMessageIsSpokenInstead(t *testing.T) {
	svc := newSegmentRecordingTTS()
	s := newStreamedReplyStage(svc)

	results := runReply(t, s, []stage.StreamElement{delta("Hello there. Bad wor"), assistant("Sorry.")})

	assert.Equal(t, []string{"Hello there.", "Sorry."}, svc.spoken())
	assert.Contains(t, kinds(results), "message:Hello there. Sorry.")
}

// A replacement before anything of the reply was heard is recorded as the
// guardrail's message alone.
func TestTTSStageWithInterruption_ReplacementBeforeAnythingHeardIsTheMessage(t *testing.T) {
	svc := newSegmentRecordingTTS()
	s := newStreamedReplyStage(svc)

	results := runReply(t, s, []stage.StreamElement{
		delta("Then a dra"), replacement("Blocked."), assistant("Blocked."),
	})

	assert.Equal(t, []string{"Blocked."}, svc.spoken())
	assert.Contains(t, kinds(results), "message:Blocked.")
}

// A delta that ends a released segment (stream_hold's SegmentEnd) is spoken
// at once, without waiting for the text after it to show a sentence end.
func TestTTSStageWithInterruption_SegmentEndSpeaksAtOnce(t *testing.T) {
	svc := newSegmentRecordingTTS()
	r := startReplyRun(t, newStreamedReplyStage(svc))

	released := delta("The weather is mild today.")
	released.Meta.SegmentEnd = true
	r.send(t, released)
	require.Equal(t, []string{"delta:The weather is mild today."}, kinds([]stage.StreamElement{r.next(t)}))
	require.NotNil(t, r.next(t).Audio, "the released segment is spoken before more text arrives")
	r.finish(t)
	assert.Equal(t, []string{"The weather is mild today."}, svc.spoken())
}

// A completed reply that goes on past the text released speaks the rest.
func TestTTSStageWithInterruption_MessageBeyondReleasedTextSpeaksTheRest(t *testing.T) {
	svc := newSegmentRecordingTTS()
	s := newStreamedReplyStage(svc)

	runReply(t, s, []stage.StreamElement{delta("Hi."), assistant("Hi. More.")})

	assert.Equal(t, []string{"Hi.", "More."}, svc.spoken())
}

// A reply over several tool-loop rounds streams all its rounds' text before
// any of its Messages: each Message is matched to its part of it, and the
// cost of what was spoken by each goes on it.
func TestTTSStageWithInterruption_MatchesEachRoundsMessage(t *testing.T) {
	svc := newSegmentRecordingTTS()
	s := newStreamedReplyStage(svc)

	first := &types.Message{Role: "assistant", Content: "Let me check.",
		ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "weather"}}}
	results := runReply(t, s, []stage.StreamElement{
		delta("Let me check. "), delta("It is sunny."),
		stage.NewMessageElement(first),
		stage.NewMessageElement(&types.Message{Role: "tool", Content: "sunny"}),
		assistant("It is sunny."),
		stage.NewEndOfTurnElement(),
	})

	assert.Equal(t, []string{"Let me check.", "It is sunny."}, svc.spoken())
	var costs []float64
	for _, e := range results {
		if e.Message != nil && e.Message.Role == "assistant" {
			costs = append(costs, ttsCostUSD(e.Message))
		}
	}
	require.Len(t, costs, 2)
	assert.InDelta(t, float64(len("Let me check."))*0.0001, costs[0], 1e-12)
	assert.InDelta(t, float64(len("It is sunny."))*0.0001, costs[1], 1e-12)
}

// A reply whose first round only calls a tool streams its answer before its
// Messages arrive: the cost of what was spoken goes on the Message that holds
// the text, not on the empty tool-call one, and other elements pass through.
func TestTTSStageWithInterruption_CostGoesOnTheMessageWithTheText(t *testing.T) {
	svc := newSegmentRecordingTTS()
	s := newStreamedReplyStage(svc)

	toolCall := &types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "weather"}}}
	results := runReply(t, s, []stage.StreamElement{
		delta("It is sunny. "),
		{Reasoning: &stage.ReasoningDelta{Text: "thinking"}},
		stage.NewMessageElement(toolCall),
		stage.NewMessageElement(&types.Message{Role: "tool", Content: "sunny"}),
		assistant("It is sunny."),
		stage.NewEndOfTurnElement(),
	})

	assert.Equal(t, []string{"It is sunny."}, svc.spoken())
	var reasoning int
	costs := map[string]float64{}
	for _, e := range results {
		if e.Reasoning != nil {
			reasoning++
		}
		if e.Message != nil {
			costs[e.Message.Role+":"+e.Message.Content] = ttsCostUSD(e.Message)
		}
	}
	assert.Equal(t, 1, reasoning, "a reasoning element passes through")
	assert.Equal(t, map[string]float64{
		"assistant:":             -1,
		"tool:sunny":             -1,
		"assistant:It is sunny.": float64(len("It is sunny.")) * 0.0001,
	}, costs)
}

// Text left when the turn ends without a Message is still spoken; text left
// when the turn fails is not.
func TestTTSStageWithInterruption_TurnEndSpeaksTheRestAndFailureDropsIt(t *testing.T) {
	svc := newSegmentRecordingTTS()
	s := newStreamedReplyStage(svc)
	runReply(t, s, []stage.StreamElement{delta("Unfinished thought"), stage.NewEndOfTurnElement()})
	assert.Equal(t, []string{"Unfinished thought"}, svc.spoken())

	svc = newSegmentRecordingTTS()
	s = newStreamedReplyStage(svc)
	runReply(t, s, []stage.StreamElement{
		delta("Half a thought"), stage.NewErrorElement(errors.New("turn failed")), stage.NewEndOfTurnElement(),
	})
	assert.Empty(t, svc.spoken(), "a failed turn's unfinished text is not spoken")
}

// A failed synthesis stops the reply: nothing after it is synthesized, and the
// reply's Message carries the error.
func TestTTSStageWithInterruption_SegmentFailureStopsTheReply(t *testing.T) {
	svc := newSegmentRecordingTTS()
	boom := errors.New("synthesis failed")
	svc.synth = func(_ context.Context, n int, _ string) (io.ReadCloser, error) {
		if n == 2 {
			return nil, boom
		}
		return io.NopCloser(bytes.NewReader(make([]byte, 320))), nil
	}
	s := newStreamedReplyStage(svc)

	results := runReply(t, s, []stage.StreamElement{
		delta("One. Two. Three. "), assistant("One. Two. Three."), stage.NewEndOfTurnElement(),
	})

	assert.Equal(t, []string{"One.", "Two."}, svc.spoken())
	var msg *stage.StreamElement
	for i := range results {
		if results[i].Message != nil {
			msg = &results[i]
		}
	}
	require.NotNil(t, msg)
	assert.ErrorIs(t, msg.Error, boom, "the reply's Message carries the failure")
	assert.InDelta(t, float64(len("One."))*0.0001, ttsCostUSD(msg.Message), 1e-12, "only the spoken segment is priced")
}

// A failure with no Message to carry it is reported when the turn ends.
func TestTTSStageWithInterruption_SegmentFailureWithoutMessageIsReported(t *testing.T) {
	svc := newSegmentRecordingTTS()
	svc.synth = func(context.Context, int, string) (io.ReadCloser, error) {
		return nil, errors.New("synthesis failed")
	}
	s := newStreamedReplyStage(svc)

	results := runReply(t, s, []stage.StreamElement{delta("Lonely words"), stage.NewEndOfTurnElement()})

	assert.Equal(t, []string{"error", "end"}, kinds(results)[1:])
}

// A barge-in cancels the segment being synthesized and drops the rest of the
// reply, though the reply's remaining elements are still queued behind the
// synthesis: the reader sees the Interrupt at once.
func TestTTSStageWithInterruption_InterruptStopsTheStreamedReply(t *testing.T) {
	svc := newSegmentRecordingTTS()
	started := make(chan struct{})
	svc.synth = func(ctx context.Context, n int, _ string) (io.ReadCloser, error) {
		if n == 1 {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return io.NopCloser(bytes.NewReader(make([]byte, 320))), nil
	}
	r := startReplyRun(t, newStreamedReplyStage(svc))

	r.send(t, delta("One. "))
	awaitSignal(t, started, "the first sentence was never synthesized")
	r.send(t, delta("Two. Three. "), assistant("One. Two. Three."), stage.NewEndOfTurnElement(),
		stage.NewInterruptElement())
	got := r.finish(t)

	assert.Equal(t, []string{"One."}, svc.spoken(), "nothing after the barge-in is synthesized")
	assert.Equal(t, []string{
		"delta:One. ", "delta:Two. Three. ", "message:", "end", "interrupt",
	}, kinds(got), "the caller heard none of the reply, so its Message records none of it")
	msg := got[2].Message
	assert.Equal(t, float64(-1), ttsCostUSD(msg), "a canceled segment is not priced")
	assert.Equal(t, types.FinishReasonInterrupted, msg.FinishReason)
	assert.Equal(t, types.PlayoutCutBargeIn, msg.Meta[types.MetaPlayoutCut])
	assert.Equal(t, int64(0), msg.Meta[types.MetaHeardMs])
}

// blockAfterReader yields first, then blocks until ctx ends.
type blockAfterReader struct {
	ctx   context.Context
	first []byte
	err   error
}

func (r *blockAfterReader) Read(p []byte) (int, error) {
	if len(r.first) > 0 {
		n := copy(p, r.first)
		r.first = r.first[n:]
		return n, nil
	}
	if r.err != nil {
		return 0, r.err
	}
	<-r.ctx.Done()
	return 0, r.ctx.Err()
}

// A barge-in during a segment's audio stops it after the piece already
// delivered; the cut segment is not priced.
func TestTTSStageWithInterruption_InterruptMidSegmentStopsItsAudio(t *testing.T) {
	svc := newSegmentRecordingTTS()
	svc.synth = func(ctx context.Context, _ int, _ string) (io.ReadCloser, error) {
		return io.NopCloser(&blockAfterReader{ctx: ctx, first: make([]byte, 3200)}), nil
	}
	r := startReplyRun(t, newStreamedReplyStage(svc))

	r.send(t, delta("A long sentence. "))
	require.Equal(t, []string{"delta:A long sentence. "}, kinds([]stage.StreamElement{r.next(t)}))
	require.NotNil(t, r.next(t).Audio, "the first piece arrives while the segment is still being read")
	r.send(t, assistant("A long sentence."), stage.NewInterruptElement())
	rest := r.finish(t)[2:]

	require.Len(t, rest, 2)
	assert.True(t, rest[1].Interrupt)
	msg := rest[0].Message
	require.NotNil(t, msg)
	assert.Equal(t, float64(-1), ttsCostUSD(msg), "a segment cut short is not priced")
	assert.Equal(t, types.FinishReasonInterrupted, msg.FinishReason)
	assert.True(t, strings.HasPrefix("A long sentence.", msg.Content),
		"the Message records at most what the caller heard: %q", msg.Content)
}

// A read failure mid-segment ends that segment, unpriced, without failing the
// reply: the next segment is still spoken.
func TestTTSStageWithInterruption_ReadFailureEndsOnlyThatSegment(t *testing.T) {
	svc := newSegmentRecordingTTS()
	svc.synth = func(ctx context.Context, n int, _ string) (io.ReadCloser, error) {
		if n == 1 {
			return io.NopCloser(&blockAfterReader{ctx: ctx, first: make([]byte, 320), err: errors.New("reset")}), nil
		}
		return io.NopCloser(bytes.NewReader(make([]byte, 320))), nil
	}
	s := newStreamedReplyStage(svc)

	results := runReply(t, s, []stage.StreamElement{delta("One. Two."), assistant("One. Two.")})

	assert.Equal(t, []string{"One.", "Two."}, svc.spoken())
	msg := results[len(results)-1].Message
	require.NotNil(t, msg)
	assert.NoError(t, results[len(results)-1].Error)
	assert.InDelta(t, float64(len("Two."))*0.0001, ttsCostUSD(msg), 1e-12, "only the segment read in full is priced")
}
