// Package handlers provides eval type handler implementations.
package handlers

import (
	"context"
	"encoding/json"
	"strings"
)

const (
	// defaultJudgeSystemPrompt is the system prompt used when no custom prompt is provided.
	defaultJudgeSystemPrompt = "You are an evaluation judge. Evaluate the following content " +
		"and respond with a JSON object containing: " +
		"\"passed\" (boolean), \"score\" (float 0-1), and \"reasoning\" (string)."

	// judgeMaxTokens is the maximum token limit for judge LLM calls.
	judgeMaxTokens = 1024

	// defaultPassThreshold is the default score threshold when no explicit passed field or minScore is set.
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
//
//nolint:unparam // error return kept for future extensibility
func parseJudgeResponse(raw string, minScore *float64) (*JudgeResult, error) {
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
		return &JudgeResult{
			Passed:    true,
			Score:     defaultPassThreshold,
			Reasoning: "Could not parse judge response",
			Raw:       raw,
		}, nil
	}

	result := &JudgeResult{
		Score:     parsed.Score,
		Reasoning: parsed.Reasoning,
		Raw:       raw,
	}

	if parsed.Passed != nil {
		result.Passed = *parsed.Passed
	} else if minScore != nil {
		result.Passed = parsed.Score >= *minScore
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

	// MinScore is the minimum score threshold for passing (optional).
	MinScore *float64

	// Extra holds additional parameters for provider-specific features.
	Extra map[string]any
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
