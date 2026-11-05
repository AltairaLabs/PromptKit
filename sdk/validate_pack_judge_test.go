package sdk_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// Regression coverage for https://github.com/AltairaLabs/PromptKit/issues/2356.
//
// A judge-backed validator that names no provider relies on the host's default
// judge (sdk.WithJudgeProvider). Open() builds it; ValidatePack had no way to
// be told about that judge and reported it as an error.

type stubJudge struct{}

//nolint:gocritic // JudgeOpts is passed by value by the JudgeProvider interface
func (stubJudge) Judge(_ context.Context, _ handlers.JudgeOpts) (*handlers.JudgeResult, error) {
	return &handlers.JudgeResult{Passed: true, Score: 1.0}, nil
}

func judgeBackedPack(params map[string]any) map[string]any {
	p := baseValidPack()
	def := p["prompts"].(map[string]any)["default"].(map[string]any)
	def["validators"] = []any{
		map[string]any{"type": "toxicity", "enabled": true, "params": params},
	}
	return p
}

func TestValidatePackWithOptions_HostDefaultJudgeAccepted(t *testing.T) {
	path := writeValidatePackFixture(t, judgeBackedPack(map[string]any{}))

	t.Run("no judge: reported, as before", func(t *testing.T) {
		issues, err := sdk.ValidatePack(path, false)
		require.NoError(t, err)
		require.Len(t, issues, 1)
		assert.Equal(t, "validator", issues[0].Kind)
		assert.Contains(t, issues[0].Reason, "judge")
	})

	t.Run("host default judge: nothing to report", func(t *testing.T) {
		issues, err := sdk.ValidatePackWithOptions(path, false, sdk.WithJudgeProvider(stubJudge{}))
		require.NoError(t, err)
		assert.Empty(t, issues)
	})

	t.Run("pack naming a provider needs no judge", func(t *testing.T) {
		named := writeValidatePackFixture(t, judgeBackedPack(map[string]any{"provider": "grader"}))
		issues, err := sdk.ValidatePack(named, false)
		require.NoError(t, err)
		assert.Empty(t, issues)
	})
}

func TestValidatePackWithOptions_RegistryOptionHonored(t *testing.T) {
	path := writeValidatePackFixture(t, customTypePack(customGuardrailType, "blocked"))

	issues, err := sdk.ValidatePackWithOptions(path, false, sdk.WithEvalRegistry(customEvalRegistry()))
	require.NoError(t, err)
	assert.Empty(t, issues)
}
