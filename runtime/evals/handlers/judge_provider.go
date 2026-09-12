// Package handlers provides eval type handler implementations.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

const (
	// defaultJudgeSystemPrompt is the system prompt used when no custom prompt is provided.
	defaultJudgeSystemPrompt = "You are an evaluation judge. Evaluate the following content " +
		"and respond with a JSON object containing: " +
		"\"passed\" (boolean), \"score\" (float 0-1), and \"reasoning\" (string)."

	// judgeMaxTokens is the maximum token limit for judge LLM calls.
	judgeMaxTokens = 1024

	// defaultPassThreshold is the score threshold used as the fallback
	// when the judge model didn't include an explicit `passed` field in
	// its response. Threshold judgment proper lives on the
	// `type: assertion` wrapper, not here.
	defaultPassThreshold = 0.5
)

// JudgeProvider abstracts LLM access for judge-based evaluations.
// Arena, SDK, and eval workers each provide their own implementation
// wiring their respective provider infrastructure.
type JudgeProvider interface {
	// Judge sends the evaluation prompt to an LLM and returns
	// the parsed verdict. Implementations handle provider selection,
	// prompt formatting, and response parsing.
	Judge(ctx context.Context, opts JudgeOpts) (*JudgeResult, error)
}

// parseJudgeResponse parses the LLM judge response into a JudgeResult.
// The handler emits Score = jr.Score as a pure eval primitive; threshold
// judgment lives on the `type: assertion` wrapper, not here. We still
// record jr.Passed when the model returned it so consumers that inspect
// Details can see the model's own opinion alongside the score.
//
// A response that cannot be read is an ERROR, not a measurement. It used to
// return Score 0.5 (the pass threshold), Passed true and a nil error, so the
// runner recorded a judge that could not be read as one that scored exactly
// half — a fabricated number that reached gauges and anything gating on score,
// with only a Reasoning string as evidence and nothing acting on it (#1882).
//
// The likeliest trigger is a rubric: Score is a float64, so a judge asked for
// per-dimension scores answers with an object and the unmarshal fails outright.
func parseJudgeResponse(raw string) (*JudgeResult, error) {
	var parsed struct {
		Passed    *bool   `json:"passed"`
		Score     float64 `json:"score"`
		Reasoning string  `json:"reasoning"`
	}

	// Extract JSON from response (might be wrapped in markdown)
	jsonStr := raw
	if idx := strings.Index(raw, "{"); idx >= 0 {
		if end := strings.LastIndex(raw, "}"); end >= idx {
			jsonStr = raw[idx : end+1]
		}
	}

	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil, fmt.Errorf("judge response could not be parsed: %w (raw: %s)",
			err, truncateForError(raw))
	}

	result := &JudgeResult{
		Score:     parsed.Score,
		Reasoning: parsed.Reasoning,
		Raw:       raw,
	}

	// Honor the model's explicit verdict when present; otherwise fall
	// back to the default pass threshold (kept for reporting only —
	// EvalResult.Score carries the raw signal to the wrapper).
	if parsed.Passed != nil {
		result.Passed = *parsed.Passed
	} else {
		result.Passed = parsed.Score >= defaultPassThreshold
	}

	return result, nil
}

// JudgeOpts configures a judge evaluation request.
type JudgeOpts struct {
	// Content is the text being evaluated (assistant response or full conversation).
	Content string

	// Criteria describes what the judge should evaluate (e.g. "Is the response helpful?").
	Criteria string

	// Rubric provides detailed scoring guidance (optional).
	Rubric string

	// Model specifies which model to use for judging (optional, provider decides default).
	Model string

	// SystemPrompt overrides the default judge system prompt (optional).
	SystemPrompt string

	// Extra holds additional parameters for provider-specific features.
	Extra map[string]any

	// Emitter is an optional event emitter for provider call telemetry.
	Emitter *events.Emitter
}

// JudgeResult captures the output of an LLM judge evaluation.
type JudgeResult struct {
	// Passed indicates whether the content met the evaluation criteria.
	Passed bool

	// Score is the numerical score assigned by the judge (typically 0.0-1.0).
	Score float64

	// Reasoning explains the judge's evaluation.
	Reasoning string

	// Raw is the unprocessed LLM response text.
	Raw string
}

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
// and parses the verdict. Emits ProviderCallStarted/Completed/Failed
// events if an emitter is set on opts.
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

	// Emit provider call started
	if opts.Emitter != nil {
		opts.Emitter.ProviderCallStarted(provider.ID(), provider.Model(), 1, 0, nil)
	}

	startTime := time.Now()
	resp, err := provider.Predict(ctx, providers.PredictionRequest{
		System:      systemPrompt,
		Messages:    []types.Message{userMsg},
		Temperature: 0.0,
		MaxTokens:   judgeMaxTokens,
	})
	duration := time.Since(startTime)

	if err != nil {
		if opts.Emitter != nil {
			opts.Emitter.ProviderCallFailedCtx(ctx, &events.ProviderCallFailedData{
				Provider: provider.ID(),
				Model:    provider.Model(),
				Error:    err,
				Duration: duration,
				Source:   events.SourceJudge,
			})
		}
		return nil, fmt.Errorf("judge predict failed: %w", err)
	}

	// Emit provider call completed
	if opts.Emitter != nil {
		completedData := &events.ProviderCallCompletedData{
			Provider: provider.ID(),
			Model:    provider.Model(),
			Duration: duration,
			Source:   events.SourceJudge,
		}
		if resp.CostInfo != nil {
			completedData.InputTokens = resp.CostInfo.InputTokens
			completedData.OutputTokens = resp.CostInfo.OutputTokens
			completedData.CachedTokens = resp.CostInfo.CachedTokens
			completedData.Cost = resp.CostInfo.TotalCost
		}
		opts.Emitter.ProviderCallCompletedCtx(ctx, completedData)
	}

	return parseJudgeResponse(resp.Content)
}

// Ensure SpecJudgeProvider implements JudgeProvider.
var _ JudgeProvider = (*SpecJudgeProvider)(nil)

// truncateForError bounds a raw judge response so a parse failure names what
// came back without pasting an entire model reply into a log line.
func truncateForError(raw string) string {
	const maxRawInError = 200
	if len(raw) <= maxRawInError {
		return raw
	}
	return raw[:maxRawInError] + "…"
}
