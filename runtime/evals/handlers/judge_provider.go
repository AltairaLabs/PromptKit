// Package handlers provides eval type handler implementations.
package handlers

import "context"

// JudgeProvider abstracts LLM access for judge-based evaluations.
// Arena, SDK, and eval workers each provide their own implementation
// wiring their respective provider infrastructure.
type JudgeProvider interface {
	// Judge sends the evaluation prompt to an LLM and returns
	// the parsed verdict. Implementations handle provider selection,
	// prompt formatting, and response parsing.
	Judge(ctx context.Context, opts JudgeOpts) (*JudgeResult, error)
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
