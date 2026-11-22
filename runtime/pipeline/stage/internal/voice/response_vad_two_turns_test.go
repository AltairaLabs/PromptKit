package voice_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/voice"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// turnEnd is a turn's EndOfStream carrying its reply.
func turnEnd(reply string) core.StreamElement {
	e := core.NewEndOfStreamElement()
	e.Message = &types.Message{Role: "assistant", Content: reply}
	return e
}

// A second turn's EndOfStream that arrives while the first turn's is still
// held (its audio's silence not yet confirmed) does not replace it: the
// caller heard both replies, so both reach the transcript, in the order they
// were spoken, and each turn ends once.
func TestResponseVADStage_ASecondTurnsEndDoesNotDropTheFirst(t *testing.T) {
	config := voice.DefaultResponseVADConfig()
	config.SilenceDuration = time.Minute // never confirmed: the audio keeps playing
	config.MaxWaitDuration = 20 * time.Millisecond
	speaking := make([]audio.VADState, 10)
	for i := range speaking {
		speaking[i] = audio.VADStateSpeaking
	}
	config.VAD = &mockVADAnalyzer{states: speaking}
	s, err := voice.NewResponseVADStage(config)
	require.NoError(t, err)
	voice.SetResponseVADCheckInterval(s, time.Millisecond)

	input := make(chan core.StreamElement, 4)
	output := make(chan core.StreamElement, 8)
	done := make(chan error, 1)
	go func() { done <- s.Process(context.Background(), input, output) }()

	input <- makeAudioElement(generateTestPCMAudio(480), 24000)
	input <- turnEnd("First reply.")
	input <- makeAudioElement(generateTestPCMAudio(480), 24000)
	input <- turnEnd("Second reply.")

	var got []string
	closed := false
	deadline := time.After(2 * time.Second)
	for {
		select {
		case e, ok := <-output:
			if !ok {
				require.NoError(t, recvWithinErr(t, done))
				assert.Equal(t, []string{"audio", "audio", "end:First reply.", "end:Second reply."}, got)
				return
			}
			switch {
			case e.EndOfStream && e.Message != nil:
				got = append(got, "end:"+e.Message.Content)
			case e.EndOfStream:
				got = append(got, "end")
			case e.Audio != nil:
				got = append(got, "audio")
			default:
				got = append(got, "other")
			}
			if len(got) == 4 && !closed {
				closed = true
				close(input) // both turns are out: nothing more is held
			}
		case <-deadline:
			t.Fatalf("downstream saw %v, want both turns' ends", got)
		}
	}
}

// recvWithinErr receives Process's result, failing the test if it does not
// return.
func recvWithinErr(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("Process did not return")
		return nil
	}
}
