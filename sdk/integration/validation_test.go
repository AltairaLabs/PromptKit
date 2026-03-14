package integration

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// ---------------------------------------------------------------------------
// Hook implementations for validation tests
// ---------------------------------------------------------------------------

// forbiddenWordHook denies requests whose messages contain a forbidden word.
type forbiddenWordHook struct {
	forbidden string
}

func (h *forbiddenWordHook) Name() string { return "forbidden_word" }

func (h *forbiddenWordHook) BeforeCall(_ context.Context, req *hooks.ProviderRequest) hooks.Decision {
	for _, msg := range req.Messages {
		if strings.Contains(strings.ToLower(msg.GetContent()), strings.ToLower(h.forbidden)) {
			return hooks.Deny("message contains forbidden word: " + h.forbidden)
		}
	}
	return hooks.Allow
}

func (h *forbiddenWordHook) AfterCall(_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse) hooks.Decision {
	return hooks.Allow
}

// afterCallTracker is a hook that records whether AfterCall was invoked.
type afterCallTracker struct {
	called atomic.Bool
}

func (h *afterCallTracker) Name() string { return "after_call_tracker" }

func (h *afterCallTracker) BeforeCall(_ context.Context, _ *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (h *afterCallTracker) AfterCall(_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse) hooks.Decision {
	h.called.Store(true)
	return hooks.Allow
}

// ---------------------------------------------------------------------------
// 6.1 — BeforeCall hook blocks forbidden messages
// ---------------------------------------------------------------------------

func TestValidation_HookBeforeCall(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	ec := newEventCollector(bus)

	hook := &forbiddenWordHook{forbidden: "forbidden"}
	conv := openTestConv(t,
		sdk.WithEventBus(bus),
		sdk.WithProviderHook(hook),
	)

	ctx := context.Background()

	// Sending a message with the forbidden word should be denied.
	_, err := conv.Send(ctx, "This message contains forbidden content")
	require.Error(t, err, "send with forbidden word should return an error")

	var denied *hooks.HookDeniedError
	assert.True(t, errors.As(err, &denied), "error should be HookDeniedError, got %T: %v", err, err)
	if denied != nil {
		assert.Contains(t, denied.Reason, "forbidden")
	}

	// Sending a normal message should succeed.
	resp, err := conv.Send(ctx, "Hello, how are you?")
	require.NoError(t, err, "send with normal message should succeed")
	assert.NotEmpty(t, resp.Text(), "response should have text")

	// Wait for pipeline events to arrive.
	ec.waitForEvent(events.EventPipelineCompleted, 2*time.Second)
}

// ---------------------------------------------------------------------------
// 6.2 — AfterCall hook runs on responses
// ---------------------------------------------------------------------------

func TestValidation_HookAfterCall(t *testing.T) {
	tracker := &afterCallTracker{}
	conv := openTestConv(t,
		sdk.WithProviderHook(tracker),
	)

	ctx := context.Background()
	resp, err := conv.Send(ctx, "Hello")
	require.NoError(t, err, "send should succeed")
	assert.NotEmpty(t, resp.Text(), "response should have text")

	// The AfterCall hook should have been invoked during the pipeline.
	assert.True(t, tracker.called.Load(), "AfterCall hook should have been called")
}
