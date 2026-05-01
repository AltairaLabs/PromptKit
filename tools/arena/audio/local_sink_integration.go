package audio

import (
	"io"

	"github.com/ebitengine/oto/v3"
)

// realOtoContext is the production constructor — wires the actual
// oto.NewContext + ready-channel handshake. This file is excluded from
// coverage because it needs a real audio device to exercise; the no-op
// fallback path is covered via the newContext injection point.
func realOtoContext(rate, channels int) (otoContext, error) {
	options := &oto.NewContextOptions{
		SampleRate:   rate,
		ChannelCount: channels,
		Format:       oto.FormatSignedInt16LE,
	}
	ctx, ready, err := oto.NewContext(options)
	if err != nil {
		return nil, err
	}
	<-ready
	return realOtoContextAdapter{ctx: ctx}, nil
}

type realOtoContextAdapter struct {
	ctx *oto.Context
}

// NewPlayer creates an oto.Player from the given reader and wraps it in the
// otoPlayer interface.
func (a realOtoContextAdapter) NewPlayer(reader io.Reader) otoPlayer {
	return realOtoPlayerAdapter{p: a.ctx.NewPlayer(reader)}
}

type realOtoPlayerAdapter struct {
	p *oto.Player
}

// Play starts oto playback on the underlying player.
func (a realOtoPlayerAdapter) Play() { a.p.Play() }
