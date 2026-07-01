// Package audiohelper provides a minimal microphone + speaker implementation of
// the audio.Source / audio.Sink / audio.Session interfaces (runtime/audio),
// backed by PortAudio loaded at runtime via purego/dlopen (no CGO).
//
// It exists so the SDK voice examples can drive real audio hardware without the
// SDK core (or runtime) depending on a sound-card binding. It is an
// example-grade copy of the device I/O; the production implementation lives in
// tools/arena/voice/portaudio. See issue #1536.
package audiohelper
