package stage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	providersmock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

// fakeTTS is a TTS service that records each synthesis and answers with
// audio(text). With gate set, a synthesis waits for it to close.
type fakeTTS struct {
	voices  []tts.Voice
	formats []tts.AudioFormat
	audio   func(text string) ([]byte, error)
	gate    chan struct{}

	mu      sync.Mutex
	configs []tts.SynthesisConfig
	texts   []string
}

func (f *fakeTTS) Name() string                        { return "fake" }
func (f *fakeTTS) SupportedVoices() []tts.Voice        { return f.voices }
func (f *fakeTTS) SupportedFormats() []tts.AudioFormat { return f.formats }

func (f *fakeTTS) Synthesize(ctx context.Context, text string, cfg tts.SynthesisConfig) (io.ReadCloser, error) {
	f.mu.Lock()
	f.configs = append(f.configs, cfg)
	f.texts = append(f.texts, text)
	f.mu.Unlock()
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	pcm, err := f.audio(text)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(pcm)), nil
}

// calls is how many times text was synthesized.
func (f *fakeTTS) calls(text string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, t := range f.texts {
		if t == text {
			n++
		}
	}
	return n
}

func (f *fakeTTS) lastConfig() tts.SynthesisConfig {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.configs[len(f.configs)-1]
}

var pcmAt24k = tts.AudioFormat{Name: "pcm", MIMEType: "audio/pcm", SampleRate: 24000, BitDepth: 16, Channels: 1}

// newFakeTTS answers every text with pcm, as PCM at 24 kHz.
func newFakeTTS(pcm []byte) *fakeTTS {
	return &fakeTTS{
		voices:  []tts.Voice{{ID: "alloy", Name: "Alloy"}, {ID: "v-2", Name: "Verse"}},
		formats: []tts.AudioFormat{tts.FormatMP3, pcmAt24k},
		audio:   func(string) ([]byte, error) { return pcm, nil },
	}
}

// pricedTTS is a fakeTTS billed per character.
type pricedTTS struct{ *fakeTTS }

func (pricedTTS) Pricing() *base.PricingDescriptor {
	return &base.PricingDescriptor{Items: []base.PriceItem{{Unit: "character", Rate: 0.001}}}
}
func (pricedTTS) ImplName() string  { return "fake" }
func (pricedTTS) ModelName() string { return "" }

// spokenSession is a session whose replies are spoken in voice at rate.
func spokenSession(voice string, rate int) *providersmock.MockStreamSession {
	return providersmock.NewMockStreamSession().WithOutputAudio(
		providers.OutputAudio{Voice: voice, SampleRate: rate, Family: "test-live"})
}

// cueStageForSession is a cue stage wired to a provider stage, told that the
// provider stage created sess.
func cueStageForSession(t *testing.T, cfg cueConfig, sess providers.StreamInputSession) *cueStage {
	t.Helper()
	return cueStageForSessionWith(t, cfg, sess, nil)
}

// cueStageForSessionWith is cueStageForSession reporting through emitter.
func cueStageForSessionWith(t *testing.T, cfg cueConfig, sess providers.StreamInputSession,
	emitter *events.Emitter, blockMessages ...string,
) *cueStage {
	t.Helper()
	duplex := NewDuplexProviderStage(providersmock.NewStreamingProvider("t", "m", false), baseConfig())
	cue := newCueStageFor(duplex, cfg)
	require.Same(t, cue.bank, duplex.cues)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	cue.bank.sessionStarted(ctx, sess, emitter, blockMessages)
	return cue
}

// playedFor runs one cue with text through cue and returns its clip's audio.
func playedFor(t *testing.T, cue *cueStage, text string) []byte {
	t.Helper()
	c := startChain(t, cue)
	c.in <- guardrailCueElem(text)
	got := c.finish(t)
	require.NotEmpty(t, got)
	assert.Equal(t, text, *got[0].Text, "the text goes out")
	return cueAudioOf(got)
}

// lockedLog is a log sink safe to read while background renders write.
type lockedLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// captureCueLogs routes the package logger into a lockedLog for the test.
func captureCueLogs(t *testing.T) *lockedLog {
	t.Helper()
	l := &lockedLog{}
	logger.SetOutput(l)
	t.Cleanup(func() { logger.SetOutput(nil) })
	return l
}

func resampled(t *testing.T, pcm []byte, from, to int) []byte {
	t.Helper()
	out, err := audio.ResamplePCM16(pcm, from, to)
	require.NoError(t, err)
	return out
}

// The TTS source speaks in the service voice whose ID or name is the
// session's voice, ignoring case, asking for PCM at the session's rate. With
// no such voice it uses the service's default and warns once, naming both.
func TestCueVoice_TTSMatchesTheSessionVoice(t *testing.T) {
	cases := []struct {
		sessionVoice string
		wantVoice    string
		wantWarning  bool
	}{
		{"ALLOY", "alloy", false},
		{"verse", "v-2", false},
		{"nova", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.sessionVoice, func(t *testing.T) {
			logs := captureCueLogs(t)
			pcm := cueRamp(4800)
			svc := newFakeTTS(pcm)
			cue := cueStageForSession(t, cueConfig{TTS: svc, Cache: cueclip.NewMemoryCache(1 << 20)},
				spokenSession(tc.sessionVoice, 24000))
			text := "Voice match " + tc.sessionVoice
			assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue, text))
			assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue, text+"!"))

			cfg := svc.lastConfig()
			assert.Equal(t, tc.wantVoice, cfg.Voice)
			assert.Equal(t, pcmAt24k, cfg.Format)
			warnings := strings.Count(logs.String(), "no voice matching the session's")
			if tc.wantWarning {
				assert.Equal(t, 1, warnings, "one warning per session")
				assert.Contains(t, logs.String(), "session_voice=nova")
				assert.Contains(t, logs.String(), "cue_voice=default")
			} else {
				assert.Zero(t, warnings)
			}
		})
	}
}

// A service that renders PCM only at another rate is asked for that rate,
// and its clip is resampled once to the session's.
func TestCueVoice_TTSClipIsResampled(t *testing.T) {
	pcm16 := cueRamp(3200) // 200 ms at 16 kHz
	pcmAt16k := tts.AudioFormat{Name: "pcm", SampleRate: 16000, BitDepth: 16, Channels: 1}
	svc := newFakeTTS(pcm16)
	svc.formats = []tts.AudioFormat{pcmAt16k}
	cue := cueStageForSession(t, cueConfig{TTS: svc, Cache: cueclip.NewMemoryCache(1 << 20)},
		spokenSession("alloy", 24000))

	got := playedFor(t, cue, "Resampled clip.")
	assert.Equal(t, cueFaded(resampled(t, pcm16, 16000, 24000), 24000), got)
	assert.Equal(t, pcmAt16k, svc.lastConfig().Format)
}

// A service without PCM is skipped for every text: the earcon plays.
func TestCueVoice_TTSWithoutPCMIsSkipped(t *testing.T) {
	svc := newFakeTTS(cueRamp(4800))
	svc.formats = []tts.AudioFormat{tts.FormatMP3, tts.FormatOpus}
	cue := cueStageForSession(t, cueConfig{TTS: svc, Cache: cueclip.NewMemoryCache(1 << 20)},
		spokenSession("alloy", 16000))

	assert.Equal(t, earconPCM(16000), playedFor(t, cue, "No PCM here."))
	assert.Zero(t, svc.calls("No PCM here."))
}

// A TTS error falls to the next source for that text only.
func TestCueVoice_TTSErrorFallsThroughForThatTextOnly(t *testing.T) {
	pcm := cueRamp(4800)
	svc := newFakeTTS(pcm)
	svc.audio = func(text string) ([]byte, error) {
		if text == "Bad text." {
			return nil, errors.New("synthesis failed")
		}
		return pcm, nil
	}
	cue := cueStageForSession(t, cueConfig{TTS: svc, Cache: cueclip.NewMemoryCache(1 << 20)},
		spokenSession("alloy", 24000))

	assert.Equal(t, earconPCM(24000), playedFor(t, cue, "Bad text."))
	assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue, "Good text."))
}

// waitStoredAll waits for n stored clips and returns their texts.
func waitStoredAll(t *testing.T, ch <-chan string, n int) []string {
	t.Helper()
	var texts []string
	for i := 0; i < n; i++ {
		texts = append(texts, waitStored(t, ch))
	}
	return texts
}

// At session start the configured texts and the guardrails' messages render,
// each once, so their cues play at once.
func TestCueVoice_SessionStartRendersKnownTexts(t *testing.T) {
	pcm := cueRamp(4800)
	svc := newFakeTTS(pcm)
	duplex := NewDuplexProviderStage(providersmock.NewStreamingProvider("t", "m", false), baseConfig())
	cue := newCueStageFor(duplex, cueConfig{TTS: svc, Cache: cueclip.NewMemoryCache(1 << 20),
		Texts: []string{"Known one.", ""}})
	stored := storedTexts(cue)
	cue.bank.sessionStarted(context.Background(), spokenSession("alloy", 24000), nil,
		[]string{"Known two.", "Known one."})

	assert.ElementsMatch(t, []string{"Known one.", "Known two."}, waitStoredAll(t, stored, 2))
	assert.Equal(t, 1, svc.calls("Known one."))
	assert.Equal(t, 1, svc.calls("Known two."))

	// Ready: the clip plays ahead of the reply audio that follows it, with
	// no render.
	c := startChain(t, cue)
	reply := cueRamp(100)
	c.in <- guardrailCueElem("Known two.")
	c.in <- replyAudioOf(reply)
	got := c.finish(t)
	require.Len(t, got, 4)
	assert.Equal(t, cueFaded(pcm, 24000), cueAudioOf(got[1:3]))
	assert.Equal(t, reply, got[3].Audio.Samples)
	assert.Equal(t, 1, svc.calls("Known two."))
}

// Fifty sessions of one pack starting at once against one cache render each
// text once, and every session plays the same bytes.
func TestCueVoice_ConcurrentSessionsRenderOnce(t *testing.T) {
	pcm := cueRamp(4800)
	svc := newFakeTTS(pcm)
	svc.gate = make(chan struct{})
	cache := cueclip.NewMemoryCache(cueclip.DefaultCacheBytes)
	texts := []string{"Shared one.", "Shared two."}

	const sessions = 50
	stored := make(chan string, 2*sessions)
	banks := make([]*cueBank, sessions)
	var wg sync.WaitGroup
	for i := range banks {
		cue := newCueStageFor(
			NewDuplexProviderStage(providersmock.NewStreamingProvider("t", "m", false), baseConfig()),
			cueConfig{TTS: svc, Cache: cache, Texts: texts})
		cue.bank.stored = func(text string, _ cueClip) { stored <- text }
		banks[i] = cue.bank
		wg.Add(1)
		go func() {
			defer wg.Done()
			cue.bank.sessionStarted(context.Background(), spokenSession("alloy", 24000), nil, nil)
		}()
	}
	wg.Wait()
	close(svc.gate)
	waitStoredAll(t, stored, 2*sessions)

	assert.Equal(t, 1, svc.calls("Shared one."))
	assert.Equal(t, 1, svc.calls("Shared two."))
	for _, text := range texts {
		for i, b := range banks {
			clip, isReady, ok := b.get(text)
			require.True(t, ok, "bank %d has %q", i, text)
			assert.True(t, isReady)
			assert.Equal(t, cueFaded(pcm, 24000), clip.Audio.Samples, "bank %d", i)
		}
	}
}

// faultyCache fails every Get and keeps what is Put.
type faultyCache struct {
	cueclip.Cache
	puts int
}

func (c *faultyCache) Get(context.Context, cueclip.Key) (cueclip.Clip, bool, error) {
	return cueclip.Clip{}, false, errors.New("cache down")
}

func (c *faultyCache) Put(ctx context.Context, key cueclip.Key, clip cueclip.Clip) error {
	c.puts++
	return c.Cache.Put(ctx, key, clip)
}

// A cache whose Get fails is a miss: the clip renders and still plays.
func TestCueVoice_CacheFaultIsAMiss(t *testing.T) {
	pcm := cueRamp(4800)
	svc := newFakeTTS(pcm)
	cache := &faultyCache{Cache: cueclip.NewMemoryCache(1 << 20)}
	cue := cueStageForSession(t, cueConfig{TTS: svc, Cache: cache}, spokenSession("alloy", 24000))

	assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue, "Cache fault."))
	assert.Equal(t, 1, svc.calls("Cache fault."))
	assert.Equal(t, 1, cache.puts)
}

// A cache warmed ahead of time is played from with no synthesis at all.
func TestCueVoice_PrewarmedCacheNeedsNoSynthesis(t *testing.T) {
	f := fixturePrewarmed
	svc := newFakeTTS(cueRamp(4800))
	cue := cueStageForSession(t, cueConfig{TTS: svc, Cache: cueFixtureCache(t), Texts: []string{f.text}},
		spokenSession("Alloy", 24000))

	assert.Equal(t, f.pcm(), playedFor(t, cue, f.text), "a cached clip plays as it is")
	assert.Zero(t, svc.calls(f.text))
}

// A clip the bank renders into a directory cache is what a later bank on a
// read-only copy of that directory plays with no TTS at all.
func TestCueVoice_DirCacheCarriesARenderToAnotherSession(t *testing.T) {
	dir := t.TempDir()
	w, err := cueclip.NewDirCache(dir)
	require.NoError(t, err)
	pcm := cueRamp(4800)
	svc := newFakeTTS(pcm)
	text := "Rendered into a directory."
	first := cueStageForSession(t, cueConfig{TTS: svc, Cache: w, Texts: []string{text}}, spokenSession("alloy", 24000))
	want := playedFor(t, first, text)
	assert.Equal(t, cueFaded(pcm, 24000), want)

	ro, err := cueclip.NewDirCache(dir, cueclip.ReadOnly())
	require.NoError(t, err)
	got, ok, err := ro.Get(context.Background(), cueclip.KeyFor(text, "alloy", "tts:fake", pcm24k))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, cueclip.Meta{Text: text, Voice: "alloy", Source: "tts:fake"}, got.Meta)

	quiet := newFakeTTS(cueRamp(10))
	second := cueStageForSession(t, cueConfig{TTS: quiet, Cache: ro, Texts: []string{text}}, spokenSession("alloy", 24000))
	assert.Equal(t, want, playedFor(t, second, text))
	assert.Zero(t, quiet.calls(text))
}

// A text known only when cued renders on demand. Past OnDemandBudget the
// earcon plays at once and what waited goes on; the late clip is cached and
// plays for the next cue with that text.
func TestCueVoice_OnDemandBudgetPlaysTheEarcon(t *testing.T) {
	pcm := cueRamp(4800)
	svc := newFakeTTS(pcm)
	svc.gate = make(chan struct{})
	cache := cueclip.NewMemoryCache(1 << 20)
	cue := cueStageForSession(t, cueConfig{TTS: svc, Cache: cache, OnDemandBudget: 5 * time.Millisecond},
		spokenSession("alloy", 24000))
	stored := storedTexts(cue)
	text := "A replacement decided when it fired."

	c := startChain(t, cue)
	reply := cueRamp(100)
	c.in <- guardrailCueElem(text)
	c.in <- replyAudioOf(reply)
	assert.Equal(t, text, *c.next(t).Text, "the text is shown at once")
	var earcon []byte
	for i := 0; i < 2; i++ { // 200 ms of earcon in 100 ms chunks
		e := c.next(t)
		require.NotNil(t, e.Meta.CueAudio)
		assert.Equal(t, &Cue{Kind: CueGuardrailMessage, Text: text}, e.Meta.CueAudio)
		earcon = append(earcon, e.Audio.Samples...)
	}
	assert.Equal(t, earconPCM(24000), earcon)
	assert.Equal(t, reply, c.next(t).Audio.Samples)

	close(svc.gate)
	assert.Equal(t, text, waitStored(t, stored))
	cached, ok, err := cache.Get(context.Background(), cueclip.KeyFor(text, "alloy", "tts:fake", pcm24k))
	require.NoError(t, err)
	require.True(t, ok, "the late clip is cached")
	assert.Equal(t, cueFaded(pcm, 24000), cached.PCM)

	c.in <- guardrailCueElem(text)
	assert.Equal(t, cueFaded(pcm, 24000), cueAudioOf(c.finish(t)))
	assert.Equal(t, 1, svc.calls(text))
}

// ttsEvents collects the tts.call.completed events on bus.
func ttsEvents(t *testing.T, bus *events.EventBus) func() []*events.TTSCallCompletedData {
	t.Helper()
	var mu sync.Mutex
	var got []*events.TTSCallCompletedData
	bus.Subscribe(events.EventTTSCallCompleted, func(e *events.Event) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, e.Data.(*events.TTSCallCompletedData))
	})
	return func() []*events.TTSCallCompletedData {
		bus.Close()
		mu.Lock()
		defer mu.Unlock()
		return got
	}
}

// A render's cost is one cost event on the session that rendered it; a
// session that finds the clip in the cache reports none.
func TestCueVoice_RenderCostIsReportedOnce(t *testing.T) {
	pcm := cueRamp(4800)
	svc := pricedTTS{newFakeTTS(pcm)}
	cache := cueclip.NewMemoryCache(1 << 20)
	text := "Billed text."

	bus1 := events.NewEventBus()
	got1 := ttsEvents(t, bus1)
	cue1 := cueStageForSessionWith(t, cueConfig{TTS: svc, Cache: cache}, spokenSession("alloy", 24000),
		events.NewEmitter(bus1, "run1", "sess1", "conv1"))
	assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue1, text))

	bus2 := events.NewEventBus()
	got2 := ttsEvents(t, bus2)
	cue2 := cueStageForSessionWith(t, cueConfig{TTS: svc, Cache: cache}, spokenSession("alloy", 24000),
		events.NewEmitter(bus2, "run2", "sess2", "conv2"))
	assert.Equal(t, cueFaded(pcm, 24000), playedFor(t, cue2, text))

	evs := got1()
	require.Len(t, evs, 1)
	ev := evs[0]
	assert.Equal(t, "fake", ev.Provider)
	assert.Equal(t, "tts", ev.Capability)
	assert.Equal(t, "cue", ev.Source)
	assert.Equal(t, len(text), ev.Characters)
	assert.InDelta(t, 0.001*float64(len(text)), ev.Cost, 1e-12)
	assert.Equal(t, map[string]float64{"character": float64(len(text))}, ev.Quantities)
	assert.InDelta(t, cueFadedSeconds(pcm), ev.AudioSeconds, 1e-9)
	assert.Empty(t, got2(), "a cache hit is free")
	assert.Equal(t, 1, svc.calls(text))
}

// cueFadedSeconds is how long pcm at 24 kHz plays.
func cueFadedSeconds(pcm []byte) float64 {
	return float64(len(pcm)/2) / 24000
}

// A session whose replies are not spoken gets no cue audio, not even a
// configured clip, and nothing renders for it.
func TestCueVoice_TextOnlySession(t *testing.T) {
	svc := newFakeTTS(cueRamp(4800))
	text := "Text only."
	cue := cueStageForSession(t, cueConfig{TTS: svc, Cache: cueclip.NewMemoryCache(1 << 20),
		Texts: []string{text},
		Clips: map[string]cueClip{text: pcmClip(cueRamp(2400), 24000)}},
		providersmock.NewMockStreamSession())

	assert.Empty(t, playedFor(t, cue, text))
	assert.Empty(t, playedFor(t, cue, "Another."))
	assert.False(t, cue.bank.voices(text))
	assert.Zero(t, svc.calls(text))
}

// voices reports whether a cue will be spoken: for every text in a spoken
// session (the earcon is always there), for a held clip's text before any
// session, and for none in a text-only session.
func TestCueVoice_Voices(t *testing.T) {
	spoken := cueStageForSession(t, cueConfig{Cache: cueclip.NewMemoryCache(1 << 20)}, spokenSession("alloy", 24000))
	assert.True(t, spoken.bank.voices("Anything at all."))

	plain := newCueStage(cueConfig{Clips: map[string]cueClip{brunoText: pcmClip(cueRamp(2400), 24000)}})
	assert.True(t, plain.bank.voices(brunoText))
	assert.False(t, plain.bank.voices("Unknown."))

	silent := cueStageForSession(t, cueConfig{Cache: cueclip.NewMemoryCache(1 << 20)},
		providersmock.NewMockStreamSession())
	assert.False(t, silent.bank.voices("Anything at all."))
}

// Without a TTS service a spoken session's cues play the earcon at the
// session's output rate, before any reply audio fixes the rate.
func TestCueVoice_EarconAtTheSessionRate(t *testing.T) {
	cue := cueStageForSession(t, cueConfig{Cache: cueclip.NewMemoryCache(1 << 20)}, spokenSession("Puck", 16000))
	c := startChain(t, cue)
	c.in <- guardrailCueElem("Earcon only.")
	got := c.finish(t)
	require.Len(t, got, 3)
	for _, e := range got[1:] {
		assert.Equal(t, 16000, e.Audio.SampleRate)
	}
	assert.Equal(t, earconPCM(16000), cueAudioOf(got))
}

// blockMessageHook is a provider hook that blocks with msg.
type blockMessageHook struct{ msg string }

func (h blockMessageHook) Name() string { return "block" }
func (h blockMessageHook) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (h blockMessageHook) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}
func (h blockMessageHook) BlockMessages() []string { return []string{h.msg} }

// When the provider stage creates its session, the cue stage starts
// rendering its guardrails' messages in the session's voice, and the first
// reply does not wait for it.
func TestCueVoice_SessionStartDoesNotDelayTheFirstReply(t *testing.T) {
	pcm := cueRamp(4800)
	svc := newFakeTTS(pcm)
	svc.gate = make(chan struct{})
	provider := providersmock.NewStreamingProvider("t", "m", false).WithAutoRespond("Hello.").
		WithOutputAudio(providers.OutputAudio{Voice: "Verse", SampleRate: 24000, Family: "test-live"})
	duplex := NewDuplexProviderStage(provider, baseConfig())
	duplex.SetHookRegistry(hooks.NewRegistry(hooks.WithProviderHook(blockMessageHook{msg: "Guarded."})))
	cue := newCueStageFor(duplex, cueConfig{TTS: svc, Cache: cueclip.NewMemoryCache(1 << 20)})
	stored := storedTexts(cue)

	text := "hi"
	out, err := runDuplexStage(t, duplex, []StreamElement{{Text: &text, EndOfStream: true}})
	require.NoError(t, err)
	var replies []string
	for _, e := range out {
		if e.Message != nil && e.Message.Role == roleAssistant {
			replies = append(replies, e.Message.Content)
		}
	}
	require.NotEmpty(t, replies, "the reply came while the clip was still rendering")
	assert.Equal(t, "Hello.", replies[0])
	select {
	case got := <-stored:
		require.FailNow(t, fmt.Sprintf("the clip for %q rendered before it was released", got))
	default:
	}

	close(svc.gate)
	assert.Equal(t, "Guarded.", waitStored(t, stored))
	assert.Equal(t, "v-2", svc.lastConfig().Voice, "in the session's voice")
	clip, isReady, ok := cue.bank.get("Guarded.")
	require.True(t, ok)
	assert.True(t, isReady)
	assert.Equal(t, cueFaded(pcm, 24000), clip.Audio.Samples)
}
