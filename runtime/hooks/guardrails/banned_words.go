package guardrails

import (
	"context"
	"regexp"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// BannedWordsHook denies provider responses that contain any of the configured
// banned words. It implements both ProviderHook and ChunkInterceptor for
// streaming support.
type BannedWordsHook struct {
	words    []string
	patterns []*regexp.Regexp
}

// Compile-time interface checks.
var (
	_ hooks.ProviderHook     = (*BannedWordsHook)(nil)
	_ hooks.ChunkInterceptor = (*BannedWordsHook)(nil)
)

// NewBannedWordsHook creates a guardrail that rejects responses containing
// any of the given words. Matching is case-insensitive with word boundaries.
func NewBannedWordsHook(words []string) *BannedWordsHook {
	patterns := make([]*regexp.Regexp, len(words))
	for i, w := range words {
		patterns[i] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(w) + `\b`)
	}
	return &BannedWordsHook{words: words, patterns: patterns}
}

// Name returns the guardrail type identifier.
func (h *BannedWordsHook) Name() string { return nameBannedWords }

// BeforeCall is a no-op — banned words are checked after generation.
func (h *BannedWordsHook) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.Allow
}

// AfterCall checks the completed response for banned words.
func (h *BannedWordsHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, resp *hooks.ProviderResponse,
) hooks.Decision {
	return h.check(resp.Message.Content)
}

// OnChunk checks accumulated streaming content for banned words.
func (h *BannedWordsHook) OnChunk(
	_ context.Context, chunk *providers.StreamChunk,
) hooks.Decision {
	return h.check(chunk.Content)
}

func (h *BannedWordsHook) check(content string) hooks.Decision {
	for i, pattern := range h.patterns {
		if pattern.MatchString(content) {
			return hooks.DenyWithMetadata(
				"banned word detected: "+h.words[i],
				map[string]any{
					"validator_type": nameBannedWords,
					"violation":      h.words[i],
				},
			)
		}
	}
	return hooks.Allow
}
