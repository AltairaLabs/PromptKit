package events

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEmitter_NilDataPublishesNothing pins that a struct-taking emitter method
// given nil data publishes no event at all, rather than an event whose Data is
// a typed nil that a subscriber's type assertion would then dereference.
func TestEmitter_NilDataPublishesNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	calls := map[string]func(e *Emitter){
		"ProviderCallCompleted":     func(e *Emitter) { e.ProviderCallCompleted(nil) },
		"ProviderCallStartedCtx":    func(e *Emitter) { e.ProviderCallStartedCtx(ctx, nil) },
		"ProviderCallCompletedCtx":  func(e *Emitter) { e.ProviderCallCompletedCtx(ctx, nil) },
		"ProviderCallFailedCtx":     func(e *Emitter) { e.ProviderCallFailedCtx(ctx, nil) },
		"ToolCallEventCtx":          func(e *Emitter) { e.ToolCallEventCtx(ctx, EventToolCallStarted, nil) },
		"GuardrailStarted":          func(e *Emitter) { e.GuardrailStarted(nil) },
		"GuardrailResult":           func(e *Emitter) { e.GuardrailResult(nil) },
		"MessageCreatedCtx":         func(e *Emitter) { e.MessageCreatedCtx(ctx, nil) },
		"ClientToolRequest":         func(e *Emitter) { e.ClientToolRequest(nil) },
		"ClientToolResolved":        func(e *Emitter) { e.ClientToolResolved(nil) },
		"ImageGenCallCompletedCtx":  func(e *Emitter) { e.ImageGenCallCompletedCtx(ctx, nil) },
		"ImageGenCallFailedCtx":     func(e *Emitter) { e.ImageGenCallFailedCtx(ctx, nil) },
		"TTSCallCompletedCtx":       func(e *Emitter) { e.TTSCallCompletedCtx(ctx, nil) },
		"TTSCallFailedCtx":          func(e *Emitter) { e.TTSCallFailedCtx(ctx, nil) },
		"STTCallCompletedCtx":       func(e *Emitter) { e.STTCallCompletedCtx(ctx, nil) },
		"STTCallFailedCtx":          func(e *Emitter) { e.STTCallFailedCtx(ctx, nil) },
		"InferenceCallCompletedCtx": func(e *Emitter) { e.InferenceCallCompletedCtx(ctx, nil) },
		"InferenceCallFailedCtx":    func(e *Emitter) { e.InferenceCallFailedCtx(ctx, nil) },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			bus := NewEventBus()
			var mu sync.Mutex
			var got []EventType
			bus.SubscribeAll(func(ev *Event) {
				mu.Lock()
				got = append(got, ev.Type)
				mu.Unlock()
			})
			emitter := NewEmitter(bus, "run", "session", "conv")

			call(emitter)
			// A sentinel proves the bus delivers; Close drains every queue.
			emitter.PipelineStarted(1)
			bus.Close()

			mu.Lock()
			defer mu.Unlock()
			assert.Equal(t, []EventType{EventPipelineStarted}, got)
		})
	}
}

// TestEmitter_NilReceiverAndNilBusAreSafe pins the nil-safety contract: a nil
// *Emitter, or one built without a bus, can be called freely, so callers need
// not guard every emit site.
func TestEmitter_NilReceiverAndNilBusAreSafe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for name, emitter := range map[string]*Emitter{
		"nil emitter": nil,
		"nil bus":     NewEmitter(nil, "run", "session", "conv"),
	} {
		t.Run(name, func(t *testing.T) {
			require.NotPanics(t, func() {
				emitter.PipelineStarted(1)
				emitter.MessageCreated("user", "hello", 0, nil, nil, nil, nil)
				emitter.MessageUpdated(0, 100, 10, 20, 0.001)
				emitter.ConversationStarted("system prompt")
				emitter.ProviderCallCompletedCtx(ctx, &ProviderCallCompletedData{})
				emitter.ProviderCallFailedCtx(ctx, &ProviderCallFailedData{})
				emitter.ToolCallCompletedCtx(ctx, "t", "c", time.Second, "s", nil, nil)
				emitter.ToolCallFailedCtx(ctx, "t", "c", errors.New("e"), time.Second, nil)
			})
		})
	}
}
