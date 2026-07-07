// Package stage provides pipeline stages for audio processing.
package stage

import (
	"context"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// AudioPacingStage delays each audio element so that it is forwarded at
// the rate the bytes would play back at, derived from the chunk's own
// sample rate and length. Non-audio elements pass through unchanged.
//
// # Why this exists
//
// Pacing is the SOLE cadence authority on the audio data path. Audio
// chunks downstream of this stage carry implicit wall-clock timing
// because they're emitted at audio rate; consumers — the duplex
// provider's VAD, fan-out observers like the arena LocalSink — read
// that timing as a feature.
//
// Cadence cannot come from consumers (sinks, observers): see the
// arena/audio package's observer-model doc. Consumers fan-out from a
// shared bus and are not allowed to push back, because:
//
//   - The same audio is broadcast to the LLM provider's session and to
//     local observers. Backpressure from any observer would warp the
//     provider's VAD timing.
//   - In parallel CI, most runs have no live observer at all, so
//     consumer-driven cadence wouldn't apply uniformly across runs.
//   - A run's correctness can't depend on whether someone is currently
//     listening to it.
//
// So pacing happens here, on the data path, before the broadcast — not
// at any sink. Without it, a TTS source that delivers chunks faster
// than realtime (mock, or buffered HTTP) collapses an utterance into a
// single instant; the LLM thinks zero seconds of speech happened and
// fires turn-end immediately.
//
// # When to skip the stage
//
// Pipelines that don't need realtime delivery (headless CI runs of
// selfplay scenarios with VAD disabled and no live observer attached,
// file-based offline processing, etc.) should leave this stage out —
// it just spends real wall-clock time sleeping for nothing. The arena
// duplex pipeline gates the stage on needsAudioPacing(req) for this
// reason.
//
// # Format support
//
// This is a Transform stage: 1 input element → 1 output element, with
// audio elements emitted at chunk-duration intervals. Operates on PCM16
// (and PCM Float32) audio only; chunks with no fixed bytes-per-sample
// (Opus / MP3 / AAC) pass through without pacing — their wire-rate
// duration can't be derived from a chunk's byte length, and we'd
// rather forward immediately than pace based on a guess.
//
// # Direction singularity
//
// State is shared across all audio elements that pass through, so a
// single instance assumes a single audio direction (input or output,
// not both at once). If a future pipeline ever needs to pace both
// directions, instantiate two stages — one per direction.
// defaultPrerollChunks is how many chunks the stage forwards
// immediately at the start of a sequence before pacing kicks in. The
// reason this matters: pacing at *exactly* playback rate means any
// scheduler jitter (Go runtime, oto thread, OS, SSE relay, browser
// AudioContext) lands a sink pull on an empty channel and the audio
// thread substitutes silence — audible drops at consistent intervals.
// Forwarding a few chunks up-front gives the sink a buffer ahead of
// real-time so jitter is absorbed for the rest of the sequence.
//
// 5 chunks balances input and output direction needs:
//   - Input (selfplay TTS at 20 ms chunks): 5 × 20 ms = 100 ms preroll,
//     well under any provider VAD's no-audio-arrival timeout (typically
//     500 ms+), so it doesn't trip false turn-end on the LLM side.
//   - Output (realtime providers at ~250 ms chunks): 5 × 250 ms ≈ 1.2 s
//     of head-buffer, absorbing the SSE-relay + browser-scheduler
//     jitter envelope observed in production (occasional gaps of
//     80–320 ms mid-utterance with 3-chunk preroll).
const defaultPrerollChunks = 5

// AudioPacingStage paces audio chunks toward downstream stages so they are
// forwarded at roughly real-time rate, smoothing bursty producers (e.g. file
// readers) into a steady stream that real-provider VAD and local sinks can
// consume without buffer overrun or premature turn-end.
type AudioPacingStage struct {
	BaseStage

	// last is the wall-clock time at which the previous audio chunk was
	// forwarded. The next chunk's earliest forward time is
	// last + chunkDuration(currentChunk). Reset between turns happens
	// implicitly when a non-audio element interrupts (the gap absorbs
	// any drift), so the stage doesn't need explicit turn-boundary
	// handling.
	last time.Time

	// chunksThisSequence counts audio chunks forwarded since the last
	// reset (last==zero). The first prerollChunks chunks bypass pacing
	// and forward immediately, building a buffer at downstream
	// consumers (LocalSink, real-provider VAD). Reset by
	// non-audio elements that clear s.last.
	chunksThisSequence int

	// prerollChunks is how many chunks at the start of a sequence
	// forward without pacing. Default defaultPrerollChunks; tests may
	// override.
	prerollChunks int

	// clock is overridable for tests so they can simulate time advancing
	// during sleep without burning wall-clock time. nil falls back to
	// realClock{} (time.Now / time.After).
	clock pacingClock
}

// pacingClock abstracts time access so tests can simulate the wall
// clock advancing during sleep — needed to exercise the
// past-the-deadline drift-recovery branch and ctx-cancel-during-sleep
// behavior.
type pacingClock interface {
	now() time.Time
	sleep(ctx context.Context, d time.Duration) error
}

type realClock struct{}

func (realClock) now() time.Time { return time.Now() }
func (realClock) sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// NewAudioPacingStage creates a new audio pacing stage with the
// default preroll (defaultPrerollChunks chunks forwarded immediately
// before pacing kicks in) and the canonical name "audio-pacing".
//
// Use NewNamedAudioPacingStage when wiring two instances in the same
// pipeline (input + output direction); the pipeline builder rejects
// duplicate stage names.
func NewAudioPacingStage() *AudioPacingStage {
	return NewNamedAudioPacingStage("audio-pacing")
}

// NewNamedAudioPacingStage is like NewAudioPacingStage but lets the
// caller pick the stage name. Necessary when two pacing stages
// coexist in the same pipeline (e.g. one for the input audio path,
// one for the output) — the pipeline builder treats stage names as
// unique IDs and would reject a second "audio-pacing".
func NewNamedAudioPacingStage(name string) *AudioPacingStage {
	return &AudioPacingStage{
		BaseStage:     NewBaseStage(name, StageTypeTransform),
		clock:         realClock{},
		prerollChunks: defaultPrerollChunks,
	}
}

// Process implements the Stage interface. For audio elements, blocks
// until the chunk's audio-duration deadline is reached before forwarding.
// All other elements forward immediately and reset the pacing clock.
func (s *AudioPacingStage) Process(
	ctx context.Context,
	input <-chan StreamElement,
	output chan<- StreamElement,
) error {
	defer close(output)

	for elem := range input {
		if elem.Audio != nil && len(elem.Audio.Samples) > 0 && elem.Audio.SampleRate > 0 {
			if err := s.paceFor(ctx, &elem); err != nil {
				return err
			}
		}
		// Reset pacer state only on the true turn-boundary signal
		// (EndOfStream). Other non-audio elements — transcript deltas
		// from Realtime providers, text chunks interleaved with audio —
		// pass through without resetting. Resetting on every non-audio
		// element re-arms preroll mid-utterance, so each interleaved
		// transcript chunk releases another preroll burst and the
		// downstream consumer accumulates seconds of buffered audio.
		if elem.EndOfStream {
			s.last = time.Time{}
			s.chunksThisSequence = 0
		}

		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// paceFor sleeps until it's time to forward the given audio element.
// The first prerollChunks chunks of a sequence forward immediately to
// give downstream consumers a buffer ahead of real-time (avoids
// underrun-induced silence padding at sink pull boundaries). After
// preroll, pacing kicks in and chunks are released at chunk-duration
// intervals from when preroll completed.
//
// Chunks in formats with no fixed bytes-per-sample forward immediately
// (chunkDurationFor returns 0).
func (s *AudioPacingStage) paceFor(ctx context.Context, elem *StreamElement) error {
	dur := chunkDurationFor(elem.Audio)
	now := s.clock.now()
	s.chunksThisSequence++
	// Preroll: forward the first prerollChunks chunks without pacing,
	// keeping s.last bumped to "now" so once preroll ends, pacing
	// schedules the next chunk relative to the most recent forward.
	if s.last.IsZero() || dur <= 0 || s.chunksThisSequence <= s.prerollChunks {
		if s.last.IsZero() {
			logger.Debug("AudioPacingStage: starting new audio sequence",
				"stage", s.Name(), "first_chunk_dur_ms", dur.Milliseconds(),
				"sample_rate", elem.Audio.SampleRate, "samples", len(elem.Audio.Samples))
		}
		s.last = now
		return nil
	}
	deadline := s.last.Add(dur)
	if delay := deadline.Sub(now); delay > 0 {
		if s.chunksThisSequence == s.prerollChunks+1 {
			logger.Debug("AudioPacingStage: pacing kicked in",
				"stage", s.Name(), "chunk_dur_ms", dur.Milliseconds(),
				"first_delay_ms", delay.Milliseconds())
		}
		if err := s.clock.sleep(ctx, delay); err != nil {
			return err
		}
		s.last = deadline
	} else {
		// We're already past the deadline (consumer running behind);
		// don't try to claw back the gap, just resume from now. Record it:
		// this is the "cannot hold real time" health signal, reported
		// direct-update (never via the event bus — see #853).
		s.last = now
		providers.DefaultStreamMetrics().PacingBehindDeadlineInc(pacingDirection(s.Name()))
	}
	return nil
}

// pacingDirection maps a pacing stage's name to a bounded metric label.
// The output-direction instance is named with a "-output" suffix
// (created in the Arena pipeline); every other pacing stage is input.
func pacingDirection(stageName string) string {
	if strings.HasSuffix(stageName, "-output") {
		return "output"
	}
	return "input"
}

// chunkDurationFor returns the wall-clock playback time of an
// AudioData chunk. Returns 0 for formats with no fixed bytes-per-sample
// (Opus / MP3 / AAC) — those chunks should forward immediately rather
// than be paced based on a guess.
func chunkDurationFor(a *AudioData) time.Duration {
	if a == nil || a.SampleRate <= 0 {
		return 0
	}
	bps := a.Format.BytesPerSample()
	if bps <= 0 {
		return 0
	}
	channels := a.Channels
	if channels < 1 {
		channels = 1
	}
	samples := len(a.Samples) / (bps * channels)
	return time.Duration(samples) * time.Second / time.Duration(a.SampleRate)
}
