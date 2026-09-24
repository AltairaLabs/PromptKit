package sdk

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

func TestEvaluateBinding_PrefersTheCallersBinding(t *testing.T) {
	explicit := testOnlyBinding{}
	opts := EvaluateOpts{
		ProviderBinding: explicit,
		JudgeTargets:    map[string]any{"grader": providers.ProviderSpec{ID: "x", Type: "mock"}},
	}

	assert.Equal(t, explicit, evaluateBinding(&opts),
		"an explicit binding must win over judge targets, which are only a convenience")
}

// Nothing supplied means no mapping at all — stated against the case where
// something IS supplied, because a bare nil check would also pass for an
// implementation that never returned a binding.
func TestEvaluateBinding_NilOnlyWhenTheCallerSuppliedNothing(t *testing.T) {
	assert.Nil(t, evaluateBinding(&EvaluateOpts{}),
		"with nothing supplied there is no mapping, and checks must say so rather than guess")

	fromTargets := evaluateBinding(&EvaluateOpts{
		JudgeTargets: map[string]any{
			"grader": providers.ProviderSpec{ID: "grader", Type: "mock", Model: "mock-model"},
		},
	})
	require.NotNil(t, fromTargets)
	p, err := fromTargets.LLM("grader")
	require.NoError(t, err)
	assert.Equal(t, "grader", p.ID(), "judge targets must resolve as the binding they stand in for")
}

// Judge targets are already names pointing at provider specs, so they serve as
// a binding for the offline path without the caller building one.
func TestJudgeTargetBinding_ResolvesASpec(t *testing.T) {
	b := evaluateBinding(&EvaluateOpts{
		JudgeTargets: map[string]any{
			"grader": providers.ProviderSpec{ID: "grader", Type: "mock", Model: "mock-model"},
		},
	})
	require.NotNil(t, b)

	p, err := b.LLM("grader")

	require.NoError(t, err)
	assert.Equal(t, "grader", p.ID())
}

func TestJudgeTargetBinding_Failures(t *testing.T) {
	b := evaluateBinding(&EvaluateOpts{
		JudgeTargets: map[string]any{
			"grader":   providers.ProviderSpec{ID: "grader", Type: "mock", Model: "mock-model"},
			"nonsense": 42,
		},
	})
	require.NotNil(t, b)

	// The fixture resolves what it should, so every failure below is about the
	// case it names rather than a binding that never worked.
	resolved, err := b.LLM("grader")
	require.NoError(t, err)
	require.Equal(t, "grader", resolved.ID())

	t.Run("name nobody supplied", func(t *testing.T) {
		_, err := b.LLM("missing")
		require.ErrorIs(t, err, evals.ErrUnboundKey)
		assert.Equal(t, evals.ErrUnboundKey.Error(), err.Error(),
			"an unsupplied name is plainly unbound; anything more specific would be a claim "+
				"about a provider the caller never gave")
	})

	t.Run("target that is not a spec", func(t *testing.T) {
		_, err := b.LLM("nonsense")
		require.ErrorIs(t, err, evals.ErrWrongKind)
		assert.Contains(t, err.Error(), "not a provider spec")
	})

	t.Run("a classify check cannot use a judge target", func(t *testing.T) {
		_, err := b.Inference("grader")
		require.ErrorIs(t, err, evals.ErrWrongKind)
		assert.Contains(t, err.Error(), "ProviderBinding",
			"it must point at the option that would supply a classifier")
	})

	t.Run("classifier for a name nobody supplied", func(t *testing.T) {
		_, err := b.Inference("missing")
		require.ErrorIs(t, err, evals.ErrUnboundKey)
		assert.Equal(t, evals.ErrUnboundKey.Error(), err.Error(),
			"nothing was supplied for this name, so it is unbound rather than the wrong kind")
	})
}

// A pointer spec is as valid as a value one; callers assemble targets either way.
func TestJudgeTargetBinding_AcceptsAPointerSpec(t *testing.T) {
	spec := providers.ProviderSpec{ID: "grader", Type: "mock", Model: "mock-model"}
	b := evaluateBinding(&EvaluateOpts{JudgeTargets: map[string]any{"grader": &spec}})
	require.NotNil(t, b)

	p, err := b.LLM("grader")

	require.NoError(t, err)
	assert.Equal(t, "grader", p.ID())
}

// testOnlyBinding is a distinguishable binding for the precedence test.
type testOnlyBinding struct{}

func (testOnlyBinding) LLM(string) (providers.Provider, error) { return nil, evals.ErrUnboundKey }

func (testOnlyBinding) Inference(string) (inference.Provider, error) { return nil, evals.ErrUnboundKey }
