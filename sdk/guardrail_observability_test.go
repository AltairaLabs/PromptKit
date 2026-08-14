package sdk

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A pack with no validators of its own — the guardrail comes from WithGuardrail.
const plainObsPackJSON = `{
  "$schema": "https://promptpack.org/schema/2025.1/promptpack.schema.json",
  "schema_version": "2025.1",
  "id": "guardrail-obs-plain",
  "version": "1.0.0",
  "template_engine": {"version": "v1", "syntax": "handlebars", "features": []},
  "prompts": {
    "default": {
      "id": "default", "name": "Default", "description": "helpful",
      "version": "1.0.0",
      "system_template": "You are helpful."
    }
  }
}`

// A pack whose prompt declares a length guardrail generous enough to pass.
const guardrailObsPackJSON = `{
  "$schema": "https://promptpack.org/schema/2025.1/promptpack.schema.json",
  "schema_version": "2025.1",
  "id": "guardrail-obs-pack",
  "version": "1.0.0",
  "template_engine": {"version": "v1", "syntax": "handlebars", "features": []},
  "prompts": {
    "default": {
      "id": "default", "name": "Default", "description": "helpful",
      "version": "1.0.0",
      "system_template": "You are helpful.",
      "validators": [
        {"type": "length", "enabled": true, "params": {"max": 5000}}
      ]
    }
  }
}`

// A pack-declared guardrail is compiled long before the conversation exists, so
// it cannot build its own emitter. Unless the pipeline hands it one, it stays
// silent and guardrail enforcement remains unobservable — which is the whole of
// #1771. This is the end-to-end proof that the handoff happens.
func TestPackGuardrail_EmitsValidationEventsThroughTheConversation(t *testing.T) {
	packPath := filepath.Join(t.TempDir(), "obs.pack.json")
	require.NoError(t, os.WriteFile(packPath, []byte(guardrailObsPackJSON), 0o600))

	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })

	var mu sync.Mutex
	seen := map[events.EventType]int{}
	for _, et := range []events.EventType{events.EventValidationStarted, events.EventValidationPassed} {
		bus.Subscribe(et, func(e *events.Event) {
			mu.Lock()
			defer mu.Unlock()
			seen[e.Type]++
		})
	}

	conv, err := Open(packPath, "default",
		WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithEventBus(bus),
	)
	require.NoError(t, err)
	defer conv.Close()

	_, err = conv.Send(context.Background(), "hello")
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return seen[events.EventValidationStarted] > 0 && seen[events.EventValidationPassed] > 0
	}, 2*time.Second, 10*time.Millisecond,
		"a pack guardrail must emit started and passed; without started the OTel span is never created")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, seen[events.EventValidationStarted], seen[events.EventValidationPassed],
		"every started must be closed by a passed, or spans leak into pendingEnds")
}

// The func-backed path reaches the same wiring: WithGuardrail builds a
// funcGuardrail, which is not a GuardrailHookAdapter, so it becomes observable
// only because Registry.SetEmitter walks every provider hook rather than
// looking for one concrete type.
func TestFuncGuardrail_EmitsValidationEventsThroughTheConversation(t *testing.T) {
	packPath := filepath.Join(t.TempDir(), "func.pack.json")
	require.NoError(t, os.WriteFile(packPath, []byte(plainObsPackJSON), 0o600))

	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })

	var mu sync.Mutex
	seen := map[events.EventType]int{}
	for _, et := range []events.EventType{events.EventValidationStarted, events.EventValidationPassed} {
		bus.Subscribe(et, func(e *events.Event) {
			mu.Lock()
			defer mu.Unlock()
			seen[e.Type]++
		})
	}

	var ran bool
	conv, err := Open(packPath, "default",
		WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithEventBus(bus),
		WithGuardrail(guardrails.OutputFunc("length-watch",
			func(_ context.Context, _ *hooks.OutputRequest) hooks.Decision {
				mu.Lock()
				ran = true
				mu.Unlock()
				return hooks.Allow
			})),
	)
	require.NoError(t, err)
	defer conv.Close()

	_, err = conv.Send(context.Background(), "hello")
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return seen[events.EventValidationStarted] > 0 && seen[events.EventValidationPassed] > 0
	}, 2*time.Second, 10*time.Millisecond,
		"a func guardrail must emit started and passed, or its span is never created")

	mu.Lock()
	defer mu.Unlock()
	require.True(t, ran, "the guardrail must actually have run — otherwise the events prove nothing")
}
