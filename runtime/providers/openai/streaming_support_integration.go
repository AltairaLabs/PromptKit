// Package openai provides OpenAI Realtime API streaming support.
package openai

import (
	"context"
	"fmt"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// CreateStreamSession creates a new bidirectional streaming session with OpenAI Realtime API.
//
// The session supports real-time audio input/output with the following features:
// - Bidirectional audio streaming (send and receive audio simultaneously)
// - Server-side voice activity detection (VAD) for automatic turn detection
// - Function/tool calling during the streaming session
// - Input and output audio transcription
//
// Audio Format:
// OpenAI Realtime API uses 24kHz 16-bit PCM mono audio by default.
// The session automatically handles base64 encoding/decoding of audio data.
//
// Concurrency bounds:
// If the provider has `stream_max_concurrent` configured, this call will
// block until a slot is available (respecting the caller's ctx) or return
// a rejection error recorded on
// promptkit_stream_concurrency_rejections_total. The slot is released
// when the session ends — whether via Close() or via the underlying
// Done() channel (e.g. context cancellation). This makes Realtime
// sessions subject to the same Phase 3 back-pressure as SSE streams.
//
// Example usage:
//
//	session, err := provider.CreateStreamSession(ctx, &providers.StreamingInputConfig{
//	    Config: types.StreamingMediaConfig{
//	        Type:       types.ContentTypeAudio,
//	        SampleRate: 24000,
//	        Encoding:   "pcm16",
//	        Channels:   1,
//	    },
//	    SystemInstruction: "You are a helpful assistant.",
//	})
func (p *Provider) CreateStreamSession(
	ctx context.Context,
	req *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	if err := p.validateStreamRequest(req); err != nil {
		return nil, err
	}

	// Acquire a concurrent-stream slot before dialing the WebSocket.
	// Nil semaphore is a no-op; a saturated semaphore blocks until
	// the caller's ctx is done, at which point AcquireStreamSlot
	// emits the rejection metric with the correct reason label.
	if err := p.AcquireStreamSlot(ctx); err != nil {
		return nil, fmt.Errorf("failed to acquire stream slot: %w", err)
	}
	slotReleased := false
	defer func() {
		if !slotReleased {
			p.ReleaseStreamSlot()
		}
	}()

	metrics := providers.DefaultStreamMetrics()
	providerID := p.ID()
	metrics.StreamsInFlightInc(providerID)
	metrics.ProviderCallsInFlightInc(providerID)
	metricsReleased := false
	defer func() {
		if !metricsReleased {
			metrics.StreamsInFlightDec(providerID)
			metrics.ProviderCallsInFlightDec(providerID)
		}
	}()

	config := p.buildRealtimeSessionConfig(req)
	p.applyStreamMetadata(req.Metadata, &config)
	p.applyStreamTools(req.Tools, &config)

	session, err := NewRealtimeSession(ctx, p.apiKey, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to create realtime session: %w", err)
	}

	// Transfer slot and gauge ownership to the bookkeeping wrapper.
	// The outer defers above are now no-ops on this success path; the
	// wrapper is responsible for releasing on either session.Close() or
	// session.Done() firing (whichever comes first).
	slotReleased = true
	metricsReleased = true

	wrapper := &realtimeSessionBookkeeping{
		StreamInputSession: session,
		release: func() {
			metrics.StreamsInFlightDec(providerID)
			metrics.ProviderCallsInFlightDec(providerID)
			p.ReleaseStreamSlot()
		},
	}
	// A caller that never calls Close() — for example, one whose ctx
	// is canceled and who then drops the reference — would otherwise
	// leak the semaphore slot. Watch Done() as a backup so every
	// session ever created gets its slot back, exactly once.
	go func() {
		<-session.Done()
		wrapper.releaseOnce.Do(wrapper.release)
	}()

	return wrapper, nil
}

// realtimeSessionBookkeeping wraps a RealtimeSession with a
// release-exactly-once callback that decrements the in-flight gauges
// and returns a slot to the concurrent-stream semaphore. It is
// transparent to callers: all non-Close methods are promoted from the
// embedded StreamInputSession interface; only Close is intercepted so
// the release runs at the first of {explicit Close, session Done}.
type realtimeSessionBookkeeping struct {
	providers.StreamInputSession
	release     func()
	releaseOnce sync.Once
}

// Close runs the bookkeeping release (once) and then closes the
// underlying session. sync.Once ensures the release is idempotent even
// if Close is called multiple times (which the interface contract
// explicitly permits) or concurrently with the Done()-watching
// goroutine started in CreateStreamSession.
func (s *realtimeSessionBookkeeping) Close() error {
	s.releaseOnce.Do(s.release)
	return s.StreamInputSession.Close()
}
