package stage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
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

// A sentence is synthesized as soon as its delta completes it, before the rest
// of the reply has arrived; the completed Message is not spoken again, and it
// carries the cost of every segment.
func TestTTSStageWithInterruption_SpeaksStreamedSentencesAsTheyComplete(t *testing.T) {
	svc := newSegmentRecordingTTS()
	s := newStreamedReplyStage(svc)

	input := make(chan stage.StreamElement)
	output := make(chan stage.StreamElement, 32)
	done := make(chan error, 1)
	go func() { done <- s.Process(context.Background(), input, output) }()

	input <- delta("One. ")
	var got []stage.StreamElement
	deadline := time.After(2 * time.Second)
	for len(audioElements(got)) == 0 {
		select {
		case e := <-output:
			got = append(got, e)
		case <-deadline:
			t.Fatal("the first sentence was not spoken before the rest of the reply arrived")
		}
	}
	input <- delta("Two")
	input <- delta(" three.")
	input <- assistant("One. Two three.")
	input <- stage.NewEndOfTurnElement()
	close(input)
	require.NoError(t, <-done)
	for e := range output {
		got = append(got, e)
	}

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

	results := runStage(t, s, []stage.StreamElement{delta("Hello."), assistant("Hello.")}, 2*time.Second)

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

// A guardrail's mid-stream replacement drops what was released but not yet
// spoken and speaks the guardrail's message in its place.
func TestTTSStageWithInterruption_ReplacementDropsUnspokenText(t *testing.T) {
	svc := newSegmentRecordingTTS()
	s := newStreamedReplyStage(svc)

	results := runStage(t, s, []stage.StreamElement{
		delta("Fine. Then a dra"), replacement("Blocked."), assistant("Blocked."), stage.NewEndOfTurnElement(),
	}, 2*time.Second)

	assert.Equal(t, []string{"Fine.", "Blocked."}, svc.spoken())
	assert.Contains(t, kinds(results), "message:Blocked.")
}

// A completed reply whose text is not what streamed (a guardrail replaced it
// after the fact) is spoken in place of what was not yet spoken.
func TestTTSStageWithInterruption_ReplacedMessageIsSpokenInstead(t *testing.T) {
	svc := newSegmentRecordingTTS()
	s := newStreamedReplyStage(svc)

	runStage(t, s, []stage.StreamElement{delta("Hello there. Bad wor"), assistant("Sorry.")}, 2*time.Second)

	assert.Equal(t, []string{"Hello there.", "Sorry."}, svc.spoken())
}

// A completed reply that goes on past the text released speaks the rest.
func TestTTSStageWithInterruption_MessageBeyondReleasedTextSpeaksTheRest(t *testing.T) {
	svc := newSegmentRecordingTTS()
	s := newStreamedReplyStage(svc)

	runStage(t, s, []stage.StreamElement{delta("Hi."), assistant("Hi. More.")}, 2*time.Second)

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
	results := runStage(t, s, []stage.StreamElement{
		delta("Let me check. "), delta("It is sunny."),
		stage.NewMessageElement(first),
		stage.NewMessageElement(&types.Message{Role: "tool", Content: "sunny"}),
		assistant("It is sunny."),
		stage.NewEndOfTurnElement(),
	}, 2*time.Second)

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
	results := runStage(t, s, []stage.StreamElement{
		delta("It is sunny. "),
		{Reasoning: &stage.ReasoningDelta{Text: "thinking"}},
		stage.NewMessageElement(toolCall),
		stage.NewMessageElement(&types.Message{Role: "tool", Content: "sunny"}),
		assistant("It is sunny."),
		stage.NewEndOfTurnElement(),
	}, 2*time.Second)

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
	runStage(t, s, []stage.StreamElement{delta("Unfinished thought"), stage.NewEndOfTurnElement()}, 2*time.Second)
	assert.Equal(t, []string{"Unfinished thought"}, svc.spoken())

	svc = newSegmentRecordingTTS()
	s = newStreamedReplyStage(svc)
	runStage(t, s, []stage.StreamElement{
		delta("Half a thought"), stage.NewErrorElement(errors.New("turn failed")), stage.NewEndOfTurnElement(),
	}, 2*time.Second)
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

	results := runStage(t, s, []stage.StreamElement{
		delta("One. Two. Three. "), assistant("One. Two. Three."), stage.NewEndOfTurnElement(),
	}, 2*time.Second)

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

	results := runStage(t, s, []stage.StreamElement{delta("Lonely words"), stage.NewEndOfTurnElement()}, 2*time.Second)

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
	s := newStreamedReplyStage(svc)

	input := make(chan stage.StreamElement)
	output := make(chan stage.StreamElement, 32)
	done := make(chan error, 1)
	go func() { done <- s.Process(context.Background(), input, output) }()

	input <- delta("One. ")
	<-started
	input <- delta("Two. Three. ")
	input <- assistant("One. Two. Three.")
	input <- stage.NewEndOfTurnElement()
	input <- stage.NewInterruptElement()
	close(input)
	require.NoError(t, <-done)
	var got []stage.StreamElement
	for e := range output {
		got = append(got, e)
	}

	assert.Equal(t, []string{"One."}, svc.spoken(), "nothing after the barge-in is synthesized")
	assert.Equal(t, []string{
		"delta:One. ", "delta:Two. Three. ", "message:One. Two. Three.", "end", "interrupt",
	}, kinds(got))
	assert.Equal(t, float64(-1), ttsCostUSD(got[2].Message), "a canceled segment is not priced")
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
	s := newStreamedReplyStage(svc)

	input := make(chan stage.StreamElement)
	output := make(chan stage.StreamElement, 32)
	done := make(chan error, 1)
	go func() { done <- s.Process(context.Background(), input, output) }()

	input <- delta("A long sentence. ")
	first := <-output // the delta
	require.Equal(t, []string{"delta:A long sentence. "}, kinds([]stage.StreamElement{first}))
	piece := <-output
	require.NotNil(t, piece.Audio, "the first piece arrives while the segment is still being read")
	input <- assistant("A long sentence.")
	input <- stage.NewInterruptElement()
	close(input)
	require.NoError(t, <-done)
	var rest []stage.StreamElement
	for e := range output {
		rest = append(rest, e)
	}

	assert.Equal(t, []string{"message:A long sentence.", "interrupt"}, kinds(rest))
	assert.Equal(t, float64(-1), ttsCostUSD(rest[0].Message), "a segment cut short is not priced")
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

	results := runStage(t, s, []stage.StreamElement{delta("One. Two."), assistant("One. Two.")}, 2*time.Second)

	assert.Equal(t, []string{"One.", "Two."}, svc.spoken())
	msg := results[len(results)-1].Message
	require.NotNil(t, msg)
	assert.NoError(t, results[len(results)-1].Error)
	assert.InDelta(t, float64(len("Two."))*0.0001, ttsCostUSD(msg), 1e-12, "only the segment read in full is priced")
}

// A turn completed while the bot owes a reply interrupts it, even though the
// VAD only ever reported the utterance as Starting.
func TestAudioTurnStage_TurnDuringAPendingReplyInterrupts(t *testing.T) {
	run := func(t *testing.T, engaged, turnBased bool) []stage.StreamElement {
		t.Helper()
		config := stage.DefaultAudioTurnConfig()
		config.EmitEndOfTurn = turnBased
		config.MinSpeechDuration = 50 * time.Millisecond
		config.SilenceDuration = 100 * time.Millisecond
		config.VAD = &mockVADAnalyzer{states: []audio.VADState{
			audio.VADStateStarting, audio.VADStateStarting, audio.VADStateQuiet, audio.VADStateQuiet,
		}}
		handler := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
		if engaged {
			handler.BeginResponse()
		}
		config.InterruptionHandler = handler
		s, err := stage.NewAudioTurnStage(config)
		require.NoError(t, err)
		var inputs []stage.StreamElement
		for i := 0; i < 4; i++ {
			inputs = append(inputs, makeAudioElement(generateTestPCMAudio(3200), 16000))
		}
		return runStage(t, s, inputs, 2*time.Second)
	}

	got := run(t, true, true)
	require.GreaterOrEqual(t, len(got), 2)
	assert.True(t, got[0].Interrupt, "the interrupt goes ahead of the turn's audio")
	assert.NotNil(t, got[1].Audio)

	for _, tc := range []struct {
		name               string
		engaged, turnBased bool
	}{{"bot idle", false, true}, {"not turn-based", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			for _, e := range run(t, tc.engaged, tc.turnBased) {
				assert.False(t, e.Interrupt, "no reply to interrupt")
			}
		})
	}
}
