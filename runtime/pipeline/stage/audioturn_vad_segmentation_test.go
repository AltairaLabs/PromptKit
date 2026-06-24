package stage_test

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// vadSpeechPCM returns `samples` of 16-bit PCM sine at ~0.106 RMS (amplitude
// 0.15), matching measured real speech levels (recorded greeting/question PCM
// sit ~0.10 RMS). The energy SimpleVAD treats this as speech.
func vadSpeechPCM(samples int) []byte {
	b := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		v := int16(0.15 * 32767 * math.Sin(float64(i)*0.2))
		binary.LittleEndian.PutUint16(b[i*2:], uint16(v))
	}
	return b
}

// vadSilencePCM returns `samples` of 16-bit PCM zeros (silence).
func vadSilencePCM(samples int) []byte { return make([]byte, samples*2) }

// TestAudioTurnStage_SegmentsTwoUtterancesOnSilence is the fair VAD test: a clip
// that just ENDS proves nothing (the turn is emitted at EndOfStream regardless
// of the VAD). This feeds speech → silence → speech → silence through the REAL
// AudioTurnStage + SimpleVAD and asserts it segments into TWO turns — i.e. the
// VAD detected speech and used the silence to close a turn mid-stream.
//
// AudioTurnStage times silence by WALL-CLOCK (time.Since), so the audio MUST be
// fed in real time (100 ms per 100 ms chunk) for the silence gap to elapse — an
// instant feed would never accumulate silence and would emit a single dump at
// EndOfStream. A miscalibrated/broken VAD (the pre-recalibration state) never
// detects speech and yields ONE turn, failing this test. No live keys, no STT.
func TestAudioTurnStage_SegmentsTwoUtterancesOnSilence(t *testing.T) {
	s, err := stage.NewAudioTurnStage(stage.DefaultAudioTurnConfig())
	if err != nil {
		t.Fatalf("NewAudioTurnStage: %v", err)
	}

	const chunkSamples = 1600 // 100 ms @ 16 kHz
	const chunkDur = 100 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement)
	output := make(chan stage.StreamElement, 64)
	go func() {
		// Process closes output itself when input is drained.
		_ = s.Process(ctx, input, output)
	}()

	// Real-time paced feed: two utterances separated by >0.8 s silence (default
	// SilenceDuration), so the VAD must close turn 1 on silence before turn 2.
	feed := func(gen func(int) []byte, chunks int) {
		for i := 0; i < chunks; i++ {
			input <- makeAudioElement(gen(chunkSamples), 16000)
			time.Sleep(chunkDur)
		}
	}
	go func() {
		feed(vadSpeechPCM, 10)  // ~1.0 s speech
		feed(vadSilencePCM, 12) // ~1.2 s silence -> closes turn 1
		feed(vadSpeechPCM, 10)  // ~1.0 s speech
		feed(vadSilencePCM, 12) // ~1.2 s silence -> closes turn 2
		input <- stage.StreamElement{EndOfStream: true}
		close(input)
	}()

	turns := 0
	for e := range output {
		if e.Audio != nil && len(e.Audio.Samples) > 0 {
			turns++
		}
	}

	if turns < 2 {
		t.Fatalf("expected >=2 VAD-segmented turns (silence must split the two utterances), got %d; "+
			"VAD is not detecting speech / not segmenting on silence", turns)
	}
}

// vadQuietSpeechPCM returns `samples` of 16-bit PCM sine at amplitude 0.04
// (~0.028 RMS). This is intentionally below the level that SimpleVAD reliably
// detects with its fixed threshold, but within range for AdaptiveVAD.
func vadQuietSpeechPCM(samples int) []byte {
	b := make([]byte, samples*2)
	for i := 0; i < samples; i++ {
		v := int16(0.04 * 32767 * math.Sin(float64(i)*0.2))
		binary.LittleEndian.PutUint16(b[i*2:], uint16(v))
	}
	return b
}

// TestAudioTurnStage_SegmentsQuietMicWithAdaptiveVAD mirrors
// TestAudioTurnStage_SegmentsTwoUtterancesOnSilence but uses an AdaptiveVAD
// and quiet speech (amplitude 0.04, ~0.028 RMS) that the fixed SimpleVAD
// threshold would miss. Two turns separated by >0.8 s silence must be produced.
func TestAudioTurnStage_SegmentsQuietMicWithAdaptiveVAD(t *testing.T) {
	vad, err := audio.NewAdaptiveVAD(audio.DefaultVADParams())
	if err != nil {
		t.Fatalf("NewAdaptiveVAD: %v", err)
	}

	cfg := stage.DefaultAudioTurnConfig()
	cfg.VAD = vad

	s, err := stage.NewAudioTurnStage(cfg)
	if err != nil {
		t.Fatalf("NewAudioTurnStage: %v", err)
	}

	const chunkSamples = 1600 // 100 ms @ 16 kHz
	const chunkDur = 100 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	input := make(chan stage.StreamElement)
	output := make(chan stage.StreamElement, 64)
	go func() {
		_ = s.Process(ctx, input, output)
	}()

	feed := func(gen func(int) []byte, chunks int) {
		for i := 0; i < chunks; i++ {
			input <- makeAudioElement(gen(chunkSamples), 16000)
			time.Sleep(chunkDur)
		}
	}
	go func() {
		feed(vadQuietSpeechPCM, 10) // ~1.0 s quiet speech
		feed(vadSilencePCM, 12)     // ~1.2 s silence -> closes turn 1
		feed(vadQuietSpeechPCM, 10) // ~1.0 s quiet speech
		feed(vadSilencePCM, 12)     // ~1.2 s silence -> closes turn 2
		input <- stage.StreamElement{EndOfStream: true}
		close(input)
	}()

	turns := 0
	for e := range output {
		if e.Audio != nil && len(e.Audio.Samples) > 0 {
			turns++
		}
	}

	if turns < 2 {
		t.Fatalf("AdaptiveVAD: expected >=2 turns from quiet-mic speech (amplitude 0.04), got %d; "+
			"adaptive noise-floor tracking is not detecting quiet speech", turns)
	}
}
