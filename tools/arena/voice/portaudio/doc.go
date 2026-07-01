// Package portaudio provides microphone capture and speaker playback backed by
// PortAudio, loaded at runtime via purego/dlopen (no CGO). It implements the
// audio.Session / audio.Source / audio.Sink interfaces defined in runtime/audio.
//
// The device I/O lives here in tools/arena rather than in runtime/audio so the
// sound-card binding (and its purego dependency, which forces dynamic linking)
// stays out of the pure-Go library foundation that server/controller binaries
// import via pkg/config. Only binaries that genuinely open a sound card pull it
// in. See issue #1536.
package portaudio
