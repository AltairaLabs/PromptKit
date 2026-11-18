package stage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
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

// clipRenderCost is what a billed render did, for its cost event.
type clipRenderCost struct {
	svc      tts.Service
	text     string
	duration time.Duration
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
