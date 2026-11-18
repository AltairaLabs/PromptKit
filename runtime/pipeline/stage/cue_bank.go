package stage

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/singleflight"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

// cueSessionRenders is how many texts a session renders at once at its start.
const cueSessionRenders = 4

// cueRetryBackoff is how long a text whose better sources failed plays the
// earcon before those sources are tried again for it.
const cueRetryBackoff = 30 * time.Second

// cueRenderSource is the Source of the cost event a clip render reports.
const cueRenderSource = "cue"

// clipFlights is the process-wide single-flight group clip renders go
// through, keyed by ClipKey: sessions that need the same clip at once render
// it once.
var clipFlights singleflight.Group

// clipFlightJoined, when set, runs once a fetch has started or joined its
// render; tests wait on it to know a session is in a render.
var clipFlightJoined atomic.Pointer[func()]

// errNoClipSource is a render's error when the session has no clip source.
var errNoClipSource = errors.New("no clip source")

// cueSessionState is what the bank knows of the session's spoken output.
type cueSessionState int

const (
	// cueSessionUnknown: no session has started (a CueStage used without
	// NewCueStageFor plays the clips it is given and renders nothing).
	cueSessionUnknown cueSessionState = iota
	// cueSessionSpoken: the session's replies are spoken; clips render from
	// the sources.
	cueSessionSpoken
	// cueSessionSilent: the session's replies are not spoken; cues are text
	// only.
	cueSessionSilent
)

// cueBank holds the clips by text and renders those it does not hold. The
// provider stage starts it when it creates its session (sessionStarted);
// renders finish on their own goroutines, so it is safe for concurrent use.
type cueBank struct {
	mu sync.Mutex
	// clips are clips as configured or rendered by a CueStage's render seam:
	// they are prepared (trimmed, resampled, faded) before they play.
	clips map[string]CueClip
	// ready are clips from the sources, ready to play at their rate.
	ready map[string]CueClip
	// stored is called with each clip a render produced, once it is held,
	// whether or not it was played.
	stored func(text string, clip CueClip)
	// inflight are the renders still running, by text: a text cued again
	// while its render runs waits for that render.
	inflight map[string]*cueFlight
	// failed is when the sources ahead of the earcon last failed for a text;
	// until cueRetryBackoff has passed the text plays the earcon without
	// trying them. now is the clock it is read by.
	failed map[string]time.Time
	now    func() time.Time

	// Configuration (CueConfig) and the session's, set by sessionStarted.
	cache         ClipCache
	tts           tts.Service
	texts         []string
	renderTimeout time.Duration
	state         cueSessionState
	format        ClipFormat
	sources       []clipSource
	emitter       *events.Emitter
}

// cueFlight is one render of a text. done is closed once clip, ready, keep
// and err are set.
type cueFlight struct {
	done  chan struct{}
	clip  CueClip
	ready bool
	// keep is false for a clip played in place of the text's own (the
	// earcon after a better source failed): it is not held for the text.
	keep bool
	err  error
}

func newCueBank(cfg *CueConfig) *cueBank {
	b := &cueBank{
		clips:         make(map[string]CueClip, len(cfg.Clips)),
		ready:         map[string]CueClip{},
		inflight:      map[string]*cueFlight{},
		failed:        map[string]time.Time{},
		now:           time.Now,
		cache:         cfg.Cache,
		tts:           cfg.TTS,
		texts:         cfg.Texts,
		renderTimeout: cfg.RenderTimeout,
		state:         cueSessionUnknown,
	}
	if b.cache == nil {
		b.cache = defaultClipCache()
	}
	for text, clip := range cfg.Clips {
		b.clips[text] = clip
	}
	return b
}

// get is the clip held for text, and whether it is ready to play as it is.
func (b *cueBank) get(text string) (clip CueClip, isReady, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if raw, held := b.clips[text]; held {
		return raw, false, true
	}
	clip, ok = b.ready[text]
	return clip, ok, ok
}

// silent reports whether the session's replies are not spoken.
func (b *cueBank) silent() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state == cueSessionSilent
}

// outputRate is the session's output rate, 0 when no spoken session started.
func (b *cueBank) outputRate() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != cueSessionSpoken {
		return 0
	}
	return b.format.SampleRate
}

// canRender reports whether a source can render a clip on demand.
func (b *cueBank) canRender() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state == cueSessionSpoken && len(b.sources) > 0
}

// voices reports whether a cue with text will be spoken: a clip for it is
// ready or rendering, or a source can render it. With the earcon as the last
// source this is true for every spoken session.
//
//nolint:unused // the provider stage asks it once guardrail messages are cues
func (b *cueBank) voices(text string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == cueSessionSilent {
		return false
	}
	if _, ok := b.clips[text]; ok {
		return true
	}
	if _, ok := b.ready[text]; ok {
		return true
	}
	if _, ok := b.inflight[text]; ok {
		return true
	}
	return b.state == cueSessionSpoken && len(b.sources) > 0
}

// startCues tells the cue stage after s, if any, of the session just created.
// The clips render on their own goroutine: the first reply never waits for
// them.
func (s *DuplexProviderStage) startCues(ctx context.Context) {
	if s.cues != nil {
		s.cues.sessionStarted(ctx, s.session, s.emitter, s.hookRegistry.BlockMessages())
	}
}

// sessionStarted is called by the provider stage right after it creates its
// session. It reads the session's spoken output (providers.OutputAudioReporter),
// picks the clip sources and starts rendering the texts known in advance (the
// configured ones and blockMessages), a few at a time, on a goroutine tied to
// ctx. It never blocks the caller, and a render failing never fails the
// session.
func (b *cueBank) sessionStarted(ctx context.Context, sess providers.StreamInputSession,
	emitter *events.Emitter, blockMessages []string,
) {
	var out providers.OutputAudio
	spoken := true
	if r, ok := sess.(providers.OutputAudioReporter); ok {
		out, spoken = r.OutputAudio()
	} else {
		logger.Warn("CueStage: the session does not report its output audio; cues render as PCM16 at "+
			"24 kHz in the TTS service's default voice", "session", fmt.Sprintf("%T", sess))
	}
	if !spoken {
		b.mu.Lock()
		b.state = cueSessionSilent
		b.mu.Unlock()
		logger.Info("CueStage: the session's replies are not spoken; cues are text only")
		return
	}
	if out.SampleRate <= 0 {
		out.SampleRate = defaultCueSampleRate
	}
	format := ClipFormat{Encoding: AudioFormatPCM16, SampleRate: out.SampleRate, Channels: 1}
	// The session's own out-of-band audio, in its exact voice, goes first
	// once a provider offers one (providers.OutOfBandSpeaker).
	var sources []clipSource
	if src := newTTSClipSource(b.tts, out.Voice, out.SampleRate); src != nil {
		sources = append(sources, src)
	}
	sources = append(sources, earconSource{})

	b.mu.Lock()
	b.state = cueSessionSpoken
	b.format = format
	b.sources = sources
	b.emitter = emitter
	texts := dedupeTexts(b.texts, blockMessages)
	b.mu.Unlock()

	go b.renderAll(ctx, texts, describeSources(sources), out.Voice)
}

// dedupeTexts is the non-empty texts of lists, each once, in order.
func dedupeTexts(lists ...[]string) []string {
	var out []string
	seen := map[string]bool{}
	for _, list := range lists {
		for _, t := range list {
			if t != "" && !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// renderAll renders texts, cueSessionRenders at a time, and logs the outcome.
func (b *cueBank) renderAll(ctx context.Context, texts []string, sources, voice string) {
	sem := make(chan struct{}, cueSessionRenders)
	flights := make([]*cueFlight, 0, len(texts))
	for _, text := range texts {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return
		}
		f := b.render(ctx, text, nil)
		flights = append(flights, f)
		go func() {
			<-f.done
			<-sem
		}()
	}
	rendered := 0
	for _, f := range flights {
		select {
		case <-f.done:
		case <-ctx.Done():
			return
		}
		if f.err == nil {
			rendered++
		}
	}
	logger.Info("CueStage: session clips ready", "sources", sources, "session_voice", voice,
		"texts", len(texts), "rendered", rendered)
}

// render starts rendering text, or joins its render still running: with
// seam (a CueStage's own render func) when it is set, else from the
// sources. The flight's clip is held in the bank once it is done.
func (b *cueBank) render(ctx context.Context, text string,
	seam func(context.Context, string) (CueClip, error),
) *cueFlight {
	b.mu.Lock()
	if f, ok := b.inflight[text]; ok {
		b.mu.Unlock()
		return f
	}
	f := &cueFlight{done: make(chan struct{})}
	b.inflight[text] = f
	b.mu.Unlock()

	go func() {
		// The render outlives the playback that asked for it: a cut or the
		// budget only stops the clip from playing now.
		if seam != nil {
			rctx, cancel := context.WithTimeout(ctx, b.renderTimeout)
			f.clip, f.err = seam(rctx, text)
			f.keep = true
			cancel()
		} else {
			f.clip, f.keep, f.err = b.fromSources(ctx, text)
			f.ready = true
		}
		b.mu.Lock()
		delete(b.inflight, text)
		switch {
		case f.err != nil, !f.keep:
		case f.ready:
			b.ready[text] = f.clip
		default:
			b.clips[text] = f.clip
		}
		stored := b.stored
		b.mu.Unlock()
		if f.err == nil && f.keep && stored != nil {
			stored(text, f.clip)
		}
		close(f.done)
	}()
	return f
}

// fromSources renders text from the first source that can, in order, and
// reports whether the clip is the text's own to keep. A clip from a source
// after one that failed (the earcon) is played this time only: the failed
// sources are tried again for the text once cueRetryBackoff has passed.
func (b *cueBank) fromSources(ctx context.Context, text string) (CueClip, bool, error) {
	b.mu.Lock()
	sources, format := b.sources, b.format
	failedAt, failed := b.failed[text]
	backingOff := failed && b.now().Sub(failedAt) < cueRetryBackoff
	b.mu.Unlock()
	var lastErr error
	fellBack := false
	for i, src := range sources {
		if backingOff && i < len(sources)-1 {
			fellBack = true
			continue
		}
		clip, err := b.fetch(ctx, src, text, format)
		if err == nil {
			if fellBack && !backingOff {
				b.mu.Lock()
				b.failed[text] = b.now()
				b.mu.Unlock()
			}
			return clip, !fellBack, nil
		}
		logger.Debug("CueStage: a clip source failed for a text; trying the next", "source", src.kind(),
			"error", err)
		fellBack = true
		lastErr = err
	}
	logger.Warn("CueStage: no clip source rendered the text; the cue is text only", "text", text,
		"error", lastErr)
	if lastErr == nil {
		lastErr = errNoClipSource
	}
	return CueClip{}, false, lastErr
}

// fetch is src's clip for text in format: from this bank's cache, else
// rendered once for the process and put in the cache. Sessions that need a
// clip at once share one render (the leader's); each puts the clip in its own
// cache. A render that bills reports its cost against the leader's session
// only; a cache hit, and a render joined, are free.
func (b *cueBank) fetch(ctx context.Context, src clipSource, text string, format ClipFormat) (CueClip, error) {
	key := ClipKeyFor(src.keyText(text), src.voice(), src.kind(), format)
	if clip, ok := b.cached(ctx, key); ok {
		return clip, nil
	}
	led := false
	ch := clipFlights.DoChan(string(key[:]), func() (any, error) {
		led = true
		// A session that ends does not fail the render for others waiting.
		return b.renderAndKeep(context.WithoutCancel(ctx), src, text, format, key)
	})
	if hook := clipFlightJoined.Load(); hook != nil {
		(*hook)()
	}
	select {
	case res := <-ch:
		if res.Err != nil {
			return CueClip{}, res.Err
		}
		clip := res.Val.(CueClip)
		if !led {
			// Joined another session's render: keep it in this cache too.
			b.keep(context.WithoutCancel(ctx), key, clip)
		}
		return clip, nil
	case <-ctx.Done():
		return CueClip{}, ctx.Err()
	}
}

// cached is the clip under key in the bank's cache. A cache fault is logged
// and is a miss.
func (b *cueBank) cached(ctx context.Context, key ClipKey) (CueClip, bool) {
	clip, ok, err := b.cache.Get(ctx, key)
	if err != nil {
		logger.Warn("CueStage: the clip cache failed; rendering instead", "error", err)
		return CueClip{}, false
	}
	return clip, ok
}

// keep puts clip in the bank's cache under key, logging a cache fault.
func (b *cueBank) keep(ctx context.Context, key ClipKey, clip CueClip) {
	if err := b.cache.Put(ctx, key, clip); err != nil {
		logger.Warn("CueStage: the clip cache failed to keep a clip", "error", err)
	}
}

// renderAndKeep is the leader's part of fetch: the cache once more (another
// leader may have just put the clip), else a render reported and kept.
func (b *cueBank) renderAndKeep(ctx context.Context, src clipSource, text string, format ClipFormat,
	key ClipKey,
) (any, error) {
	if clip, ok := b.cached(ctx, key); ok {
		return clip, nil
	}
	rctx, cancel := context.WithTimeout(ctx, b.renderTimeout)
	defer cancel()
	start := time.Now()
	clip, cost, err := src.render(rctx, text, format)
	if cost != nil {
		b.reportCost(ctx, cost, clip)
	}
	if err != nil {
		return CueClip{}, err
	}
	logger.Debug("CueStage: rendered a clip", "source", src.kind(), "duration", time.Since(start))
	clip.Meta = ClipMeta{Text: src.keyText(text), Voice: src.voice(), Source: src.kind()}
	b.keep(ctx, key, clip)
	return clip, nil
}

// reportCost emits the cost event of a billed render on the bank's session.
func (b *cueBank) reportCost(ctx context.Context, c *clipRenderCost, clip CueClip) {
	b.mu.Lock()
	emitter := b.emitter
	b.mu.Unlock()
	if emitter == nil {
		return
	}
	data := &events.TTSCallCompletedData{
		CapabilityCallData: events.CapabilityCallData{
			Provider:   c.svc.Name(),
			Capability: string(base.ProviderTypeTTS),
			Source:     cueRenderSource,
			Duration:   c.duration,
		},
		Characters: utf8.RuneCountInString(c.text),
	}
	if clip.Audio != nil {
		data.AudioSeconds = clip.Audio.Duration.Seconds()
	}
	if cost := tts.ComputeTTSCost(c.svc, c.text, c.duration); cost != nil {
		data.Cost = cost.TotalCost
		data.Quantities = cost.Quantities
	}
	emitter.TTSCallCompletedCtx(ctx, data)
}
