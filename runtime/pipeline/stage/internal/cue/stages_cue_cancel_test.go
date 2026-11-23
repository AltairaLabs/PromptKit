package cue

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runCanceledCue runs a cue stage on in and out, returning what Process
// returned once it ends.
func runCanceledCue(ctx context.Context, cue *CueStage, in chan core.StreamElement, out chan core.StreamElement) <-chan error {
	done := make(chan error, 1)
	go func() { done <- cue.Process(ctx, in, out) }()
	return done
}

// waitProcess waits for a stage's Process to return.
func waitProcess(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		require.FailNow(t, "Process did not return")
		return nil
	}
}

// replyTexts is the content of each message in elems, and whether any of
// them carried a cue's audio.
func replyTexts(elems []core.StreamElement) (texts []string, cueAudio bool) {
	for i := range elems {
		if elems[i].Message != nil {
			texts = append(texts, elems[i].Message.Content)
		}
		if elems[i].Meta.CueAudio != nil {
			cueAudio = true
		}
	}
	return texts, cueAudio
}

// A canceled cue stage still carries on what its upstream delivers after the
// cancellation: the partial reply a canceled provider emits reaches the save
// stage (#2847). It used to return at once and drop it.
func TestCueStage_CancelPassesTheCanceledReplyThrough(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in, out := make(chan core.StreamElement, 4), make(chan core.StreamElement, 8)
	cancel()
	done := runCanceledCue(ctx, NewStage(Config{}), in, out)
	in <- replyEnd("half a")
	close(in)

	assert.ErrorIs(t, waitProcess(t, done), context.Canceled)
	texts, _ := replyTexts(collectCueOutput(out))
	assert.Equal(t, []string{"half a"}, texts)
}

// Canceled while a clip renders, the stage plays no clip, and the reply that
// waited behind it, with what follows, still goes on, in order.
func TestCueStage_CancelWhileRenderingReleasesWhatWaited(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in, out := make(chan core.StreamElement, 4), make(chan core.StreamElement, 8)
	done := runCanceledCue(ctx, stuckRenderCue(t), in, out)

	reply := cueRamp(2400)
	in <- guardrailCueElem(brunoText)
	first := <-out
	require.NotNil(t, first.Text)
	assert.Equal(t, brunoText, *first.Text)
	in <- replyAudioOf(reply)
	require.Eventually(t, func() bool { return len(in) == 0 }, time.Second, time.Millisecond,
		"the reply's audio waits behind the clip")
	cancel()
	in <- replyEnd("half a")
	close(in)

	assert.ErrorIs(t, waitProcess(t, done), context.Canceled)
	got := collectCueOutput(out)
	require.Len(t, got, 2, "the reply's audio, then its end; no clip")
	assert.Equal(t, reply, got[0].Audio.Samples)
	texts, cueAudio := replyTexts(got)
	assert.Equal(t, []string{"half a"}, texts)
	assert.False(t, cueAudio, "no clip plays after the cancellation")
}

// stuckRenderCue is a cue stage whose clip renders never finish, even when
// canceled, so only the stage's own cancellation can end its wait.
func stuckRenderCue(t *testing.T) *CueStage {
	stuck := make(chan struct{})
	t.Cleanup(func() { close(stuck) })
	cue := NewStage(Config{OnDemandBudget: time.Minute})
	cue.render = func(context.Context, string) (Clip, error) {
		<-stuck
		return Clip{}, context.Canceled
	}
	return cue
}

// Canceled after its input ended, while it waits for a clip still rendering,
// the stage plays no clip and still passes on the reply that waited.
func TestCueStage_CancelAfterInputEndsReleasesWhatWaited(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in, out := make(chan core.StreamElement, 4), make(chan core.StreamElement, 8)
	done := runCanceledCue(ctx, stuckRenderCue(t), in, out)

	in <- guardrailCueElem(brunoText)
	<-out
	reply := cueRamp(2400)
	in <- replyAudioOf(reply)
	in <- replyEnd("half a")
	close(in)
	time.Sleep(20 * time.Millisecond) // the stage reaches the end of its input
	cancel()

	assert.ErrorIs(t, waitProcess(t, done), context.Canceled)
	got := collectCueOutput(out)
	require.Len(t, got, 2, "the reply's audio, then its end; no clip")
	assert.Equal(t, reply, got[0].Audio.Samples)
	texts, cueAudio := replyTexts(got)
	assert.Equal(t, []string{"half a"}, texts)
	assert.False(t, cueAudio, "no clip plays after the cancellation")
}

// An element the stage is sending when the cancellation lands is still
// delivered to a reader that takes it, and so is what comes after it.
func TestCueStage_CancelDuringASendKeepsTheElement(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	in, out := make(chan core.StreamElement, 4), make(chan core.StreamElement)
	done := runCanceledCue(ctx, NewStage(Config{}), in, out)

	in <- replyEnd("first")
	// Let the stage block sending "first" to the reader that is not reading.
	require.Eventually(t, func() bool { return len(in) == 0 }, time.Second, time.Millisecond)
	cancel()
	in <- replyEnd("second")
	close(in)

	got := collectCueOutput(out)
	assert.ErrorIs(t, waitProcess(t, done), context.Canceled)
	texts, _ := replyTexts(got)
	assert.Equal(t, []string{"first", "second"}, texts)
}

// collectCueOutput reads out until the stage closes it.
func collectCueOutput(out <-chan core.StreamElement) []core.StreamElement {
	var got []core.StreamElement
	for elem := range out {
		got = append(got, elem)
	}
	return got
}
