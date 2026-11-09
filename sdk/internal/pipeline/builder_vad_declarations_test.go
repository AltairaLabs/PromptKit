package pipeline

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// promptCapturingHook records the pack prompt declaration each call carries.
type promptCapturingHook struct{ prompts []*packspec.Prompt }

func (*promptCapturingHook) Name() string { return "prompt-capturing-hook" }

func (h *promptCapturingHook) BeforeCall(_ context.Context, req *hooks.ProviderRequest) hooks.Decision {
	h.prompts = append(h.prompts, req.Prompt)
	return hooks.Allow
}

func (*promptCapturingHook) AfterCall(
	context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse,
) hooks.Decision {
	return hooks.Allow
}

// A provider hook learns which pack prompt the model is invoked for from the
// prompt declarations (RFC 0016). The text topology hands them to its
// provider stage; the VAD topology must too, or a governance hook keyed on
// the prompt sees nil on every voice turn.
func TestVADPipeline_ProviderHooksSeeThePromptDeclaration(t *testing.T) {
	decl := &packspec.Prompt{ID: "chat", Description: "the chat prompt"}
	hook := &promptCapturingHook{}
	vadConfig := stage.AudioTurnConfig{VAD: &mockVADAnalyzer{}, TurnDetector: &mockTurnDetector{}}
	cfg := &Config{
		Provider:           newRecordingProvider(),
		VADConfig:          &vadConfig,
		HookRegistry:       hooks.NewRegistry(hooks.WithProviderHook(hook)),
		PromptDeclarations: map[string]*packspec.Prompt{"chat": decl},
	}
	turnState := stage.NewTurnState()
	turnState.Template = &prompt.Template{TaskType: "chat"}

	stages, err := buildVADPipelineStages(cfg, turnState)
	require.NoError(t, err)

	input := make(chan stage.StreamElement, 4)
	input <- stage.NewMessageElement(&types.Message{Role: "user", Content: "say something"})
	input <- stage.NewEndOfTurnElement()
	close(input)
	output := make(chan stage.StreamElement, 32)
	require.NoError(t, providerStageFrom(t, stages).Process(context.Background(), input, output))
	assistants, errs := drainTurn(output)
	require.Empty(t, errs)
	require.Len(t, assistants, 1)

	require.Len(t, hook.prompts, 1)
	assert.Same(t, decl, hook.prompts[0], "the voice turn's hook must see the pack's prompt declaration")
}
