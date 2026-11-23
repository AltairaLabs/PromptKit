package cue

import (
	"context"
	"encoding/binary"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

// Clip is a cue's pre-rendered media.
type Clip struct {
	// Audio is PCM16 mono. A clip at another sample rate than the output's is
	// resampled once, before it is first played.
	Audio *core.AudioData
	// Meta says where the clip came from (cueclip.Clip's Meta).
	Meta cueclip.Meta
}

// Config configures a cue stage.
type Config struct {
	// Clips are ready clips by cue text.
	Clips map[string]Clip
	// ChunkDuration is the length of the audio elements a clip is played as;
	// default 100ms.
	ChunkDuration time.Duration
	// OnDemandBudget bounds how long reply output waits behind a clip still
	// rendering; default 1.5s. Past it what waited goes on, after the earcon
	// when the clip renders from the session's sources (NewDuplexCueStage), and
	// with no clip otherwise. The clip itself is kept for the next cue.
	OnDemandBudget time.Duration
	// RenderTimeout bounds one clip's render; default 3s. The session's own
	// audio gets it for each of its tries (it is asked again when it says
	// other than the text). A render that
	// finishes after OnDemandBudget, or after a cut dropped its cue, still
	// keeps its clip for the next cue with that text.
	RenderTimeout time.Duration
	// Cache holds rendered clips across sessions; nil is the process-wide
	// in-memory cache of cueclip.DefaultCacheBytes.
	Cache cueclip.Cache
	// TTS renders clips in the session's voice when the session has no audio
	// of its own to offer. Only a service that renders PCM is used.
	TTS tts.Service
	// Texts are rendered as soon as the session starts, with the messages of
	// the provider stage's guardrails, so their cues play without waiting.
	Texts []string
}

const (
	defaultCueChunkDuration  = 100 * time.Millisecond
	defaultCueOnDemandBudget = 1500 * time.Millisecond
	defaultCueRenderTimeout  = 3 * time.Second
	// defaultCueSampleRate is the output rate before any reply audio is seen,
	// the default DuplexProviderStage gives reply audio.
	defaultCueSampleRate = 24000
	// cueFadeOut is how much of a clip's end is faded to silence, so it never
	// ends on a click.
	cueFadeOut = 10 * time.Millisecond
	// cueSilenceLevel is the largest sample magnitude counted as silence when
	// a clip's leading and trailing silence is trimmed (about -54 dBFS).
	cueSilenceLevel = 64
)

// CueStage plays a pre-rendered clip for each element carrying a Cue. It
// goes directly after the provider stage, ahead of transcript reordering,
// playout tracking, recording and pacing, which treat its audio as they treat
// reply audio (marked Meta.CueAudio).
//
// Every element goes on unchanged and in order. After an element with a Cue
// the stage plays the cue's clip, when it has one, as audio elements of
// ChunkDuration each, PCM16 mono at the reply audio's rate (the first reply
// audio seen, else 24 kHz). A clip is trimmed of leading and trailing
// silence, resampled to that rate and faded out at its end once, the first
// time it plays at that rate. A cue with no clip plays nothing: the element's
// text is already out.
//
// While a clip is still rendering, the stage holds reply output behind it:
// elements go on until the first one carrying audio, which waits, and every
// element after it waits too, so a reply's end never overtakes its audio.
// The clip plays when it is ready, then what waited. A clip not ready within
// OnDemandBudget plays nothing, a cut (barge-in) while it renders drops the
// cue, and reply audio drops a preemptible cue: in each case what waited goes
// on at once, and the render goes on to keep its clip for next time.
type CueStage struct {
	core.BaseStage
	cfg  Config
	bank *Bank
	// render renders a clip for a text the bank does not hold; nil means none
	// can be.
	render func(ctx context.Context, text string) (Clip, error)
}

// NewStage creates a cue stage named "cue".
func NewStage(cfg Config) *CueStage {
	if cfg.ChunkDuration <= 0 {
		cfg.ChunkDuration = defaultCueChunkDuration
	}
	if cfg.OnDemandBudget <= 0 {
		cfg.OnDemandBudget = defaultCueOnDemandBudget
	}
	if cfg.RenderTimeout <= 0 {
		cfg.RenderTimeout = defaultCueRenderTimeout
	}
	return &CueStage{BaseStage: core.NewBaseStage("cue", core.StageTypeTransform), cfg: cfg, bank: newCueBank(&cfg)}
}

// CueOptions configures the cue stage NewDuplexCueStage builds.
type CueOptions struct {
	// Voice renders clips in the session's voice when the session has no
	// audio of its own to offer. Only a service that renders PCM is used; nil
	// plays the clips pre-warmed for the session's voice (PrewarmClips), and
	// the earcon for any other text.
	Voice tts.Service
	// Cache holds rendered clips across sessions; nil is the process-wide
	// in-memory cache of cueclip.DefaultCacheBytes.
	Cache cueclip.Cache
}

// cueRender is the playback of a clip still rendering for cue.
type cueRender struct {
	cue    core.Cue
	flight *cueFlight
	budget *time.Timer
}

// cueQueued is an element waiting behind a rendering clip. sent marks one
// already forwarded whose cue's clip still has to play.
type cueQueued struct {
	elem core.StreamElement
	sent bool
}

// cueRun is one Process run's state.
type cueRun struct {
	s      *CueStage
	output chan<- core.StreamElement
	// prepared are clips ready to play, by text and output rate.
	prepared map[cuePreparedKey][]byte
	// rate is the output sample rate: the first reply audio's, 0 until seen.
	rate    int
	pending *cueRender
	queue   []cueQueued
	// fwd carries the run's output across a cancellation (see send).
	fwd core.CancelForwarder
}

type cuePreparedKey struct {
	text string
	rate int
}

// Process implements the Stage interface.
func (s *CueStage) Process(
	ctx context.Context, input <-chan core.StreamElement, output chan<- core.StreamElement,
) error {
	defer close(output)
	r := &cueRun{s: s, output: output, prepared: map[cuePreparedKey][]byte{}}
	defer r.discard()
	for {
		done, err := r.step(ctx, input)
		if err != nil && !done && ctx.Err() != nil {
			// A send or a clip the cancellation cut short.
			return r.passThroughCanceled(ctx, input)
		}
		if done || err != nil {
			return err
		}
	}
}

// step handles the next input element or the end of the clip rendering,
// whichever comes first. It reports whether the run is over.
func (r *cueRun) step(ctx context.Context, input <-chan core.StreamElement) (bool, error) {
	var result <-chan struct{}
	var budget <-chan time.Time
	if r.pending != nil {
		result, budget = r.pending.flight.done, r.pending.budget.C
	}
	select {
	case elem, ok := <-input:
		if !ok {
			return true, r.finish(ctx, input)
		}
		return false, r.handle(ctx, elem)
	case <-result:
		return false, r.resolve(ctx)
	case <-budget:
		return false, r.overBudget(ctx)
	case <-ctx.Done():
		return true, r.passThroughCanceled(ctx, input)
	}
}

// passThroughCanceled ends a canceled run: a cue still to play is not played,
// but what the stages upstream still deliver (the partial reply a canceled
// provider emits, its error) is carried on to the save stage, with what
// waited behind the cue first. See CancelForwarder.
func (r *cueRun) passThroughCanceled(ctx context.Context, input <-chan core.StreamElement) error {
	r.discard()
	for i := range r.queue {
		if !r.queue[i].sent {
			r.fwd.Forward(ctx, r.output, r.queue[i].elem)
		}
	}
	r.queue = nil
	for elem := range input {
		r.fwd.Forward(ctx, r.output, elem)
	}
	return ctx.Err()
}

// send passes an element on. Once the run is canceled the element is still
// offered downstream for a short while (see CancelForwarder), so the one in
// hand when the cancellation lands is not lost, and send returns the
// cancellation so that no clip plays after it.
func (r *cueRun) send(ctx context.Context, elem core.StreamElement) error {
	r.fwd.Forward(ctx, r.output, elem)
	return ctx.Err()
}

// finish waits for a clip still rendering, within its budget, and plays it
// and what waited behind it: the input is over, its output is not.
func (r *cueRun) finish(ctx context.Context, input <-chan core.StreamElement) error {
	for r.pending != nil {
		var err error
		select {
		case <-r.pending.flight.done:
			err = r.resolve(ctx)
		case <-r.pending.budget.C:
			err = r.overBudget(ctx)
		case <-ctx.Done():
			return r.passThroughCanceled(ctx, input)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// handle forwards one input element and plays its cue's clip.
func (r *cueRun) handle(ctx context.Context, elem core.StreamElement) error {
	r.noteRate(&elem)
	if r.pending == nil {
		return r.forward(ctx, elem)
	}
	if core.IsCut(&elem) {
		return r.cutPending(ctx, elem)
	}
	if r.pending.cue.Preemptible && hasAudio(&elem) && elem.Meta.CueAudio == nil {
		// Reply audio does not wait for a preemptible cue.
		r.discard()
		if err := r.drain(ctx); err != nil {
			return err
		}
		return r.handle(ctx, elem)
	}
	if len(r.queue) > 0 || hasAudio(&elem) {
		r.queue = append(r.queue, cueQueued{elem: elem})
		return nil
	}
	if err := r.send(ctx, elem); err != nil {
		return err
	}
	if elem.Cue != nil {
		r.queue = append(r.queue, cueQueued{elem: elem, sent: true})
	}
	return nil
}

// forward sends elem on and plays its cue's clip.
func (r *cueRun) forward(ctx context.Context, elem core.StreamElement) error {
	if err := r.send(ctx, elem); err != nil {
		return err
	}
	if elem.Cue != nil {
		return r.play(ctx, *elem.Cue)
	}
	return nil
}

// cutPending handles a cut while a clip renders: the clip would have been cut
// anyway, so it and the clips of the cues queued behind it are dropped, and
// the elements queued behind it go on, in order, ahead of the cut.
func (r *cueRun) cutPending(ctx context.Context, cut core.StreamElement) error {
	r.discard()
	queued := r.queue
	r.queue = nil
	for i := range queued {
		if !queued[i].sent {
			_ = r.send(ctx, queued[i].elem) // canceled: the rest still go on
		}
	}
	return r.send(ctx, cut)
}

// hasAudio reports whether elem carries audio samples.
func hasAudio(elem *core.StreamElement) bool {
	return elem.Audio != nil && len(elem.Audio.Samples) > 0
}

// noteRate takes the output rate from the first reply audio.
func (r *cueRun) noteRate(elem *core.StreamElement) {
	if r.rate == 0 && hasAudio(elem) && elem.Audio.SampleRate > 0 {
		r.rate = elem.Audio.SampleRate
	}
}

// outputRate is the rate clips play at: the first reply audio's, else the
// session's output rate, else 24 kHz.
func (r *cueRun) outputRate() int {
	if r.rate > 0 {
		return r.rate
	}
	if rate := r.s.bank.outputRate(); rate > 0 {
		return rate
	}
	return defaultCueSampleRate
}

// play plays cue's clip when it is ready, starts rendering it (or waits for
// its render still running) when it can be rendered, and otherwise plays
// nothing. A session whose replies are not spoken gets no clip.
func (r *cueRun) play(ctx context.Context, cue core.Cue) error {
	bank := r.s.bank
	if bank.silent() {
		return nil
	}
	if clip, isReady, ok := bank.get(cue.Text); ok {
		return r.emit(ctx, cue, clip, isReady)
	}
	if r.s.render == nil && !bank.canRender() {
		logger.Debug("CueStage: no clip for the cue", "kind", cue.Kind)
		return nil
	}
	r.pending = &cueRender{cue: cue, flight: bank.render(ctx, cue.Text, r.s.render),
		budget: time.NewTimer(r.s.cfg.OnDemandBudget)}
	return nil
}

// resolve plays a finished render's clip, then what waited behind it.
func (r *cueRun) resolve(ctx context.Context) error {
	p := r.pending
	r.discard()
	if err := p.flight.err; err != nil {
		logger.Warn("CueStage: the cue's clip did not render", "kind", p.cue.Kind, "error", err)
	} else if err := r.emit(ctx, p.cue, p.flight.clip, p.flight.ready); err != nil {
		return err
	}
	return r.drain(ctx)
}

// overBudget gives up waiting for a clip that is not ready within
// OnDemandBudget. When the session's clips come from the sources, the earcon
// plays at once in its place (not for a preemptible cue, which gives way).
func (r *cueRun) overBudget(ctx context.Context) error {
	select {
	case <-r.pending.flight.done: // it finished as the budget ran out
		return r.resolve(ctx)
	default:
	}
	cue := r.pending.cue
	logger.Warn("CueStage: the cue's clip was not ready in time", "kind", cue.Kind,
		"budget", r.s.cfg.OnDemandBudget)
	r.discard()
	if !cue.Preemptible && r.s.bank.canRender() {
		rate := r.outputRate()
		if err := r.emitPCM(ctx, cue, earconPCM(rate), rate); err != nil {
			return err
		}
	}
	return r.drain(ctx)
}

// discard gives up the playback of the clip rendering, if any. The render
// itself goes on.
func (r *cueRun) discard() {
	if r.pending != nil {
		r.pending.budget.Stop()
		r.pending = nil
	}
}

// drain forwards what waited behind a render, until a queued cue's clip has
// to render in turn.
func (r *cueRun) drain(ctx context.Context) error {
	for len(r.queue) > 0 && r.pending == nil {
		q := r.queue[0]
		r.queue = r.queue[1:]
		var err error
		switch {
		case q.sent && q.elem.Cue.Preemptible && r.replyAudioQueued():
			// A preemptible cue gives way to the reply audio waiting behind it.
		case q.sent:
			err = r.play(ctx, *q.elem.Cue)
		default:
			err = r.forward(ctx, q.elem)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// replyAudioQueued reports whether reply audio waits in the queue.
func (r *cueRun) replyAudioQueued() bool {
	for i := range r.queue {
		if e := &r.queue[i].elem; !r.queue[i].sent && hasAudio(e) && e.Meta.CueAudio == nil {
			return true
		}
	}
	return false
}

// emit plays clip for cue as audio elements of ChunkDuration each. A ready
// clip at the output rate plays as it is; any other is prepared first.
func (r *cueRun) emit(ctx context.Context, cue core.Cue, clip Clip, isReady bool) error {
	rate := r.outputRate()
	key := cuePreparedKey{text: cue.Text, rate: rate}
	pcm, ok := r.prepared[key]
	if !ok {
		if isReady && clip.Audio != nil && clip.Audio.SampleRate == rate {
			// The bytes are shared with the cache: play a copy.
			pcm = append([]byte(nil), clip.Audio.Samples...)
		} else {
			pcm = prepareCueAudio(clip.Audio, rate)
		}
		r.prepared[key] = pcm
	}
	return r.emitPCM(ctx, cue, pcm, rate)
}

// emitPCM plays pcm, ready to play at rate, for cue.
func (r *cueRun) emitPCM(ctx context.Context, cue core.Cue, pcm []byte, rate int) error {
	chunk := core.BytesPerSamplePCM16 * int(int64(rate)*int64(r.s.cfg.ChunkDuration)/int64(time.Second))
	if chunk < core.BytesPerSamplePCM16 {
		chunk = core.BytesPerSamplePCM16
	}
	for start := 0; start < len(pcm); start += chunk {
		end := min(start+chunk, len(pcm))
		samples := pcm[start:end:end]
		elem := core.NewAudioElement(&core.AudioData{
			Samples:    samples,
			SampleRate: rate,
			Channels:   1,
			Format:     core.AudioFormatPCM16,
			Duration:   audio.PCM16Duration(len(samples), rate, 1),
		})
		c := cue
		elem.Meta.CueAudio = &c
		if err := core.SendStreamElement(ctx, elem, r.output); err != nil {
			return err
		}
	}
	return nil
}

// prepareCueAudio is a clip's audio ready to play at rate: trimmed of leading
// and trailing silence, resampled, and faded out at its end. A clip that is
// not PCM16 mono plays nothing.
func prepareCueAudio(a *core.AudioData, rate int) []byte {
	if a == nil || len(a.Samples) == 0 {
		return nil
	}
	if a.Format != core.AudioFormatPCM16 || a.Channels > 1 {
		logger.Warn("CueStage: a clip that is not PCM16 mono is not played",
			"format", a.Format.String(), "channels", a.Channels)
		return nil
	}
	pcm := trimPCM16Silence(a.Samples)
	if from := a.SampleRate; from > 0 && from != rate && len(pcm) > 0 {
		resampled, err := audio.ResamplePCM16(pcm, from, rate)
		if err != nil {
			logger.Warn("CueStage: the clip could not be resampled", "error", err)
			return nil
		}
		pcm = resampled
	} else {
		pcm = append([]byte(nil), pcm...)
	}
	fade := min(int(int64(rate)*int64(cueFadeOut)/int64(time.Second)), len(pcm)/core.BytesPerSamplePCM16)
	tail := len(pcm) - core.BytesPerSamplePCM16*fade
	copy(pcm[tail:], audio.FadeOutPCM16(pcm[tail:], fade))
	return pcm
}

// trimPCM16Silence is pcm (little-endian mono PCM16) without its leading and
// trailing samples at or below cueSilenceLevel.
func trimPCM16Silence(pcm []byte) []byte {
	n := len(pcm) / core.BytesPerSamplePCM16
	loud := func(i int) bool {
		//nolint:gosec // reinterpreting PCM16 wire bytes as a signed sample
		s := int(int16(binary.LittleEndian.Uint16(pcm[core.BytesPerSamplePCM16*i:])))
		return s > cueSilenceLevel || s < -cueSilenceLevel
	}
	first := 0
	for first < n && !loud(first) {
		first++
	}
	last := n
	for last > first && !loud(last-1) {
		last--
	}
	return pcm[core.BytesPerSamplePCM16*first : core.BytesPerSamplePCM16*last]
}
