package audio

import (
	"context"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
)

// monitorForwardBufSize is the buffer between an active run's AudioRouter
// and the Monitor's forwarder goroutine. Sized to absorb burst-mode user
// audio (a multi-second utterance arriving within a millisecond).
const monitorForwardBufSize = 512

// monitorForwardSubscriberID is the consumer ID the Monitor uses when
// subscribing to a run's AudioRouter for sink forwarding.
const monitorForwardSubscriberID = "audio-monitor"

// MonitorConfig configures a Monitor.
type MonitorConfig struct {
	// Rate is the canonical sample rate for playback. Must match what
	// per-run AudioRouters publish at; both sides resample to this rate.
	Rate int
	// EnableLocalSink toggles host-audio playback. When false the monitor
	// still distributes RMS frames (useful for tests / headless runs that
	// want the meter without sound).
	EnableLocalSink bool
	// Headless skips opening an audio device but still drives the sink at
	// audio rate via a software ticker. Combined with CapturePath it lets
	// CI / bash invocations produce sample-accurate capture files without
	// a host audio interface.
	Headless bool
	// CapturePath, when non-empty, mirrors every byte the sink pulled to
	// a file at this path. Format: interleaved s16le stereo at Rate.
	// Useful for debugging "is my audio data choppy or is the audio
	// device just misbehaving?" — play the file with ffplay/sox to hear
	// exactly what the audio thread saw.
	CapturePath string
}

// Monitor owns the process-wide host-audio playback path and routes one
// run's audio to it at a time. It exists because oto/v3's audio context
// is a process-wide singleton — concurrent runs cannot each open their
// own LocalSink. The Monitor keeps a single sink alive, holds a registry
// of per-run AudioRouters, and forwards frames from the *active* run to
// the sink. Callers switch between runs with SetActiveRun.
//
// RMS frames originate from the sink (driven by oto's pull cadence so the
// meter follows playback timing) and are fanned out to subscribers via
// SubscribeRMS — independent of which run is active.
//
// Lifecycle: NewMonitor opens the sink eagerly (or returns a no-sink
// monitor if oto fails). Close stops forwarding and releases the sink.
// AttachRouter / DetachRouter are paired with each run's lifetime; the
// engine doesn't have to know about the Monitor at all — the CLI wires
// it via the engine's AudioMonitorHook.
type Monitor struct {
	rate int
	sink *LocalSink

	mu               sync.Mutex
	closed           bool
	routers          map[string]*AudioRouter
	activeRunID      string
	activeRouter     *AudioRouter
	forwardCancel    context.CancelFunc
	autoActivateNext bool

	rmsMu          sync.RWMutex
	rmsSubscribers []chan RMSFrame
}

// NewMonitor constructs a Monitor. When EnableLocalSink is true and the
// host audio device is available, host playback is enabled; otherwise the
// monitor runs in metering-only mode and never produces sound.
func NewMonitor(cfg MonitorConfig) (*Monitor, error) {
	if cfg.Rate == 0 {
		cfg.Rate = Rate24k
	}
	m := &Monitor{
		rate:             cfg.Rate,
		routers:          make(map[string]*AudioRouter),
		autoActivateNext: true,
	}
	if !cfg.EnableLocalSink {
		return m, nil
	}
	sink, err := NewLocalSink(LocalSinkConfig{
		Rate:         cfg.Rate,
		RMSPublisher: m.publishRMS,
		CapturePath:  cfg.CapturePath,
		Headless:     cfg.Headless,
	})
	if err != nil {
		return nil, err
	}
	m.sink = sink
	return m, nil
}

// AttachRouter registers a per-run AudioRouter with the monitor. If no
// run is currently active for audio, this run becomes active immediately
// (so a single-run TTY session "just works"). For concurrent runs only
// the first auto-activates; subsequent runs sit in the registry until
// the user picks them via SetActiveRun.
//
// The router self-removes from the registry when it closes (the engine
// closes per-run routers at run end), so callers don't need to pair this
// with DetachRouter unless they want to detach early.
func (m *Monitor) AttachRouter(runID string, router *AudioRouter) {
	if router == nil || runID == "" {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.routers[runID] = router
	sink := m.sink
	if m.activeRouter == nil && m.autoActivateNext {
		m.activateLocked(runID, router)
	}
	m.mu.Unlock()

	// Register the sink as the router's drain handler so the duplex
	// executor's turn loop can wait for local playback to finish
	// between turns. The handler is a no-op when the sink isn't the
	// active receiver for this run (different run is being listened
	// to, or the sink is in noop mode).
	if sink != nil {
		router.RegisterDrainHandler(func(ctx context.Context) {
			// Only block if THIS router is the active source — otherwise
			// the sink isn't actually playing this run's audio.
			m.mu.Lock()
			isActive := m.activeRunID == runID
			m.mu.Unlock()
			if !isActive {
				return
			}
			sink.WaitDrained(ctx, drainMaxWait)
		})
	}

	// Auto-detach when the router shuts down. This avoids a stale entry
	// in the registry once the run has ended.
	go func() {
		<-router.Done()
		m.DetachRouter(runID)
	}()
}

// DetachRouter removes a run's AudioRouter from the monitor. If that run
// was the active audio source, the monitor falls back to any other
// registered router (so listening "moves on" automatically when the run
// you were listening to ends and another is still going).
func (m *Monitor) DetachRouter(runID string) {
	if runID == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.routers, runID)
	if m.activeRunID != runID {
		return
	}
	m.deactivateLocked()
	// Fall back to any other registered router so audio doesn't go silent
	// just because the run you were listening to wrapped up first.
	for id, router := range m.routers {
		m.activateLocked(id, router)
		return
	}
}

// SetActiveRun switches host playback (and the meter) to the named run.
// Returns false when the run isn't registered with the monitor — callers
// can use this to know whether the switch took effect.
func (m *Monitor) SetActiveRun(runID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	router, ok := m.routers[runID]
	if !ok {
		return false
	}
	if m.activeRunID == runID {
		return true
	}
	m.deactivateLocked()
	m.activateLocked(runID, router)
	return true
}

// ActiveRunID returns the run currently routed to the sink, or "" if
// nothing is active.
func (m *Monitor) ActiveRunID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeRunID
}

// SubscribeRMS returns a channel of RMS frames sourced from the sink's
// playback timing (so the meter is in sync with the user's ears,
// regardless of which run is active). The buffer is per-subscriber;
// frames overflow drop silently for that subscriber only.
func (m *Monitor) SubscribeRMS(bufSize int) <-chan RMSFrame {
	if bufSize <= 0 {
		bufSize = 1
	}
	ch := make(chan RMSFrame, bufSize)
	m.rmsMu.Lock()
	m.rmsSubscribers = append(m.rmsSubscribers, ch)
	m.rmsMu.Unlock()
	return ch
}

// Close stops forwarding and releases the sink. Safe to call multiple
// times. After Close, AttachRouter / SetActiveRun become no-ops.
func (m *Monitor) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	m.deactivateLocked()
	m.routers = make(map[string]*AudioRouter)
	m.autoActivateNext = false
	m.mu.Unlock()

	if m.sink != nil {
		m.sink.Close()
	}

	m.rmsMu.Lock()
	for _, ch := range m.rmsSubscribers {
		close(ch)
	}
	m.rmsSubscribers = nil
	m.rmsMu.Unlock()
}

// publishRMS fans out a sink-sourced RMS frame to every subscriber.
// Called from the audio thread inside LocalSink.
func (m *Monitor) publishRMS(frame RMSFrame) {
	m.rmsMu.RLock()
	defer m.rmsMu.RUnlock()
	for _, ch := range m.rmsSubscribers {
		select {
		case ch <- frame:
		default:
			// drop — this subscriber is behind
		}
	}
}

// activateLocked starts forwarding the named run's frames to the sink.
// Must be called with m.mu held and m.activeRouter == nil.
func (m *Monitor) activateLocked(runID string, router *AudioRouter) {
	m.activeRunID = runID
	m.activeRouter = router

	if m.sink == nil {
		// Metering-only mode: still mark the run active so RMS frames
		// from the sink path (if it ever exists) can flow.
		return
	}

	ch := router.Subscribe(monitorForwardSubscriberID, monitorForwardBufSize)
	ctx, cancel := context.WithCancel(context.Background())
	m.forwardCancel = cancel

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case frame, ok := <-ch:
				if !ok {
					return
				}
				m.sink.Push(frame)
			}
		}
	}()

	logger.Debug("audio monitor: activated run", "run_id", runID)
}

// deactivateLocked stops forwarding from the active router (if any) and
// clears active-run state. Must be called with m.mu held.
func (m *Monitor) deactivateLocked() {
	if m.forwardCancel != nil {
		m.forwardCancel()
		m.forwardCancel = nil
	}
	if m.activeRouter != nil {
		m.activeRouter.Unsubscribe(monitorForwardSubscriberID)
	}
	m.activeRunID = ""
	m.activeRouter = nil
}
