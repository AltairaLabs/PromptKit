package base_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers/base"
	"github.com/stretchr/testify/assert"
)

func TestTokenUsage_Quantities_OnlyNonzero(t *testing.T) {
	u := base.TokenUsage{Input: 100, Output: 50, Reasoning: 20}
	q := u.Quantities()
	assert.Equal(t, map[string]float64{
		base.UnitInputToken:     100,
		base.UnitOutputToken:    50,
		base.UnitReasoningToken: 20,
	}, q)
}

func TestTokenUsage_Quantities_IncludesAllUnitsAndExtra(t *testing.T) {
	u := base.TokenUsage{
		Input: 1, CacheRead: 2, CacheWrite: 3, Output: 4, Reasoning: 5,
		AudioInput: 6, AudioOutput: 7,
		Extra: map[string]float64{"custom_token": 8},
	}
	q := u.Quantities()
	assert.Equal(t, 1.0, q[base.UnitInputToken])
	assert.Equal(t, 2.0, q[base.UnitCacheReadToken])
	assert.Equal(t, 3.0, q[base.UnitCacheWriteToken])
	assert.Equal(t, 4.0, q[base.UnitOutputToken])
	assert.Equal(t, 5.0, q[base.UnitReasoningToken])
	assert.Equal(t, 6.0, q[base.UnitAudioInputToken])
	assert.Equal(t, 7.0, q[base.UnitAudioOutputToken])
	assert.Equal(t, 8.0, q["custom_token"])
}
