package providers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A provider with no top-k parameter warns once that it drops one, however
// many requests carry it, and says nothing when none does.
func TestWarnTopKDropped_OncePerProvider(t *testing.T) {
	buf := captureExtraBodyLogs(t)
	topK := 40

	WarnTopKDropped("topk-silent", &PredictionRequest{})
	assert.Empty(t, buf.String(), "a request without top_k drops nothing")

	for range 3 {
		WarnTopKDropped("topk-once", &PredictionRequest{TopK: &topK})
	}
	WarnTopKDropped("topk-other", &PredictionRequest{TopK: &topK})

	out := buf.String()
	assert.Equal(t, 1, strings.Count(out, "provider=topk-once"), out)
	assert.Equal(t, 1, strings.Count(out, "provider=topk-other"), out)
	assert.Contains(t, out, "top_k=40")
}
