package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// ToolAntiPatternHandler checks that tool calls do NOT contain forbidden subsequences.
// Params: patterns []map[string]any — each with "sequence" ([]string) and optional "message" (string).
type ToolAntiPatternHandler struct{}

// Type returns the eval type identifier.
func (h *ToolAntiPatternHandler) Type() string { return "tool_anti_pattern" }

// Eval checks for forbidden tool call subsequences.
func (h *ToolAntiPatternHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	views := viewsFromRecords(evalCtx.ToolCalls)
	patterns := extractPatterns(params)

	if len(patterns) == 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(true),
			Explanation: "no anti-patterns specified",
		}, nil
	}

	var violations []map[string]any
	for _, p := range patterns {
		matched, _ := coreToolCallSequence(views, p.sequence)
		if matched >= len(p.sequence) {
			violations = append(violations, map[string]any{
				"sequence": p.sequence,
				"message":  p.message,
			})
		}
	}

	if len(violations) > 0 {
		msgs := make([]string, len(violations))
		for i, v := range violations {
			seq := v["sequence"].([]string)
			msg := v["message"].(string)
			if msg != "" {
				msgs[i] = fmt.Sprintf("[%s]: %s", strings.Join(seq, " → "), msg)
			} else {
				msgs[i] = fmt.Sprintf("[%s]", strings.Join(seq, " → "))
			}
		}
		return &evals.EvalResult{
			Type:        h.Type(),
			Score:       boolScore(false),
			Explanation: fmt.Sprintf("found %d anti-pattern(s): %s", len(violations), strings.Join(msgs, "; ")),
			Value:       map[string]any{"violations": violations},
			Details:     map[string]any{"violations": violations},
		}, nil
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Score:       boolScore(true),
		Explanation: fmt.Sprintf("none of %d anti-patterns detected", len(patterns)),
		Value:       map[string]any{"violations": []map[string]any{}},
	}, nil
}

type antiPattern struct {
	sequence []string
	message  string
}

func extractPatterns(params map[string]any) []antiPattern {
	raw, ok := params["patterns"].([]any)
	if !ok {
		return nil
	}
	patterns := make([]antiPattern, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		seq := extractStringSlice(m, "sequence")
		if len(seq) == 0 {
			continue
		}
		msg, _ := m["message"].(string)
		patterns = append(patterns, antiPattern{sequence: seq, message: msg})
	}
	return patterns
}
