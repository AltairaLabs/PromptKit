package guardrails

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// recordingJudge answers every judging call the same way and remembers that it
// was asked. The verdict is deliberately a clean pass, so a test that sees a
// guardrail fire knows the firing did not come from the judge.
type recordingJudge struct{ calls int }

//nolint:gocritic // JudgeOpts is passed by value by the JudgeProvider interface
func (j *recordingJudge) Judge(_ context.Context, _ handlers.JudgeOpts) (*handlers.JudgeResult, error) {
	j.calls++
	return &handlers.JudgeResult{Passed: true, Score: 1.0, Reasoning: "fine"}, nil
}

func userTurn(text string) []types.Message {
	m := types.Message{Role: "user"}
	m.AddTextPart(text)
	return []types.Message{m}
}

// TestGuardrail_JudgeBackedRefusesToBuildWithoutJudge is the load-time half of
// #1996. A judge-backed guardrail that names no provider, with no host default
// either, scored 0.0 against the 1.0 floor and blocked every turn, reporting a
// content violation as the reason. Nothing about that is discoverable at
// runtime, and all of it is knowable at load.
func TestGuardrail_JudgeBackedRefusesToBuildWithoutJudge(t *testing.T) {
	for _, evalType := range []string{"toxicity", "bias", "role_violation", "pii_leakage", "faithfulness"} {
		t.Run(evalType, func(t *testing.T) {
			_, err := NewGuardrailHook(evalType, map[string]any{})

			require.Error(t, err, "a judge-backed guardrail with no judge must not build")
			assert.ErrorIs(t, err, ErrGuardrailNeedsJudge)
			assert.Contains(t, err.Error(), "params.provider",
				"the error should name the param a pack uses to point at its required provider")
			assert.Contains(t, err.Error(), "requires",
				"…and the requires block, which is where that name is declared")
		})
	}
}

// A pack that names its provider builds: resolution happens per turn, through
// the host's binding, so construction only has to see that a name was given.
func TestGuardrail_JudgeBackedBuildsWhenThePackNamesAProvider(t *testing.T) {
	hook, err := NewGuardrailHook("toxicity", map[string]any{"provider": "grader"})

	require.NoError(t, err)
	adapter, ok := hook.(*GuardrailHookAdapter)
	require.True(t, ok)
	assert.Equal(t, "grader", adapter.params["provider"],
		"the guardrail must carry the name its pack gave, since that is what it resolves per turn")
}

func TestGuardrail_JudgeBackedBuildsWithHostDefaultJudge(t *testing.T) {
	judge := &recordingJudge{}

	hook, err := NewGuardrailHook("toxicity", map[string]any{}, WithJudge(judge))

	require.NoError(t, err)
	adapter, ok := hook.(*GuardrailHookAdapter)
	require.True(t, ok)
	assert.Same(t, judge, adapter.judge,
		"the guardrail built, but not around the judge it was given — it would find none at turn time")
	assert.Equal(t, "toxicity", adapter.evalType)
}

// TestGuardrail_NonJudgeTypeStillBuildsWithoutJudge: the refusal is scoped to
// handlers that actually need a judge. A regex or length check never did.
func TestGuardrail_NonJudgeTypeStillBuildsWithoutJudge(t *testing.T) {
	hook, err := NewGuardrailHook("contains", map[string]any{"text": "forbidden"})

	require.NoError(t, err)
	adapter, ok := hook.(*GuardrailHookAdapter)
	require.True(t, ok)
	assert.Equal(t, "contains", adapter.evalType)
	assert.Nil(t, adapter.judge, "a deterministic check must not acquire a judge it never uses")

	// And it still works: the refusal is scoped to judge-backed types, so a
	// check that needs nothing keeps enforcing.
	adapter.direction = DirectionInput
	d := adapter.BeforeCall(context.Background(), &hooks.ProviderRequest{
		Messages: userTurn("this contains forbidden material"),
	})
	assert.False(t, d.Allow, "the contains guardrail did not fire on matching input")
}

// TestGuardrail_JudgeReachesTheHandler is the producer half: the judge must
// arrive in the eval context's metadata, which is the only channel the
// judge-backed handlers read it from.
func TestGuardrail_JudgeReachesTheHandler(t *testing.T) {
	judge := &recordingJudge{}
	hook, err := NewGuardrailHook("toxicity", map[string]any{}, WithJudge(judge))
	require.NoError(t, err)

	adapter, ok := hook.(*GuardrailHookAdapter)
	require.True(t, ok)
	adapter.direction = DirectionInput

	d := adapter.BeforeCall(context.Background(), &hooks.ProviderRequest{
		Messages: userTurn("some input to judge"),
	})

	assert.Positive(t, judge.calls,
		"the handler never called the judge, so it did not find one in the metadata")
	assert.True(t, d.Allow, "a clean verdict must not block")
}

// TestCompileValidators_JudgeBackedIsFatalWithoutJudge pins the compile path
// SDK.Open uses: unusable guardrails abort the whole set rather than being
// dropped, which is the existing policy for an unknown type and a rejected
// param set.
func TestCompileValidators_JudgeBackedIsFatalWithoutJudge(t *testing.T) {
	specs := []prompt.ValidatorConfig{{Type: "toxicity"}} // names no provider

	hooksOut, err := CompileValidators(specs)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrGuardrailNeedsJudge))
	assert.Empty(t, hooksOut, "no partial guardrail set on a fatal error")
}

func TestCompileValidatorsWithOptions_JudgeAppliesToEveryValidator(t *testing.T) {
	specs := []prompt.ValidatorConfig{
		{Type: "toxicity"},
		{Type: "bias"},
		{Type: "contains", Params: map[string]any{"text": "forbidden"}},
	}

	hooksOut, err := CompileValidatorsWithOptions(specs, nil, WithJudge(&recordingJudge{}))

	require.NoError(t, err)
	assert.Len(t, hooksOut, 3)
}

// A validator's own message still wins over the shared options, which are
// applied first.
func TestCompileValidatorsWithOptions_ValidatorMessageWins(t *testing.T) {
	specs := []prompt.ValidatorConfig{{Type: "toxicity", Message: "custom wording"}}

	hooksOut, err := CompileValidatorsWithOptions(specs, nil,
		WithJudge(&recordingJudge{}), WithMessage("shared wording"))

	require.NoError(t, err)
	require.Len(t, hooksOut, 1)
	adapter, ok := hooksOut[0].(*GuardrailHookAdapter)
	require.True(t, ok)
	assert.Equal(t, "custom wording", adapter.message)
}
