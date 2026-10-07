package guardrails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TestValidatorDeclarationReachesItsOwnEvents compiles two pack validators,
// each with its own declaration, and checks every validation event carries
// the declaration of the validator that emitted it. A firing reports it on
// the decision for the stage's validation.failed event.
func TestValidatorDeclarationReachesItsOwnEvents(t *testing.T) {
	bus := events.NewEventBus()
	t.Cleanup(func() { bus.Close() })
	sink := &eventSink{}
	bus.Subscribe(events.EventValidationStarted, func(e *events.Event) { sink.record(e) })

	banned := &packspec.Validator{ID: "no-cards", Type: "banned_words",
		Extensions: map[string]any{"acme:control": "PCI-3.4"}}
	length := &packspec.Validator{ID: "short", Type: "max_length",
		Extensions: map[string]any{"acme:control": "UX-1"}}
	hooksOut, err := CompileValidatorsWithOptions([]prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]any{"words": []any{"4111"}}, Declaration: banned},
		{Type: "max_length", Params: map[string]any{"max_characters": 1000}, Declaration: length},
	}, evals.NewEvalTypeRegistry(), WithEmitter(events.NewEmitter(bus, "e", "s", "c")))
	require.NoError(t, err)
	require.Len(t, hooksOut, 2)

	resp := &hooks.ProviderResponse{Message: types.Message{Role: "assistant", Content: "card 4111"}}
	var fired hooks.Decision
	for _, h := range hooksOut {
		if d := h.AfterCall(context.Background(), &hooks.ProviderRequest{}, resp); !d.Allow {
			fired = d
		}
	}

	sink.awaitCount(t, 2)
	byType := map[string]string{}
	for _, e := range sink.events {
		data := e.Data.(*events.ValidationEventData)
		require.NotNil(t, data.Validator, "event for %s lost its declaration", data.ValidatorType)
		byType[data.ValidatorType] = data.Validator.ID
	}
	require.Equal(t, map[string]string{"banned_words": "no-cards", "max_length": "short"}, byType)

	require.Same(t, banned, fired.Metadata[hooks.MetadataKeyValidatorDeclaration],
		"the firing decision must carry the declaration of the validator that fired")
}

// TestValidatorWithoutDeclarationReportsNil — a guardrail not declared in a
// pack reports nil, and puts nothing on its firing decision.
func TestValidatorWithoutDeclarationReportsNil(t *testing.T) {
	hooksOut, err := CompileValidatorsWithOptions([]prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]any{"words": []any{"4111"}}},
	}, evals.NewEvalTypeRegistry())
	require.NoError(t, err)

	resp := &hooks.ProviderResponse{Message: types.Message{Role: "assistant", Content: "card 4111"}}
	d := hooksOut[0].AfterCall(context.Background(), &hooks.ProviderRequest{}, resp)

	require.False(t, d.Allow)
	require.NotContains(t, d.Metadata, hooks.MetadataKeyValidatorDeclaration)
}

// TestConfigSourcedValidatorReportsItsAuthoredDeclaration — a validator from
// a prompt config, with no pack declaration, still reports its authored id
// and extensions on a firing; one that authors neither reports nothing.
func TestConfigSourcedValidatorReportsItsAuthoredDeclaration(t *testing.T) {
	hooksOut, err := CompileValidatorsWithOptions([]prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]any{"words": []any{"4111"}},
			ID: "no-cards", Extensions: map[string]any{"acme:control": "PCI"}},
	}, evals.NewEvalTypeRegistry())
	require.NoError(t, err)

	resp := &hooks.ProviderResponse{Message: types.Message{Role: "assistant", Content: "card 4111"}}
	d := hooksOut[0].AfterCall(context.Background(), &hooks.ProviderRequest{}, resp)

	require.False(t, d.Allow)
	decl, ok := d.Metadata[hooks.MetadataKeyValidatorDeclaration].(*packspec.Validator)
	require.True(t, ok)
	require.Equal(t, "no-cards", decl.ID)
	require.Equal(t, map[string]any{"acme:control": "PCI"}, decl.Extensions)
}
