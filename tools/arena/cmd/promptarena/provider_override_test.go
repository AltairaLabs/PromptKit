package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/pkg/config"
)

func TestExtractOverrideFlags_CollectsRepeatedPairs(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().StringArray("override-provider", nil, "")
	require.NoError(t, cmd.Flags().Set("override-provider", "mock-judge=claude"))
	require.NoError(t, cmd.Flags().Set("override-provider", "mock-user=claude"))

	params := &RunParameters{}
	require.NoError(t, extractOverrideFlags(cmd, params))

	assert.Equal(t, []string{"mock-judge=claude", "mock-user=claude"}, params.ProviderOverrides)
}

func TestApplyProviderOverrides_CopiesTargetSpecKeepingFromID(t *testing.T) {
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{
			"mock-judge": {ID: "mock-judge", Type: "mock", Model: "judge-model"},
			"claude":     {ID: "claude", Type: "anthropic", Model: "claude-haiku"},
		},
	}

	err := applyProviderOverrides(cfg, []string{"mock-judge=claude"})
	require.NoError(t, err)

	got := cfg.LoadedProviders["mock-judge"]
	require.NotNil(t, got)
	assert.Equal(t, "mock-judge", got.ID, "keeps the original key/id")
	assert.Equal(t, "anthropic", got.Type, "takes target type")
	assert.Equal(t, "claude-haiku", got.Model, "takes target model")
}

func TestApplyProviderOverrides_UnknownTargetErrors(t *testing.T) {
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{
			"mock-judge": {ID: "mock-judge", Type: "mock", Model: "judge-model"},
		},
	}

	err := applyProviderOverrides(cfg, []string{"mock-judge=nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nope")
}

func TestApplyProviderOverrides_UnknownSourceErrors(t *testing.T) {
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{
			"claude": {ID: "claude", Type: "anthropic", Model: "claude-haiku"},
		},
	}

	err := applyProviderOverrides(cfg, []string{"nope=claude"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nope")
}

func TestApplyProviderOverrides_MalformedPairErrors(t *testing.T) {
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{
			"claude": {ID: "claude", Type: "anthropic", Model: "claude-haiku"},
		},
	}

	for _, bad := range []string{"no-equals", "=claude", "claude="} {
		err := applyProviderOverrides(cfg, []string{bad})
		require.Errorf(t, err, "expected error for %q", bad)
	}
}

func TestApplyProviderOverrides_MultiplePairs(t *testing.T) {
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{
			"mock-judge": {ID: "mock-judge", Type: "mock", Model: "judge-model"},
			"mock-user":  {ID: "mock-user", Type: "mock", Model: "user-model"},
			"claude":     {ID: "claude", Type: "anthropic", Model: "claude-haiku"},
		},
	}

	err := applyProviderOverrides(cfg, []string{"mock-judge=claude", "mock-user=claude"})
	require.NoError(t, err)

	assert.Equal(t, "anthropic", cfg.LoadedProviders["mock-judge"].Type)
	assert.Equal(t, "anthropic", cfg.LoadedProviders["mock-user"].Type)
	assert.Equal(t, "mock-user", cfg.LoadedProviders["mock-user"].ID, "keeps its own id")
}

func TestApplyProviderOverrides_NoPairsIsNoOp(t *testing.T) {
	orig := &config.Provider{ID: "claude", Type: "anthropic", Model: "claude-haiku"}
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{"claude": orig},
	}

	require.NoError(t, applyProviderOverrides(cfg, nil))

	assert.Same(t, orig, cfg.LoadedProviders["claude"], "untouched when no overrides")
	assert.Equal(t, "anthropic", cfg.LoadedProviders["claude"].Type)
}

// The override must mutate the provider in place so consumers that already hold
// a pointer to it — notably resolved judge targets (JudgeTarget.Provider, since
// #1264) — transparently see the new spec without re-resolution.
func TestApplyProviderOverrides_ReachesResolvedJudgeTarget(t *testing.T) {
	judgeProvider := &config.Provider{ID: "mock-judge", Type: "mock", Model: "judge-model"}
	cfg := &config.Config{
		LoadedProviders: map[string]*config.Provider{
			"mock-judge": judgeProvider,
			"claude":     {ID: "claude", Type: "anthropic", Model: "claude-haiku"},
		},
		// Mirrors post-LoadConfig state: the judge target shares the provider pointer.
		LoadedJudges: map[string]*config.JudgeTarget{
			"quality": {Name: "quality", Provider: judgeProvider},
		},
	}

	require.NoError(t, applyProviderOverrides(cfg, []string{"mock-judge=claude"}))

	jt := cfg.LoadedJudges["quality"]
	require.NotNil(t, jt.Provider)
	assert.Equal(t, "anthropic", jt.Provider.Type, "judge target sees the substituted provider")
	assert.Equal(t, "claude-haiku", jt.Provider.Model)
	assert.Equal(t, "mock-judge", jt.Provider.ID, "judge still resolves under its original id")
}
