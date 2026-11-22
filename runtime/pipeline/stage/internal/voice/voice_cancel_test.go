package voice_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/voice"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// cancelingVAD is a VAD that reports silence and cancels the stage's context
// the first time it analyzes audio, so a test knows the context ends while
// the stage is handling that audio, not before it reads it.
func cancelingVAD(cancel context.CancelFunc) *mockVADAnalyzer {
	return &mockVADAnalyzer{analyzeFunc: func(context.Context, []byte) (float64, error) {
		cancel()
		return 0.1, nil
	}}
}

// drained is every element left on output once it closes.
func drained(output <-chan core.StreamElement) []core.StreamElement {
	var got []core.StreamElement
	for e := range output {
		got = append(got, e)
	}
	return got
}

// An AudioTurnStage whose context ends while it holds a turn's audio, with no
// one reading its output, returns the context's error rather than blocking
// or reporting success.
func TestAudioTurnStage_ContextEndsWhileTheTurnIsEmitted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	config := voice.DefaultAudioTurnConfig()
	config.SilenceDuration = 10 * time.Second
	config.VAD = cancelingVAD(cancel)
	s, err := voice.NewAudioTurnStage(config)
	require.NoError(t, err)

	input := make(chan core.StreamElement, 1)
	input <- makeAudioElement(generateTestPCMAudio(3200), 16000) // 100 ms, above the minimum
	output := make(chan core.StreamElement)                      // never read while Process runs

	errCh := make(chan error, 1)
	go func() { errCh <- s.Process(ctx, input, output) }()

	select {
	case err := <-errCh:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Process did not return after its context ended")
	}
	assert.Empty(t, drained(output))
}

// A ResponseVADStage whose context ends while it forwards audio, with no one
// reading its output, returns the context's error.
func TestResponseVADStage_ContextEndsWhileForwarding(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	config := voice.DefaultResponseVADConfig()
	config.VAD = cancelingVAD(cancel)
	s, err := voice.NewResponseVADStage(config)
	require.NoError(t, err)

	input := make(chan core.StreamElement, 1)
	input <- makeAudioElement(generateTestPCMAudio(480), 24000)
	output := make(chan core.StreamElement) // never read while Process runs

	errCh := make(chan error, 1)
	go func() { errCh <- s.Process(ctx, input, output) }()

	select {
	case err := <-errCh:
		assert.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("Process did not return after its context ended")
	}
	assert.Empty(t, drained(output))
}

// Audio that arrives after a held EndOfStream, once the silence has lasted
// long enough, releases the EndOfStream right after it, ahead of the audio
// that follows, and the stage starts the next turn afresh: the input closing
// then has nothing more to release.
func TestResponseVADStage_SilenceAfterAudioReleasesTheHeldEndOfStream(t *testing.T) {
	config := voice.DefaultResponseVADConfig()
	config.SilenceDuration = time.Nanosecond
	config.MaxWaitDuration = time.Minute
	config.VAD = &mockVADAnalyzer{} // always quiet
	s, err := voice.NewResponseVADStage(config)
	require.NoError(t, err)

	// All are queued before Process starts, so it reads them back to back,
	// with no idle check between them.
	end := core.NewEndOfStreamElement()
	end.Message = &types.Message{Role: "assistant", Content: "done"}
	end.Sequence = 1
	first := makeAudioElement(generateTestPCMAudio(480), 24000)
	first.Sequence = 2
	next := makeAudioElement(generateTestPCMAudio(480), 24000)
	next.Sequence = 3
	input := make(chan core.StreamElement, 3)
	input <- end
	input <- first
	input <- next
	close(input)
	output := make(chan core.StreamElement, 4)

	require.NoError(t, s.Process(context.Background(), input, output))
	var seqs []int64
	for _, e := range drained(output) {
		seqs = append(seqs, e.Sequence)
	}
	assert.Equal(t, []int64{2, 1, 3}, seqs, "the first audio, the EndOfStream it released, then the next audio")
}

// dataAfterCancelReader blocks until ctx ends and then yields data, as a
// service whose response was already in flight when the synthesis was
// canceled does.
type dataAfterCancelReader struct {
	ctx  context.Context
	data []byte
}

func (r *dataAfterCancelReader) Read(p []byte) (int, error) {
	<-r.ctx.Done()
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// Audio a canceled synthesis still delivers is not handed on: the barge-in
// that canceled it stopped the reply.
func TestTTSStageWithInterruption_AudioArrivingAfterABargeInIsDropped(t *testing.T) {
	svc := newSegmentRecordingTTS()
	svc.synth = func(ctx context.Context, _ int, _ string) (io.ReadCloser, error) {
		return io.NopCloser(&dataAfterCancelReader{ctx: ctx, data: make([]byte, 3200)}), nil
	}
	r := startReplyRun(t, newStreamedReplyStage(svc))

	r.send(t, delta("A long sentence. "))
	require.Equal(t, []string{"delta:A long sentence. "}, kinds([]core.StreamElement{r.next(t)}))
	r.send(t, core.NewInterruptElement())

	rest := r.finish(t)[1:]
	for _, e := range rest {
		assert.Nil(t, e.Audio, "no audio after the barge-in")
	}
	require.NotEmpty(t, rest)
	assert.True(t, rest[len(rest)-1].Interrupt)
}

// A ResponseVADStage whose input closes while it holds an EndOfStream, and
// whose context has ended with no one reading its output, returns the
// context's error. Which of the two it sees first is the runtime's choice, so
// the run is repeated 100 times, which takes the path through the held
// EndOfStream all but certainly; either way the error is the context's and
// nothing is emitted.
func TestResponseVADStage_ContextEndedWhenTheInputClosesOnAHeldEndOfStream(t *testing.T) {
	config := voice.DefaultResponseVADConfig()
	config.SilenceDuration = time.Minute
	config.MaxWaitDuration = time.Minute
	config.VAD = &mockVADAnalyzer{}
	s, err := voice.NewResponseVADStage(config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range 100 {
		input := make(chan core.StreamElement, 1)
		input <- core.NewEndOfStreamElement()
		close(input)
		output := make(chan core.StreamElement)
		assert.ErrorIs(t, s.Process(ctx, input, output), context.Canceled)
		assert.Empty(t, drained(output))
	}
}
