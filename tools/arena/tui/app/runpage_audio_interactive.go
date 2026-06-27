package app

import (
	"os"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	arenaaudio "github.com/AltairaLabs/PromptKit/tools/arena/audio"
	"github.com/AltairaLabs/PromptKit/tools/arena/tui"
)

// attachAudioMonitor wires a process-wide host-playback Monitor for
// realtime/duplex runs, feeding the conversation meter and routing concurrent
// runs' audio to a single output device. It is a no-op when audio monitoring is
// disabled (ordinary text runs), which keeps the meter realtime-only.
//
// Lives in an _interactive.go file because the meaningful paths open a real
// audio device (oto) and cannot run in CI.
func (p *RunPage) attachAudioMonitor(adapter *tui.EventAdapter) {
	if !p.engine.AudioMonitorEnabled() {
		return
	}
	opts := p.engine.AudioMonitorOptions()
	mon, err := arenaaudio.NewMonitor(arenaaudio.MonitorConfig{
		Rate:            opts.Rate,
		EnableLocalSink: opts.LocalSink,
		CapturePath:     os.Getenv("AUDIO_CAPTURE_PATH"),
	})
	if err != nil {
		logger.Warn("audio monitor: disabled (sink unavailable)", "error", err)
		return
	}
	p.audioMonitor = mon
	p.engine.RegisterAudioMonitorHook(func(runID string, router *arenaaudio.AudioRouter, _ int) {
		mon.AttachRouter(runID, router)
	})
	adapter.AttachAudioMonitor(mon)
	p.model.SetAudioMonitor(mon)
}
