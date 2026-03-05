// Package tokenizer provides token counting functionality for LLM context management.
//
// Token counting is essential for managing context windows and ensuring prompts
// fit within model limits. This package provides:
//   - TokenCounter interface for pluggable implementations
//   - HeuristicTokenCounter with model-aware word-to-token ratios
//   - MessageTokenCounter for counting tokens across multimodal messages
//   - Support for different model families (GPT, Claude, Gemini, etc.)
//   - Content-aware ratio adjustment (code, CJK text, mixed content)
//
// The heuristic approach is suitable for context truncation decisions where
// approximate counts are sufficient. For exact token counts (billing, etc.),
// use provider-specific CostInfo from API responses.
package tokenizer

import (
	"strings"
	"sync"
	"unicode"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TokenCounter provides token counting functionality.
// Implementations may use heuristics or actual tokenization.
type TokenCounter interface {
	// CountTokens returns the estimated or actual token count for the given text.
	CountTokens(text string) int

	// CountMultiple returns the total token count for multiple text segments.
	CountMultiple(texts []string) int
}

// ModelFamily represents a family of LLM models with similar tokenization.
type ModelFamily string

const (
	// ModelFamilyGPT covers OpenAI GPT models (GPT-3.5, GPT-4, etc.)
	// Uses cl100k_base tokenizer - approximately 1.3 tokens per word for English.
	ModelFamilyGPT ModelFamily = "gpt"

	// ModelFamilyClaude covers Anthropic Claude models.
	// Similar to GPT tokenization - approximately 1.3 tokens per word.
	ModelFamilyClaude ModelFamily = "claude"

	// ModelFamilyGemini covers Google Gemini models.
	// Uses SentencePiece tokenizer - approximately 1.4 tokens per word.
	ModelFamilyGemini ModelFamily = "gemini"

	// ModelFamilyLlama covers Meta Llama models.
	// Uses SentencePiece tokenizer - approximately 1.4 tokens per word.
	ModelFamilyLlama ModelFamily = "llama"

	// ModelFamilyDefault is used when the model family is unknown.
	// Uses a conservative estimate of 1.35 tokens per word.
	ModelFamilyDefault ModelFamily = "default"
)

// tokenRatios maps model families to their approximate tokens-per-word ratios.
// These ratios are derived from empirical testing on English text.
// Non-English text and code may have different ratios.
//
//nolint:mnd // These are well-documented empirical token ratios, not arbitrary magic numbers
var tokenRatios = map[ModelFamily]float64{
	ModelFamilyGPT:     1.30, // cl100k_base tokenizer
	ModelFamilyClaude:  1.30, // Similar to GPT
	ModelFamilyGemini:  1.40, // SentencePiece tokenizer
	ModelFamilyLlama:   1.40, // SentencePiece tokenizer
	ModelFamilyDefault: 1.35, // Conservative middle ground
}

// HeuristicTokenCounter estimates token counts using word-based heuristics.
// This is fast and suitable for context management decisions where exact
// counts are not required. For accurate counts, use a tokenizer library
// like tiktoken-go.
type HeuristicTokenCounter struct {
	ratio float64
	mu    sync.RWMutex
}

// NewHeuristicTokenCounter creates a token counter for the specified model family.
func NewHeuristicTokenCounter(family ModelFamily) *HeuristicTokenCounter {
	ratio, ok := tokenRatios[family]
	if !ok {
		ratio = tokenRatios[ModelFamilyDefault]
	}
	return &HeuristicTokenCounter{ratio: ratio}
}

// NewHeuristicTokenCounterWithRatio creates a token counter with a custom ratio.
// Use this when you have measured the actual token ratio for your specific use case.
func NewHeuristicTokenCounterWithRatio(ratio float64) *HeuristicTokenCounter {
	if ratio <= 0 {
		ratio = tokenRatios[ModelFamilyDefault]
	}
	return &HeuristicTokenCounter{ratio: ratio}
}

// CountTokens estimates token count for the given text.
// Returns 0 for empty text.
func (h *HeuristicTokenCounter) CountTokens(text string) int {
	if text == "" {
		return 0
	}
	h.mu.RLock()
	ratio := h.ratio
	h.mu.RUnlock()

	words := strings.Fields(text)
	return int(float64(len(words)) * ratio)
}

// CountMultiple returns the total token count for multiple text segments.
func (h *HeuristicTokenCounter) CountMultiple(texts []string) int {
	total := 0
	for _, text := range texts {
		total += h.CountTokens(text)
	}
	return total
}

// SetRatio updates the token ratio. Thread-safe.
func (h *HeuristicTokenCounter) SetRatio(ratio float64) {
	if ratio <= 0 {
		return
	}
	h.mu.Lock()
	h.ratio = ratio
	h.mu.Unlock()
}

// Ratio returns the current token ratio. Thread-safe.
func (h *HeuristicTokenCounter) Ratio() float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.ratio
}

// DefaultTokenCounter is a package-level counter using the default model family.
// Use this when you don't need model-specific tokenization.
var DefaultTokenCounter = NewHeuristicTokenCounter(ModelFamilyDefault)

// CountTokens is a convenience function using the default token counter.
func CountTokens(text string) int {
	return DefaultTokenCounter.CountTokens(text)
}

// GetModelFamily returns the appropriate ModelFamily for a model name.
// This performs prefix matching to categorize models.
func GetModelFamily(modelName string) ModelFamily {
	modelLower := strings.ToLower(modelName)

	switch {
	case strings.HasPrefix(modelLower, "gpt-") ||
		strings.HasPrefix(modelLower, "text-davinci") ||
		strings.HasPrefix(modelLower, "text-embedding"):
		return ModelFamilyGPT

	case strings.HasPrefix(modelLower, "claude"):
		return ModelFamilyClaude

	case strings.HasPrefix(modelLower, "gemini") ||
		strings.HasPrefix(modelLower, "models/gemini"):
		return ModelFamilyGemini

	case strings.HasPrefix(modelLower, "llama") ||
		strings.HasPrefix(modelLower, "meta-llama"):
		return ModelFamilyLlama

	default:
		return ModelFamilyDefault
	}
}

// NewTokenCounterForModel creates a token counter appropriate for the given model.
func NewTokenCounterForModel(modelName string) TokenCounter {
	return NewHeuristicTokenCounter(GetModelFamily(modelName))
}

// Content type ratio adjustments for more accurate token counting.
// These are multiplied with the base model ratio.
const (
	// codeRatioMultiplier adjusts for code-heavy content.
	// Code typically produces more tokens due to operators, punctuation,
	// and short identifiers that each consume a token.
	codeRatioMultiplier = 1.55

	// cjkRatioMultiplier adjusts for CJK (Chinese, Japanese, Korean) text.
	// CJK characters are typically split into multiple tokens by
	// byte-pair encoding tokenizers.
	cjkRatioMultiplier = 1.15

	// cjkCharThreshold is the fraction of CJK characters needed to apply the
	// CJK adjustment.
	cjkCharThreshold = 0.3

	// codeCharThreshold is the fraction of code-like characters needed to
	// apply the code adjustment.
	codeCharThreshold = 0.15

	// imageTokensLowDetail is the estimated token count for a low-detail image.
	// Based on provider documentation: ~85 tokens for thumbnail/low-res.
	imageTokensLowDetail = 85

	// imageTokensHighDetail is the estimated token count for a high-detail image.
	// Based on provider documentation: ~170K tokens for full-resolution.
	imageTokensHighDetail = 170000

	// imageTokensAutoDetail is the estimated token count for auto-detail images.
	// Uses a middle-ground estimate since actual detail is chosen by the provider.
	imageTokensAutoDetail = 1024

	// perMessageOverhead accounts for per-message formatting tokens added by
	// providers (role markers, separators, etc.).
	perMessageOverhead = 4
)

// DetectContentType analyzes text content and returns an adjusted token ratio
// multiplier based on content characteristics (code, CJK text, etc.).
func DetectContentType(text string) float64 {
	if text == "" {
		return 1.0
	}

	var cjkCount, codeCount, totalCount int
	for _, r := range text {
		totalCount++
		if isCJK(r) {
			cjkCount++
		}
		if isCodeChar(r) {
			codeCount++
		}
	}

	if totalCount == 0 {
		return 1.0
	}

	cjkFraction := float64(cjkCount) / float64(totalCount)
	codeFraction := float64(codeCount) / float64(totalCount)

	// Apply the highest applicable multiplier
	if codeFraction > codeCharThreshold {
		return codeRatioMultiplier
	}
	if cjkFraction > cjkCharThreshold {
		return cjkRatioMultiplier
	}

	return 1.0
}

// isCJK returns true if the rune is a CJK Unified Ideograph or common
// Japanese/Korean script character.
func isCJK(r rune) bool {
	return unicode.In(r,
		unicode.Han,      // CJK Unified Ideographs
		unicode.Hiragana, // Japanese
		unicode.Katakana, // Japanese
		unicode.Hangul,   // Korean
	)
}

// isCodeChar returns true if the rune is common in source code but uncommon
// in natural language prose.
func isCodeChar(r rune) bool {
	switch r {
	case '{', '}', '(', ')', '[', ']', ';', '=', '<', '>', '|',
		'&', '!', '~', '^', '%', '#', '@', '\\', '`':
		return true
	default:
		return false
	}
}

// CountTokensContentAware estimates token count with content-type awareness.
// It adjusts the base ratio based on whether the text appears to be code,
// CJK text, or regular prose.
func (h *HeuristicTokenCounter) CountTokensContentAware(text string) int {
	if text == "" {
		return 0
	}
	h.mu.RLock()
	ratio := h.ratio
	h.mu.RUnlock()

	multiplier := DetectContentType(text)
	words := strings.Fields(text)
	return int(float64(len(words)) * ratio * multiplier)
}

// CountMessageTokens estimates the total token count for a slice of messages.
// It handles multimodal content by estimating image tokens based on detail
// level and counting text tokens with content-aware heuristics.
func (h *HeuristicTokenCounter) CountMessageTokens(messages []types.Message) int {
	total := 0
	for i := range messages {
		total += h.countSingleMessage(&messages[i])
	}
	return total
}

// countSingleMessage estimates the token count for a single message.
func (h *HeuristicTokenCounter) countSingleMessage(msg *types.Message) int {
	tokens := perMessageOverhead // role markers, separators

	// Handle multimodal parts
	if len(msg.Parts) > 0 {
		for i := range msg.Parts {
			tokens += h.countContentPart(&msg.Parts[i])
		}
		return tokens
	}

	// Text-only message
	content := msg.GetContent()
	if content != "" {
		tokens += h.CountTokensContentAware(content)
	}

	// Tool call arguments contribute tokens
	for _, tc := range msg.ToolCalls {
		tokens += h.CountTokensContentAware(tc.Name)
		if len(tc.Args) > 0 {
			tokens += h.CountTokensContentAware(string(tc.Args))
		}
	}

	return tokens
}

// countContentPart estimates token count for a single content part.
func (h *HeuristicTokenCounter) countContentPart(part *types.ContentPart) int {
	switch part.Type {
	case types.ContentTypeText:
		if part.Text != nil {
			return h.CountTokensContentAware(*part.Text)
		}
		return 0

	case types.ContentTypeImage:
		return estimateImageTokens(part.Media)

	default:
		// Audio, video, document — use a conservative estimate
		// based on any caption text present.
		if part.Media != nil && part.Media.Caption != nil {
			return h.CountTokensContentAware(*part.Media.Caption)
		}
		return 0
	}
}

// estimateImageTokens returns an estimated token count for an image based
// on its detail level, following provider documentation guidelines.
func estimateImageTokens(media *types.MediaContent) int {
	if media == nil {
		return imageTokensAutoDetail
	}

	detail := "auto"
	if media.Detail != nil {
		detail = *media.Detail
	}

	switch detail {
	case "low":
		return imageTokensLowDetail
	case "high":
		return imageTokensHighDetail
	default:
		return imageTokensAutoDetail
	}
}

// CountMessageTokensDefault is a convenience function that counts message
// tokens using the default token counter.
func CountMessageTokensDefault(messages []types.Message) int {
	return DefaultTokenCounter.CountMessageTokens(messages)
}
