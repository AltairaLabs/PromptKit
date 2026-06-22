//go:build !voice

package voice

import "errors"

// ErrVoiceNotCompiled is returned by NewAudioIO in builds without the `voice`
// tag. Build with `-tags voice` (and PortAudio installed) to enable voice.
var ErrVoiceNotCompiled = errors.New("voice not compiled in (build with -tags voice)")

// NewAudioIO returns ErrVoiceNotCompiled in non-voice builds.
func NewAudioIO() (AudioIO, error) {
	return nil, ErrVoiceNotCompiled
}
