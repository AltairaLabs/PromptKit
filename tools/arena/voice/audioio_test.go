package voice

import (
	"errors"
	"strings"
	"testing"
)

// TestPortAudioCandidatesFor verifies each OS gets sensible, ordered library
// names (the discovery list dlopen walks).
func TestPortAudioCandidatesFor(t *testing.T) {
	cases := map[string]struct {
		first    string
		contains string
	}{
		"darwin":  {first: "libportaudio.2.dylib", contains: ".dylib"},
		"linux":   {first: "libportaudio.so.2", contains: ".so"},
		"windows": {first: "portaudio.dll", contains: ".dll"},
		"freebsd": {first: "libportaudio.so.2", contains: ".so"}, // default branch
	}
	for goos, want := range cases {
		t.Run(goos, func(t *testing.T) {
			got := portAudioCandidatesFor(goos)
			if len(got) == 0 {
				t.Fatalf("no candidates for %s", goos)
			}
			if got[0] != want.first {
				t.Fatalf("%s: first candidate = %q, want %q", goos, got[0], want.first)
			}
			for _, c := range got {
				if !strings.Contains(c, want.contains) {
					t.Fatalf("%s: candidate %q missing %q", goos, c, want.contains)
				}
			}
		})
	}
}

// TestNewAudioIO_LoadsOrReportsMissing exercises the real purego binding. On a
// machine with PortAudio installed it must load + initialize successfully
// (proving the CGO-free FFI works); otherwise it must return errPortAudioMissing
// with actionable guidance — never crash.
func TestNewAudioIO_LoadsOrReportsMissing(t *testing.T) {
	io, err := NewAudioIO()
	if err != nil {
		if !errors.Is(err, errPortAudioMissing) {
			t.Fatalf("expected errPortAudioMissing when load fails, got: %v", err)
		}
		if !strings.Contains(err.Error(), voiceDocsURL) {
			t.Fatalf("missing-PortAudio error should link the docs (%s), got: %v", voiceDocsURL, err)
		}
		t.Skipf("PortAudio not installed on this host: %v", err)
	}
	// Loaded + Pa_Initialize succeeded — the runtime-load binding works. Close
	// terminates PortAudio; no audio device was opened (Start was never called).
	if cerr := io.Close(); cerr != nil {
		t.Fatalf("Close: %v", cerr)
	}
}
