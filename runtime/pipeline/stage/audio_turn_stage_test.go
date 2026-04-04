package stage_test

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAudioTurnStage(t *testing.T) {
	config := stage.DefaultAudioTurnConfig()
	s, err := stage.NewAudioTurnStage(config)
	require.NoError(t, err)

	assert.Equal(t, stage.StageTypeAccumulate, s.Type())
	assert.Equal(t, "audio_turn", s.Name())
}

func TestDefaultAudioTurnConfig(t *testing.T) {
	config := stage.DefaultAudioTurnConfig()

	assert.NotZero(t, config.SilenceDuration, "SilenceDuration should have default value")
	assert.NotZero(t, config.MinSpeechDuration, "MinSpeechDuration should have default value")
	assert.NotZero(t, config.MaxTurnDuration, "MaxTurnDuration should have default value")
	assert.NotZero(t, config.SampleRate, "SampleRate should have default value")
}

func TestAudioTurnStage_PassThroughNonAudio(t *testing.T) {
	config := stage.DefaultAudioTurnConfig()
	s, err := stage.NewAudioTurnStage(config)
	require.NoError(t, err)

	inputs := []stage.StreamElement{
		makeTextElement("Hello, world!"),
	}
	results := runStage(t, s, inputs, 2*time.Second)

	require.Len(t, results, 1)
	require.NotNil(t, results[0].Text)
	assert.Equal(t, "Hello, world!", *results[0].Text)
}

func TestAudioTurnStage_PassthroughMetadata(t *testing.T) {
	config := stage.DefaultAudioTurnConfig()
	s, err := stage.NewAudioTurnStage(config)
	require.NoError(t, err)

	audioData := generateTestPCMAudio(160)
	elem := stage.StreamElement{
		Audio: &stage.AudioData{
			Samples:    audioData,
			SampleRate: 16000,
			Channels:   1,
			Format:     stage.AudioFormatPCM16,
		},
		Metadata: map[string]interface{}{
			"passthrough": true,
		},
	}

	results := runStage(t, s, []stage.StreamElement{elem}, 2*time.Second)

	require.Len(t, results, 1)
	require.NotNil(t, results[0].Audio)
	assert.Equal(t, audioData, results[0].Audio.Samples)
}

func TestAudioTurnStage_AccumulatesAudio(t *testing.T) {
	config := stage.DefaultAudioTurnConfig()
	config.SilenceDuration = 100 * time.Millisecond
	config.MinSpeechDuration = 50 * time.Millisecond

	mockVAD := &mockVADAnalyzer{
		states: []audio.VADState{
			audio.VADStateSpeaking,
			audio.VADStateSpeaking,
			audio.VADStateStopping,
			audio.VADStateQuiet,
			audio.VADStateQuiet,
			audio.VADStateQuiet,
		},
	}
	config.VAD = mockVAD

	s, err := stage.NewAudioTurnStage(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 10)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	for i := 0; i < 6; i++ {
		input <- makeAudioElement(generateTestPCMAudio(160), 16000)
		time.Sleep(20 * time.Millisecond)
	}
	close(input)

	select {
	case elem := <-output:
		require.NotNil(t, elem.Audio, "Expected audio output")
		assert.NotEmpty(t, elem.Audio.Samples, "Expected non-empty audio samples")
		assert.NotNil(t, elem.Metadata, "Expected metadata with turn_complete")
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for accumulated audio output")
	}
}

func TestAudioTurnStage_MinSpeechDuration(t *testing.T) {
	config := stage.DefaultAudioTurnConfig()
	config.SilenceDuration = 50 * time.Millisecond
	config.MinSpeechDuration = 500 * time.Millisecond // Long min speech - won't complete via shouldCompleteTurn

	mockVAD := &mockVADAnalyzer{
		states: []audio.VADState{
			audio.VADStateSpeaking,
			audio.VADStateStopping,
			audio.VADStateQuiet,
			audio.VADStateQuiet,
		},
	}
	config.VAD = mockVAD

	s, err := stage.NewAudioTurnStage(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 10)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	// Send chunks instantly (no delay) - turn should NOT complete
	for i := 0; i < 4; i++ {
		input <- makeAudioElement(generateTestPCMAudio(160), 16000)
	}
	close(input)

	// Audio should arrive via emitRemainingAudio on stream close
	select {
	case elem := <-output:
		require.NotNil(t, elem.Audio, "Expected audio emitted on stream close")
		assert.NotEmpty(t, elem.Audio.Samples)
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for audio output on stream close")
	}
}

func TestAudioTurnStage_MaxTurnDuration(t *testing.T) {
	config := stage.DefaultAudioTurnConfig()
	config.MaxTurnDuration = 100 * time.Millisecond
	config.SilenceDuration = 10 * time.Second // Won't trigger

	mockVAD := &mockVADAnalyzer{
		states: []audio.VADState{
			audio.VADStateSpeaking,
			audio.VADStateSpeaking,
			audio.VADStateSpeaking,
			audio.VADStateSpeaking,
			audio.VADStateSpeaking,
			audio.VADStateSpeaking,
			audio.VADStateSpeaking,
			audio.VADStateSpeaking,
		},
	}
	config.VAD = mockVAD

	s, err := stage.NewAudioTurnStage(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 20)
	output := make(chan stage.StreamElement, 20)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	// Send 8 chunks with 20ms delay each (160ms > 100ms max)
	for i := 0; i < 8; i++ {
		input <- makeAudioElement(generateTestPCMAudio(160), 16000)
		time.Sleep(20 * time.Millisecond)
	}
	close(input)

	select {
	case elem := <-output:
		require.NotNil(t, elem.Audio, "Expected audio output from max turn duration")
		assert.NotEmpty(t, elem.Audio.Samples)
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for max-turn-duration output")
	}
}

func TestAudioTurnStage_EndOfStream_EmitsBufferedAudio(t *testing.T) {
	config := stage.DefaultAudioTurnConfig()
	config.SilenceDuration = 10 * time.Second // Won't trigger via silence

	mockVAD := &mockVADAnalyzer{
		states: []audio.VADState{
			audio.VADStateSpeaking,
			audio.VADStateSpeaking,
		},
	}
	config.VAD = mockVAD

	s, err := stage.NewAudioTurnStage(config)
	require.NoError(t, err)

	inputs := []stage.StreamElement{
		makeAudioElement(generateTestPCMAudio(160), 16000),
		makeAudioElement(generateTestPCMAudio(160), 16000),
		makeEndOfStreamElement(),
	}

	results := runStage(t, s, inputs, 2*time.Second)

	// Should have audio then EndOfStream
	var audioElems, eosElems int
	for _, r := range results {
		if r.Audio != nil {
			audioElems++
		}
		if r.EndOfStream {
			eosElems++
		}
	}
	assert.Equal(t, 1, audioElems, "Expected 1 accumulated audio element before EOS")
	assert.Equal(t, 1, eosElems, "Expected 1 EndOfStream element")

	// Audio should come before EOS
	if len(results) >= 2 {
		assert.NotNil(t, results[0].Audio, "Audio should come before EOS")
		assert.True(t, results[len(results)-1].EndOfStream, "EOS should be last")
	}
}

func TestAudioTurnStage_EndOfStream_EmptyBuffer(t *testing.T) {
	config := stage.DefaultAudioTurnConfig()
	s, err := stage.NewAudioTurnStage(config)
	require.NoError(t, err)

	inputs := []stage.StreamElement{
		makeEndOfStreamElement(),
	}

	results := runStage(t, s, inputs, 2*time.Second)

	// Only EndOfStream should be forwarded; no audio
	require.Len(t, results, 1)
	assert.True(t, results[0].EndOfStream)
	assert.Nil(t, results[0].Audio)
}

func TestAudioTurnStage_TurnDetector_Integration(t *testing.T) {
	config := stage.DefaultAudioTurnConfig()
	config.SilenceDuration = 10 * time.Second // Won't trigger via silence

	mockVAD := &mockVADAnalyzer{
		states: []audio.VADState{
			audio.VADStateSpeaking,
			audio.VADStateSpeaking,
			audio.VADStateSpeaking,
		},
	}
	config.VAD = mockVAD

	td := &mockTurnDetector{isUserSpeakingVal: true}
	processAudioCalled := 0
	td.processAudioFunc = func(ctx context.Context, audioData []byte) (bool, error) {
		processAudioCalled++
		return false, nil
	}
	config.TurnDetector = td

	s, err := stage.NewAudioTurnStage(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 10)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	// Send 2 chunks with delay
	input <- makeAudioElement(generateTestPCMAudio(160), 16000)
	time.Sleep(20 * time.Millisecond)
	input <- makeAudioElement(generateTestPCMAudio(160), 16000)
	time.Sleep(20 * time.Millisecond)

	// Flip to not speaking
	td.isUserSpeakingVal = false

	// Send 3rd chunk
	input <- makeAudioElement(generateTestPCMAudio(160), 16000)
	close(input)

	// Should complete the turn
	select {
	case elem := <-output:
		require.NotNil(t, elem.Audio, "Expected audio output from turn detector integration")
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for turn completion via TurnDetector")
	}

	assert.Greater(t, processAudioCalled, 0, "processAudioFunc should have been called")
}

func TestAudioTurnStage_Interruption_ResetsState(t *testing.T) {
	config := stage.DefaultAudioTurnConfig()
	config.SilenceDuration = 10 * time.Second // Won't trigger

	mockVAD := &mockVADAnalyzer{
		states: []audio.VADState{
			audio.VADStateSpeaking,
			audio.VADStateSpeaking,
			audio.VADStateSpeaking,
		},
	}
	config.VAD = mockVAD

	handler := audio.NewInterruptionHandler(audio.InterruptionImmediate, nil)
	handler.SetBotSpeaking(true)
	config.InterruptionHandler = handler

	s, err := stage.NewAudioTurnStage(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 10)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	// First chunk - triggers interruption (bot speaking + user speaking VAD state)
	input <- makeAudioElement(generateTestPCMAudio(160), 16000)
	time.Sleep(20 * time.Millisecond)

	// Reset the handler to clear the interruption
	handler.Reset()

	// Send another chunk and close
	input <- makeAudioElement(generateTestPCMAudio(160), 16000)
	close(input)

	// Expect post-interruption audio (only the chunk after reset)
	select {
	case elem := <-output:
		require.NotNil(t, elem.Audio, "Expected post-interruption audio")
		// Should be ~160 bytes (1 chunk post-reset)
		assert.Equal(t, 160, len(elem.Audio.Samples))
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for post-interruption audio")
	}
}

func TestAudioTurnStage_MultipleTurns(t *testing.T) {
	config := stage.DefaultAudioTurnConfig()
	// SilenceDuration=30ms. Turn completion fires when a new chunk arrives after
	// the silence timer has run for ≥30ms. We send the Stopping chunk then wait
	// 35ms before sending the next chunk so shouldCompleteTurn fires on that chunk.
	config.SilenceDuration = 30 * time.Millisecond
	config.MinSpeechDuration = 10 * time.Millisecond

	// VAD sequence: turn1 = [Speaking, Stopping, Quiet], turn2 = [Speaking, Stopping, Quiet]
	// State reset between turns does not reset the mock VAD index.
	// Turn 1 consumes indices 0-2; turn 2 consumes indices 3-5.
	mockVAD := &mockVADAnalyzer{
		states: []audio.VADState{
			audio.VADStateSpeaking, // T1 chunk1
			audio.VADStateStopping, // T1 chunk2 — silenceStart set here
			audio.VADStateQuiet,    // T1 chunk3 — shouldCompleteTurn fires (silence ≥ 30ms after wait)
			audio.VADStateSpeaking, // T2 chunk4 — speechDetected=true for turn 2
			audio.VADStateStopping, // T2 chunk5 — silenceStart set for turn 2
			audio.VADStateQuiet,    // T2 chunk6 — shouldCompleteTurn fires for turn 2
		},
	}
	config.VAD = mockVAD

	s, err := stage.NewAudioTurnStage(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 20)
	output := make(chan stage.StreamElement, 20)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	// Turn 1:
	input <- makeAudioElement(generateTestPCMAudio(160), 16000) // chunk1: Speaking
	time.Sleep(20 * time.Millisecond)
	input <- makeAudioElement(generateTestPCMAudio(160), 16000) // chunk2: Stopping, silenceStart set
	time.Sleep(35 * time.Millisecond)                           // wait for SilenceDuration to expire
	input <- makeAudioElement(generateTestPCMAudio(160), 16000) // chunk3: Quiet, shouldCompleteTurn=true

	// Wait for first turn output
	var turn1 *stage.StreamElement
	select {
	case elem := <-output:
		turn1 = &elem
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for turn 1 output")
	}
	require.NotNil(t, turn1.Audio, "Turn 1 should produce audio")

	// Turn 2 (same timing pattern):
	input <- makeAudioElement(generateTestPCMAudio(160), 16000) // chunk4: Speaking
	time.Sleep(20 * time.Millisecond)
	input <- makeAudioElement(generateTestPCMAudio(160), 16000) // chunk5: Stopping, silenceStart set
	time.Sleep(35 * time.Millisecond)                           // wait for SilenceDuration to expire
	input <- makeAudioElement(generateTestPCMAudio(160), 16000) // chunk6: Quiet, shouldCompleteTurn=true
	close(input)

	// Wait for second turn output
	var turn2 *stage.StreamElement
	select {
	case elem := <-output:
		turn2 = &elem
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for turn 2 output")
	}
	require.NotNil(t, turn2.Audio, "Turn 2 should produce audio")
}

func TestAudioTurnStage_ContextCancellation(t *testing.T) {
	config := stage.DefaultAudioTurnConfig()
	config.SilenceDuration = 10 * time.Second
	s, err := stage.NewAudioTurnStage(config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	input := make(chan stage.StreamElement, 10)
	output := make(chan stage.StreamElement, 10)

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Process(ctx, input, output)
	}()

	input <- makeAudioElement(generateTestPCMAudio(160), 16000)
	cancel()
	close(input)

	select {
	case <-errCh:
		// Process should return after context cancellation or input close
	case <-time.After(2 * time.Second):
		t.Fatal("Process should have returned after context cancellation")
	}
}
