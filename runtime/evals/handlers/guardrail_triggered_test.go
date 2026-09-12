package handlers

import (
	"context"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestGuardrailTriggeredHandler_Type(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	if h.Type() != "guardrail_triggered" {
		t.Fatalf("unexpected type: %s", h.Type())
	}
}

func TestGuardrailTriggered_TriggeredAsExpected(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		PriorResults: []evals.EvalResult{
			{EvalID: "gr_banned", Type: "content_excludes", Score: boolScore(false)},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "content_excludes",
		"should_trigger": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestGuardrailTriggered_MatchByEvalID(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		PriorResults: []evals.EvalResult{
			{EvalID: "banned_words_check", Type: "content_excludes", Score: boolScore(false)},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "banned_words_check",
		"should_trigger": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("expected pass when matching by EvalID: %s", result.Explanation)
	}
}

func TestGuardrailTriggered_NotTriggeredAsExpected(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		PriorResults: []evals.EvalResult{
			{EvalID: "gr_length", Type: "max_length", Score: boolScore(true)},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "max_length",
		"should_trigger": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("expected pass: %s", result.Explanation)
	}
}

func TestGuardrailTriggered_ExpectedTriggerButPassed(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		PriorResults: []evals.EvalResult{
			{EvalID: "gr_banned", Type: "content_excludes", Score: boolScore(true)},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "content_excludes",
		"should_trigger": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 0.0 {
		t.Fatal("expected fail when trigger expected but eval passed")
	}
}

func TestGuardrailTriggered_ValidatorNotFound(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "banned_words",
		"should_trigger": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 0.0 {
		t.Fatal("expected fail when validator not found but expected to trigger")
	}
}

func TestGuardrailTriggered_ValidatorNotFoundButNotExpected(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "nonexistent",
		"should_trigger": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("expected pass when not-found and should_trigger=false: %s", result.Explanation)
	}
}

func TestGuardrailTriggered_NoValidatorType(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 0.0 {
		t.Fatal("expected fail with no validator_type")
	}
}

func TestGuardrailTriggered_DefaultShouldTriggerTrue(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		PriorResults: []evals.EvalResult{
			{Type: "content_excludes", Score: boolScore(false)},
		},
	}

	// Omit should_trigger — defaults to true.
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "content_excludes",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("expected pass with default should_trigger=true: %s", result.Explanation)
	}
}

// bothDirectionsFired is the situation the direction param exists for: one eval
// type declared as an input guardrail AND as an output guardrail, so the
// transcript carries two firings that differ only in the side they judged. The
// input one fired (score 0); the output one did not (score 1).
func bothDirectionsFired() []evals.EvalResult {
	return []evals.EvalResult{
		{Type: "pii_leakage", Score: boolScore(false), Details: map[string]any{"direction": "input"}},
		{Type: "pii_leakage", Score: boolScore(true), Details: map[string]any{"direction": "output"}},
	}
}

// TestGuardrailTriggered_DirectionSelectsInputAmongBothDirections pins that
// direction picks a specific firing rather than the most recent one.
//
// The no-direction case is asserted alongside deliberately: it reaches the
// output entry (the last match) and therefore fails, so the direction filter is
// demonstrably what changes the outcome. Delete the filter and the first
// assertion fails.
func TestGuardrailTriggered_DirectionSelectsInputAmongBothDirections(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{PriorResults: bothDirectionsFired()}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "pii_leakage",
		"should_trigger": true,
		"direction":      "input",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("direction=input must select the input firing: %s", result.Explanation)
	}

	unfiltered, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "pii_leakage",
		"should_trigger": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if unfiltered.Score == nil || *unfiltered.Score != 0.0 {
		t.Fatal("without direction the last match (the output entry, which did not fire) must still win")
	}
}

// TestGuardrailTriggered_DirectionSelectsOutputAmongBothDirections is the mirror:
// a filter hardwired to "the input one" would pass the test above and fail here.
func TestGuardrailTriggered_DirectionSelectsOutputAmongBothDirections(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{PriorResults: bothDirectionsFired()}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "pii_leakage",
		"should_trigger": false,
		"direction":      "output",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("direction=output must select the output entry, which did not fire: %s", result.Explanation)
	}

	// And it must report that entry's outcome, not the input entry's.
	triggered, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "pii_leakage",
		"should_trigger": true,
		"direction":      "output",
	})
	if err != nil {
		t.Fatal(err)
	}
	if triggered.Score == nil || *triggered.Score != 0.0 {
		t.Fatal("the output entry did not fire, so should_trigger=true must fail")
	}
}

// TestGuardrailTriggered_DirectionSkipsResultsWithoutDirection pins that a prior
// result carrying no direction never satisfies a direction-qualified assertion.
// Treating a missing key as a match would report an ordinary eval result — or an
// output firing recorded before the key existed — as an input firing.
func TestGuardrailTriggered_DirectionSkipsResultsWithoutDirection(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		PriorResults: []evals.EvalResult{
			{Type: "banned_words", Score: boolScore(false)},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "banned_words",
		"should_trigger": true,
		"direction":      "input",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 0.0 {
		t.Fatal("a result without a recorded direction must not match a direction-qualified search")
	}
	if !strings.Contains(result.Explanation, `direction "input"`) {
		t.Fatalf("the failure must name the side it looked at, got: %s", result.Explanation)
	}
}

// TestGuardrailTriggered_DirectionBothMatchesEitherSide pins that "both" — legal
// on the guardrail declaration — means "either side" here, since a firing is
// only ever recorded as input or output and would otherwise match nothing.
func TestGuardrailTriggered_DirectionBothMatchesEitherSide(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		PriorResults: []evals.EvalResult{
			{Type: "pii_leakage", Score: boolScore(false), Details: map[string]any{"direction": "output"}},
		},
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "pii_leakage",
		"should_trigger": true,
		"direction":      "both",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("direction=both must match an output firing: %s", result.Explanation)
	}
}

// TestGuardrailTriggered_ReportsDirectionOnlyWhenRequested pins that the
// reported value gains a direction key exactly when the assertion asked for one.
// guardrail_triggered is widely declared without direction; stamping one
// unconditionally would change what every existing scenario reports.
func TestGuardrailTriggered_ReportsDirectionOnlyWhenRequested(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{PriorResults: bothDirectionsFired()}

	qualified, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "pii_leakage",
		"should_trigger": true,
		"direction":      "input",
	})
	if err != nil {
		t.Fatal(err)
	}
	value, _ := qualified.Value.(map[string]any)
	if value["direction"] != "input" {
		t.Fatalf("a direction-qualified result must report the side, got %v", qualified.Value)
	}
	if qualified.Details["direction"] != "input" {
		t.Fatalf("details must name the side too, got %v", qualified.Details)
	}

	plain, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "pii_leakage",
		"should_trigger": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	plainValue, _ := plain.Value.(map[string]any)
	if _, present := plainValue["direction"]; present {
		t.Fatalf("an unqualified assertion must not invent a direction, got %v", plain.Value)
	}
	if _, present := plain.Details["direction"]; present {
		t.Fatalf("an unqualified assertion must not invent a direction in details, got %v", plain.Details)
	}
}

// TestGuardrailTriggered_ObservesFiringThroughBuildEvalContext drives the join
// the design flagged as unpinned: message.Validations → BuildEvalContext →
// PriorResults → this handler. Both halves have their own tests; before #1718
// nothing exercised the bridge, and it dropped Details on the way across.
func TestGuardrailTriggered_ObservesFiringThroughBuildEvalContext(t *testing.T) {
	messages := []types.Message{
		types.NewUserMessage("please arrange a wire transfer"),
		{
			Role:    "assistant",
			Content: "I can't help with that.",
			Validations: []types.ValidationResult{{
				ValidatorType: "banned_words",
				Passed:        false,
				Details:       map[string]any{"direction": "input", "reason": "matched \"wire transfer\""},
			}},
		},
	}

	evalCtx := evals.BuildEvalContext(messages, 0, "s1", "chat", nil)
	h := &GuardrailTriggeredHandler{}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "banned_words",
		"should_trigger": true,
		"direction":      "input",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("an input firing must be observable through PriorResults: %s", result.Explanation)
	}

	// Discriminating: the same firing must not answer for the output side.
	wrongSide, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator_type": "banned_words",
		"should_trigger": true,
		"direction":      "output",
	})
	if err != nil {
		t.Fatal(err)
	}
	if wrongSide.Score == nil || *wrongSide.Score != 0.0 {
		t.Fatal("an input firing must not satisfy a direction=output assertion")
	}
}

// TestGuardrailTriggered_DirectionDisambiguatesThroughBuildEvalContext is the
// case the change exists for, driven through the real bridge: one eval type
// recorded on both sides. Without the direction filter only the last entry is
// reachable, so the input assertion below cannot pass.
//
// The two validations are stamped on one message rather than produced by a live
// pipeline because an input firing blocks the provider call, so the output
// guardrail never runs in the same turn — the transcript, not the stage, is what
// this contract is about.
func TestGuardrailTriggered_DirectionDisambiguatesThroughBuildEvalContext(t *testing.T) {
	messages := []types.Message{
		types.NewUserMessage("my SSN is 123-45-6789"),
		{
			Role:    "assistant",
			Content: "I can't help with that.",
			Validations: []types.ValidationResult{
				{
					ValidatorType: "pii_leakage",
					Passed:        false,
					Details:       map[string]any{"direction": "input"},
				},
				{
					ValidatorType: "pii_leakage",
					Passed:        true,
					Details:       map[string]any{"direction": "output"},
				},
			},
		},
	}

	evalCtx := evals.BuildEvalContext(messages, 0, "s1", "chat", nil)
	h := &GuardrailTriggeredHandler{}

	cases := []struct {
		name      string
		direction string
		want      float64
	}{
		{"input side fired", "input", 1.0},
		{"output side did not fire", "output", 0.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := map[string]any{
				"validator_type": "pii_leakage",
				"should_trigger": true,
				"direction":      tc.direction,
			}
			result, err := h.Eval(context.Background(), evalCtx, params)
			if err != nil {
				t.Fatal(err)
			}
			if result.Score == nil || *result.Score != tc.want {
				t.Fatalf("want score %v, got %v (%s)", tc.want, result.Score, result.Explanation)
			}
		})
	}
}

func TestGuardrailTriggered_ValidatorAliasParam(t *testing.T) {
	h := &GuardrailTriggeredHandler{}
	evalCtx := &evals.EvalContext{
		PriorResults: []evals.EvalResult{
			{Type: "content_excludes", Score: boolScore(false)},
		},
	}

	// Use "validator" param instead of "validator_type".
	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"validator":      "content_excludes",
		"should_trigger": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Score == nil || *result.Score != 1.0 {
		t.Fatalf("expected pass with validator alias: %s", result.Explanation)
	}
}
