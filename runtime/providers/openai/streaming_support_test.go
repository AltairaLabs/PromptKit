package openai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestProvider_SupportsStreamInput(t *testing.T) {
	tests := []struct {
		name     string
		model    string
		wantLen  int
		wantType string
	}{
		{
			name:     "realtime model",
			model:    "gpt-4o-realtime-preview",
			wantLen:  1,
			wantType: types.ContentTypeAudio,
		},
		{
			name:     "realtime model variant",
			model:    "gpt-4o-realtime-preview-2024-12-17",
			wantLen:  1,
			wantType: types.ContentTypeAudio,
		},
		{
			name:    "non-realtime model",
			model:   "gpt-4o",
			wantLen: 0,
		},
		{
			name:    "gpt-4",
			model:   "gpt-4",
			wantLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProvider("test", tt.model, "https://api.openai.com", providers.ProviderDefaults{}, false)
			mediaTypes := p.SupportsStreamInput()

			if len(mediaTypes) != tt.wantLen {
				t.Errorf("expected %d media types, got %d", tt.wantLen, len(mediaTypes))
			}

			if tt.wantLen > 0 && mediaTypes[0] != tt.wantType {
				t.Errorf("expected media type %s, got %s", tt.wantType, mediaTypes[0])
			}
		})
	}
}

func TestProvider_GetStreamingCapabilities(t *testing.T) {
	p := NewProvider("test", "gpt-4o-realtime-preview", "https://api.openai.com", providers.ProviderDefaults{}, false)
	caps := p.GetStreamingCapabilities()

	if len(caps.SupportedMediaTypes) == 0 {
		t.Error("expected supported media types")
	}

	if !caps.BidirectionalSupport {
		t.Error("expected bidirectional support")
	}

	if caps.Audio == nil {
		t.Fatal("expected audio capabilities")
	}

	if caps.Audio.PreferredSampleRate != 24000 {
		t.Errorf("expected sample rate 24000, got %d", caps.Audio.PreferredSampleRate)
	}

	if caps.Audio.PreferredEncoding != "pcm16" {
		t.Errorf("expected encoding pcm16, got %s", caps.Audio.PreferredEncoding)
	}
}

func TestProvider_validateStreamRequest(t *testing.T) {
	p := NewProvider("test", "gpt-4o-realtime-preview", "https://api.openai.com", providers.ProviderDefaults{}, false)

	tests := []struct {
		name    string
		config  types.StreamingMediaConfig
		wantErr bool
	}{
		{
			name: "valid audio config",
			config: types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				SampleRate: 24000,
				Encoding:   "pcm16",
				Channels:   1,
				ChunkSize:  1024,
			},
			wantErr: false,
		},
		{
			name: "valid audio with different sample rate (warning only)",
			config: types.StreamingMediaConfig{
				Type:       types.ContentTypeAudio,
				SampleRate: 16000,
				Encoding:   "pcm",
				Channels:   1,
				ChunkSize:  1024,
			},
			wantErr: false,
		},
		{
			name: "video not supported",
			config: types.StreamingMediaConfig{
				Type:      types.ContentTypeVideo,
				ChunkSize: 1024,
			},
			wantErr: true,
		},
		{
			name: "text not supported",
			config: types.StreamingMediaConfig{
				Type:      types.ContentTypeText,
				ChunkSize: 1024,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &providers.StreamingInputConfig{
				Config: tt.config,
			}
			err := p.validateStreamRequest(req)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestProvider_buildRealtimeSessionConfig(t *testing.T) {
	p := NewProvider("test", "gpt-4o-realtime-preview", "https://api.openai.com", providers.ProviderDefaults{}, false)

	t.Run("with system instruction", func(t *testing.T) {
		req := &providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type: types.ContentTypeAudio,
			},
			SystemInstruction: "You are a helpful assistant.",
		}

		config := p.buildRealtimeSessionConfig(req)

		if config.Instructions != "You are a helpful assistant." {
			t.Errorf("expected system instruction, got %s", config.Instructions)
		}
	})

	t.Run("uses realtime model", func(t *testing.T) {
		req := &providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{
				Type: types.ContentTypeAudio,
			},
		}

		config := p.buildRealtimeSessionConfig(req)

		if config.Model != "gpt-4o-realtime-preview" {
			t.Errorf("expected realtime model, got %s", config.Model)
		}
	})
}

func TestProvider_applyStreamMetadata(t *testing.T) {
	p := NewProvider("test", "gpt-4o-realtime-preview", "https://api.openai.com", providers.ProviderDefaults{}, false)

	t.Run("applies voice", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		metadata := map[string]interface{}{
			"voice": "shimmer",
		}

		p.applyStreamMetadata(metadata, &config)

		if config.Voice != "shimmer" {
			t.Errorf("expected voice shimmer, got %s", config.Voice)
		}
	})

	t.Run("applies modalities as []string", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		metadata := map[string]interface{}{
			"modalities": []string{"text"},
		}

		p.applyStreamMetadata(metadata, &config)

		if len(config.Modalities) != 1 || config.Modalities[0] != "text" {
			t.Errorf("expected text modality, got %v", config.Modalities)
		}
	})

	t.Run("applies modalities as []interface{}", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		metadata := map[string]interface{}{
			"modalities": []interface{}{"audio"},
		}

		p.applyStreamMetadata(metadata, &config)

		if len(config.Modalities) != 1 || config.Modalities[0] != "audio" {
			t.Errorf("expected audio modality, got %v", config.Modalities)
		}
	})

	t.Run("enables input transcription by default", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		metadata := map[string]interface{}{}

		p.applyStreamMetadata(metadata, &config)

		if config.InputAudioTranscription == nil {
			t.Error("expected input transcription config to be enabled by default")
		}
	})

	t.Run("disables input transcription when explicitly set to false", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		metadata := map[string]interface{}{
			"input_transcription": false,
		}

		p.applyStreamMetadata(metadata, &config)

		if config.InputAudioTranscription != nil {
			t.Error("expected input transcription to be disabled")
		}
	})

	t.Run("disables VAD", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		metadata := map[string]interface{}{
			"vad_disabled": true,
		}

		p.applyStreamMetadata(metadata, &config)

		if config.TurnDetection != nil {
			t.Error("expected turn detection to be nil")
		}
	})

	t.Run("applies temperature", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		metadata := map[string]interface{}{
			"temperature": 0.5,
		}

		p.applyStreamMetadata(metadata, &config)

		if config.Temperature != 0.5 {
			t.Errorf("expected temperature 0.5, got %f", config.Temperature)
		}
	})

	t.Run("nil metadata is safe", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		p.applyStreamMetadata(nil, &config)
		// Should not panic
	})
}

func TestProvider_applyStreamTools(t *testing.T) {
	p := NewProvider("test", "gpt-4o-realtime-preview", "https://api.openai.com", providers.ProviderDefaults{}, false)

	t.Run("applies tools", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		tools := []providers.StreamingToolDefinition{
			{
				Name:        "get_weather",
				Description: "Get the weather for a location",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"location": map[string]interface{}{
							"type": "string",
						},
					},
				},
			},
		}

		p.applyStreamTools(tools, &config)

		if len(config.Tools) != 1 {
			t.Fatalf("expected 1 tool, got %d", len(config.Tools))
		}

		if config.Tools[0].Name != "get_weather" {
			t.Errorf("expected tool name get_weather, got %s", config.Tools[0].Name)
		}

		if config.Tools[0].Type != "function" {
			t.Errorf("expected tool type function, got %s", config.Tools[0].Type)
		}
	})

	t.Run("empty tools is safe", func(t *testing.T) {
		config := DefaultRealtimeSessionConfig()
		p.applyStreamTools(nil, &config)

		if len(config.Tools) != 0 {
			t.Errorf("expected 0 tools, got %d", len(config.Tools))
		}
	})
}

func TestProvider_StreamInputSupport_Interface(t *testing.T) {
	// Verify Provider implements StreamInputSupport
	var _ providers.StreamInputSupport = (*Provider)(nil)
}

// --- Realtime concurrency bounds (#862) ---
//
// The tests below verify the semaphore + gauge bookkeeping added to
// CreateStreamSession without hitting a real WebSocket. The semaphore
// acquire happens BEFORE NewRealtimeSession is called, so we can test
// the rejection path cleanly by pre-draining a small semaphore and
// asserting CreateStreamSession fails on acquire.
//
// The bookkeeping wrapper (realtimeSessionBookkeeping) is tested
// directly with a fake StreamInputSession because constructing a real
// session requires a live WebSocket handshake (covered by the
// integration-tag tests in realtime_integration_test.go).

// When the concurrent-stream semaphore is pre-drained, CreateStreamSession
// must return an acquire error before any WebSocket dial. The test uses a
// short context deadline so the acquire times out quickly rather than
// blocking forever on a saturated semaphore.
func TestCreateStreamSession_SemaphoreRejectsBeforeDial(t *testing.T) {
	p := NewProvider(
		"openai-realtime-test",
		"gpt-4o-realtime-preview",
		"https://api.openai.com",
		providers.ProviderDefaults{},
		false,
	)
	// Install a size-1 semaphore and drain its only slot.
	p.SetStreamSemaphore(providers.NewStreamSemaphore(1))
	if err := p.AcquireStreamSlot(context.Background()); err != nil {
		t.Fatalf("priming acquire failed: %v", err)
	}
	defer p.ReleaseStreamSlot()

	// Short deadline so the saturated acquire inside CreateStreamSession
	// times out quickly.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	session, err := p.CreateStreamSession(ctx, &providers.StreamingInputConfig{
		Config: types.StreamingMediaConfig{
			Type:       types.ContentTypeAudio,
			SampleRate: 24000,
			Encoding:   "pcm16",
			Channels:   1,
			ChunkSize:  1024,
		},
	})
	if err == nil {
		_ = session.Close()
		t.Fatal("expected acquire error on saturated semaphore, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) &&
		!strings.Contains(err.Error(), "failed to acquire stream slot") {
		t.Errorf("expected acquire-related error, got %v", err)
	}
}

// The nil-semaphore no-op path for CreateStreamSession is covered
// indirectly: providers.BaseProvider.AcquireStreamSlot has explicit
// nil-receiver handling tested by
// TestBaseProvider_AcquireStreamSlot_NilSemaphore in the providers
// package, and CreateStreamSession calls it unconditionally. We do
// not add a direct end-to-end test here because the Realtime
// WebSocket URL is hardcoded inside NewRealtimeWebSocket (does not
// use Provider.baseURL), so any test that reaches NewRealtimeSession
// would have to touch real OpenAI infrastructure — fragile for CI.

// --- realtimeSessionBookkeeping wrapper tests ---

// fakeStreamInputSession is a minimal StreamInputSession implementation
// for unit-testing the bookkeeping wrapper without a live WebSocket.
// Both Close() and signalDoneExternal() close the shared done channel
// via a single sync.Once so either path can be called first without
// panicking on a double close.
type fakeStreamInputSession struct {
	done     chan struct{}
	doneOnce sync.Once
	closed   atomic.Bool
	closeErr error
}

func newFakeStreamSession() *fakeStreamInputSession {
	return &fakeStreamInputSession{done: make(chan struct{})}
}

func (f *fakeStreamInputSession) SendChunk(_ context.Context, _ *types.MediaChunk) error {
	return nil
}

func (f *fakeStreamInputSession) SendText(_ context.Context, _ string) error {
	return nil
}

func (f *fakeStreamInputSession) SendSystemContext(_ context.Context, _ string) error {
	return nil
}

func (f *fakeStreamInputSession) Response() <-chan providers.StreamChunk {
	return make(chan providers.StreamChunk)
}

func (f *fakeStreamInputSession) Close() error {
	f.closed.Store(true)
	f.doneOnce.Do(func() { close(f.done) })
	return f.closeErr
}

func (f *fakeStreamInputSession) Error() error          { return nil }
func (f *fakeStreamInputSession) Done() <-chan struct{} { return f.done }

// signalDoneExternal simulates the session's done channel closing from
// its internal lifecycle (e.g. ctx cancellation in the receive loop)
// without the caller having called Close. Safe to call alongside
// Close — doneOnce ensures the channel is closed exactly once.
func (f *fakeStreamInputSession) signalDoneExternal() {
	f.doneOnce.Do(func() { close(f.done) })
}

// The wrapper's Close must be idempotent under repeated calls — the
// StreamInputSession contract explicitly permits multiple Close calls,
// and the release callback must fire exactly once.
func TestRealtimeSessionBookkeeping_CloseIsIdempotent(t *testing.T) {
	fake := newFakeStreamSession()
	var releases atomic.Int32
	wrapper := &realtimeSessionBookkeeping{
		StreamInputSession: fake,
		release:            func() { releases.Add(1) },
	}

	// First Close: release fires, fake.Close runs.
	if err := wrapper.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	// Second Close: release must NOT fire again.
	if err := wrapper.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	// Third, for good measure.
	if err := wrapper.Close(); err != nil {
		t.Errorf("third Close: %v", err)
	}

	if got := releases.Load(); got != 1 {
		t.Errorf("release fired %d times, want exactly 1", got)
	}
	if !fake.closed.Load() {
		t.Error("underlying session was not closed")
	}
}

// When a caller forgets to call Close() but the session signals Done()
// (e.g. because its ctx was cancelled), the release callback must still
// fire exactly once — via the Done-watching goroutine installed in
// CreateStreamSession. This guarantees no slot leaks on abandoned
// sessions.
//
// We simulate the goroutine manually here because the wrapper itself
// doesn't start the watcher; CreateStreamSession does. The test
// mirrors that goroutine's logic.
func TestRealtimeSessionBookkeeping_DoneTriggersReleaseExactlyOnce(t *testing.T) {
	fake := newFakeStreamSession()
	var releases atomic.Int32
	wrapper := &realtimeSessionBookkeeping{
		StreamInputSession: fake,
		release:            func() { releases.Add(1) },
	}

	// Simulate the watcher goroutine CreateStreamSession starts.
	done := make(chan struct{})
	go func() {
		<-fake.Done()
		wrapper.releaseOnce.Do(wrapper.release)
		close(done)
	}()

	// Signal Done externally (as if the session's ctx was cancelled).
	fake.signalDoneExternal()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watcher goroutine did not run after Done signal")
	}

	// Caller then tries to Close anyway. Release must still only have
	// fired once total.
	if err := wrapper.Close(); err != nil {
		t.Errorf("Close after Done: %v", err)
	}

	if got := releases.Load(); got != 1 {
		t.Errorf("release fired %d times, want exactly 1 "+
			"(Done signal + explicit Close must be idempotent)", got)
	}
}

// Concurrent Close and Done-watcher race test — sync.Once must make
// the release exactly-once even under -race with both paths firing
// simultaneously.
func TestRealtimeSessionBookkeeping_ConcurrentCloseAndDone(t *testing.T) {
	for iter := 0; iter < 100; iter++ {
		fake := newFakeStreamSession()
		var releases atomic.Int32
		wrapper := &realtimeSessionBookkeeping{
			StreamInputSession: fake,
			release:            func() { releases.Add(1) },
		}

		var wg sync.WaitGroup

		// Watcher goroutine.
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-fake.Done()
			wrapper.releaseOnce.Do(wrapper.release)
		}()

		// Caller goroutine that races the watcher.
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = wrapper.Close()
		}()

		wg.Wait()

		if got := releases.Load(); got != 1 {
			t.Fatalf("iter %d: release fired %d times, want exactly 1", iter, got)
		}
	}
}
