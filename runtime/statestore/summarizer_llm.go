package statestore

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	summarizerMaxTokens   = 500
	summarizerTemperature = 0.3
)

// LLMSummarizer uses an LLM provider to compress messages into summaries.
type LLMSummarizer struct {
	provider providers.Provider
}

// NewLLMSummarizer creates a new LLM-based summarizer.
// A cheaper/faster model is recommended (e.g., GPT-3.5, Claude Haiku).
func NewLLMSummarizer(provider providers.Provider) *LLMSummarizer {
	return &LLMSummarizer{provider: provider}
}

// Summarize compresses the given messages into a concise summary.
func (s *LLMSummarizer) Summarize(ctx context.Context, messages []types.Message) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}

	// Format messages into a prompt
	var sb strings.Builder
	for i := range messages {
		fmt.Fprintf(&sb, "[%s]: %s\n", messages[i].Role, messages[i].Content)
	}

	req := providers.PredictionRequest{
		System: "You are a conversation summarizer. Summarize the following conversation segment " +
			"concisely, preserving key facts, decisions, and context that would be important " +
			"for continuing the conversation. Focus on information that would be lost if these " +
			"messages were removed. Be factual and brief.",
		Messages: []types.Message{
			{
				Role:    "user",
				Content: fmt.Sprintf("Summarize this conversation segment:\n\n%s", sb.String()),
			},
		},
		MaxTokens:   summarizerMaxTokens,
		Temperature: summarizerTemperature,
	}

	resp, err := s.provider.Predict(ctx, req)
	if err != nil {
		return "", fmt.Errorf("summarizer: prediction failed: %w", err)
	}

	return resp.Content, nil
}
