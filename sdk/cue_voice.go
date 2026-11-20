package sdk

import (
	"errors"
	"sync/atomic"

	"github.com/AltairaLabs/PromptKit/runtime/v2/audio/cueclip"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tts"
)

// CueOption configures how a realtime session's cues are voiced
// ([WithCueVoice]).
type CueOption func(*cueSettings)

// cueSettings is what the CueOptions of WithCueVoice set.
type cueSettings struct {
	cache cueclip.Cache
}

// CueClipCache keeps the rendered cue clips in cache, so a clip is rendered
// once and every conversation sharing the cache plays it without rendering it
// again. Clips are keyed by content: the text, the voice, the source and the
// audio format. A host serving one pack from many processes can share clips
// across them with its own [cueclip.Cache] over a shared store, or ship a
// directory filled offline ([cueclip.NewDirCache], [PrewarmCueClips]).
//
// The default is one process-wide in-memory cache of
// [cueclip.DefaultCacheBytes], shared by every conversation the SDK opens.
func CueClipCache(cache cueclip.Cache) CueOption {
	return func(s *cueSettings) { s.cache = cache }
}

// WithCueVoice sets the TTS service that voices a realtime session's cues: a
// guardrail's message, spoken when a guardrail cuts a spoken reply or blocks
// typed input, instead of by the model. It applies to a realtime session
// ([OpenDuplex] or [OpenVoice] on a provider that takes streaming input, such
// as OpenAI Realtime or Gemini Live); other sessions have no cues.
//
// A clip comes from the first source that can say it: the session's own
// voice outside the conversation, when the session can speak that way
// (OpenAI Realtime); then voice, in the session's voice when it has a voice
// of that ID or name; then a short tone (an earcon), with the message shown as
// text. Only a service that renders PCM is used, and each render is billed
// like any TTS call. A nil voice renders nothing itself but still plays the
// clips pre-warmed for the session's voice ([PrewarmCueClips]) from the cache.
//
// [WithTTS] does not voice cues: it is the VAD-mode reply voice.
func WithCueVoice(voice tts.Service, opts ...CueOption) Option {
	return func(c *config) error {
		var settings cueSettings
		for _, o := range opts {
			o(&settings)
		}
		c.cueVoice = voice
		c.cueCache = settings.cache
		return nil
	}
}

// ttsOnRealtimeWarned is set once Open has warned that WithTTS does nothing on
// a realtime session.
var ttsOnRealtimeWarned atomic.Bool

// isRealtime reports whether a session opened with cfg is realtime (ASM): a
// duplex session whose input goes to the model itself, with no VAD front,
// custom ingestion or composition authoring the input side.
func (c *config) isRealtime(duplex bool) bool {
	return duplex && c.vadModeConfig == nil && c.ingestion == nil && c.activeComposition == nil
}

// checkVoiceOptions refuses the voice options that would do nothing on the
// session being opened, and warns of a TTS service a realtime session does
// not use.
func checkVoiceOptions(cfg *config, duplex bool) error {
	realtime := cfg.isRealtime(duplex)
	if cfg.playoutTracking && !realtime {
		return errors.New("WithPlayoutTracking applies only to a realtime (ASM) duplex session, " +
			"where the model speaks; this session's replies are not tracked")
	}
	if realtime && cfg.ttsService != nil && ttsOnRealtimeWarned.CompareAndSwap(false, true) {
		logger.Warn("the TTS service speaks VAD-mode replies and has no effect on a realtime session; "+
			"use WithCueVoice to voice its guardrail messages",
			"tts", cfg.ttsService.Name())
	}
	return nil
}
