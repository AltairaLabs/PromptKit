// Package tokenizer provides token counting functionality for LLM context management.
//
// Token counting is essential for managing context windows and ensuring prompts
// fit within model limits. This package provides:
//   - TokenCounter interface for pluggable implementations
//   - HeuristicTokenCounter with model-aware word-to-token ratios
//   - Support for different model families (GPT, Claude, Gemini, etc.)
//
// The heuristic approach is suitable for context truncation decisions where
// approximate counts are sufficient. For exact token counts (billing, etc.),
// use provider-specific CostInfo from API responses.
package tokenizer

import (
	"strings"
	"sync"
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
