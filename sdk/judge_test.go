package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

type stubJudge struct{}

//nolint:gocritic // JudgeOpts is passed by value by the JudgeProvider interface
func (stubJudge) Judge(_ context.Context, _ handlers.JudgeOpts) (*handlers.JudgeResult, error) {
	return &handlers.JudgeResult{Passed: true, Score: 1.0}, nil
}

func TestResolveJudge(t *testing.T) {
	// The "no judge at all" case is asserted inside "resolves by key" below,
	// where it is stated against a populated pool and followed by the judge
	// resolving — a bare nil check on an empty config passes for an
	// implementation that falls back to the agent provider too.
	t.Run("explicit WithJudgeProvider wins", func(t *testing.T) {
		explicit := stubJudge{}
		cfg := &config{}
		require.NoError(t, WithJudgeProvider(explicit)(cfg))

		ensureProviderPool(cfg)
		cfg.providers.Register(mock.NewProvider(JudgeProviderKey, "mock-model", false))

		assert.Equal(t, explicit, resolveJudge(cfg),
			"the explicit judge is the escape hatch for a pack that names its judge differently")
	})

	t.Run("falls back to the pooled judge provider", func(t *testing.T) {
		cfg := &config{}
		ensureProviderPool(cfg)
		cfg.providers.Register(mock.NewProvider(JudgeProviderKey, "mock-model", false))

		judge := resolveJudge(cfg)

		require.NotNil(t, judge, "a provider registered under %q should back the judge", JudgeProviderKey)
		assert.IsType(t, &handlers.ProviderJudge{}, judge)
	})

	// The judge is looked up by key, not by "whatever llm is around": a pool
	// full of other providers still resolves nothing, and the one named judge
	// resolves even when it is not the only entry.
	t.Run("resolves by key, not by availability", func(t *testing.T) {
		cfg := &config{}
		ensureProviderPool(cfg)
		cfg.providers.Register(mock.NewProvider("agent", "mock-model", false))
		cfg.providers.Register(mock.NewProvider("summarizer", "mock-model", false))
		require.Nil(t, resolveJudge(cfg), "another role's provider must not be borrowed as a judge")

		cfg.providers.Register(mock.NewProvider(JudgeProviderKey, "judge-model", false))
		judge := resolveJudge(cfg)

		require.NotNil(t, judge)
		pj, ok := judge.(*handlers.ProviderJudge)
		require.True(t, ok)
		assert.Equal(t, "judge-model", pj.Model(),
			"resolved some other provider in the pool rather than the one named %q", JudgeProviderKey)
	})
}
