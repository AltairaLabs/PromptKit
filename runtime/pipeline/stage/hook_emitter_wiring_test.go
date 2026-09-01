package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// emitterAwareHook is a provider hook that wants the conversation's emitter,
// as guardrails do.
type emitterAwareHook struct {
	got *events.Emitter
}

func (h *emitterAwareHook) Name() string { return "emitter-aware" }

func (h *emitterAwareHook) BeforeCall(_ context.Context, _ *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (h *emitterAwareHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

func (h *emitterAwareHook) SetEmitter(e *events.Emitter) { h.got = e }

// plainHook implements no emitter awareness and must be left alone.
type plainHook struct{}

func (h *plainHook) Name() string { return "plain" }

func (h *plainHook) BeforeCall(_ context.Context, _ *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (h *plainHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, _ *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

// A hook that emits its own events is useless unless something hands it the
// conversation's emitter. The stage constructor is the one place that holds
// both the emitter and the hook registry, so it is where the two are joined —
// which means SDK and Arena both get it without wiring anything themselves.
func TestNewProviderStage_GivesHooksTheEmitter(t *testing.T) {
	aware := &emitterAwareHook{}
	registry := hooks.NewRegistry(
		hooks.WithProviderHook(aware),
		hooks.WithProviderHook(&plainHook{}),
	)

	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	emitter := events.NewEmitter(bus, "exec", "sess", "conv")

	_ = NewProviderStageWithHooks(
		mock.NewProvider("t", "m", false), nil, nil, nil, emitter, registry)

	require.NotNil(t, aware.got, "an emitter-aware hook must receive the stage's emitter")
	assert.Same(t, emitter, aware.got)
}

// Constructing without an emitter, or without hooks, must stay safe — both are
// ordinary configurations.
func TestNewProviderStage_EmitterWiringIsOptional(t *testing.T) {
	aware := &emitterAwareHook{}
	registry := hooks.NewRegistry(hooks.WithProviderHook(aware))

	require.NotPanics(t, func() {
		_ = NewProviderStageWithHooks(mock.NewProvider("t", "m", false), nil, nil, nil, nil, registry)
		_ = NewProviderStageWithHooks(mock.NewProvider("t", "m", false), nil, nil, nil, nil, nil)
	})
	assert.Nil(t, aware.got, "no emitter means nothing to hand out")
}
