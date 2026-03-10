package handlers

import (
	"context"
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

const percentMultiplier = 100.0

// ToolEfficiencyHandler checks tool usage efficiency metrics.
// Params:
//   - max_calls: int — maximum total tool calls allowed (optional)
//   - max_errors: int — maximum tool errors allowed (optional)
//   - max_error_rate: float64 — maximum error rate 0.0-1.0 (optional)
type ToolEfficiencyHandler struct{}

// Type returns the eval type identifier.
func (h *ToolEfficiencyHandler) Type() string { return "tool_efficiency" }

// Eval checks tool efficiency metrics.
func (h *ToolEfficiencyHandler) Eval(
	_ context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	views := viewsFromRecords(evalCtx.ToolCalls)
	stats := computeToolStats(views)

	maxCalls := extractIntPtr(params, "max_calls")
	maxErrors := extractIntPtr(params, "max_errors")
	maxErrorRate := extractFloat64Ptr(params, "max_error_rate")

	if maxCalls == nil && maxErrors == nil && maxErrorRate == nil {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Explanation: "no efficiency thresholds specified (need max_calls, max_errors, or max_error_rate)",
		}, nil
	}

	failures := checkEfficiencyThresholds(stats, maxCalls, maxErrors, maxErrorRate)

	details := map[string]any{
		"total_calls": stats.totalCalls,
		"errors":      stats.errorCount,
	}
	if stats.totalCalls > 0 {
		details["error_rate"] = stats.errorRate()
	}

	value := map[string]any{
		"total_calls": stats.totalCalls,
		"errors":      stats.errorCount,
	}
	if stats.totalCalls > 0 {
		value["error_rate"] = stats.errorRate()
	}

	if len(failures) > 0 {
		return &evals.EvalResult{
			Type:        h.Type(),
			Passed:      false,
			Score:       scorePtr(0),
			Explanation: fmt.Sprintf("efficiency check failed: %v", failures),
			Value:       value,
			Details:     details,
		}, nil
	}

	score := 1.0
	if maxCalls != nil && *maxCalls > 0 {
		score = 1.0 - float64(stats.totalCalls)/float64(*maxCalls)
		if score < 0 {
			score = 0
		}
	}

	return &evals.EvalResult{
		Type:        h.Type(),
		Passed:      true,
		Score:       &score,
		Explanation: fmt.Sprintf("%d calls, %d errors — within limits", stats.totalCalls, stats.errorCount),
		Value:       value,
		Details:     details,
	}, nil
}

type toolStats struct {
	totalCalls int
	errorCount int
}

func (s toolStats) errorRate() float64 {
	if s.totalCalls == 0 {
		return 0
	}
	return float64(s.errorCount) / float64(s.totalCalls)
}

func computeToolStats(views []toolCallView) toolStats {
	var s toolStats
	s.totalCalls = len(views)
	for _, v := range views {
		if v.Error != "" {
			s.errorCount++
		}
	}
	return s
}

func checkEfficiencyThresholds(s toolStats, maxCalls, maxErrors *int, maxErrorRate *float64) []string {
	var failures []string
	if maxCalls != nil && s.totalCalls > *maxCalls {
		failures = append(failures,
			fmt.Sprintf("too many calls: %d (max %d)", s.totalCalls, *maxCalls))
	}
	if maxErrors != nil && s.errorCount > *maxErrors {
		failures = append(failures,
			fmt.Sprintf("too many errors: %d (max %d)", s.errorCount, *maxErrors))
	}
	if maxErrorRate != nil && s.totalCalls > 0 {
		rate := s.errorRate()
		if rate > *maxErrorRate {
			failures = append(failures,
				fmt.Sprintf("error rate %.1f%% exceeds max %.1f%%",
					rate*percentMultiplier, *maxErrorRate*percentMultiplier))
		}
	}
	return failures
}
