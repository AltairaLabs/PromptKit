package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// SpecJudgeProvider implements JudgeProvider by creating a provider
// from a ProviderSpec. This is the standard implementation used by
// Arena and any caller that has judge targets as ProviderSpecs.
type SpecJudgeProvider struct {
	spec *providers.ProviderSpec
}

// NewSpecJudgeProvider creates a JudgeProvider from a provider spec.
func NewSpecJudgeProvider(spec *providers.ProviderSpec) *SpecJudgeProvider {
	return &SpecJudgeProvider{spec: spec}
}

// Judge creates a provider from the spec, sends the evaluation prompt,
// and parses the verdict.
//
//nolint:gocritic // JudgeOpts passed by value intentionally for simplicity
func (sp *SpecJudgeProvider) Judge(ctx context.Context, opts JudgeOpts) (*JudgeResult, error) {
	provider, err := providers.CreateProviderFromSpec(*sp.spec)
	if err != nil {
		return nil, fmt.Errorf("create judge provider: %w", err)
	}
	defer provider.Close()

	systemPrompt := opts.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = defaultJudgeSystemPrompt
	}

	userContent := fmt.Sprintf(
		"Content to evaluate:\n%s\n\nCriteria: %s",
		opts.Content, opts.Criteria,
	)
	if opts.Rubric != "" {
		userContent += fmt.Sprintf("\n\nRubric: %s", opts.Rubric)
	}

	userMsg := types.Message{Role: "user"}
	userMsg.AddTextPart(userContent)

	resp, err := provider.Predict(ctx, providers.PredictionRequest{
		System:      systemPrompt,
		Messages:    []types.Message{userMsg},
		Temperature: 0.0,
		MaxTokens:   judgeMaxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("judge predict failed: %w", err)
	}

	return parseJudgeResponse(resp.Content, opts.MinScore)
}

// Ensure SpecJudgeProvider implements JudgeProvider.
var _ JudgeProvider = (*SpecJudgeProvider)(nil)
