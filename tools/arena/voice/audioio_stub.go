//go:build !voice

package voice

// NewAudioIO returns ErrVoiceNotCompiled in non-voice builds.
// Build with -tags voice (and PortAudio installed) to enable voice.
func NewAudioIO() (AudioIO, error) {
	return nil, ErrVoiceNotCompiled
}
