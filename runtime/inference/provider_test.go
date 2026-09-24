package inference_test

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/stretchr/testify/assert"
)

func TestResponse_Score_CaseInsensitiveMatch(t *testing.T) {
	r := inference.Response{
		Scores: []inference.LabelScore{
			{Label: "on-topic", Score: 0.9},
			{Label: "off-topic", Score: 0.1},
		},
	}

	score, ok := r.Score("On-Topic")
	assert.True(t, ok)
	assert.InDelta(t, 0.9, score, 0.0001)
}

func TestResponse_Score_MissingLabel(t *testing.T) {
	r := inference.Response{
		Scores: []inference.LabelScore{
			{Label: "on-topic", Score: 0.9},
		},
	}

	score, ok := r.Score("nonexistent")
	assert.False(t, ok)
	assert.Equal(t, 0.0, score)
}
