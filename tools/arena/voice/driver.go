package voice

import "context"

// LiveRunner consumes mic frames and emits playback frames, returning when mic
// closes or ctx ends. engine.(*DuplexConversationExecutor).RunInteractiveVoice
// is adapted to this signature by the chat command.
type LiveRunner func(ctx context.Context, mic <-chan []byte, play func([]byte)) error

// Driver wires hardware AudioIO to a LiveRunner and (optionally) reports RMS
// levels for the TUI meter. It owns no LLM or pipeline knowledge.
type Driver struct {
	io      AudioIO
	run     LiveRunner
	onLevel func(user, agent float32)
	guard   *EchoGuard
}

// NewDriver constructs a Driver. onLevel may be nil.
func NewDriver(io AudioIO, run LiveRunner, onLevel func(user, agent float32)) *Driver {
	return &Driver{io: io, run: run, onLevel: onLevel}
}

// NewDriverWithGuard constructs a Driver with an optional half-duplex echo guard.
// When guard is non-nil, mic frames are gated by g.Allow before reaching the runner,
// and g.SetAgentSpeaking is toggled around each playback frame.
//
// v1 limitation: the per-frame SetAgentSpeaking toggle is effective only for synchronous
// playback (where Play blocks for the full frame duration). Real buffered hardware drivers
// return from Play before the audio is audible, so the flag stays true for only microseconds
// and does not suppress echo. Hardware wiring must hold SetAgentSpeaking true for the entire
// audible playback duration — see the TODO in Driver.Run.
func NewDriverWithGuard(io AudioIO, run LiveRunner, onLevel func(user, agent float32), guard *EchoGuard) *Driver {
	return &Driver{io: io, run: run, onLevel: onLevel, guard: guard}
}

// Run starts audio I/O and drives the conversation until ctx ends or mic closes.
func (d *Driver) Run(ctx context.Context) error {
	if err := d.io.Start(ctx); err != nil {
		return err
	}
	defer func() { _ = d.io.Close() }()

	play := func(frame []byte) {
		if d.guard != nil {
			// TODO(voice): hold SetAgentSpeaking for the audible playback duration, not just
			// the Play call. Buffered hardware drivers return from Play before audio is emitted,
			// so the guard window closes before the mic can pick up the echo. A future wiring
			// should accept a playback-duration hint (e.g. frame length / sample rate) and
			// defer SetAgentSpeaking(false) until after the audio is audible.
			d.guard.SetAgentSpeaking(true)
		}
		d.io.Play(frame)
		if d.onLevel != nil {
			d.onLevel(0, rms(frame))
		}
		if d.guard != nil {
			d.guard.SetAgentSpeaking(false)
		}
	}

	mic := d.io.CaptureChunks()
	if d.onLevel != nil || d.guard != nil {
		mic = d.tapLevels(ctx, mic)
	}
	return d.run(ctx, mic, play)
}

// tapLevels forwards mic frames while reporting their RMS as the user level.
// When a guard is configured, frames that fail Allow are dropped silently.
// The send to out is ctx-guarded so the goroutine exits promptly when ctx is
// canceled instead of blocking on a full or closed downstream channel.
func (d *Driver) tapLevels(ctx context.Context, in <-chan []byte) <-chan []byte {
	out := make(chan []byte)
	go func() {
		defer close(out)
		for f := range in {
			if d.guard != nil && !d.guard.Allow(f) {
				continue
			}
			if d.onLevel != nil {
				d.onLevel(rms(f), 0)
			}
			select {
			case out <- f:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}
