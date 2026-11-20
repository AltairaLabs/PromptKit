package openai

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		if !reflect.DeepEqual(config, DefaultRealtimeSessionConfig()) {
			t.Errorf("nil metadata changed the config: %+v", config)
		}
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

		p.applyStreamTools(&providers.StreamingInputConfig{Tools: tools}, &config)

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
		p.applyStreamTools(&providers.StreamingInputConfig{}, &config)

		if len(config.Tools) != 0 {
			t.Errorf("expected 0 tools, got %d", len(config.Tools))
		}
	})
}

var _ providers.StreamInputSupport = (*Provider)(nil)

// --- Realtime concurrency bounds (#862) ---
//
// The tests below verify the semaphore + gauge bookkeeping added to
// CreateStreamSession without hitting a real WebSocket. The semaphore
// acquire happens BEFORE NewRealtimeSession is called, so we can test
// the rejection path cleanly by pre-draining a small semaphore and
// asserting CreateStreamSession fails on acquire.

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

// --- the session's stream-slot release ---

// slotLimitedProvider is an Azure-platform provider dialing srv whose
// concurrent-stream semaphore holds one slot.
func slotLimitedProvider(srv *httptest.Server) *Provider {
	p := NewProviderFromConfig(&ProviderConfig{
		ID: "azure-realtime", Model: "dep", Credential: &tokenCredential{}, Platform: "azure",
		PlatformConfig: &providers.PlatformConfig{Type: "azure", Endpoint: srv.URL + "/"},
	})
	p.SetStreamSemaphore(providers.NewStreamSemaphore(1))
	return p
}

// slotHeld reports whether p's only stream slot is taken: a session cannot
// be opened within a short wait.
func slotHeld(t *testing.T, p *Provider) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	session, err := p.CreateStreamSession(ctx, audioStreamRequest())
	if err != nil {
		require.ErrorIs(t, err, context.DeadlineExceeded)
		return true
	}
	_ = session.Close()
	return false
}

// A session holds its stream slot until it is closed, and returns it exactly
// once however often it is closed: closing it again neither releases a slot
// another session holds nor panics on an over-release.
func TestRealtimeSession_CloseReturnsTheStreamSlotOnce(t *testing.T) {
	srv := newClientEventServer(t, make(chan map[string]any, 64))
	p := slotLimitedProvider(srv)
	first, err := p.CreateStreamSession(context.Background(), audioStreamRequest())
	require.NoError(t, err)
	require.True(t, slotHeld(t, p), "an open session holds the slot")

	for range 3 {
		require.NoError(t, first.Close())
	}
	second, err := p.CreateStreamSession(context.Background(), audioStreamRequest())
	require.NoError(t, err, "closing the session returned its slot")
	t.Cleanup(func() { _ = second.Close() })
	assert.True(t, slotHeld(t, p), "closing the first session again released the second's slot")
}

// A session whose connection ends returns its slot without being closed, and
// a later Close does not return it again.
func TestRealtimeSession_EndedSessionReturnsTheStreamSlotOnce(t *testing.T) {
	drop := make(chan struct{})
	var conns atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := bargeInUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		first := conns.Add(1) == 1
		if conn.WriteJSON(map[string]any{"type": "session.created",
			"session": map[string]any{"id": "sess_test", "model": "gpt-realtime"}}) != nil {
			return
		}
		if first {
			<-drop // the first connection ends when the test says
			return
		}
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	p := slotLimitedProvider(srv)
	first, err := p.CreateStreamSession(context.Background(), audioStreamRequest())
	require.NoError(t, err)

	close(drop)
	select {
	case <-first.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the session did not end when its connection did")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	second, err := p.CreateStreamSession(ctx, audioStreamRequest())
	require.NoError(t, err, "an ended session returns its slot")
	t.Cleanup(func() { _ = second.Close() })

	require.NoError(t, first.Close())
	assert.True(t, slotHeld(t, p), "closing the ended session released a slot it no longer held")
}

// The session runs the configured model, whatever its name. A model without
// "realtime" in it used to be swapped for a retired preview default, so the
// API's refusal named a model nobody had configured.
func TestBuildRealtimeSessionConfig_UsesConfiguredModel(t *testing.T) {
	p := NewProvider("test", "gpt-live-1", "https://api.openai.com", providers.ProviderDefaults{}, false)
	config := p.buildRealtimeSessionConfig(&providers.StreamingInputConfig{})
	if config.Model != "gpt-live-1" {
		t.Errorf("session model = %q, want the configured gpt-live-1", config.Model)
	}
}

// The GA realtime session takes max_output_tokens from the prompt; nothing
// else, so the rest are not sent (#2219).
func TestApplyStreamSampling_SetsMaxOutputTokens(t *testing.T) {
	p := NewProvider("rt", "gpt-realtime-2.1", "https://api.openai.com", providers.ProviderDefaults{}, false)
	config := p.buildRealtimeSessionConfig(&providers.StreamingInputConfig{})
	p.applyStreamSampling(&providers.StreamingSampling{MaxTokens: 300, Temperature: 0.2}, &config)
	if got := buildRealtimeSessionConfig(config).MaxOutputTokens; got != 300 {
		t.Errorf("max_output_tokens = %v, want 300", got)
	}

	untouched := p.buildRealtimeSessionConfig(&providers.StreamingInputConfig{})
	p.applyStreamSampling(nil, &untouched)
	if want := DefaultRealtimeSessionConfig().MaxResponseOutputTokens; untouched.MaxResponseOutputTokens != want {
		t.Errorf("nil sampling changed max_output_tokens to %v, want the default %v",
			untouched.MaxResponseOutputTokens, want)
	}
}
