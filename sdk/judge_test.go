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
	t.Run("nil when the host supplied nothing", func(t *testing.T) {
		assert.Nil(t, resolveJudge(&config{}),
			"inventing a judge would make the agent grade its own output and bill for it")
	})

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

	t.Run("a pool without a judge key resolves nothing", func(t *testing.T) {
		cfg := &config{}
		ensureProviderPool(cfg)
		cfg.providers.Register(mock.NewProvider("agent", "mock-model", false))

		assert.Nil(t, resolveJudge(cfg),
			"the agent provider must not be borrowed as a judge")
	})
}
