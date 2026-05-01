package audio

import "testing"

func TestIsValidRate(t *testing.T) {
	for _, r := range ValidRates {
		if !IsValidRate(r) {
			t.Fatalf("expected %d to be valid", r)
		}
	}
	for _, r := range []int{0, 8000, 22050, 32000, 44100, 100000} {
		if IsValidRate(r) {
			t.Fatalf("expected %d to be invalid", r)
		}
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()
	if opts.Mode != ModeAuto {
		t.Fatalf("default Mode: got %q, want %q", opts.Mode, ModeAuto)
	}
	if opts.Rate != Rate24k {
		t.Fatalf("default Rate: got %d, want %d", opts.Rate, Rate24k)
	}
	if !opts.LocalSink || !opts.SSEPlayback || !opts.LevelMeter {
		t.Fatalf("expected all surfaces enabled by default, got %+v", opts)
	}
}
