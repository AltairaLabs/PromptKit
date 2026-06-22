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
}

// NewDriver constructs a Driver. onLevel may be nil.
func NewDriver(io AudioIO, run LiveRunner, onLevel func(user, agent float32)) *Driver {
	return &Driver{io: io, run: run, onLevel: onLevel}
}

// Run starts audio I/O and drives the conversation until ctx ends or mic closes.
func (d *Driver) Run(ctx context.Context) error {
	if err := d.io.Start(ctx); err != nil {
		return err
	}
	defer func() { _ = d.io.Close() }()

	play := func(frame []byte) {
		d.io.Play(frame)
		if d.onLevel != nil {
			d.onLevel(0, rms(frame))
		}
	}

	mic := d.io.CaptureChunks()
	if d.onLevel != nil {
		mic = d.tapLevels(mic)
	}
	return d.run(ctx, mic, play)
}

// tapLevels forwards mic frames while reporting their RMS as the user level.
func (d *Driver) tapLevels(in <-chan []byte) <-chan []byte {
	out := make(chan []byte)
	go func() {
		defer close(out)
		for f := range in {
			d.onLevel(rms(f), 0)
			out <- f
		}
	}()
	return out
}
