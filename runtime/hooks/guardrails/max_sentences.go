package guardrails

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
)

// sentenceSplitter splits text on sentence-ending punctuation.
var sentenceSplitter = regexp.MustCompile(`[.!?]+`)

// MaxSentencesHook denies provider responses that contain more than a
// configured number of sentences. It does not support streaming because
// sentence boundaries can only be reliably counted on complete content.
type MaxSentencesHook struct {
	maxSentences int
}

// Compile-time interface check.
var _ hooks.ProviderHook = (*MaxSentencesHook)(nil)

// NewMaxSentencesHook creates a guardrail that rejects responses exceeding
// the given sentence count.
func NewMaxSentencesHook(maxSentences int) *MaxSentencesHook {
	return &MaxSentencesHook{maxSentences: maxSentences}
}

// Name returns the guardrail type identifier.
func (h *MaxSentencesHook) Name() string { return nameMaxSentences }

// BeforeCall is a no-op — sentence count is checked after generation.
func (h *MaxSentencesHook) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.Allow
}

// AfterCall checks the completed response for sentence count.
func (h *MaxSentencesHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, resp *hooks.ProviderResponse,
) hooks.Decision {
	count := countSentences(resp.Message.Content)
	if count > h.maxSentences {
		return hooks.DenyWithMetadata(
			fmt.Sprintf("exceeded max_sentences limit (%d > %d)", count, h.maxSentences),
			map[string]any{
				"validator_type": nameMaxSentences,
				"count":          count,
				"max":            h.maxSentences,
			},
		)
	}
	return hooks.Allow
}

// countSentences counts sentences by splitting on [.!?]+ punctuation.
func countSentences(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	parts := sentenceSplitter.Split(text, -1)
	count := 0
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			count++
		}
	}
	if count == 0 && text != "" {
		count = 1
	}
	return count
}
