package engine

import (
	"os"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	arenaaudio "github.com/AltairaLabs/PromptKit/tools/arena/audio"
)

// buildAudioMonitor returns a per-run AudioRouter when audio monitoring is
// enabled and applicable to this scenario. Returns nil when monitoring is
// disabled, the scenario isn't duplex, or the auto-mode gate (TTY) isn't
// satisfied.
//
// The engine doesn't own host-audio playback — that lives in
// arenaaudio.Monitor and is wired by the caller via the AudioMonitorHook
// (see RegisterAudioMonitorHook). Doing it that way keeps the engine
// agnostic to whether anyone is listening; oto-singleton constraints,
// run selection, and meter distribution are all the Monitor's problem.
//
// The caller is responsible for calling router.Close() once the run
// completes.
func (e *Engine) buildAudioMonitor(scenario *config.Scenario) *arenaaudio.AudioRouter {
	if e.audioMonitorOpts == nil {
		return nil
	}
	opts := *e.audioMonitorOpts

	// Skip explicit-off and non-duplex scenarios early.
	if opts.Mode == arenaaudio.ModeOff {
		return nil
	}
	if scenario == nil || scenario.Duplex == nil {
		return nil
	}

	// Auto mode: only wire when stdout is a terminal (interactive).
	if opts.Mode == arenaaudio.ModeAuto && !isStdoutTTY() {
		return nil
	}

	return arenaaudio.NewAudioRouter(opts.Rate)
}

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
