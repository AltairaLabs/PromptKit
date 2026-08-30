package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseJudgeResponse_UnparseableIsAnError is the regression for #1882.
//
// An unparseable judge response was returned as Score 0.5, Passed true and a
// nil error, so the runner recorded it as a successful measurement. A judge
// that could not be read became one that scored exactly the pass threshold,
// and that number reached gauges and anything gating on score.
//
// The most likely trigger is a rubric: Score is typed float64, so a judge asked
// for per-dimension scores answers with an object and the unmarshal fails
// outright.
func TestParseJudgeResponse_UnparseableIsAnError(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{
			name: "rubric returns per-dimension scores, so score is an object",
			raw:  `{"score": {"clarity": 0.9, "accuracy": 0.7}, "reasoning": "mixed"}`,
		},
		{
			name: "prose with no JSON at all",
			raw:  "I think the response was pretty good, honestly.",
		},
		{
			name: "truncated JSON",
			raw:  `{"score": 0.8, "reason`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := parseJudgeResponse(tc.raw)

			require.Error(t, err,
				"an unreadable judge response must be an error, not a measurement")
			assert.Nil(t, result,
				"no JudgeResult may be invented for a response that could not be read")
		})
	}
}

// TestParseJudgeResponse_ValidResponsesStillParse guards the other direction:
// the shapes a judge legitimately returns must keep working.
func TestParseJudgeResponse_ValidResponsesStillParse(t *testing.T) {
	t.Run("score only", func(t *testing.T) {
		r, err := parseJudgeResponse(`{"score": 0.9, "reasoning": "good"}`)
		require.NoError(t, err)
		assert.InDelta(t, 0.9, r.Score, 1e-9)
		assert.Equal(t, "good", r.Reasoning)
	})

	t.Run("explicit passed wins over the threshold", func(t *testing.T) {
		r, err := parseJudgeResponse(`{"score": 0.1, "passed": true, "reasoning": "ok"}`)
		require.NoError(t, err)
		assert.True(t, r.Passed, "the model's own verdict is honored when present")
	})

	t.Run("markdown-wrapped JSON is still extracted", func(t *testing.T) {
		r, err := parseJudgeResponse("Here you go:\n```json\n{\"score\": 0.75}\n```")
		require.NoError(t, err)
		assert.InDelta(t, 0.75, r.Score, 1e-9)
	})
}
