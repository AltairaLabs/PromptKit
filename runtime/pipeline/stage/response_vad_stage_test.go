package stage_test

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewResponseVADStage(t *testing.T) {
	config := stage.DefaultResponseVADConfig()
	s, err := stage.NewResponseVADStage(config)
	require.NoError(t, err)

	assert.Equal(t, stage.StageTypeTransform, s.Type())
	assert.Equal(t, "response_vad", s.Name())
}

func TestNewResponseVADStage_CustomVAD(t *testing.T) {
	config := stage.DefaultResponseVADConfig()
	config.VAD = &mockVADAnalyzer{states: []audio.VADState{audio.VADStateQuiet}}
	s, err := stage.NewResponseVADStage(config)
	require.NoError(t, err)
	require.NotNil(t, s)
}

func TestResponseVADStage_PassesThroughAudio(t *testing.T) {
	config := stage.DefaultResponseVADConfig()
	s, err := stage.NewResponseVADStage(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 10)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	audioData := generateTestPCMAudio(1000)
	input <- stage.StreamElement{
		Audio: &stage.AudioData{
			Samples:    audioData,
			SampleRate: 24000,
			Channels:   1,
			Format:     stage.AudioFormatPCM16,
		},
	}

	select {
	case elem := <-output:
		require.NotNil(t, elem.Audio)
		assert.Equal(t, audioData, elem.Audio.Samples)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Audio should be forwarded immediately")
	}

	close(input)
}

func TestResponseVADStage_PassesThroughText(t *testing.T) {
	config := stage.DefaultResponseVADConfig()
	s, err := stage.NewResponseVADStage(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 10)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	input <- makeTextElement("forwarded text")

	select {
	case elem := <-output:
		require.NotNil(t, elem.Text)
		assert.Equal(t, "forwarded text", *elem.Text)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Text should be forwarded immediately")
	}

	close(input)
}

func TestResponseVADStage_NoEndOfStream_NoHold(t *testing.T) {
	config := stage.DefaultResponseVADConfig()
	s, err := stage.NewResponseVADStage(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 10)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	// Send 3 audio elements without EOS — all should be forwarded immediately
	for i := 0; i < 3; i++ {
		input <- makeAudioElement(generateTestPCMAudio(100), 24000)
	}

	received := 0
	deadline := time.After(200 * time.Millisecond)
loop:
	for {
		select {
		case elem := <-output:
			if elem.Audio != nil {
				received++
			}
			if received == 3 {
				break loop
			}
		case <-deadline:
			break loop
		}
	}

	assert.Equal(t, 3, received, "All 3 audio elements should be forwarded immediately")
	close(input)
}

func TestResponseVADStage_DelaysEndOfStream(t *testing.T) {
	config := stage.DefaultResponseVADConfig()
	config.SilenceDuration = 200 * time.Millisecond
	config.MaxWaitDuration = 2 * time.Second

	// VAD reports speaking
	mockVAD := &mockVADAnalyzer{
		states: []audio.VADState{audio.VADStateSpeaking, audio.VADStateSpeaking, audio.VADStateSpeaking},
	}
	config.VAD = mockVAD

	s, err := stage.NewResponseVADStage(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 10)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	// Send EOS first
	input <- makeEndOfStreamElement()

	// Then send audio
	input <- makeAudioElement(generateTestPCMAudio(100), 24000)

	// Audio should arrive
	select {
	case elem := <-output:
		require.NotNil(t, elem.Audio, "Audio should arrive")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Audio should be forwarded")
	}

	// EOS should NOT arrive immediately (VAD speaking)
	select {
	case elem := <-output:
		if elem.EndOfStream {
			t.Error("EOS should be delayed when VAD is speaking")
		}
	case <-time.After(50 * time.Millisecond):
		// Expected - EOS is held
	}

	close(input)
}

func TestResponseVADStage_EmitsEndOfStream_AfterSilence(t *testing.T) {
	config := stage.DefaultResponseVADConfig()
	config.SilenceDuration = 50 * time.Millisecond
	config.MaxWaitDuration = 2 * time.Second

	// VAD reports quiet
	mockVAD := &mockVADAnalyzer{
		states: []audio.VADState{audio.VADStateQuiet},
	}
	config.VAD = mockVAD

	s, err := stage.NewResponseVADStage(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 10)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	input <- makeEndOfStreamElement()

	// EOS should arrive after silence duration (~50ms + polling interval)
	select {
	case elem := <-output:
		assert.True(t, elem.EndOfStream, "Should receive EndOfStream after silence")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("EOS should be emitted after silence duration")
	}

	close(input)
}

func TestResponseVADStage_EmitsEndOfStream_MaxWait(t *testing.T) {
	config := stage.DefaultResponseVADConfig()
	config.SilenceDuration = 10 * time.Second // Won't trigger
	config.MaxWaitDuration = 150 * time.Millisecond

	// VAD always speaking
	mockVAD := &mockVADAnalyzer{}
	mockVAD.analyzeFunc = func(_ context.Context, _ []byte) (float64, error) {
		return 0.9, nil // Always speaking
	}
	mockVAD.currentState = audio.VADStateSpeaking
	config.VAD = mockVAD

	s, err := stage.NewResponseVADStage(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 20)
	output := make(chan stage.StreamElement, 20)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	// Send EOS
	input <- makeEndOfStreamElement()

	// Keep sending audio after EOS. Use a done channel so we can stop the
	// goroutine cleanly before closing input (to avoid a send-on-closed panic).
	audioDone := make(chan struct{})
	go func() {
		defer close(audioDone)
		for i := 0; i < 20; i++ {
			select {
			case input <- makeAudioElement(generateTestPCMAudio(100), 24000):
			case <-ctx.Done():
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// EOS should arrive after MaxWaitDuration
	start := time.Now()
	var eosReceived bool
	deadline := time.After(500 * time.Millisecond)
collect:
	for {
		select {
		case elem := <-output:
			if elem.EndOfStream {
				eosReceived = true
				break collect
			}
		case <-deadline:
			break collect
		}
	}

	assert.True(t, eosReceived, "EOS should be emitted after max wait")
	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, 100*time.Millisecond, "EOS should wait at least MaxWaitDuration")

	// Stop the audio goroutine and wait for it to finish before closing input.
	cancel()
	<-audioDone
	close(input)
}

func TestResponseVADStage_InputClosed_EmitsPendingEndOfStream(t *testing.T) {
	config := stage.DefaultResponseVADConfig()
	config.SilenceDuration = 10 * time.Second // Won't trigger
	config.MaxWaitDuration = 10 * time.Second // Won't trigger

	// VAD speaking
	mockVAD := &mockVADAnalyzer{
		states: []audio.VADState{audio.VADStateSpeaking},
	}
	config.VAD = mockVAD

	s, err := stage.NewResponseVADStage(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 10)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	// Send EOS then close input
	input <- makeEndOfStreamElement()
	close(input)

	// EOS should be flushed when input closes
	select {
	case elem := <-output:
		assert.True(t, elem.EndOfStream, "Pending EOS should be flushed when input closes")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("EOS should have been flushed when input closed")
	}
}

func TestResponseVADStage_PointerAliasingBug(t *testing.T) {
	config := stage.DefaultResponseVADConfig()
	config.SilenceDuration = 100 * time.Millisecond
	config.MaxWaitDuration = 500 * time.Millisecond

	s, err := stage.NewResponseVADStage(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 10)
	output := make(chan stage.StreamElement, 10)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	// Send EndOfStream with Message
	msgText := "Expected response text"
	msg := &types.Message{Role: "assistant", Content: msgText}
	endOfStreamElem := stage.StreamElement{
		Message:     msg,
		EndOfStream: true,
		Metadata:    map[string]interface{}{"important_data": "should_be_preserved"},
	}
	input <- endOfStreamElem

	// Send 3 audio elements after EOS (like Gemini sending audio after turnComplete)
	for i := 0; i < 3; i++ {
		input <- makeAudioElement(generateTestPCMAudio(100), 24000)
	}

	// Collect outputs
	var endOfStreamReceived *stage.StreamElement
	var audioCount int
	timeout := time.After(1 * time.Second)

collectLoop:
	for {
		select {
		case elem, ok := <-output:
			if !ok {
				break collectLoop
			}
			if elem.EndOfStream {
				endOfStreamReceived = &elem
				break collectLoop
			}
			if elem.Audio != nil {
				audioCount++
			}
		case <-timeout:
			break collectLoop
		}
	}

	close(input)

	require.NotNil(t, endOfStreamReceived, "EndOfStream should have been received")
	require.NotNil(t, endOfStreamReceived.Message, "Message should be preserved in EndOfStream element")
	assert.Equal(t, msgText, endOfStreamReceived.Message.Content, "Message content should be preserved")
	assert.Equal(t, "assistant", endOfStreamReceived.Message.Role, "Message role should be preserved")
	assert.Equal(t, 3, audioCount, "All audio elements should have been forwarded")
}

func TestResponseVADStage_MultipleTurns(t *testing.T) {
	config := stage.DefaultResponseVADConfig()
	config.SilenceDuration = 50 * time.Millisecond
	config.MaxWaitDuration = 200 * time.Millisecond

	s, err := stage.NewResponseVADStage(config)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement, 20)
	output := make(chan stage.StreamElement, 20)

	go func() {
		_ = s.Process(ctx, input, output)
	}()

	// Turn 1: audio + EOS with Message
	input <- makeAudioElement(generateTestPCMAudio(100), 24000)
	turn1Msg := &types.Message{Role: "assistant", Content: "Turn 1 response"}
	input <- stage.StreamElement{Message: turn1Msg, EndOfStream: true}

	var turn1EOS *stage.StreamElement
	timeout := time.After(500 * time.Millisecond)
turn1Loop:
	for {
		select {
		case elem := <-output:
			if elem.EndOfStream {
				turn1EOS = &elem
				break turn1Loop
			}
		case <-timeout:
			break turn1Loop
		}
	}
	require.NotNil(t, turn1EOS, "Turn 1 EndOfStream should be received")
	require.NotNil(t, turn1EOS.Message, "Turn 1 Message should be preserved")
	assert.Equal(t, "Turn 1 response", turn1EOS.Message.Content)

	// Turn 2: audio + EOS with Message
	input <- makeAudioElement(generateTestPCMAudio(100), 24000)
	turn2Msg := &types.Message{Role: "assistant", Content: "Turn 2 response"}
	input <- stage.StreamElement{Message: turn2Msg, EndOfStream: true}

	var turn2EOS *stage.StreamElement
	timeout = time.After(500 * time.Millisecond)
turn2Loop:
	for {
		select {
		case elem := <-output:
			if elem.EndOfStream {
				turn2EOS = &elem
				break turn2Loop
			}
		case <-timeout:
			break turn2Loop
		}
	}
	require.NotNil(t, turn2EOS, "Turn 2 EndOfStream should be received")
	require.NotNil(t, turn2EOS.Message, "Turn 2 Message should be preserved")
	assert.Equal(t, "Turn 2 response", turn2EOS.Message.Content)

	close(input)
}

func TestResponseVADStage_ContextCancellation(t *testing.T) {
	config := stage.DefaultResponseVADConfig()
	config.SilenceDuration = 10 * time.Second
	config.MaxWaitDuration = 10 * time.Second

	s, err := stage.NewResponseVADStage(config)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	input := make(chan stage.StreamElement, 10)
	output := make(chan stage.StreamElement, 10)

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.Process(ctx, input, output)
	}()

	input <- makeEndOfStreamElement()
	cancel()

	select {
	case <-errCh:
		// Process should return
	case <-time.After(2 * time.Second):
		t.Fatal("Process should have returned after context cancellation")
	}
}
