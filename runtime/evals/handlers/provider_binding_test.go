package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// bindingStub stands in for a host: it answers the names it was told about and
// reports the right kind of failure for everything else.
type bindingStub struct {
	llm        providers.Provider
	classifier classify.Backend
	llmErr     error
	classErr   error
}

func (b bindingStub) LLM(string) (providers.Provider, error) {
	if b.llmErr != nil {
		return nil, b.llmErr
	}
	return b.llm, nil
}

func (b bindingStub) Classifier(string) (classify.Backend, error) {
	if b.classErr != nil {
		return nil, b.classErr
	}
	return b.classifier, nil
}

type textStub struct{}

func (textStub) ClassifyText(
	_ context.Context, _ string, _ classify.TextOptions,
) ([]classify.LabelScore, error) {
	return []classify.LabelScore{{Label: "positive", Score: 1}}, nil
}

func assertText(b classify.Backend) (classify.TextClassifier, bool) {
	c, ok := b.(classify.TextClassifier)
	return c, ok
}

func TestClassifierFor_ResolvesThroughTheBinding(t *testing.T) {
	ctx := evals.WithProviderBinding(context.Background(), bindingStub{classifier: textStub{}})

	got, err := classifierFor(ctx, "screener", "text classifier", assertText)

	require.NoError(t, err)
	assert.NotNil(t, got)
}

// Each failure has to name the key and say what kind of thing was wanted, so
// the person reading it knows where to look.
func TestClassifierFor_Failures(t *testing.T) {
	t.Run("no key named", func(t *testing.T) {
		ctx := evals.WithProviderBinding(context.Background(), bindingStub{classifier: textStub{}})

		_, err := classifierFor(ctx, "", "text classifier", assertText)

		require.Error(t, err)
		assert.Contains(t, err.Error(), ProviderParam,
			"it must name the param a pack uses to point at its provider")
		assert.Contains(t, err.Error(), "requires")
	})

	t.Run("no binding at all", func(t *testing.T) {
		_, err := classifierFor(context.Background(), "screener", "text classifier", assertText)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "screener")
	})

	t.Run("host bound nothing", func(t *testing.T) {
		ctx := evals.WithProviderBinding(context.Background(),
			bindingStub{classErr: evals.ErrUnboundKey})

		_, err := classifierFor(ctx, "screener", "text classifier", assertText)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "screener")
		assert.Contains(t, err.Error(), "supply a provider")
	})

	t.Run("bound a classifier that does not do this task", func(t *testing.T) {
		// A backend the host bound, which simply is not a text classifier.
		ctx := evals.WithProviderBinding(context.Background(),
			bindingStub{classifier: struct{}{}})

		_, err := classifierFor(ctx, "screener", "text classifier", assertText)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "is not a text classifier",
			"a backend bound to the name but unable to do the task must say so")
	})
}

func TestDefaultClassifier_NoRegistry(t *testing.T) {
	_, err := defaultClassifier(context.Background(), "text classifier",
		func(r *classify.Registry) (classify.TextClassifier, error) { return r.TextClassifier("") })

	require.Error(t, err)
	assert.Contains(t, err.Error(), "names no provider")
	assert.Contains(t, err.Error(), ProviderParam)
}

func TestDefaultClassifier_UsesTheHostsDefault(t *testing.T) {
	reg := classify.NewRegistry()
	reg.RegisterText("host-default", textStub{})
	require.NoError(t, reg.SetDefaultText("host-default"))
	ctx := classify.WithRegistry(context.Background(), reg)

	got, err := defaultClassifier(ctx, "text classifier",
		func(r *classify.Registry) (classify.TextClassifier, error) { return r.TextClassifier("") })

	require.NoError(t, err)
	assert.NotNil(t, got, "a check that names nothing falls back to what the HOST made default")
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
