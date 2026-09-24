package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// bindingStub stands in for a host: it answers the names it was told about and
// reports the right kind of failure for everything else.
type bindingStub struct {
	llm       providers.Provider
	inference inference.Provider
	llmErr    error
	inferErr  error
	other     any // a bound value that is not an inference.Provider
}

func (b bindingStub) LLM(string) (providers.Provider, error) {
	if b.llmErr != nil {
		return nil, b.llmErr
	}
	return b.llm, nil
}

func (b bindingStub) Classifier(string) (any, error) {
	if b.inferErr != nil {
		return nil, b.inferErr
	}
	if b.other != nil {
		return b.other, nil
	}
	return b.inference, nil
}

// labelStub answers every request with one fixed label, so a test can tell
// which provider it was handed.
type labelStub struct{ label string }

func (s labelStub) Infer(context.Context, inference.Request) (inference.Response, error) {
	return inference.Response{Scores: []inference.LabelScore{{Label: s.label, Score: 1}}}, nil
}

func inferLabel(t *testing.T, p inference.Provider) string {
	t.Helper()
	resp, err := p.Infer(context.Background(), inference.Request{})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Scores)
	return resp.Scores[0].Label
}

func TestResolveInference_ResolvesANamedKeyThroughTheBinding(t *testing.T) {
	ctx := evals.WithProviderBinding(context.Background(), bindingStub{inference: labelStub{"bound"}})

	got, err := resolveInference(ctx, "screener", "text classifier")

	require.NoError(t, err)
	assert.Equal(t, "bound", inferLabel(t, got), "the resolved provider is not the one the host bound")
}

// Each failure has to name the key and say what kind of thing was wanted, so
// the person reading it knows where to look.
func TestResolveInference_Failures(t *testing.T) {
	t.Run("no binding at all", func(t *testing.T) {
		_, err := resolveInference(context.Background(), "screener", "text classifier")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "screener")
		assert.Contains(t, err.Error(), "text classifier")
	})

	t.Run("host bound nothing", func(t *testing.T) {
		ctx := evals.WithProviderBinding(context.Background(), bindingStub{inferErr: evals.ErrUnboundKey})

		_, err := resolveInference(ctx, "screener", "text classifier")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "screener")
		assert.Contains(t, err.Error(), "supply a provider")
	})

	t.Run("binding returned a value that does not implement Infer", func(t *testing.T) {
		ctx := evals.WithProviderBinding(context.Background(), bindingStub{other: struct{}{}})

		_, err := resolveInference(ctx, "screener", "text classifier")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "screener")
		assert.Contains(t, err.Error(), "not an inference provider")
	})

	t.Run("bound something that is not an inference provider", func(t *testing.T) {
		ctx := evals.WithProviderBinding(context.Background(), bindingStub{inferErr: evals.ErrWrongKind})

		_, err := resolveInference(ctx, "screener", "text classifier")

		require.Error(t, err)
		assert.ErrorContains(t, err, "cannot do what the check needs")
	})
}

func TestResolveInference_NoKeyAndNoRegistry(t *testing.T) {
	_, err := resolveInference(context.Background(), "", "text classifier")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "names no provider")
	assert.Contains(t, err.Error(), ProviderParam)
}

func TestResolveInference_NoKeyUsesTheHostsDefault(t *testing.T) {
	reg := inference.NewRegistry()
	require.NoError(t, reg.Register("host-default", labelStub{"default"}))
	require.NoError(t, reg.Register("other", labelStub{"other"}))
	ctx := inference.WithRegistry(context.Background(), reg)

	got, err := resolveInference(ctx, "", "text classifier")

	require.NoError(t, err)
	assert.Equal(t, "default", inferLabel(t, got),
		"a check that names nothing falls back to what the HOST made default")
}

// The judge side of the same mechanism: a named provider resolves through the
// binding, and nothing else is consulted.
func TestResolveJudgeProvider_UsesTheNamedProvider(t *testing.T) {
	ctx := evals.WithProviderBinding(context.Background(),
		bindingStub{llm: mock.NewProvider("grader", "mock-model", false)})

	judge, err := resolveJudgeProvider(ctx, &evals.EvalContext{},
		map[string]any{ProviderParam: "grader"})

	require.NoError(t, err)
	pj, ok := judge.(*ProviderJudge)
	require.True(t, ok)
	assert.Equal(t, "grader", pj.ID())
}

func TestResolveJudgeProvider_NamedButUnresolvable(t *testing.T) {
	ctx := evals.WithProviderBinding(context.Background(),
		bindingStub{llmErr: evals.ErrWrongKind})

	_, err := resolveJudgeProvider(ctx, &evals.EvalContext{},
		map[string]any{ProviderParam: "grader"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "grader")
}

// A check that names nothing still honors a judge a direct caller put in the
// metadata — the offline Evaluate() path, which has no pack binding in play.
func TestResolveJudgeProvider_FallsBackToMetadata(t *testing.T) {
	supplied := &ProviderJudge{}
	evalCtx := &evals.EvalContext{Metadata: map[string]any{"judge_provider": JudgeProvider(supplied)}}

	judge, err := resolveJudgeProvider(context.Background(), evalCtx, nil)

	require.NoError(t, err)
	assert.Same(t, supplied, judge)
}

func TestProviderKeyFrom(t *testing.T) {
	assert.Equal(t, "grader", providerKeyFrom(map[string]any{ProviderParam: "grader"}))
	assert.Empty(t, providerKeyFrom(map[string]any{ProviderParam: 42}),
		"a non-string is not a name")
	assert.Empty(t, providerKeyFrom(nil))
}
