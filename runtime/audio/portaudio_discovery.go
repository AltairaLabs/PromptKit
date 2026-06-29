package audio

import "runtime"

// voiceDocsURL points users at the voice setup instructions when PortAudio is
// missing. Included in errPortAudioMissing.
const voiceDocsURL = "https://promptkit.altairalabs.ai/arena/how-to/voice-console/"

// portAudioCandidates returns the libportaudio file names / paths to try, in
// order, for the current OS.
func portAudioCandidates() []string {
	return portAudioCandidatesFor(runtime.GOOS)
}

// portAudioCandidatesFor returns candidate libportaudio names for goos. It is
// split out (parameterized by goos) so the per-platform list is unit-testable
// without a real library present.
func portAudioCandidatesFor(goos string) []string {
	switch goos {
	case "darwin":
		// Bare names rely on the dynamic loader; the Homebrew paths cover the
		// common installs (Apple Silicon vs Intel) that bare-name dlopen misses.
		return []string{
			"libportaudio.2.dylib",
			"/opt/homebrew/lib/libportaudio.2.dylib",
			"/usr/local/lib/libportaudio.2.dylib",
			"libportaudio.dylib",
		}
	case "windows":
		return []string{
			"portaudio.dll",
			"libportaudio-2.dll",
			"libportaudio.dll",
		}
	default: // linux and other unixes
		return []string{
			"libportaudio.so.2",
			"libportaudio.so",
		}
	}
}
