package cue

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"
)

// BankOf is the clip bank s plays from. stage's provider stage keeps it to
// tell it of the session it creates (Bank.SessionStarted) and to ask it
// whether a cue will be spoken (Bank.Voices). stage does not re-export it.
func BankOf(s *CueStage) *Bank {
	return s.bank
}

// HoldBudget is how long the cue stage playing from b holds reply output
// behind a clip still rendering (Config.OnDemandBudget, after defaults), and
// the default budget when no cue stage plays (b is nil). stage's provider
// stage gives a BlockedMessageFunc at most this long. stage does not
// re-export it.
func HoldBudget(b *Bank) time.Duration {
	if b != nil && b.budget > 0 {
		return b.budget
	}
	return defaultCueOnDemandBudget
}

// SetRender makes s render a clip its bank does not hold with render, in
// place of the session's sources. It lets stage's tests hold a clip
// rendering: they cannot reach the stage's unexported fields from outside
// this package. stage does not re-export it.
func SetRender(s *CueStage, render func(ctx context.Context, text string) (Clip, error)) {
	s.render = render
}

// OnStored makes b call stored with each clip a render produced, once b holds
// it. It lets stage's tests wait for a render. stage does not re-export it.
func OnStored(b *Bank, stored func(text string, clip Clip)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.stored = stored
}

// Held is the clip b holds for text, whether it is ready to play as it is,
// and whether b holds one. stage does not re-export it.
func Held(b *Bank, text string) (clip Clip, isReady, ok bool) {
	return b.get(text)
}

// AwaitRender renders text from b's sources, or joins its render still
// running, and returns once it is done. stage does not re-export it.
func AwaitRender(ctx context.Context, b *Bank, text string) {
	<-b.render(ctx, text, nil).done
}

// EarconPCM is the earcon a cue stage plays at rate for a cue no other
// source voices. stage does not re-export it.
func EarconPCM(rate int) []byte {
	return earconPCM(rate)
}

// CacheOf is the clip cache s was configured with (Config.Cache), nil when it
// plays from the process-wide default. stage does not re-export it.
func CacheOf(s *CueStage) cueclip.Cache {
	return s.cfg.Cache
}
