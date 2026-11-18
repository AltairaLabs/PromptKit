package stage

import (
	"context"
	"encoding/binary"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// CueKind says what a cue is for.
type CueKind string

const (
	// CueGuardrailMessage is a guardrail's message, spoken when it cuts a reply.
	CueGuardrailMessage CueKind = "guardrail_message"
	// CueBackchannel is a short acknowledgment ("mm-hm") played while the
	// caller speaks.
	CueBackchannel CueKind = "backchannel"
)

// Cue asks a CueStage to play the clip for Text.
type Cue struct {
	Kind CueKind
	// Text is what the clip says and the key it is looked up by.
	Text string
	// Preemptible cues (backchannels) give way to reply audio: a flush-on-cut
	// AudioPacingStage drops what is still queued of them when reply audio
	// arrives. Guardrail messages are not preemptible: a reply queues behind
	// them as behind another reply.
	Preemptible bool
}

// CueClip is a cue's pre-rendered media.
type CueClip struct {
	// Audio is PCM16 mono. A clip at another sample rate than the output's is
	// resampled once, before it is first played.
	Audio *AudioData
	// Video is reserved: no source renders it yet. A video stage goes after
	// the CueStage and lip-syncs cue audio as it does reply audio.
	Video *VideoData
}

// CueConfig configures a CueStage.
type CueConfig struct {
	// Clips are ready clips by cue text.
	Clips map[string]CueClip
	// ChunkDuration is the length of the audio elements a clip is played as;
	// default 100ms.
	ChunkDuration time.Duration
	// RenderTimeout bounds how long reply audio waits behind a clip still
	// rendering; default 3s.
	RenderTimeout time.Duration
}

const (
	defaultCueChunkDuration = 100 * time.Millisecond
	defaultCueRenderTimeout = 3 * time.Second
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
// While a clip is still rendering, the stage holds reply audio behind it:
// elements carrying audio wait, everything else goes on. The clip plays when
// it is ready, then the audio that waited. A clip not ready within
// RenderTimeout plays nothing, and a cut (barge-in) while it renders drops
// the cue: in both cases the audio that waited goes on at once.
type CueStage struct {
	BaseStage
	cfg CueConfig
	// render renders a clip for a text not in cfg.Clips; nil means none can be.
	render func(ctx context.Context, text string) (CueClip, error)
}

// NewCueStage creates a cue stage named "cue".
func NewCueStage(cfg CueConfig) *CueStage {
	if cfg.ChunkDuration <= 0 {
		cfg.ChunkDuration = defaultCueChunkDuration
	}
	if cfg.RenderTimeout <= 0 {
		cfg.RenderTimeout = defaultCueRenderTimeout
	}
	return &CueStage{BaseStage: NewBaseStage("cue", StageTypeTransform), cfg: cfg}
}

// cueRender is a clip being rendered for cue.
type cueRender struct {
	cue    Cue
	result chan cueRenderResult
	ctx    context.Context
	cancel context.CancelFunc
}

type cueRenderResult struct {
	clip CueClip
	err  error
}

// cueQueued is what waits behind a rendering clip: an element, or a cue whose
// clip plays in its place.
type cueQueued struct {
	elem StreamElement
	cue  *Cue
}

// cueRun is one Process run's state.
type cueRun struct {
	s      *CueStage
	output chan<- StreamElement
	// clips are the clips by text: the configured ones and those rendered.
	clips map[string]CueClip
	// prepared are clips ready to play, by text and output rate.
	prepared map[cuePreparedKey][]byte
	// rate is the output sample rate: the first reply audio's, 0 until seen.
	rate    int
	pending *cueRender
	queue   []cueQueued
}

type cuePreparedKey struct {
	text string
	rate int
}

// Process implements the Stage interface.
func (s *CueStage) Process(ctx context.Context, input <-chan StreamElement, output chan<- StreamElement) error {
	defer close(output)
	r := &cueRun{s: s, output: output, clips: make(map[string]CueClip, len(s.cfg.Clips)),
		prepared: map[cuePreparedKey][]byte{}}
	for text, clip := range s.cfg.Clips {
		r.clips[text] = clip
	}
	defer r.dropPending()
	for {
		if done, err := r.step(ctx, input); done || err != nil {
			return err
		}
	}
}

// step handles the next input element or the end of the clip rendering,
// whichever comes first. It reports whether the run is over.
func (r *cueRun) step(ctx context.Context, input <-chan StreamElement) (bool, error) {
	var result <-chan cueRenderResult
	var expired <-chan struct{}
	if r.pending != nil {
		result, expired = r.pending.result, r.pending.ctx.Done()
	}
	select {
	case elem, ok := <-input:
		if !ok {
			return true, r.finish(ctx)
		}
		return false, r.handle(ctx, elem)
	case res := <-result:
		return false, r.resolve(ctx, res)
	case <-expired:
		return false, r.expire(ctx)
	case <-ctx.Done():
		return true, ctx.Err()
	}
}

// finish waits for a clip still rendering, at most RenderTimeout, and plays it
// and what waited behind it: the input is over, its output is not.
func (r *cueRun) finish(ctx context.Context) error {
	for r.pending != nil {
		select {
		case res := <-r.pending.result:
			if err := r.resolve(ctx, res); err != nil {
				return err
			}
		case <-r.pending.ctx.Done():
			if err := r.expire(ctx); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// handle forwards one input element and plays its cue's clip.
func (r *cueRun) handle(ctx context.Context, elem StreamElement) error {
	r.noteRate(&elem)
	if r.pending == nil {
		if err := sendStreamElement(ctx, elem, r.output); err != nil {
			return err
		}
		if elem.Cue != nil {
			return r.play(ctx, *elem.Cue)
		}
		return nil
	}
	if isCut(&elem) {
		return r.cutPending(ctx, elem)
	}
	if hasAudio(&elem) {
		r.queue = append(r.queue, cueQueued{elem: elem})
		return nil
	}
	if err := sendStreamElement(ctx, elem, r.output); err != nil {
		return err
	}
	if elem.Cue != nil {
		c := *elem.Cue
		r.queue = append(r.queue, cueQueued{cue: &c})
	}
	return nil
}

// cutPending handles a cut while a clip renders: the clip would have been cut
// anyway, so it and the cues queued behind it are dropped, and the elements
// queued behind it go on, in order, ahead of the cut.
func (r *cueRun) cutPending(ctx context.Context, cut StreamElement) error {
	r.dropPending()
	queued := r.queue
	r.queue = nil
	for i := range queued {
		if queued[i].cue != nil {
			continue
		}
		if err := sendStreamElement(ctx, queued[i].elem, r.output); err != nil {
			return err
		}
	}
	return sendStreamElement(ctx, cut, r.output)
}

// isCueOnly reports whether elem's only payload is a Cue (a backchannel a
// mic-side stage asks for): it is for a CueStage, not for a provider session.
func isCueOnly(elem *StreamElement) bool {
	return elem.Cue != nil && !elem.HasContent() && !elem.IsControl()
}

// hasAudio reports whether elem carries audio samples.
func hasAudio(elem *StreamElement) bool {
	return elem.Audio != nil && len(elem.Audio.Samples) > 0
}

// noteRate takes the output rate from the first reply audio.
func (r *cueRun) noteRate(elem *StreamElement) {
	if r.rate == 0 && hasAudio(elem) && elem.Audio.SampleRate > 0 {
		r.rate = elem.Audio.SampleRate
	}
}

// outputRate is the rate clips play at.
func (r *cueRun) outputRate() int {
	if r.rate > 0 {
		return r.rate
	}
	return defaultCueSampleRate
}

// play plays cue's clip when it is ready, starts rendering it when it can be
// rendered, and otherwise plays nothing.
func (r *cueRun) play(ctx context.Context, cue Cue) error {
	if clip, ok := r.clips[cue.Text]; ok {
		return r.emit(ctx, cue, clip)
	}
	if r.s.render == nil {
		logger.Debug("CueStage: no clip for the cue", "kind", cue.Kind)
		return nil
	}
	rctx, cancel := context.WithTimeout(ctx, r.s.cfg.RenderTimeout)
	p := &cueRender{cue: cue, result: make(chan cueRenderResult, 1), ctx: rctx, cancel: cancel}
	render := r.s.render
	go func() {
		clip, err := render(rctx, cue.Text)
		p.result <- cueRenderResult{clip: clip, err: err}
	}()
	r.pending = p
	return nil
}

// resolve plays a finished render's clip, then what waited behind it.
func (r *cueRun) resolve(ctx context.Context, res cueRenderResult) error {
	p := r.pending
	r.pending = nil
	p.cancel()
	if res.err != nil {
		logger.Warn("CueStage: the cue's clip did not render", "kind", p.cue.Kind, "error", res.err)
	} else {
		r.clips[p.cue.Text] = res.clip
		if err := r.emit(ctx, p.cue, res.clip); err != nil {
			return err
		}
	}
	return r.drain(ctx)
}

// expire gives up on a render that took longer than RenderTimeout.
func (r *cueRun) expire(ctx context.Context) error {
	select {
	case res := <-r.pending.result: // it finished as it expired
		return r.resolve(ctx, res)
	default:
	}
	logger.Warn("CueStage: the cue's clip did not render in time", "kind", r.pending.cue.Kind,
		"timeout", r.s.cfg.RenderTimeout)
	r.dropPending()
	return r.drain(ctx)
}

// dropPending abandons the clip rendering, if any.
func (r *cueRun) dropPending() {
	if r.pending != nil {
		r.pending.cancel()
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
		if q.cue != nil {
			err = r.play(ctx, *q.cue)
		} else {
			err = sendStreamElement(ctx, q.elem, r.output)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// emit plays clip for cue as audio elements of ChunkDuration each.
func (r *cueRun) emit(ctx context.Context, cue Cue, clip CueClip) error {
	rate := r.outputRate()
	key := cuePreparedKey{text: cue.Text, rate: rate}
	pcm, ok := r.prepared[key]
	if !ok {
		pcm = prepareCueAudio(clip.Audio, rate)
		r.prepared[key] = pcm
	}
	chunk := bytesPerSamplePCM16 * int(int64(rate)*int64(r.s.cfg.ChunkDuration)/int64(time.Second))
	if chunk < bytesPerSamplePCM16 {
		chunk = bytesPerSamplePCM16
	}
	for start := 0; start < len(pcm); start += chunk {
		end := min(start+chunk, len(pcm))
		samples := pcm[start:end:end]
		elem := NewAudioElement(&AudioData{
			Samples:    samples,
			SampleRate: rate,
			Channels:   1,
			Format:     AudioFormatPCM16,
			Duration:   audio.PCM16Duration(len(samples), rate, 1),
		})
		c := cue
		elem.Meta.CueAudio = &c
		if err := sendStreamElement(ctx, elem, r.output); err != nil {
			return err
		}
	}
	return nil
}

// prepareCueAudio is a clip's audio ready to play at rate: trimmed of leading
// and trailing silence, resampled, and faded out at its end. A clip that is
// not PCM16 mono plays nothing.
func prepareCueAudio(a *AudioData, rate int) []byte {
	if a == nil || len(a.Samples) == 0 {
		return nil
	}
	if a.Format != AudioFormatPCM16 || a.Channels > 1 {
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
	fade := min(int(int64(rate)*int64(cueFadeOut)/int64(time.Second)), len(pcm)/bytesPerSamplePCM16)
	tail := len(pcm) - bytesPerSamplePCM16*fade
	copy(pcm[tail:], audio.FadeOutPCM16(pcm[tail:], fade))
	return pcm
}

// trimPCM16Silence is pcm (little-endian mono PCM16) without its leading and
// trailing samples at or below cueSilenceLevel.
func trimPCM16Silence(pcm []byte) []byte {
	n := len(pcm) / bytesPerSamplePCM16
	loud := func(i int) bool {
		//nolint:gosec // reinterpreting PCM16 wire bytes as a signed sample
		s := int(int16(binary.LittleEndian.Uint16(pcm[bytesPerSamplePCM16*i:])))
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
	return pcm[bytesPerSamplePCM16*first : bytesPerSamplePCM16*last]
}
