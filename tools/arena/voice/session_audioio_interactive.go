package voice

import "github.com/AltairaLabs/PromptKit/tools/arena/voice/portaudio"

// NewAudioIO constructs an AudioIO backed by the PortAudio Session. The hardware
// core lives in the sibling voice/portaudio package (kept out of runtime so
// server/controller binaries stay statically linkable); this thin adapter keeps
// the Driver and pipeline seam consuming the unchanged voice.AudioIO interface.
// It returns the actionable missing-PortAudio error unchanged so the console can
// surface install steps instead of crashing.
//
// This file is named *_interactive.go because it opens real audio hardware and
// cannot be unit-tested without PortAudio; the testable adapter logic lives in
// session_audioio.go.
func NewAudioIO() (AudioIO, error) {
	sess, err := portaudio.NewSession()
	if err != nil {
		return nil, err
	}
	return newSessionAudioIO(sess), nil
}
