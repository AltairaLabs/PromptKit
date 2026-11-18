package stage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// clipSource renders the clip for a text in one voice.
type clipSource interface {
	// kind is the source's family for clip keys: "tts:<service>", "earcon".
	kind() string
	// voice is the voice the source renders in, for clip keys.
	voice() string
	// keyText is the text a clip for text is keyed by.
	keyText(text string) string
	// render renders text as a clip ready to play in format, and reports
	// what the render cost, if it is billed.
	render(ctx context.Context, text string, format ClipFormat) (CueClip, *clipRenderCost, error)
}

// clipRenderCost is what a billed render did, for its cost event: a TTS
// service's synthesis (svc, priced by the service), or a session's
// out-of-band render (provider, with the cost it reported).
type clipRenderCost struct {
	svc      tts.Service
	provider string
	cost     *types.CostInfo
	text     string
	duration time.Duration
}

// add adds c to the out-of-band cost.
func (r *clipRenderCost) add(c *types.CostInfo) {
	if c == nil {
		return
	}
	if r.cost == nil {
		r.cost = &types.CostInfo{}
	}
	r.cost.TotalCost += c.TotalCost
	for unit, q := range c.Quantities {
		if r.cost.Quantities == nil {
			r.cost.Quantities = map[string]float64{}
		}
		r.cost.Quantities[unit] += q
	}
}

// billed is r when it carries a cost, else nil.
func (r *clipRenderCost) billed() *clipRenderCost {
	if r.cost == nil {
		return nil
	}
	return r
}

// earconSource renders the earcon, the same for every text. It never fails.
type earconSource struct{}

func (earconSource) kind() string          { return "earcon" }
func (earconSource) voice() string         { return "" }
func (earconSource) keyText(string) string { return "" }

func (earconSource) render(_ context.Context, _ string, format ClipFormat) (CueClip, *clipRenderCost, error) {
	return earconClip(format.SampleRate), nil, nil
}

// ttsClipSource renders with a TTS service as PCM16.
type ttsClipSource struct {
	svc tts.Service
	// voiceID is the service voice to use; empty is the service's default.
	voiceID string
	// request is the PCM format asked of the service.
	request tts.AudioFormat
}

// newTTSClipSource is a TTS source for a session speaking in sessionVoice at
// rate, or nil when svc cannot render PCM16. The voice is the service voice
// whose ID or name is sessionVoice, ignoring case; with none, the service's
// default, with a warning that the cue will not sound like the session.
func newTTSClipSource(svc tts.Service, sessionVoice string, rate int) *ttsClipSource {
	if svc == nil {
		return nil
	}
	request, ok := pcmRequest(svc.SupportedFormats(), rate)
	if !ok {
		logger.Info("CueStage: the TTS service renders no PCM; it is not used for cues", "tts", svc.Name())
		return nil
	}
	src := &ttsClipSource{svc: svc, request: request}
	for _, v := range svc.SupportedVoices() {
		if sessionVoice != "" && (strings.EqualFold(v.ID, sessionVoice) || strings.EqualFold(v.Name, sessionVoice)) {
			src.voiceID = v.ID
			return src
		}
	}
	if sessionVoice != "" {
		logger.Warn("CueStage: the TTS service has no voice matching the session's; cues use its default voice",
			"tts", svc.Name(), "session_voice", sessionVoice, "cue_voice", "default")
	}
	return src
}

// pcmRequest is the PCM format to ask of a service for audio at rate: its PCM
// format at rate when it lists one, else its first PCM format.
func pcmRequest(formats []tts.AudioFormat, rate int) (tts.AudioFormat, bool) {
	var first *tts.AudioFormat
	for i := range formats {
		if formats[i].Name != tts.FormatPCM16.Name {
			continue
		}
		if formats[i].SampleRate == rate {
			return formats[i], true
		}
		if first == nil {
			first = &formats[i]
		}
	}
	if first == nil {
		return tts.AudioFormat{}, false
	}
	f := *first
	if f.SampleRate <= 0 {
		f.SampleRate = rate
	}
	return f, true
}

func (s *ttsClipSource) kind() string               { return "tts:" + s.svc.Name() }
func (s *ttsClipSource) voice() string              { return s.voiceID }
func (s *ttsClipSource) keyText(text string) string { return text }

func (s *ttsClipSource) render(ctx context.Context, text string, format ClipFormat) (CueClip, *clipRenderCost, error) {
	start := time.Now()
	rc, err := s.svc.Synthesize(ctx, text, tts.SynthesisConfig{Voice: s.voiceID, Format: s.request})
	if err != nil {
		return CueClip{}, nil, err
	}
	pcm, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return CueClip{}, nil, err
	}
	cost := &clipRenderCost{svc: s.svc, text: text, duration: time.Since(start)}
	if len(pcm)%bytesPerSamplePCM16 != 0 {
		pcm = pcm[:len(pcm)-1]
	}
	ready := prepareCueAudio(&AudioData{Samples: pcm, SampleRate: s.request.SampleRate, Channels: 1,
		Format: AudioFormatPCM16}, format.SampleRate)
	if len(ready) == 0 {
		return CueClip{}, cost, errors.New("the TTS service returned no audible audio")
	}
	return readyClip(ready, format.SampleRate), cost, nil
}

// readyClip is pcm, ready to play at rate, as a clip.
func readyClip(pcm []byte, rate int) CueClip {
	return CueClip{Audio: &AudioData{Samples: pcm, SampleRate: rate, Channels: 1, Format: AudioFormatPCM16,
		Duration: audio.PCM16Duration(len(pcm), rate, 1)}}
}

// describeSources names sources for a log line.
func describeSources(sources []clipSource) string {
	names := make([]string, 0, len(sources))
	for _, s := range sources {
		if v := s.voice(); v != "" {
			names = append(names, fmt.Sprintf("%s(%s)", s.kind(), v))
		} else {
			names = append(names, s.kind())
		}
	}
	return strings.Join(names, ",")
}

// sessionClipSource renders with the session's own out-of-band audio
// (providers.OutOfBandSpeaker): the exact session voice, outside the
// conversation. Its clips are keyed by the session's family and voice, never
// a model, so later sessions of that family and voice play them from the
// cache with no request.
type sessionClipSource struct {
	speaker   providers.OutOfBandSpeaker
	family    string
	voiceName string
}

// outOfBandTries is how many times the session is asked to say a text before
// the text falls to the next source: the model sometimes says more than it
// was asked to ("Understood. ...").
const outOfBandTries = 2

// newSessionClipSource is the out-of-band source of sess, or nil when it has
// none, or reports no family to key its clips by.
func newSessionClipSource(sess providers.StreamInputSession, out providers.OutputAudio) *sessionClipSource {
	speaker, ok := sess.(providers.OutOfBandSpeaker)
	if !ok {
		return nil
	}
	if out.Family == "" {
		logger.Info("CueStage: the session reports no provider family; its out-of-band audio is not used for cues")
		return nil
	}
	return &sessionClipSource{speaker: speaker, family: out.Family, voiceName: out.Voice}
}

// tries is how many renders one clip can take: each gets RenderTimeout.
func (s *sessionClipSource) tries() int { return outOfBandTries }

// renderTries is how many renders src can take for one clip.
func renderTries(src clipSource) int {
	if r, ok := src.(interface{ tries() int }); ok {
		return r.tries()
	}
	return 1
}

func (s *sessionClipSource) kind() string               { return "session:" + s.family }
func (s *sessionClipSource) voice() string              { return s.voiceName }
func (s *sessionClipSource) keyText(text string) string { return text }

// render has the session say text and checks what it said: a transcript
// other than the text is rendered once more, then fails the text. Every
// try's cost is reported.
func (s *sessionClipSource) render(ctx context.Context, text string, format ClipFormat) (
	CueClip, *clipRenderCost, error,
) {
	start := time.Now()
	cost := &clipRenderCost{provider: s.family, text: text}
	var lastErr error
	for try := 0; try < outOfBandTries; try++ {
		speech, err := s.speaker.SpeakOutOfBand(ctx, text)
		cost.add(speech.Cost)
		cost.duration = time.Since(start)
		if err != nil {
			return CueClip{}, cost.billed(), err
		}
		if !sameSpeech(speech.Transcript, text) {
			logger.Debug("CueStage: the session said other than the text", "said", speech.Transcript, "try", try+1)
			lastErr = fmt.Errorf("the session said %q", speech.Transcript)
			continue
		}
		ready := prepareCueAudio(&AudioData{Samples: speech.Audio, SampleRate: speech.SampleRate, Channels: 1,
			Format: AudioFormatPCM16}, format.SampleRate)
		if len(ready) == 0 {
			return CueClip{}, cost.billed(), errors.New("the session returned no audible audio")
		}
		return readyClip(ready, format.SampleRate), cost.billed(), nil
	}
	return CueClip{}, cost.billed(), lastErr
}

// sameSpeech reports whether said is text, case, punctuation and spacing
// aside.
func sameSpeech(said, text string) bool {
	return normalizeSpeech(said) == normalizeSpeech(text)
}

// normalizeSpeech is s case-folded, with apostrophes dropped ("don't" is
// "dont"), other punctuation and symbols read as spaces ("well-known" is
// "well known", "that—let's" is "that lets"), and whitespace collapsed to
// single spaces.
func normalizeSpeech(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == '\'' || r == '’' || r == 'ʼ':
		case unicode.IsPunct(r), unicode.IsSymbol(r), unicode.IsSpace(r):
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
