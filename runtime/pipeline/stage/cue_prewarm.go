package stage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

// ErrSessionBoundSource is PrewarmClips' error for a clip source that needs a
// live session: a provider's own out-of-band audio renders only inside a
// session, so it cannot be pre-warmed.
var ErrSessionBoundSource = errors.New("the clip source is session-bound: a provider's out-of-band " +
	"audio renders only inside a live session and cannot be pre-warmed; pre-warm with a TTS service")

// ErrVoicePrewarmedByAnotherService is PrewarmClips' error for a text already
// pre-warmed for the voice and format from another TTS service: a session with
// no TTS service plays one service's clips per voice, so a second service's is
// refused rather than mixed in.
var ErrVoicePrewarmedByAnotherService = errors.New("the voice is already pre-warmed from another TTS service")

// errNotPrewarmed is prewarmedSource's render error: it only reads the cache.
var errNotPrewarmed = errors.New("the clip was not pre-warmed")

// prewarmedSource is the clips PrewarmClips kept for a session voice, whatever
// TTS service rendered them. It renders nothing: a text not pre-warmed for the
// voice falls to the next source. A session with no TTS service of its own
// reads it, so a cache warmed offline serves sessions that make no TTS call.
type prewarmedSource struct{ sessionVoice string }

func (prewarmedSource) kind() string               { return "prewarmed" }
func (s prewarmedSource) voice() string            { return strings.ToLower(s.sessionVoice) }
func (prewarmedSource) keyText(text string) string { return text }
func (prewarmedSource) render(context.Context, string, cueclip.Format) (cueClip, *clipRenderCost, error) {
	return cueClip{}, nil, errNotPrewarmed
}

// keepForVoice keeps clip under the session voice alone, for a session with
// no TTS service (prewarmedSource). A different clip already kept there came
// from another service: it is refused, not replaced.
func keepForVoice(ctx context.Context, b *cueBank, voice, text string, format cueclip.Format, clip cueClip) error {
	pw := prewarmedSource{sessionVoice: voice}
	clip.Meta = cueclip.Meta{Text: pw.keyText(text), Voice: pw.voice(), Source: pw.kind()}
	key := cueclip.KeyFor(clip.Meta.Text, clip.Meta.Voice, clip.Meta.Source, format)
	if held, ok := b.cached(ctx, key); ok && held.Audio != nil && !bytes.Equal(held.Audio.Samples, clip.Audio.Samples) {
		return fmt.Errorf("pre-warm %q for voice %q: %w", text, voice, ErrVoicePrewarmedByAnotherService)
	}
	b.keep(ctx, key, clip)
	return nil
}

// PrewarmClips renders texts with the TTS service svc into cache, so a session
// started later plays them with no render. voice is the session's voice name:
// the clips are keyed by the service voice whose ID or name matches it,
// ignoring case (the service's default voice when none does), exactly as a
// session in that voice looks them up. format is the session's output format
// (PCM16 mono; a zero SampleRate is 24 kHz). A nil cache is the process-wide
// default.
//
// Each clip is also kept under the session voice alone, so a session with no
// TTS service configured (a test, an arena run, a host that warms a directory
// offline) plays it too. That is skipped, with a warning, when the service has
// no voice matching voice. Pre-warm one TTS service per voice and format: a
// text already kept for the voice with another service's audio returns
// ErrVoicePrewarmedByAnotherService.
//
// Each text renders once, through the same trimming, fade, resampling and
// process-wide single-flight a session uses, a few at a time. A text the
// service fails is named in the returned error; the others are still cached.
// svc must render PCM; nil, or a live session's audio, is session-bound and
// returns ErrSessionBoundSource.
func PrewarmClips(ctx context.Context, cache cueclip.Cache, svc tts.Service, voice string, format cueclip.Format,
	texts []string,
) error {
	if svc == nil {
		return ErrSessionBoundSource
	}
	if _, live := svc.(providers.StreamInputSession); live {
		return fmt.Errorf("%T: %w", svc, ErrSessionBoundSource)
	}
	if format.SampleRate <= 0 {
		format.SampleRate = defaultCueSampleRate
	}
	format.Channels = 1
	src := newTTSClipSource(svc, voice, format.SampleRate)
	if src == nil {
		return fmt.Errorf("pre-warm: the TTS service %q renders no PCM", svc.Name())
	}
	// The clips are kept for the voice only when the service speaks in it: a
	// service's default voice is not the voice a session asked for.
	forVoice := voice == "" || src.voiceID != ""
	if !forVoice {
		logger.Warn("pre-warm: the TTS service has no voice matching the session's; its clips are not kept "+
			"for sessions without a TTS service", "tts", svc.Name(), "session_voice", voice)
	}
	b := newCueBank(&cueConfig{Cache: cache, RenderTimeout: defaultCueRenderTimeout})
	b.state, b.format, b.sources = cueSessionSpoken, format, []clipSource{src}

	texts = dedupeTexts(texts)
	errs := make([]error, len(texts))
	sem := make(chan struct{}, cueSessionRenders)
	var wg sync.WaitGroup
	for i, text := range texts {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer func() { <-sem; wg.Done() }()
			clip, err := b.fetch(ctx, src, text, format)
			if err != nil {
				errs[i] = fmt.Errorf("pre-warm %q: %w", text, err)
				return
			}
			if forVoice {
				errs[i] = keepForVoice(ctx, b, voice, text, format, clip)
			}
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}
