package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

type textClassifierStub struct{}

func (textClassifierStub) Infer(context.Context, inference.Request) (inference.Response, error) {
	return inference.Response{Scores: []inference.LabelScore{{Label: "positive", Score: 1}}}, nil
}

func bindingWith(t *testing.T, llmIDs []string, classifierIDs []string) evals.ProviderBinding {
	t.Helper()
	cfg := &config{}
	for _, id := range llmIDs {
		ensureProviderPool(cfg)
		cfg.providers.Register(mock.NewProvider(id, "mock-model", false))
	}
	for _, id := range classifierIDs {
		require.NoError(t, cfg.registerInferenceProvider(id, textClassifierStub{}))
	}
	return newHostBinding(cfg)
}

func TestHostBinding_ResolvesWhatTheHostBound(t *testing.T) {
	b := bindingWith(t, []string{"grader"}, []string{"screener"})

	llm, err := b.LLM("grader")
	require.NoError(t, err)
	assert.Equal(t, "grader", llm.ID())

	provider, err := b.Classifier("screener")
	require.NoError(t, err)
	assert.IsType(t, textClassifierStub{}, provider)
}

// A name nobody bound is unbound — not "wrong kind", which would send the host
// looking at a provider they never supplied.
func TestHostBinding_UnboundName(t *testing.T) {
	b := bindingWith(t, []string{"grader"}, []string{"screener"})

	_, llmErr := b.LLM("nobody")
	_, classErr := b.Classifier("nobody")

	assert.ErrorIs(t, llmErr, evals.ErrUnboundKey)
	assert.ErrorIs(t, classErr, evals.ErrUnboundKey)
}

// TestHostBinding_WrongKind is the case a host hits by accident: they bound
// something to the name, so nothing is missing, but it cannot do the job. Both
// directions have to say so, and say which way round it is.
func TestHostBinding_WrongKind(t *testing.T) {
	b := bindingWith(t, []string{"grader"}, []string{"screener"})

	_, err := b.LLM("screener")
	require.ErrorIs(t, err, evals.ErrWrongKind)
	assert.Contains(t, err.Error(), "runs completions",
		"asking for an LLM and finding a classifier must say what was needed")

	_, err = b.Classifier("grader")
	require.ErrorIs(t, err, evals.ErrWrongKind)
	assert.Contains(t, err.Error(), "role: inference",
		"asking for a classifier and finding an LLM must point at the provider role to use")
}

// Nothing wired means no binding at all, which is what lets a caller tell
// "this host offers nothing" from "this host offers nothing under that name" —
// two different errors for two different fixes.
func TestNewHostBinding_NilOnlyWhenNothingIsWired(t *testing.T) {
	assert.Nil(t, newHostBinding(&config{}),
		"a host that wired no providers has no bindings to offer")
	assert.Nil(t, newHostBinding(nil))

	wired := bindingWith(t, []string{"grader"}, nil)
	require.NotNil(t, wired, "a host that wired a provider must offer a binding")
	resolved, err := wired.LLM("grader")
	require.NoError(t, err)
	assert.Equal(t, "grader", resolved.ID())
}

func TestHostBinding_NilReceiverReportsNoBinding(t *testing.T) {
	var b *hostBinding

	_, llmErr := b.LLM("grader")
	_, classErr := b.Classifier("grader")

	assert.ErrorIs(t, llmErr, evals.ErrNoBinding)
	assert.ErrorIs(t, classErr, evals.ErrNoBinding)
}
