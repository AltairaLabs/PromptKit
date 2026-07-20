package audio_test

import (
	"context"
	"encoding/binary"
	"math"
	"math/rand"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/audio"
)

// Comparative characterization of the VAD implementations across realistic
// acoustic conditions. Reports rather than asserts: the point is to establish
// what each detector actually does before choosing a default, not to freeze
// current behavior as a contract.
//
// Run: go test ./runtime/audio/ -run TestVADComparisonMatrix -v

const (
	cmpSampleRate   = 16000
	cmpChunkSamples = cmpSampleRate / 10 // 100 ms
)

// pcmAtRMS returns `samples` of sine scaled to approximately the target RMS.
// A full-scale sine has RMS = amplitude/sqrt(2), so amplitude = rms*sqrt(2).
func pcmAtRMS(samples int, targetRMS float64) []byte {
	amp := targetRMS * math.Sqrt2
	b := make([]byte, samples*2)
	for i := range samples {
		v := int16(amp * 32767 * math.Sin(float64(i)*0.2))
		binary.LittleEndian.PutUint16(b[i*2:], uint16(v))
	}
	return b
}

// noiseAtRMS returns `samples` of deterministic white noise at approximately the
// target RMS. Uniform noise on [-a,a] has RMS = a/sqrt(3).
func noiseAtRMS(samples int, targetRMS float64, seed int64) []byte {
	r := rand.New(rand.NewSource(seed)) //nolint:gosec // deterministic test signal
	amp := targetRMS * math.Sqrt(3)
	b := make([]byte, samples*2)
	for i := range samples {
		v := int16(amp * 32767 * (r.Float64()*2 - 1))
		binary.LittleEndian.PutUint16(b[i*2:], uint16(v))
	}
	return b
}

// mixPCM sums two equal-length PCM16 buffers with clipping.
func mixPCM(a, b []byte) []byte {
	out := make([]byte, len(a))
	for i := 0; i+1 < len(a); i += 2 {
		x := int32(int16(binary.LittleEndian.Uint16(a[i:])))
		y := int32(int16(binary.LittleEndian.Uint16(b[i:])))
		s := x + y
		if s > math.MaxInt16 {
			s = math.MaxInt16
		} else if s < math.MinInt16 {
			s = math.MinInt16
		}
		binary.LittleEndian.PutUint16(out[i:], uint16(int16(s)))
	}
	return out
}

// vadRun feeds chunks and reports when (if ever) the VAD reported Speaking.
type vadRun struct {
	reachedSpeaking bool
	chunkAtSpeaking int
	finalState      audio.VADState
}

func runVAD(t *testing.T, v audio.VADAnalyzer, chunks [][]byte) vadRun {
	t.Helper()
	ctx := context.Background()
	res := vadRun{chunkAtSpeaking: -1}
	for i, c := range chunks {
		if _, err := v.Analyze(ctx, c); err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if v.State() == audio.VADStateSpeaking && !res.reachedSpeaking {
			res.reachedSpeaking = true
			res.chunkAtSpeaking = i + 1
		}
	}
	res.finalState = v.State()
	return res
}

func repeatChunks(gen func() []byte, n int) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = gen()
	}
	return out
}

// TestVADComparisonMatrix reports SimpleVAD vs AdaptiveVAD across conditions.
//
// "Detected" means the detector reached VADStateSpeaking, which is the state
// SilenceDetector and InterruptionHandler key on. For speech rows detection is
// desirable; for the silence and noise rows it is a false positive.
func TestVADComparisonMatrix(t *testing.T) {
	const secs = 2
	const nChunks = secs * 10

	// Room noise at ~0.005 RMS, the level of a quiet office mic floor.
	const roomNoiseRMS = 0.005

	cases := []struct {
		name     string
		gen      func() []byte
		isSpeech bool
	}{
		{name: "digital silence", gen: func() []byte { return make([]byte, cmpChunkSamples*2) }},
		{name: "room noise 0.005", gen: func() []byte { return noiseAtRMS(cmpChunkSamples, roomNoiseRMS, 1) }},
		{name: "loud room noise 0.02", gen: func() []byte { return noiseAtRMS(cmpChunkSamples, 0.02, 2) }},
		{name: "very quiet speech 0.02", gen: func() []byte { return pcmAtRMS(cmpChunkSamples, 0.02) }, isSpeech: true},
		{name: "quiet speech 0.04", gen: func() []byte { return pcmAtRMS(cmpChunkSamples, 0.04) }, isSpeech: true},
		{name: "normal speech 0.10", gen: func() []byte { return pcmAtRMS(cmpChunkSamples, 0.10) }, isSpeech: true},
		{name: "loud speech 0.25", gen: func() []byte { return pcmAtRMS(cmpChunkSamples, 0.25) }, isSpeech: true},
		{
			name: "quiet speech over room noise",
			gen: func() []byte {
				return mixPCM(pcmAtRMS(cmpChunkSamples, 0.02), noiseAtRMS(cmpChunkSamples, roomNoiseRMS, 3))
			},
			isSpeech: true,
		},
		{
			name: "normal speech over room noise",
			gen: func() []byte {
				return mixPCM(pcmAtRMS(cmpChunkSamples, 0.10), noiseAtRMS(cmpChunkSamples, roomNoiseRMS, 4))
			},
			isSpeech: true,
		},
	}

	t.Logf("%-32s | %-24s | %-24s", "condition", "SimpleVAD", "AdaptiveVAD")
	t.Logf("%-32s-+-%-24s-+-%-24s", "--------------------------------", "------------------------", "------------------------")

	var simpleWrong, adaptiveWrong int

	for _, tc := range cases {
		chunks := repeatChunks(tc.gen, nChunks)

		sv, err := audio.NewSimpleVAD(audio.DefaultVADParams())
		if err != nil {
			t.Fatalf("NewSimpleVAD: %v", err)
		}
		av, err := audio.NewAdaptiveVAD(audio.DefaultVADParams())
		if err != nil {
			t.Fatalf("NewAdaptiveVAD: %v", err)
		}

		s := runVAD(t, sv, chunks)
		a := runVAD(t, av, chunks)

		fmtRes := func(r vadRun) string {
			if !r.reachedSpeaking {
				return "no speech (" + r.finalState.String() + ")"
			}
			return "SPEECH @ chunk " + itoa(r.chunkAtSpeaking) + " (" + itoa(r.chunkAtSpeaking*100) + "ms)"
		}

		mark := func(r vadRun) string {
			if r.reachedSpeaking == tc.isSpeech {
				return "  ok"
			}
			if tc.isSpeech {
				return " MISS"
			}
			return " FALSE+"
		}

		if s.reachedSpeaking != tc.isSpeech {
			simpleWrong++
		}
		if a.reachedSpeaking != tc.isSpeech {
			adaptiveWrong++
		}

		t.Logf("%-32s | %-24s%s | %-24s%s", tc.name, fmtRes(s), mark(s), fmtRes(a), mark(a))
	}

	t.Logf("")
	t.Logf("incorrect classifications: SimpleVAD=%d/%d  AdaptiveVAD=%d/%d",
		simpleWrong, len(cases), adaptiveWrong, len(cases))
}

// TestVADQuietTailMatrix reports whether each VAD holds Speaking through the
// quiet end of an utterance.
//
// This is the failure mode that dropped "Springfield" from "742 Evergreen
// Terrace, Springfield": the trailing word measured 0.021 RMS. What matters is
// not whether a detector can find 0.02 speech from cold — neither can — but
// whether one already in Speaking stays there when the speaker tails off, or
// falls to Quiet and lets the turn close mid-utterance.
//
// The final state after the quiet tail is the signal. Speaking or Stopping means
// the turn survives; Quiet means it was cut.
func TestVADQuietTailMatrix(t *testing.T) {
	cases := []struct {
		name     string
		loudRMS  float64
		tailRMS  float64
		tailSecs int
	}{
		{name: "0.10 then 0.021 tail (Springfield)", loudRMS: 0.10, tailRMS: 0.021, tailSecs: 1},
		{name: "0.10 then 0.04 tail", loudRMS: 0.10, tailRMS: 0.04, tailSecs: 1},
		{name: "0.10 then 0.01 tail (very soft)", loudRMS: 0.10, tailRMS: 0.01, tailSecs: 1},
		{name: "0.10 then true silence", loudRMS: 0.10, tailRMS: 0, tailSecs: 1},
	}

	t.Logf("%-38s | %-14s | %-14s", "utterance", "SimpleVAD", "AdaptiveVAD")
	t.Logf("%-38s-+-%-14s-+-%-14s", "--------------------------------------", "--------------", "--------------")

	for _, tc := range cases {
		build := func() [][]byte {
			var chunks [][]byte
			for range 10 { // 1s of normal speech to establish Speaking
				chunks = append(chunks, pcmAtRMS(cmpChunkSamples, tc.loudRMS))
			}
			for range tc.tailSecs * 10 {
				if tc.tailRMS == 0 {
					chunks = append(chunks, make([]byte, cmpChunkSamples*2))
				} else {
					chunks = append(chunks, pcmAtRMS(cmpChunkSamples, tc.tailRMS))
				}
			}
			return chunks
		}

		sv, err := audio.NewSimpleVAD(audio.DefaultVADParams())
		if err != nil {
			t.Fatalf("NewSimpleVAD: %v", err)
		}
		av, err := audio.NewAdaptiveVAD(audio.DefaultVADParams())
		if err != nil {
			t.Fatalf("NewAdaptiveVAD: %v", err)
		}

		s := runVAD(t, sv, build())
		a := runVAD(t, av, build())

		verdict := func(r vadRun) string {
			switch r.finalState {
			case audio.VADStateQuiet:
				return "CUT (quiet)"
			default:
				return "held (" + r.finalState.String() + ")"
			}
		}

		t.Logf("%-38s | %-14s | %-14s", tc.name, verdict(s), verdict(a))
	}

	t.Logf("")
	t.Logf("'CUT' on a speech tail closes the turn mid-utterance and orphans the trailing word.")
	t.Logf("'CUT' on true silence is correct — that is a real end of turn.")
}

// TestVADReleaseLagMatrix reports how long each VAD keeps reporting Speaking
// after speech actually stops.
//
// This is the cost side of a more sensitive detector. AudioTurnStage counts both
// Stopping and Quiet as silence, so its SilenceDuration budget only starts
// accruing once the VAD leaves Speaking. A detector that lingers eats into that
// budget: two utterances separated by a pause shorter than (release lag +
// SilenceDuration) merge into one turn instead of segmenting.
func TestVADReleaseLagMatrix(t *testing.T) {
	const speechChunks = 10 // 1s to establish Speaking
	const silenceChunks = 30

	measure := func(v audio.VADAnalyzer) (leftSpeakingMS, reachedQuietMS int) {
		ctx := context.Background()
		leftSpeakingMS, reachedQuietMS = -1, -1
		for range speechChunks {
			if _, err := v.Analyze(ctx, pcmAtRMS(cmpChunkSamples, 0.10)); err != nil {
				t.Fatalf("Analyze: %v", err)
			}
		}
		silent := make([]byte, cmpChunkSamples*2)
		for i := range silenceChunks {
			if _, err := v.Analyze(ctx, silent); err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			ms := (i + 1) * 100
			if v.State() != audio.VADStateSpeaking && leftSpeakingMS < 0 {
				leftSpeakingMS = ms
			}
			if v.State() == audio.VADStateQuiet && reachedQuietMS < 0 {
				reachedQuietMS = ms
			}
		}
		return leftSpeakingMS, reachedQuietMS
	}

	sv, err := audio.NewSimpleVAD(audio.DefaultVADParams())
	if err != nil {
		t.Fatalf("NewSimpleVAD: %v", err)
	}
	av, err := audio.NewAdaptiveVAD(audio.DefaultVADParams())
	if err != nil {
		t.Fatalf("NewAdaptiveVAD: %v", err)
	}

	sLeft, sQuiet := measure(sv)
	aLeft, aQuiet := measure(av)

	t.Logf("after speech stops (true silence):")
	t.Logf("  SimpleVAD   left Speaking @ %dms, reached Quiet @ %dms", sLeft, sQuiet)
	t.Logf("  AdaptiveVAD left Speaking @ %dms, reached Quiet @ %dms", aLeft, aQuiet)
	t.Logf("")
	t.Logf("AudioTurnStage's SilenceDuration budget only accrues once the VAD leaves")
	t.Logf("Speaking, so the difference in 'left Speaking' is added to every turn boundary.")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
