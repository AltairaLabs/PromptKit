package evals

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func scorePtr(f float64) *float64 { return &f }

func TestScoreThresholds_Triggered(t *testing.T) {
	tests := []struct {
		name       string
		thresholds ScoreThresholds
		result     *EvalResult
		want       bool
		why        string
	}{
		{
			name:       "no thresholds requires a perfect score",
			thresholds: ScoreThresholds{},
			result:     &EvalResult{Score: scorePtr(0.99)},
			want:       true,
			why:        "the pre-existing default every declared guardrail relies on",
		},
		{
			name:       "no thresholds allows a perfect score",
			thresholds: ScoreThresholds{},
			result:     &EvalResult{Score: scorePtr(1.0)},
			want:       false,
		},
		{
			name:       "min honored above threshold",
			thresholds: ScoreThresholds{Min: scorePtr(0.5)},
			result:     &EvalResult{Score: scorePtr(0.8)},
			want:       false,
			why:        "0.8 clears 0.5 even though it is short of a perfect 1.0",
		},
		{
			name:       "min honored at exactly the threshold",
			thresholds: ScoreThresholds{Min: scorePtr(0.5)},
			result:     &EvalResult{Score: scorePtr(0.5)},
			want:       false,
			why:        "the bound is inclusive, matching AssertionEvalHandler",
		},
		{
			name:       "min honored below threshold",
			thresholds: ScoreThresholds{Min: scorePtr(0.5)},
			result:     &EvalResult{Score: scorePtr(0.49)},
			want:       true,
		},
		{
			name:       "max honored above threshold",
			thresholds: ScoreThresholds{Max: scorePtr(0.3)},
			result:     &EvalResult{Score: scorePtr(1.0)},
			want:       true,
			why:        "a high score is the failure for handlers scoring how much bad content is present",
		},
		{
			name:       "max alone does not impose the default minimum",
			thresholds: ScoreThresholds{Max: scorePtr(0.3)},
			result:     &EvalResult{Score: scorePtr(0.1)},
			want:       false,
			why:        "setting only max must not silently also require 1.0",
		},
		{
			name:       "both bounds, inside the band",
			thresholds: ScoreThresholds{Min: scorePtr(0.4), Max: scorePtr(0.6)},
			result:     &EvalResult{Score: scorePtr(0.5)},
			want:       false,
		},
		{
			name:       "both bounds, above the band",
			thresholds: ScoreThresholds{Min: scorePtr(0.4), Max: scorePtr(0.6)},
			result:     &EvalResult{Score: scorePtr(0.7)},
			want:       true,
		},
		{
			name:       "nil score fails closed",
			thresholds: ScoreThresholds{Min: scorePtr(0.5)},
			result:     &EvalResult{},
			want:       true,
			why:        "a handler that could not judge has not cleared anything",
		},
		{
			name:       "nil result fails closed",
			thresholds: ScoreThresholds{},
			result:     nil,
			want:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.thresholds.Triggered(tc.result), tc.why)
		})
	}
}

func TestExtractScoreThresholds(t *testing.T) {
	t.Run("absent leaves both nil, so the 1.0 default applies", func(t *testing.T) {
		got := ExtractScoreThresholds(map[string]any{"words": []any{"x"}})

		require.Nil(t, got.Min)
		require.Nil(t, got.Max)
		// Asserted through behavior as well: absent thresholds must leave the
		// perfect-score default in force. Checking only for nil would pass if
		// extraction started inventing a bound.
		assert.True(t, got.Triggered(&EvalResult{Score: scorePtr(0.99)}),
			"with no thresholds extracted, anything short of 1.0 must still trigger")
		assert.False(t, got.Triggered(&EvalResult{Score: scorePtr(1.0)}))
	})

	t.Run("reads floats and ints", func(t *testing.T) {
		// Ints matter: a pack author writing min_score: 1 in JSON yields an int
		// through some decoders, and silently ignoring it would leave the
		// guardrail on the default.
		got := ExtractScoreThresholds(map[string]any{"min_score": 1, "max_score": 0.75})
		require.NotNil(t, got.Min)
		require.NotNil(t, got.Max)
		assert.Equal(t, 1.0, *got.Min)
		assert.Equal(t, 0.75, *got.Max)
	})

	t.Run("nil params yields the default, not a panic", func(t *testing.T) {
		got := ExtractScoreThresholds(nil)

		require.Nil(t, got.Min)
		require.Nil(t, got.Max)
		assert.True(t, got.Triggered(&EvalResult{Score: scorePtr(0.5)}),
			"a guardrail declared with no params at all must still enforce")
	})
}

func TestStripScoreThresholds(t *testing.T) {
	t.Run("removes thresholds and keeps everything else", func(t *testing.T) {
		in := map[string]any{"min_score": 0.5, "max_score": 1.0, "words": []any{"keepme"}}
		got := StripScoreThresholds(in)

		assert.NotContains(t, got, "min_score")
		assert.NotContains(t, got, "max_score")
		assert.Contains(t, got, "words")
	})

	t.Run("does not mutate the input", func(t *testing.T) {
		// The adapter holds one params map for the life of the conversation and
		// calls this on every turn; mutating it would strip the thresholds
		// permanently and silently revert to the default after turn one.
		in := map[string]any{"min_score": 0.5, "words": []any{"keepme"}}
		_ = StripScoreThresholds(in)

		assert.Contains(t, in, "min_score", "the caller's map must be left intact")
	})

	t.Run("returns the same map when there is nothing to strip", func(t *testing.T) {
		in := map[string]any{"words": []any{"keepme"}}
		assert.Equal(t, in, StripScoreThresholds(in))
	})

	t.Run("nil params passes through as nil, not an empty map", func(t *testing.T) {
		got := StripScoreThresholds(nil)

		require.Nil(t, got,
			"handlers distinguish a nil param map from an empty one, so "+
				"stripping must not materialize one")
		// And the result stays usable: a handler receiving it reads no
		// thresholds, which is what a params-less guardrail means.
		assert.Equal(t, ScoreThresholds{}, ExtractScoreThresholds(got))
	})
}
