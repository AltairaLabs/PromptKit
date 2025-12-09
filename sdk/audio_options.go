package sdk

import (
	"github.com/AltairaLabs/PromptKit/runtime/audio"
	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

// audioSessionConfig holds configuration for an audio session.
type audioSessionConfig struct {
	vad                  audio.VADAnalyzer
	turnDetector         audio.TurnDetector
	interruptionStrategy audio.InterruptionStrategy
	autoCompleteTurn     bool
}

// AudioSessionOption configures an audio session.
type AudioSessionOption func(*audioSessionConfig)

// WithSessionVAD sets the voice activity detector for the session.
//
//	session, _ := conv.OpenAudioSession(ctx,
//	    sdk.WithSessionVAD(audio.NewSimpleVAD(audio.DefaultVADParams())),
//	)
func WithSessionVAD(vad audio.VADAnalyzer) AudioSessionOption {
	return func(c *audioSessionConfig) {
		c.vad = vad
	}
}

// WithSessionTurnDetector sets the turn detector for the session.
//
// This overrides the conversation-level turn detector for this session.
//
//	session, _ := conv.OpenAudioSession(ctx,
//	    sdk.WithSessionTurnDetector(audio.NewSilenceDetector(cfg)),
//	)
func WithSessionTurnDetector(detector audio.TurnDetector) AudioSessionOption {
	return func(c *audioSessionConfig) {
		c.turnDetector = detector
	}
}

// WithInterruptionStrategy sets how user interruptions are handled.
//
// Available strategies:
//
//   - audio.InterruptionIgnore: Continue speaking (default)
//
//   - audio.InterruptionImmediate: Immediately stop and start listening
//
//   - audio.InterruptionDeferred: Wait for current sentence, then switch
//
//     session, _ := conv.OpenAudioSession(ctx,
//     sdk.WithInterruptionStrategy(audio.InterruptionImmediate),
//     )
func WithInterruptionStrategy(strategy audio.InterruptionStrategy) AudioSessionOption {
	return func(c *audioSessionConfig) {
		c.interruptionStrategy = strategy
	}
}

// WithAutoCompleteTurn enables automatic turn completion.
//
// When enabled, the session automatically processes turn completion
// when the turn detector signals end of turn.
//
//	session, _ := conv.OpenAudioSession(ctx,
//	    sdk.WithAutoCompleteTurn(),
//	)
func WithAutoCompleteTurn() AudioSessionOption {
	return func(c *audioSessionConfig) {
		c.autoCompleteTurn = true
	}
}

// ttsConfig holds configuration for a TTS synthesis call.
type ttsConfig struct {
	voice    string
	format   tts.AudioFormat
	speed    float64
	pitch    float64
	language string
	model    string
}

// TTSOption configures a TTS synthesis call.
type TTSOption func(*ttsConfig)

// WithTTSVoice sets the voice for synthesis.
//
//	audio, _ := conv.SpeakResponse(ctx, resp,
//	    sdk.WithTTSVoice("nova"),
//	)
func WithTTSVoice(voice string) TTSOption {
	return func(c *ttsConfig) {
		c.voice = voice
	}
}

// WithTTSFormat sets the output audio format.
//
//	audio, _ := conv.SpeakResponse(ctx, resp,
//	    sdk.WithTTSFormat(tts.FormatMP3),
//	)
func WithTTSFormat(format tts.AudioFormat) TTSOption {
	return func(c *ttsConfig) {
		c.format = format
	}
}

// WithTTSSpeed sets the speech rate multiplier.
//
// Valid range is typically 0.25 to 4.0, with 1.0 being normal speed.
// Not all providers support speed adjustment.
//
//	audio, _ := conv.SpeakResponse(ctx, resp,
//	    sdk.WithTTSSpeed(1.25),
//	)
func WithTTSSpeed(speed float64) TTSOption {
	return func(c *ttsConfig) {
		c.speed = speed
	}
}

// WithTTSPitch sets the voice pitch adjustment.
//
// Valid range is typically -20 to 20 semitones.
// Not all providers support pitch adjustment.
//
//	audio, _ := conv.SpeakResponse(ctx, resp,
//	    sdk.WithTTSPitch(-2),
//	)
func WithTTSPitch(pitch float64) TTSOption {
	return func(c *ttsConfig) {
		c.pitch = pitch
	}
}

// WithTTSLanguage sets the language for synthesis.
//
//	audio, _ := conv.SpeakResponse(ctx, resp,
//	    sdk.WithTTSLanguage("fr-FR"),
//	)
func WithTTSLanguage(language string) TTSOption {
	return func(c *ttsConfig) {
		c.language = language
	}
}

// WithTTSModel sets the TTS model to use.
//
// Model availability varies by provider.
//
//	audio, _ := conv.SpeakResponse(ctx, resp,
//	    sdk.WithTTSModel("tts-1-hd"),
//	)
func WithTTSModel(model string) TTSOption {
	return func(c *ttsConfig) {
		c.model = model
	}
}
