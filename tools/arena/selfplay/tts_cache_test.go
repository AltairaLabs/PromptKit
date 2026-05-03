package selfplay

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/tts"
)

// fakeTTSBackend is a stub tts.Service that counts Synthesize calls and
// returns a deterministic byte stream so the cache layer's behavior can
// be observed without hitting a real provider.
type fakeTTSBackend struct {
	name  string
	calls atomic.Int32
	out   []byte
	err   error
}

func (f *fakeTTSBackend) Name() string { return f.name }

func (f *fakeTTSBackend) SupportedVoices() []tts.Voice {
	return []tts.Voice{{ID: "alloy", Name: "Alloy"}}
}

func (f *fakeTTSBackend) SupportedFormats() []tts.AudioFormat {
	return []tts.AudioFormat{{Name: "pcm", SampleRate: 24000}}
}

func (f *fakeTTSBackend) Synthesize(_ context.Context, _ string, _ tts.SynthesisConfig) (io.ReadCloser, error) {
	f.calls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(strings.NewReader(string(f.out))), nil
}

func TestCachedTTS_FirstCallHitsBackend(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeTTSBackend{name: "openai", out: []byte("audio-bytes-1")}
	svc, err := NewCachedTTSService(backend, dir)
	if err != nil {
		t.Fatalf("NewCachedTTSService: %v", err)
	}

	r, err := svc.Synthesize(context.Background(), "hello", tts.SynthesisConfig{Voice: "alloy"})
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	got, _ := io.ReadAll(r)
	r.Close()

	if string(got) != "audio-bytes-1" {
		t.Errorf("got %q, want %q", got, "audio-bytes-1")
	}
	if got := backend.calls.Load(); got != 1 {
		t.Errorf("expected 1 backend call, got %d", got)
	}
}

func TestCachedTTS_SecondCallReadsFromCache(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeTTSBackend{name: "openai", out: []byte("audio-bytes-2")}
	svc, err := NewCachedTTSService(backend, dir)
	if err != nil {
		t.Fatalf("NewCachedTTSService: %v", err)
	}

	cfg := tts.SynthesisConfig{Voice: "alloy"}
	for i := range 3 {
		r, err := svc.Synthesize(context.Background(), "hello", cfg)
		if err != nil {
			t.Fatalf("call %d Synthesize: %v", i, err)
		}
		got, _ := io.ReadAll(r)
		r.Close()
		if string(got) != "audio-bytes-2" {
			t.Errorf("call %d got %q, want %q", i, got, "audio-bytes-2")
		}
	}

	if got := backend.calls.Load(); got != 1 {
		t.Errorf("expected 1 backend call across 3 synthesise requests, got %d", got)
	}
}

func TestCachedTTS_DifferentTextDifferentCacheEntry(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeTTSBackend{name: "openai", out: []byte("varies-by-text")}
	svc, err := NewCachedTTSService(backend, dir)
	if err != nil {
		t.Fatalf("NewCachedTTSService: %v", err)
	}

	cfg := tts.SynthesisConfig{Voice: "alloy"}
	for _, text := range []string{"hello", "goodbye", "fun fact"} {
		r, err := svc.Synthesize(context.Background(), text, cfg)
		if err != nil {
			t.Fatalf("Synthesize %q: %v", text, err)
		}
		_, _ = io.ReadAll(r)
		r.Close()
	}

	if got := backend.calls.Load(); got != 3 {
		t.Errorf("expected 3 backend calls (one per distinct text), got %d", got)
	}
}

func TestCachedTTS_DifferentVoiceDifferentCacheEntry(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeTTSBackend{name: "openai", out: []byte("varies-by-voice")}
	svc, err := NewCachedTTSService(backend, dir)
	if err != nil {
		t.Fatalf("NewCachedTTSService: %v", err)
	}

	for _, voice := range []string{"alloy", "echo", "nova"} {
		r, err := svc.Synthesize(context.Background(), "hello", tts.SynthesisConfig{Voice: voice})
		if err != nil {
			t.Fatalf("Synthesize voice %q: %v", voice, err)
		}
		_, _ = io.ReadAll(r)
		r.Close()
	}

	if got := backend.calls.Load(); got != 3 {
		t.Errorf("expected 3 backend calls (one per distinct voice), got %d", got)
	}
}

func TestCachedTTS_BackendErrorNotCached(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeTTSBackend{name: "openai", err: errors.New("rate limited")}
	svc, err := NewCachedTTSService(backend, dir)
	if err != nil {
		t.Fatalf("NewCachedTTSService: %v", err)
	}

	cfg := tts.SynthesisConfig{Voice: "alloy"}
	for i := range 3 {
		_, err := svc.Synthesize(context.Background(), "hello", cfg)
		if err == nil {
			t.Fatalf("call %d expected error, got nil", i)
		}
	}
	if got := backend.calls.Load(); got != 3 {
		t.Errorf("expected 3 backend calls (errors must not be cached), got %d", got)
	}
}

func TestCachedTTS_EmptyDirReturnsBackendUnchanged(t *testing.T) {
	backend := &fakeTTSBackend{name: "openai"}
	svc, err := NewCachedTTSService(backend, "")
	if err != nil {
		t.Fatalf("NewCachedTTSService: %v", err)
	}
	if svc != tts.Service(backend) {
		t.Error("expected pass-through when dir is empty")
	}
}

func TestCachedTTS_NamePassesThrough(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeTTSBackend{name: "elevenlabs"}
	svc, err := NewCachedTTSService(backend, dir)
	if err != nil {
		t.Fatalf("NewCachedTTSService: %v", err)
	}
	if got := svc.Name(); got != "elevenlabs" {
		t.Errorf("Name() = %q, want %q", got, "elevenlabs")
	}
}

func TestCachedTTS_SupportedVoicesAndFormatsPassThrough(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeTTSBackend{name: "openai"}
	svc, err := NewCachedTTSService(backend, dir)
	if err != nil {
		t.Fatalf("NewCachedTTSService: %v", err)
	}
	cached, ok := svc.(*CachedTTSService)
	if !ok {
		t.Fatalf("expected *CachedTTSService, got %T", svc)
	}
	voices := cached.SupportedVoices()
	if len(voices) != 1 || voices[0].ID != "alloy" {
		t.Errorf("SupportedVoices() = %v, want [{alloy ...}]", voices)
	}
	formats := cached.SupportedFormats()
	if len(formats) != 1 || formats[0].Name != "pcm" {
		t.Errorf("SupportedFormats() = %v, want [{pcm ...}]", formats)
	}
}
