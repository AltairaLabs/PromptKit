package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

type textClassifierStub struct{}

func (textClassifierStub) ClassifyText(
	_ context.Context, _ string, _ classify.TextOptions,
) ([]classify.LabelScore, error) {
	return []classify.LabelScore{{Label: "positive", Score: 1}}, nil
}

func bindingWith(t *testing.T, llmIDs []string, classifierIDs []string) evals.ProviderBinding {
	t.Helper()
	cfg := &config{}
	for _, id := range llmIDs {
		ensureProviderPool(cfg)
		cfg.providers.Register(mock.NewProvider(id, "mock-model", false))
	}
	for _, id := range classifierIDs {
		_, err := cfg.registerClassifyBackend(id, textClassifierStub{})
		require.NoError(t, err)
	}
	return newHostBinding(cfg)
}

func TestHostBinding_ResolvesWhatTheHostBound(t *testing.T) {
	b := bindingWith(t, []string{"grader"}, []string{"screener"})

	llm, err := b.LLM("grader")
	require.NoError(t, err)
	assert.Equal(t, "grader", llm.ID())

	backend, err := b.Classifier("screener")
	require.NoError(t, err)
	assert.IsType(t, textClassifierStub{}, backend)
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

func TestNewHostBinding_NilWhenNothingIsWired(t *testing.T) {
	assert.Nil(t, newHostBinding(&config{}),
		"a host that wired no providers has no bindings to offer")
	assert.Nil(t, newHostBinding(nil))
}

func TestHostBinding_NilReceiverReportsNoBinding(t *testing.T) {
	var b *hostBinding

	_, llmErr := b.LLM("grader")
	_, classErr := b.Classifier("grader")

	assert.ErrorIs(t, llmErr, evals.ErrNoBinding)
	assert.ErrorIs(t, classErr, evals.ErrNoBinding)
}
