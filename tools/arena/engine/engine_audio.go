package engine

import (
	"os"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
	arenaaudio "github.com/AltairaLabs/PromptKit/tools/arena/audio"
)

// buildAudioMonitor returns a per-run AudioRouter and LocalSink when audio
// monitoring is enabled and applicable to this scenario. Returns (nil, nil)
// when monitoring is disabled, the scenario isn't duplex, or the auto-mode
// gate (TTY) isn't satisfied.
//
// The caller is responsible for calling router.Close() and sink.Close() once
// the run completes (or fails). Both are idempotent.
func (e *Engine) buildAudioMonitor(scenario *config.Scenario) (*arenaaudio.AudioRouter, *arenaaudio.LocalSink) {
	if e.audioMonitorOpts == nil {
		return nil, nil
	}
	opts := *e.audioMonitorOpts

	// Skip explicit-off and non-duplex scenarios early.
	if opts.Mode == arenaaudio.ModeOff {
		return nil, nil
	}
	if scenario == nil || scenario.Duplex == nil {
		return nil, nil
	}

	// Auto mode: only wire when stdout is a terminal (interactive).
	if opts.Mode == arenaaudio.ModeAuto && !isStdoutTTY() {
		return nil, nil
	}

	router := arenaaudio.NewAudioRouter(opts.Rate)

	var sink *arenaaudio.LocalSink
	if opts.LocalSink {
		s, err := arenaaudio.NewLocalSink(arenaaudio.LocalSinkConfig{Rate: opts.Rate})
		if err != nil {
			logger.Warn("audio monitor: failed to create LocalSink", "error", err)
		} else {
			sink = s
			ch := router.Subscribe("local-sink", localSinkSubscriberBufSize)
			go func() {
				for frame := range ch {
					sink.Push(frame)
				}
			}()
		}
	}

	return router, sink
}

// localSinkSubscriberBufSize is the bounded queue depth between the router
// and the LocalSink consumer goroutine. Drops at this layer mean the LocalSink
// fell behind oto's pull cadence; rare in practice.
const localSinkSubscriberBufSize = 50

// isStdoutTTY returns true when stdout is connected to a terminal device.
// Used by auto-mode to gate audio monitor construction in non-interactive
// runs (CI, file output, pipes).
func isStdoutTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
