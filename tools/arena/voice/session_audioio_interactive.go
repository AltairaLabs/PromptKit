package voice

import "github.com/AltairaLabs/PromptKit/runtime/audio"

// NewAudioIO constructs an AudioIO backed by the shared runtime/audio PortAudio
// Session. The hardware core now lives in runtime/audio; this thin adapter keeps
// the Driver and pipeline seam consuming the unchanged voice.AudioIO interface.
// It returns the actionable missing-PortAudio error unchanged so the console can
// surface install steps instead of crashing.
//
// This file is named *_interactive.go because it opens real audio hardware and
// cannot be unit-tested without PortAudio; the testable adapter logic lives in
// session_audioio.go.
func NewAudioIO() (AudioIO, error) {
	sess, err := audio.NewPortAudioSession()
	if err != nil {
		return nil, err
	}
	return newSessionAudioIO(sess), nil
}
