package selfplay

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
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
	// Call via concrete type to ensure the method is instrumented.
	cached := svc.(*CachedTTSService)
	if got := cached.Name(); got != "elevenlabs" {
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
	// Also exercise Name via the concrete type (ensures instrumentation).
	_ = cached.Name()
	voices := cached.SupportedVoices()
	if len(voices) != 1 || voices[0].ID != "alloy" {
		t.Errorf("SupportedVoices() = %v, want [{alloy ...}]", voices)
	}
	formats := cached.SupportedFormats()
	if len(formats) != 1 || formats[0].Name != "pcm" {
		t.Errorf("SupportedFormats() = %v, want [{pcm ...}]", formats)
	}
}

// TestWriteCacheFile_NonExistentDir exercises the os.CreateTemp error path.
func TestWriteCacheFile_NonExistentDir(t *testing.T) {
	path := "/nonexistent-directory/cache/key.bin"
	err := writeCacheFile(path, []byte("data"))
	if err == nil {
		t.Error("writeCacheFile with nonexistent parent dir should return error")
	}
}

// TestWriteCacheFile_HappyPath exercises the success path end-to-end.
func TestWriteCacheFile_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.bin"
	data := []byte("hello cache")
	if err := writeCacheFile(path, data); err != nil {
		t.Fatalf("writeCacheFile: %v", err)
	}
	got, err := os.ReadFile(path) //nolint:gosec // test helper
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("file content = %q, want %q", got, data)
	}
}

// --- base.TTSProvider method tests for CachedTTSService ---

func TestCachedTTS_TypeIsProviderTypeTTS(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeTTSBackend{name: "openai"}
	svc, _ := NewCachedTTSService(backend, dir)
	cached := svc.(*CachedTTSService)
	if got := cached.Type(); got != base.ProviderTypeTTS {
		t.Errorf("Type() = %v, want ProviderTypeTTS", got)
	}
}

func TestCachedTTS_PricingNilWhenBackendIsNotProvider(t *testing.T) {
	dir := t.TempDir()
	// fakeTTSBackend does not implement base.Provider, so Pricing must return nil.
	backend := &fakeTTSBackend{name: "openai"}
	svc, _ := NewCachedTTSService(backend, dir)
	cached := svc.(*CachedTTSService)
	if p := cached.Pricing(); p != nil {
		t.Errorf("Pricing() = %v, want nil when backend is not base.Provider", p)
	}
}

func TestCachedTTS_ValidateInitHealthCheckClose_NilWhenNotProvider(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeTTSBackend{name: "openai"}
	svc, _ := NewCachedTTSService(backend, dir)
	cached := svc.(*CachedTTSService)

	if err := cached.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
	if err := cached.Init(context.Background()); err != nil {
		t.Errorf("Init() = %v, want nil", err)
	}
	if err := cached.HealthCheck(context.Background()); err != nil {
		t.Errorf("HealthCheck() = %v, want nil", err)
	}
	if err := cached.Close(); err != nil {
		t.Errorf("Close() = %v, want nil", err)
	}
}

func TestCachedTTS_SynthesizeTTS_LegacyFallback_CachesMiss(t *testing.T) {
	// fakeTTSBackend only implements tts.Service, so SynthesizeTTS takes the
	// legacy Synthesize path.
	dir := t.TempDir()
	backend := &fakeTTSBackend{name: "openai", out: []byte("tts-audio")}
	svc, err := NewCachedTTSService(backend, dir)
	if err != nil {
		t.Fatalf("NewCachedTTSService: %v", err)
	}
	cached := svc.(*CachedTTSService)

	req := base.TTSRequest{Text: "hello", Voice: "alloy", Format: "pcm"}
	stream, err := cached.SynthesizeTTS(context.Background(), req)
	if err != nil {
		t.Fatalf("SynthesizeTTS: %v", err)
	}
	audio, _, readErr := base.ReadAllAudio(stream)
	if readErr != nil {
		t.Fatalf("ReadAllAudio: %v", readErr)
	}
	if string(audio) != "tts-audio" {
		t.Errorf("audio = %q, want %q", audio, "tts-audio")
	}
	if got := backend.calls.Load(); got != 1 {
		t.Errorf("expected 1 backend call, got %d", got)
	}
}

func TestCachedTTS_SynthesizeTTS_CacheHitSkipsBackend(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeTTSBackend{name: "openai", out: []byte("cached-audio")}
	svc, _ := NewCachedTTSService(backend, dir)
	cached := svc.(*CachedTTSService)

	req := base.TTSRequest{Text: "world", Voice: "nova", Format: "pcm"}

	// First call — populates cache.
	s1, err := cached.SynthesizeTTS(context.Background(), req)
	if err != nil {
		t.Fatalf("first SynthesizeTTS: %v", err)
	}
	_, _, _ = base.ReadAllAudio(s1)

	// Second call — must hit cache, not backend.
	s2, err := cached.SynthesizeTTS(context.Background(), req)
	if err != nil {
		t.Fatalf("second SynthesizeTTS: %v", err)
	}
	audio, _, _ := base.ReadAllAudio(s2)
	if string(audio) != "cached-audio" {
		t.Errorf("cached audio = %q, want %q", audio, "cached-audio")
	}
	if got := backend.calls.Load(); got != 1 {
		t.Errorf("expected 1 backend call across 2 SynthesizeTTS requests, got %d", got)
	}
}

// fakeTTSProviderBackend is a fakeTTSBackend that also satisfies base.TTSProvider.
type fakeTTSProviderBackend struct {
	fakeTTSBackend
	synthesisTTSCalls atomic.Int32
}

func (f *fakeTTSProviderBackend) Type() base.ProviderType             { return base.ProviderTypeTTS }
func (f *fakeTTSProviderBackend) Pricing() *base.PricingDescriptor    { return nil }
func (f *fakeTTSProviderBackend) Validate() error                     { return nil }
func (f *fakeTTSProviderBackend) Init(_ context.Context) error        { return nil }
func (f *fakeTTSProviderBackend) HealthCheck(_ context.Context) error { return nil }
func (f *fakeTTSProviderBackend) Close() error                        { return nil }
func (f *fakeTTSProviderBackend) SynthesizeTTS(_ context.Context, req base.TTSRequest) (base.TTSStream, error) {
	f.synthesisTTSCalls.Add(1)
	if f.err != nil {
		return nil, f.err
	}
	return newMockTTSStream(io.NopCloser(strings.NewReader(string(f.out)))), nil
}

func TestCachedTTS_SynthesizeTTS_DelegatesBaseTTSProvider(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeTTSProviderBackend{
		fakeTTSBackend: fakeTTSBackend{name: "openai", out: []byte("provider-audio")},
	}
	svc, _ := NewCachedTTSService(backend, dir)
	cached := svc.(*CachedTTSService)

	req := base.TTSRequest{Text: "delegate test", Voice: "echo", Format: "pcm"}
	stream, err := cached.SynthesizeTTS(context.Background(), req)
	if err != nil {
		t.Fatalf("SynthesizeTTS: %v", err)
	}
	audio, _, _ := base.ReadAllAudio(stream)
	if string(audio) != "provider-audio" {
		t.Errorf("audio = %q, want %q", audio, "provider-audio")
	}
	if got := backend.synthesisTTSCalls.Load(); got != 1 {
		t.Errorf("expected 1 SynthesizeTTS backend call, got %d", got)
	}
}

// TestCachedTTS_ProviderProxyMethodsForwardToBackend verifies that when the
// backend also satisfies base.Provider, the proxy methods on CachedTTSService
// forward calls through.
func TestCachedTTS_ProviderProxyMethodsForwardToBackend(t *testing.T) {
	dir := t.TempDir()
	backend := &fakeTTSProviderBackend{
		fakeTTSBackend: fakeTTSBackend{name: "openai"},
	}
	svc, _ := NewCachedTTSService(backend, dir)
	cached := svc.(*CachedTTSService)

	// Pricing proxies through (backend returns nil).
	if p := cached.Pricing(); p != nil {
		t.Errorf("Pricing() = %v, want nil", p)
	}
	// Lifecycle methods proxy without error.
	if err := cached.Validate(); err != nil {
		t.Errorf("Validate() = %v", err)
	}
	if err := cached.Init(context.Background()); err != nil {
		t.Errorf("Init() = %v", err)
	}
	if err := cached.HealthCheck(context.Background()); err != nil {
		t.Errorf("HealthCheck() = %v", err)
	}
	if err := cached.Close(); err != nil {
		t.Errorf("Close() = %v", err)
	}
}
