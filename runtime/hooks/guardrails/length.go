package guardrails

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
)

// charsPerToken is the rough estimation ratio for token counting.
const charsPerToken = 4

// LengthHook denies provider responses that exceed character or token limits.
// It implements both ProviderHook and ChunkInterceptor for streaming support.
type LengthHook struct {
	maxCharacters int // 0 means no limit
	maxTokens     int // 0 means no limit
}

// Compile-time interface checks.
var (
	_ hooks.ProviderHook     = (*LengthHook)(nil)
	_ hooks.ChunkInterceptor = (*LengthHook)(nil)
)

// NewLengthHook creates a guardrail that rejects responses exceeding the
// given character and/or token limits. Pass 0 to disable a limit.
func NewLengthHook(maxCharacters, maxTokens int) *LengthHook {
	return &LengthHook{maxCharacters: maxCharacters, maxTokens: maxTokens}
}

// Name returns the guardrail type identifier.
func (h *LengthHook) Name() string { return nameLength }

// BeforeCall is a no-op — length is checked after generation.
func (h *LengthHook) BeforeCall(
	_ context.Context, _ *hooks.ProviderRequest,
) hooks.Decision {
	return hooks.Allow
}

// AfterCall checks the completed response against length limits.
func (h *LengthHook) AfterCall(
	_ context.Context, _ *hooks.ProviderRequest, resp *hooks.ProviderResponse,
) hooks.Decision {
	content := resp.Message.Content
	tokenCount := estimateTokens(content, 0)
	return h.check(content, tokenCount)
}

// OnChunk checks accumulated streaming content against length limits.
func (h *LengthHook) OnChunk(
	_ context.Context, chunk *providers.StreamChunk,
) hooks.Decision {
	return h.check(chunk.Content, estimateTokens(chunk.Content, chunk.TokenCount))
}

func (h *LengthHook) check(content string, tokenCount int) hooks.Decision {
	if h.maxCharacters > 0 && len(content) > h.maxCharacters {
		return hooks.DenyWithMetadata(
			fmt.Sprintf("exceeded max_characters limit (%d > %d)", len(content), h.maxCharacters),
			map[string]any{
				"validator_type":  nameLength,
				"character_count": len(content),
				"max_characters":  h.maxCharacters,
			},
		)
	}
	if h.maxTokens > 0 && tokenCount > h.maxTokens {
		return hooks.DenyWithMetadata(
			fmt.Sprintf("exceeded max_tokens limit (%d > %d)", tokenCount, h.maxTokens),
			map[string]any{
				"validator_type": nameLength,
				"token_count":    tokenCount,
				"max_tokens":     h.maxTokens,
			},
		)
	}
	return hooks.Allow
}

// estimateTokens returns the actual token count if available, otherwise
// falls back to a rough estimate of 1 token per 4 characters.
func estimateTokens(content string, actualCount int) int {
	if actualCount > 0 {
		return actualCount
	}
	return len(content) / charsPerToken
}
