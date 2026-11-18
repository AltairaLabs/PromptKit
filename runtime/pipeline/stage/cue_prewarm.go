package stage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

// ErrSessionBoundSource is PrewarmClips' error for a clip source that needs a
// live session: a provider's own out-of-band audio renders only inside a
// session, so it cannot be pre-warmed.
var ErrSessionBoundSource = errors.New("the clip source is session-bound: a provider's out-of-band " +
	"audio renders only inside a live session and cannot be pre-warmed; pre-warm with a TTS service")

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
func (prewarmedSource) render(context.Context, string, ClipFormat) (CueClip, *clipRenderCost, error) {
	return CueClip{}, nil, errNotPrewarmed
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
// TTS service (a test, an arena run, a host that warms a directory offline)
// plays it too.
//
// Each text renders once, through the same trimming, fade, resampling and
// process-wide single-flight a session uses, a few at a time. A text the
// service fails is named in the returned error; the others are still cached.
// svc must render PCM; nil, or a live session's audio, is session-bound and
// returns ErrSessionBoundSource.
func PrewarmClips(ctx context.Context, cache ClipCache, svc tts.Service, voice string, format ClipFormat,
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
	format.Encoding, format.Channels = AudioFormatPCM16, 1
	src := newTTSClipSource(svc, voice, format.SampleRate)
	if src == nil {
		return fmt.Errorf("pre-warm: the TTS service %q renders no PCM", svc.Name())
	}
	b := newCueBank(&CueConfig{Cache: cache, RenderTimeout: defaultCueRenderTimeout})
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
			// Also under the session voice alone, for a session with no TTS
			// service (prewarmedSource).
			pw := prewarmedSource{sessionVoice: voice}
			clip.Meta = ClipMeta{Text: pw.keyText(text), Voice: pw.voice(), Source: pw.kind()}
			b.keep(ctx, ClipKeyFor(clip.Meta.Text, clip.Meta.Voice, clip.Meta.Source, format), clip)
		}()
	}
	wg.Wait()
	return errors.Join(errs...)
}
