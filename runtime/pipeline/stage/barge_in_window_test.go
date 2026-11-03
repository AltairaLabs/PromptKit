package stage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// tenMsChunk is 10ms of 16-bit mono PCM at 16kHz.
const tenMsChunk = 320

// bargeInTurnConfig closes a turn after 30ms of silence, timed in audio.
func bargeInTurnConfig(vad audio.VADAnalyzer, h *audio.InterruptionHandler) stage.AudioTurnConfig {
	cfg := stage.DefaultAudioTurnConfig()
	cfg.EmitEndOfTurn = true
	cfg.SilenceDuration = 30 * time.Millisecond
	cfg.MinSpeechDuration = 10 * time.Millisecond
	cfg.VAD = vad
	cfg.InterruptionHandler = h
	return cfg
}

// A barge-in must not throw away the speech that triggered it: the interrupting
// utterance continues as a normal turn, onset included (#2301). The stage used
// to reset its buffer and the VAD on barge-in, so the transcript lost its first
// word(s).
func TestAudioTurnStage_BargeInKeepsInterruptingSpeech(t *testing.T) {
	vad := &mockVADAnalyzer{states: []audio.VADState{
		audio.VADStateQuiet,    // the caller is listening
		audio.VADStateStarting, // speech onset
		audio.VADStateSpeaking, // barge-in fires here
		audio.VADStateSpeaking,
		audio.VADStateQuiet,
		audio.VADStateQuiet,
		audio.VADStateQuiet, // 30ms silence: the interrupting turn ends
	}}
	h := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
	h.BeginResponse() // the bot owes the previous turn a reply

	s, err := stage.NewAudioTurnStage(bargeInTurnConfig(vad, h))
	require.NoError(t, err)

	chunks := make([][]byte, len(vad.states))
	inputs := make([]stage.StreamElement, len(vad.states))
	for i := range chunks {
		chunks[i] = bytes.Repeat([]byte{byte(i + 1)}, tenMsChunk)
		inputs[i] = makeAudioElement(chunks[i], 16000)
	}
	results := runStage(t, s, inputs, 2*time.Second)

	require.NotEmpty(t, results)
	assert.True(t, results[0].Interrupt, "the barge-in is signaled first")
	var turns [][]byte
	sawEndOfTurn := false
	for _, r := range results[1:] {
		if r.Audio != nil {
			turns = append(turns, r.Audio.Samples)
		}
		if r.EndOfTurn {
			sawEndOfTurn = true
		}
	}
	require.Len(t, turns, 1, "the interrupting speech is one turn")
	assert.Equal(t, bytes.Join(chunks, nil), turns[0],
		"the turn carries the speech onset and everything buffered before the barge-in")
	assert.True(t, sawEndOfTurn, "the interrupting turn ends like any other")
}

// Ending a user turn begins the reply the bot owes it, before the turn's audio
// leaves the stage, so speech while the model is still generating is a barge-in
// (#2301).
func TestAudioTurnStage_TurnEndBeginsResponse(t *testing.T) {
	for _, emitEndOfTurn := range []bool{true, false} {
		vad := &mockVADAnalyzer{states: []audio.VADState{
			audio.VADStateSpeaking, audio.VADStateQuiet, audio.VADStateQuiet, audio.VADStateQuiet,
		}}
		h := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
		cfg := bargeInTurnConfig(vad, h)
		cfg.EmitEndOfTurn = emitEndOfTurn
		s, err := stage.NewAudioTurnStage(cfg)
		require.NoError(t, err)

		input := make(chan stage.StreamElement, 8)
		output := make(chan stage.StreamElement) // unbuffered: observe state at each send
		go func() { _ = s.Process(context.Background(), input, output) }()
		for range vad.states {
			input <- makeAudioElement(generateTestPCMAudio(tenMsChunk), 16000)
		}

		select {
		case elem := <-output:
			require.NotNil(t, elem.Audio)
			assert.Equal(t, emitEndOfTurn, h.IsBotSpeaking(),
				"a turn-based stage owes a reply from the moment it emits the turn (EmitEndOfTurn=%v)", emitEndOfTurn)
		case <-time.After(2 * time.Second):
			t.Fatal("the turn was never emitted")
		}
		close(input)
		for range output { //nolint:revive // drain
		}
	}
}

// pcmTTS returns n bytes of 16kHz PCM per synthesis.
func pcmTTS(n int) *mockTTSService {
	return &mockTTSService{
		synthesizeFunc: func(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(make([]byte, n))), nil
		},
	}
}

func ttsWithHandler(svc tts.Service, h *audio.InterruptionHandler) *stage.TTSStageWithInterruption {
	cfg := stage.DefaultTTSStageWithInterruptionConfig()
	cfg.InterruptionHandler = h
	return stage.NewTTSStageWithInterruption(svc, cfg)
}

func assistantElement(text string) stage.StreamElement {
	return stage.NewMessageElement(&types.Message{Role: "assistant", Content: text})
}

// The reply's audio keeps playing after the stage hands it over, so the bot
// stays interruptible until it would have finished, even once its turn is over
// (#2301). It used to stop counting as speaking the moment the audio was
// emitted.
func TestTTSStageWithInterruption_PlaybackKeepsBotSpeaking(t *testing.T) {
	const playback = 400 * time.Millisecond
	h := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
	h.BeginResponse()
	s := ttsWithHandler(pcmTTS(int(playback.Seconds()*16000)*2), h) // mock rate is 16kHz

	input := make(chan stage.StreamElement, 4)
	output := make(chan stage.StreamElement, 8)
	go func() { _ = s.Process(context.Background(), input, output) }()

	start := time.Now()
	input <- assistantElement("a reply")
	input <- stage.NewEndOfTurnElement()
	for elem := range output {
		if elem.EndOfTurn {
			break
		}
	}
	if time.Since(start) < playback/2 {
		assert.True(t, h.IsBotSpeaking(), "the reply's audio is still playing after its turn ended")
	}
	assert.Eventually(t, func() bool { return !h.IsBotSpeaking() }, 3*time.Second, 5*time.Millisecond,
		"the bot stops counting as speaking once playback would have finished")
	close(input)
}

// Between the reply's pieces (synthesis done, more of the turn still coming)
// the bot is still responding.
func TestTTSStageWithInterruption_StaysRespondingUntilEndOfTurn(t *testing.T) {
	h := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
	h.BeginResponse()
	s := ttsWithHandler(pcmTTS(2), h) // 1 sample: playback is negligible

	input := make(chan stage.StreamElement, 4)
	output := make(chan stage.StreamElement, 8)
	go func() { _ = s.Process(context.Background(), input, output) }()

	input <- assistantElement("first part")
	<-output
	time.Sleep(5 * time.Millisecond) // well past the 1-sample playback
	assert.True(t, h.IsBotSpeaking(), "the turn is not over until its EndOfTurn")

	input <- stage.NewEndOfTurnElement()
	<-output
	assert.False(t, h.IsBotSpeaking(), "the EndOfTurn ends the reply")
	close(input)
}

// A turn that fails produces no EndOfTurn, only an error; the reply ends there.
func TestTTSStageWithInterruption_ErrorEndsResponse(t *testing.T) {
	h := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
	h.BeginResponse()
	s := ttsWithHandler(pcmTTS(2), h)

	input := make(chan stage.StreamElement, 1)
	output := make(chan stage.StreamElement, 2)
	go func() { _ = s.Process(context.Background(), input, output) }()

	input <- stage.NewErrorElement(errors.New("provider failed"))
	elem := <-output
	require.Error(t, elem.Error)
	assert.False(t, h.IsBotSpeaking(), "no reply follows a failed turn")
	close(input)
}

// A user turn that transcribes to nothing gets no reply, so the reply the
// AudioTurnStage began for it ends at the STT stage. Otherwise the next thing
// the caller says would be taken as a barge-in on a reply that never comes.
func TestSTTStage_TurnWithoutTranscriptEndsResponse(t *testing.T) {
	cases := map[string]struct {
		text  string
		err   error
		audio int
	}{
		"empty transcript": {text: "  ", audio: 3200},
		"too short":        {text: "hi", audio: 10},
		"stt error":        {err: errors.New("stt down"), audio: 3200},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
			h.BeginResponse()
			cfg := stage.DefaultSTTStageConfig()
			cfg.Retry.MaxAttempts = 1
			cfg.InterruptionHandler = h
			s := stage.NewSTTStage(&mockSTTService{
				transcribeFunc: func(context.Context, base.STTRequest) (base.STTResponse, error) {
					return base.STTResponse{Text: tc.text}, tc.err
				},
			}, cfg)

			runStage(t, s, []stage.StreamElement{makeAudioElement(make([]byte, tc.audio), 16000)}, 2*time.Second)
			assert.False(t, h.IsBotSpeaking(), "no reply follows a turn without a transcript")
		})
	}

	// A transcribed turn keeps its reply pending.
	h := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
	h.BeginResponse()
	cfg := stage.DefaultSTTStageConfig()
	cfg.InterruptionHandler = h
	s := stage.NewSTTStage(&mockSTTService{
		transcribeFunc: func(context.Context, base.STTRequest) (base.STTResponse, error) {
			return base.STTResponse{Text: "hello"}, nil
		},
	}, cfg)
	runStage(t, s, []stage.StreamElement{makeAudioElement(make([]byte, 3200), 16000)}, 2*time.Second)
	assert.True(t, h.IsBotSpeaking(), "a transcribed turn is still owed its reply")
}
