package handlers

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// judgeParityCase is one judge-backed handler plus params that carry every
// input it needs to reach the judge, so a failure can only come from the judge.
type judgeParityCase struct {
	handler evals.EvalTypeHandler
	params  map[string]any
}

func judgeBackedHandlers() []judgeParityCase {
	rag := func() map[string]any {
		return map[string]any{
			"contexts":  []string{"Paris is the capital of France."},
			"reference": "Paris",
		}
	}
	return []judgeParityCase{
		{&LLMJudgeHandler{}, map[string]any{}},
		{&LLMJudgeSessionHandler{}, map[string]any{}},
		{&LLMJudgeToolCallsHandler{}, map[string]any{}},
		{&BiasHandler{}, map[string]any{}},
		{&ToxicityHandler{}, map[string]any{}},
		{&RoleViolationHandler{}, map[string]any{"role": "a support agent"}},
		{&FaithfulnessHandler{}, rag()},
		{&HallucinationHandler{}, rag()},
		{&AnswerRelevancyHandler{}, rag()},
		{&ContextualPrecisionHandler{}, rag()},
		{&ContextualRecallHandler{}, rag()},
		{&ContextualRelevancyHandler{}, rag()},
		// pii_leakage reaches the judge only when one is named or supplied;
		// with neither it is the regex-only baseline (see the no-judge test).
		{&PIILeakageHandler{}, map[string]any{}},
	}
}

func parityEvalCtx(metadata map[string]any) *evals.EvalContext {
	return &evals.EvalContext{
		CurrentOutput: "Paris is the capital of France.",
		Messages: []types.Message{
			{Role: "user", Content: "What is the capital of France?"},
			{Role: "assistant", Content: "Paris is the capital of France."},
		},
		ToolCalls: []evals.ToolCallRecord{{ToolName: "lookup", Arguments: map[string]any{"q": "France"}}},
		Metadata:  metadata,
	}
}

// assertCouldNotRun checks a result reports that no measurement was taken:
// Error set (which routes it to eval.failed), Score 0 so gates fail closed.
func assertCouldNotRun(t *testing.T, result *evals.EvalResult, wantInError string) {
	t.Helper()
	if result == nil {
		t.Fatal("nil result")
	}
	if result.Error == "" {
		t.Errorf("Error is empty; the result would read as a genuine score (explanation %q)",
			result.Explanation)
	}
	if !strings.Contains(result.Error, wantInError) {
		t.Errorf("Error = %q, want it to contain %q", result.Error, wantInError)
	}
	if result.Score == nil || *result.Score != 0 {
		t.Errorf("Score = %v, want 0 so thresholds still fail closed", result.Score)
	}
}

// A judge that cannot be resolved is a check that did not run, for every
// judge-backed handler alike — the same Error that llm_judge reports (#2334).
func TestJudgeHandlers_UnresolvedJudgeSetsError(t *testing.T) {
	t.Parallel()
	for _, tc := range judgeBackedHandlers() {
		t.Run(tc.handler.Type(), func(t *testing.T) {
			t.Parallel()
			params := tc.params
			params[ProviderParam] = "grader" // named, but no binding on the context
			result, err := tc.handler.Eval(context.Background(), parityEvalCtx(nil), params)
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			assertCouldNotRun(t, result, "grader")
		})
	}
}

func TestJudgeHandlers_MissingJudgeProviderSetsError(t *testing.T) {
	t.Parallel()
	for _, tc := range judgeBackedHandlers() {
		if _, ok := tc.handler.(*PIILeakageHandler); ok {
			continue // regex-only baseline without a judge; covered below
		}
		t.Run(tc.handler.Type(), func(t *testing.T) {
			t.Parallel()
			result, err := tc.handler.Eval(context.Background(), parityEvalCtx(nil), tc.params)
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			assertCouldNotRun(t, result, "judge_provider not found")
		})
	}
}

func TestJudgeHandlers_JudgeCallErrorSetsError(t *testing.T) {
	t.Parallel()
	for _, tc := range judgeBackedHandlers() {
		t.Run(tc.handler.Type(), func(t *testing.T) {
			t.Parallel()
			judge := &llmJudgeMock{err: errors.New("judge unavailable")}
			evalCtx := parityEvalCtx(map[string]any{"judge_provider": judge})
			result, err := tc.handler.Eval(context.Background(), evalCtx, tc.params)
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			assertCouldNotRun(t, result, "judge unavailable")
		})
	}
}

// pii_leakage with no judge named or supplied is the deterministic regex
// baseline, which is a real measurement: no Error, and a clean output passes.
func TestPIILeakage_NoJudgeIsStillAMeasurement(t *testing.T) {
	t.Parallel()
	result, err := (&PIILeakageHandler{}).Eval(context.Background(), parityEvalCtx(nil), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if result.Error != "" {
		t.Errorf("Error = %q, want none for the regex-only baseline", result.Error)
	}
	if result.Score == nil || *result.Score != 1 {
		t.Errorf("Score = %v, want 1 for clean output", result.Score)
	}
}

// RAG handlers missing the input they grade (no retrieved context, no
// question, no reference) did not run either.
func TestRAGHandlers_MissingInputSetsError(t *testing.T) {
	t.Parallel()
	judge := passMock(1.0, "should not be called")
	cases := []struct {
		handler evals.EvalTypeHandler
		params  map[string]any
		want    string
	}{
		{&FaithfulnessHandler{}, map[string]any{}, "no context provided"},
		{&HallucinationHandler{}, map[string]any{}, "no context provided"},
		{&ContextualPrecisionHandler{}, map[string]any{}, "no context provided"},
		{&ContextualRecallHandler{}, map[string]any{"contexts": []string{"c"}}, "no ground-truth answer"},
	}
	for _, tc := range cases {
		t.Run(tc.handler.Type(), func(t *testing.T) {
			t.Parallel()
			evalCtx := parityEvalCtx(map[string]any{"judge_provider": judge})
			result, err := tc.handler.Eval(context.Background(), evalCtx, tc.params)
			if err != nil {
				t.Fatalf("unexpected Go error: %v", err)
			}
			assertCouldNotRun(t, result, tc.want)
		})
	}
}

// End to end through the runner: a safety judge that cannot reach its judge
// is reported as eval.failed, exactly like llm_judge, not as eval.completed.
func TestRunner_UnresolvedSafetyJudgeEmitsEvalFailed(t *testing.T) {
	t.Parallel()
	for _, evalType := range []string{"llm_judge", "toxicity", "faithfulness"} {
		t.Run(evalType, func(t *testing.T) {
			t.Parallel()
			bus := events.NewEventBus()
			defer bus.Close()

			var mu sync.Mutex
			var got []events.EventType
			done := make(chan struct{}, 2)
			record := func(e *events.Event) {
				mu.Lock()
				got = append(got, e.Type)
				mu.Unlock()
				done <- struct{}{}
			}
			bus.Subscribe(events.EventEvalFailed, record)
			bus.Subscribe(events.EventEvalCompleted, record)

			runner := evals.NewEvalRunner(evals.NewEvalTypeRegistry())
			runner.SetEmitter(events.NewEmitter(bus, "run", "sess", "conv"))
			defs := []evals.EvalDef{{
				ID: "e1", Type: evalType, Trigger: "every_turn",
				Params: map[string]any{
					ProviderParam: "grader",
					"contexts":    []string{"Paris is the capital of France."},
				},
			}}
			results := runner.RunTurnEvals(context.Background(), defs, parityEvalCtx(nil))
			if len(results) != 1 || results[0].Error == "" {
				t.Fatalf("want one result with Error set, got %+v", results)
			}

			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("no eval event emitted")
			}
			mu.Lock()
			defer mu.Unlock()
			if len(got) != 1 || got[0] != events.EventEvalFailed {
				t.Errorf("events = %v, want [%s]", got, events.EventEvalFailed)
			}
		})
	}
}
