package audio

import (
	"context"
	"encoding/binary"
	"sync/atomic"

	runtimeaudio "github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// bytesPerInt16Sample is the byte size of a single s16le PCM sample.
const bytesPerInt16Sample = 2

// MonitorTapConfig configures the MonitorTap stage.
type MonitorTapConfig struct {
	// Position determines whether tapped audio is treated as input
	// (user/selfplay) or output (agent).
	Position stage.RecordingPosition
}

// MonitorTap is a pipeline stage that observes audio elements and forwards
// them to the AudioRouter. Pure passthrough — elements flow to downstream
// stages unmodified. Router publishing is non-blocking by contract, so a
// slow router or full consumer buffer never affects pipeline throughput.
//
// This is the *boundary* between the data path (Stage chain) and the
// observer side (router + LocalSink + RMS subscribers + ...). Audio
// flowing through here goes two places at once: forward to the next
// pipeline stage (data path), and sideways onto the router (observers).
// The observer side must not influence the data path — see the
// observer-model doc in types.go. If a sink can't keep up, frames are
// dropped at the router or sink, never propagated back as backpressure.
//
// If observer-side smooth playback matters (someone is listening live
// to a bursty mock provider), the answer is to add another
// AudioPacingStage on the data path upstream of this tap, not to make
// the observer side blocking.
type MonitorTap struct {
	stage.BaseStage
	router *AudioRouter
	config MonitorTapConfig

	// stereoWarned latches once so a non-mono input doesn't spam the
	// log every chunk. The runtime resampler underneath assumes mono;
	// stereo would resample at half-rate and the meter/playback would
	// be wrong. We warn loudly the first time we see one rather than
	// silently miscomputing.
	stereoWarned atomic.Bool
}

// NewMonitorTap constructs a tap that publishes audio elements to the
// given router.
func NewMonitorTap(router *AudioRouter, config MonitorTapConfig) *MonitorTap {
	name := "monitor_tap_" + string(config.Position)
	return &MonitorTap{
		BaseStage: stage.NewBaseStage(name, stage.StageTypeTransform),
		router:    router,
		config:    config,
	}
}

// Process implements stage.Stage. It taps audio elements to the router and
// passes every element through to the output channel unchanged.
func (t *MonitorTap) Process(
	ctx context.Context,
	input <-chan stage.StreamElement,
	output chan<- stage.StreamElement,
) error {
	defer close(output)
	for elem := range input {
		t.tap(&elem)
		select {
		case output <- elem:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (t *MonitorTap) tap(elem *stage.StreamElement) {
	if elem.Audio == nil {
		return
	}
	audio := elem.Audio
	if audio.Channels > 1 && !t.stereoWarned.Swap(true) {
		// runtimeaudio.ResamplePCM16 takes only fromRate/toRate; the
		// resample math here implicitly assumes mono, so non-mono
		// inputs would resample at the wrong byte-per-sample ratio
		// and produce off-pitch playback at the sink. We warn once
		// per stage instance so a future change that ever wires
		// stereo through here surfaces immediately rather than
		// silently corrupting playback.
		logger.Warn("audio monitor tap: non-mono input, resample assumes mono — playback may be incorrect",
			"channels", audio.Channels)
	}
	samples, err := bytesToInt16Samples(audio.Samples, audio.SampleRate, t.router.rate)
	if err != nil || len(samples) == 0 {
		return
	}
	t.router.Publish(Frame{
		Direction: t.directionFor(),
		Samples:   samples,
		Timestamp: elem.Timestamp,
	})
}

func (t *MonitorTap) directionFor() Direction {
	if t.config.Position == stage.RecordingPositionInput {
		return DirectionInput
	}
	return DirectionOutput
}

// bytesToInt16Samples decodes s16le bytes into []int16, resampling if needed.
func bytesToInt16Samples(b []byte, fromRate, toRate int) ([]int16, error) {
	resampled := b
	if fromRate != toRate && fromRate > 0 {
		out, err := runtimeaudio.ResamplePCM16(b, fromRate, toRate)
		if err != nil {
			return nil, err
		}
		resampled = out
	}
	n := len(resampled) / bytesPerInt16Sample
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		//nolint:gosec // Safe PCM16 conversion: full uint16 range maps to int16 for sample encoding.
		out[i] = int16(binary.LittleEndian.Uint16(resampled[bytesPerInt16Sample*i:]))
	}
	return out, nil
}

var _ stage.Stage = (*MonitorTap)(nil)
