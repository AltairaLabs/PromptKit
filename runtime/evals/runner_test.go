package evals

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// panicHandler panics when Eval is called.
type panicHandler struct{}

func (p *panicHandler) Type() string { return "panic" }

func (p *panicHandler) Eval(
	_ context.Context, _ *EvalContext, _ map[string]any,
) (*EvalResult, error) {
	panic("boom")
}

// errorHandler returns an error.
type errorHandler struct{}

func (e *errorHandler) Type() string { return "error" }

func (e *errorHandler) Eval(
	_ context.Context, _ *EvalContext, _ map[string]any,
) (*EvalResult, error) {
	return nil, errors.New("eval failed")
}

// nilHandler returns nil result with nil error.
type nilHandler struct{}

func (n *nilHandler) Type() string { return "nil" }

func (n *nilHandler) Eval(
	_ context.Context, _ *EvalContext, _ map[string]any,
) (*EvalResult, error) {
	return nil, nil
}

// slowHandler blocks until context is cancelled.
type slowHandler struct{}

func (s *slowHandler) Type() string { return "slow" }

func (s *slowHandler) Eval(
	ctx context.Context, _ *EvalContext, _ map[string]any,
) (*EvalResult, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// scoringHandler returns a result with a configurable score.
type scoringHandler struct {
	typeName string
	score    float64
}

func (s *scoringHandler) Type() string { return s.typeName }

func (s *scoringHandler) Eval(
	_ context.Context, _ *EvalContext, _ map[string]any,
) (*EvalResult, error) {
	return &EvalResult{Score: &s.score}, nil
}

// nilScoreHandler returns a result with a nil Score.
type nilScoreHandler struct{}

func (n *nilScoreHandler) Type() string { return "nilscore" }

func (n *nilScoreHandler) Eval(
	_ context.Context, _ *EvalContext, _ map[string]any,
) (*EvalResult, error) {
	return &EvalResult{Value: true}, nil
}

func newTestRegistry(handlers ...EvalTypeHandler) *EvalTypeRegistry {
	r := NewEmptyEvalTypeRegistry()
	for _, h := range handlers {
		r.Register(h)
	}
	return r
}

func TestNewEvalRunner_DefaultTimeout(t *testing.T) {
	r := NewEvalRunner(newTestRegistry())
	if r.timeout != DefaultEvalTimeout {
		t.Errorf("got timeout %v, want %v", r.timeout, DefaultEvalTimeout)
	}
}

func TestNewEvalRunner_WithTimeout(t *testing.T) {
	r := NewEvalRunner(newTestRegistry(), WithTimeout(5*time.Second))
	if r.timeout != 5*time.Second {
		t.Errorf("got timeout %v, want %v", r.timeout, 5*time.Second)
	}
}

func TestRunTurnEvals_Basic(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{ID: "e1", Type: "test", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1", TurnIndex: 0}

	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].EvalID != "e1" {
		t.Errorf("got EvalID %q, want %q", results[0].EvalID, "e1")
	}
	if !(results[0].Score != nil && *results[0].Score >= 1.0) {
		t.Error("expected IsPassed()=true")
	}
}

func TestRunTurnEvals_SkipsSessionTrigger(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{
			ID:      "session-only",
			Type:    "test",
			Trigger: TriggerOnSessionComplete,
		},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)
	if len(results) != 0 {
		t.Errorf("turn evals should skip session triggers, got %d", len(results))
	}
}

func TestRunSessionEvals_Basic(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{
			ID:      "e1",
			Type:    "test",
			Trigger: TriggerOnSessionComplete,
		},
	}
	evalCtx := &EvalContext{SessionID: "s1", TurnIndex: 3}

	results := runner.RunSessionEvals(context.Background(), defs, evalCtx)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].EvalID != "e1" {
		t.Errorf("got EvalID %q, want %q", results[0].EvalID, "e1")
	}
}

func TestRunSessionEvals_SkipsTurnTrigger(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{ID: "turn-only", Type: "test", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	results := runner.RunSessionEvals(context.Background(), defs, evalCtx)
	if len(results) != 0 {
		t.Errorf(
			"session evals should skip turn triggers, got %d",
			len(results),
		)
	}
}

func TestRunTurnEvals_SkipsDisabled(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{
			ID:      "disabled",
			Type:    "test",
			Trigger: TriggerEveryTurn,
			Enabled: boolPtr(false),
		},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)
	if len(results) != 0 {
		t.Errorf("disabled evals should be skipped, got %d", len(results))
	}
}

func TestRunTurnEvals_UnknownHandler(t *testing.T) {
	reg := newTestRegistry() // empty registry
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{ID: "e1", Type: "nonexistent", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Error == "" {
		t.Error("expected error for unknown handler")
	}
}

func TestRunTurnEvals_PanicRecovery(t *testing.T) {
	reg := newTestRegistry(&panicHandler{})
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{ID: "panicker", Type: "panic", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Error == "" {
		t.Error("expected error from panic recovery")
	}
}

func TestRunTurnEvals_ErrorHandler(t *testing.T) {
	reg := newTestRegistry(&errorHandler{})
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{ID: "e1", Type: "error", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Error != "eval failed" {
		t.Errorf("got error %q, want %q", results[0].Error, "eval failed")
	}
}

func TestRunTurnEvals_NilResult(t *testing.T) {
	reg := newTestRegistry(&nilHandler{})
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{ID: "e1", Type: "nil", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Error == "" {
		t.Error("expected error for nil result")
	}
}

func TestRunTurnEvals_DurationTracked(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{ID: "e1", Type: "test", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].DurationMs < 0 {
		t.Errorf("duration should be non-negative, got %d", results[0].DurationMs)
	}
}

func TestRunTurnEvals_ContextCancelled(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	defs := []EvalDef{
		{ID: "e1", Type: "test", Trigger: TriggerEveryTurn},
		{ID: "e2", Type: "test", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	results := runner.RunTurnEvals(ctx, defs, evalCtx)
	if len(results) > 1 {
		t.Errorf(
			"cancelled context should stop early, got %d results",
			len(results),
		)
	}
}

func TestRunTurnEvals_MultipleEvals(t *testing.T) {
	reg := newTestRegistry(
		&stubHandler{typeName: "test"},
		&errorHandler{},
	)
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{ID: "e1", Type: "test", Trigger: TriggerEveryTurn},
		{ID: "e2", Type: "error", Trigger: TriggerEveryTurn},
		{ID: "e3", Type: "test", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	if !(results[0].Score != nil && *results[0].Score >= 1.0) {
		t.Error("e1 should pass")
	}
	if results[1].Error == "" {
		t.Error("e2 should have error")
	}
	if !(results[2].Score != nil && *results[2].Score >= 1.0) {
		t.Error("e3 should pass")
	}
}

func TestRunTurnEvals_SampleTurns(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)

	// With 100% sampling, the eval should always run.
	defs := []EvalDef{
		{
			ID:               "sampled",
			Type:             "test",
			Trigger:          TriggerSampleTurns,
			SamplePercentage: float64Ptr(100),
		},
	}
	evalCtx := &EvalContext{SessionID: "s1", TurnIndex: 0}

	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)
	if len(results) != 1 {
		t.Fatalf("100%% sample should always run, got %d results", len(results))
	}
}

func TestRunTurnEvals_Timeout(t *testing.T) {
	reg := newTestRegistry(&slowHandler{})
	runner := NewEvalRunner(reg, WithTimeout(50*time.Millisecond))

	defs := []EvalDef{
		{ID: "slow", Type: "slow", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	start := time.Now()
	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)
	elapsed := time.Since(start)

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].Error == "" {
		t.Error("expected timeout error")
	}
	if elapsed > 2*time.Second {
		t.Errorf("timeout took too long: %v", elapsed)
	}
}

func TestRunSessionEvals_SampleSessions(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{
			ID:               "sess-sample",
			Type:             "test",
			Trigger:          TriggerSampleSessions,
			SamplePercentage: float64Ptr(100),
		},
	}
	evalCtx := &EvalContext{SessionID: "s1", TurnIndex: 5}

	results := runner.RunSessionEvals(context.Background(), defs, evalCtx)
	if len(results) != 1 {
		t.Fatalf(
			"100%% session sample should run, got %d results",
			len(results),
		)
	}
}

func TestRunTurnEvals_MetadataFilled(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{ID: "meta-test", Type: "test", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	r := results[0]
	if r.EvalID != "meta-test" {
		t.Errorf("EvalID = %q, want %q", r.EvalID, "meta-test")
	}
	if r.Type != "test" {
		t.Errorf("Type = %q, want %q", r.Type, "test")
	}
}

func TestRunConversationEvals_Basic(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{
			ID:      "conv-check",
			Type:    "test",
			Trigger: TriggerOnConversationComplete,
		},
	}
	evalCtx := &EvalContext{SessionID: "s1", TurnIndex: 5}

	results := runner.RunConversationEvals(context.Background(), defs, evalCtx)
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if results[0].EvalID != "conv-check" {
		t.Errorf("got EvalID %q, want %q", results[0].EvalID, "conv-check")
	}
	if !(results[0].Score != nil && *results[0].Score >= 1.0) {
		t.Error("expected IsPassed()=true")
	}
}

func TestRunConversationEvals_SkipsTurnTrigger(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{ID: "turn-only", Type: "test", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	results := runner.RunConversationEvals(context.Background(), defs, evalCtx)
	if len(results) != 0 {
		t.Errorf("conversation evals should skip turn triggers, got %d", len(results))
	}
}

func TestRunConversationEvals_SkipsSessionTrigger(t *testing.T) {
	reg := newTestRegistry(&stubHandler{typeName: "test"})
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{ID: "session-only", Type: "test", Trigger: TriggerOnSessionComplete},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	results := runner.RunConversationEvals(context.Background(), defs, evalCtx)
	if len(results) != 0 {
		t.Errorf("conversation evals should skip session triggers, got %d", len(results))
	}
}

// priorCapturingHandler records the PriorResults it receives.
type priorCapturingHandler struct {
	typeName      string
	score         float64
	capturedPrior []EvalResult
}

func (p *priorCapturingHandler) Type() string { return p.typeName }

func (p *priorCapturingHandler) Eval(
	_ context.Context, evalCtx *EvalContext, _ map[string]any,
) (*EvalResult, error) {
	p.capturedPrior = append(p.capturedPrior, evalCtx.PriorResults...)
	return &EvalResult{Score: &p.score}, nil
}

func TestRunTurnEvals_PriorResultsAccumulate(t *testing.T) {
	score1 := 0.5
	handler1 := &scoringHandler{typeName: "first", score: score1}
	handler2 := &priorCapturingHandler{typeName: "second", score: 1.0}

	reg := newTestRegistry(handler1, handler2)
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{ID: "e1", Type: "first", Trigger: TriggerEveryTurn},
		{ID: "e2", Type: "second", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	results := runner.RunTurnEvals(context.Background(), defs, evalCtx)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	// handler2 should have seen handler1's result in PriorResults
	if len(handler2.capturedPrior) != 1 {
		t.Fatalf("handler2 saw %d prior results, want 1", len(handler2.capturedPrior))
	}
	if handler2.capturedPrior[0].EvalID != "e1" {
		t.Errorf("prior result EvalID = %q, want %q", handler2.capturedPrior[0].EvalID, "e1")
	}
	if handler2.capturedPrior[0].Score == nil || *handler2.capturedPrior[0].Score != score1 {
		t.Errorf("prior result score = %v, want %v", handler2.capturedPrior[0].Score, score1)
	}
}

func TestRunTurnEvals_ScoreLoggedAsValue(t *testing.T) {
	// Capture log output to verify score is logged as a float value, not a pointer address.
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	t.Cleanup(func() { logger.SetOutput(nil) })

	score := 0.85
	handler := &scoringHandler{typeName: "test", score: score}
	reg := newTestRegistry(handler)
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{ID: "score-log", Type: "test", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	runner.RunTurnEvals(context.Background(), defs, evalCtx)

	logOutput := buf.String()
	if !strings.Contains(logOutput, "score=0.85") {
		t.Errorf("expected log to contain 'score=0.85', got:\n%s", logOutput)
	}
	// Ensure no pointer address is logged (pointer addresses start with 0x)
	if strings.Contains(logOutput, "score=0x") {
		t.Errorf("score logged as pointer address:\n%s", logOutput)
	}
}

func TestRunTurnEvals_NilScoreLoggedSafely(t *testing.T) {
	// Verify nil score is logged without panic or pointer address.
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	t.Cleanup(func() { logger.SetOutput(nil) })

	handler := &nilScoreHandler{}
	reg := newTestRegistry(handler)
	runner := NewEvalRunner(reg)

	defs := []EvalDef{
		{ID: "nil-score", Type: "nilscore", Trigger: TriggerEveryTurn},
	}
	evalCtx := &EvalContext{SessionID: "s1"}

	runner.RunTurnEvals(context.Background(), defs, evalCtx)

	logOutput := buf.String()
	if strings.Contains(logOutput, "score=0x") {
		t.Errorf("nil score logged as pointer address:\n%s", logOutput)
	}
}

func TestEvalRunner_Clone(t *testing.T) {
	reg := NewEvalTypeRegistry()
	hook := &recordingHook{name: "base"}
	r := NewEvalRunner(reg, WithTimeout(5*time.Second), WithEvalHook(hook))

	bus := events.NewEventBus()
	defer bus.Close()
	r.SetEmitter(events.NewEmitter(bus, "", "", ""))

	clone := r.Clone()
	if clone.registry != r.registry {
		t.Error("clone should share registry")
	}
	if clone.timeout != r.timeout {
		t.Error("clone should copy timeout")
	}
	if clone.emitter != nil {
		t.Error("clone should have nil emitter")
	}
	if len(clone.hooks) != 1 {
		t.Fatalf("clone should copy hooks, got %d", len(clone.hooks))
	}

	// Appending a hook to the clone must not mutate the source.
	clone.AddHook(&recordingHook{name: "extra"})
	if len(r.hooks) != 1 {
		t.Errorf("source runner should still have 1 hook, got %d", len(r.hooks))
	}
	if len(clone.hooks) != 2 {
		t.Errorf("clone should now have 2 hooks, got %d", len(clone.hooks))
	}
}

func TestEvalRunner_EmitResult(t *testing.T) {
	reg := NewEvalTypeRegistry()
	bus := events.NewEventBus()
	defer bus.Close()

	received := make(chan *events.Event, 10)
	bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) {
		received <- e
	})

	emitter := events.NewEmitter(bus, "run1", "sess1", "conv1")
	r := NewEvalRunner(reg, WithEmitter(emitter))

	r.emitResult(nil, &EvalResult{
		EvalID: "e1",
		Type:   "test",
		Score:  func() *float64 { v := 1.0; return &v }(),
	})

	select {
	case e := <-received:
		data := e.Data.(*events.EvalCompletedData)
		if data.EvalID != "e1" {
			t.Errorf("expected eval ID e1, got %q", data.EvalID)
		}
		// A bare eval carries NO verdict, whatever it scored.
		//
		// This assertion used to read "expected passed=true for score 1.0",
		// pinning a fallback that derived pass/fail from `score >= 1.0`. That
		// derivation is what reported an llm_judge scoring 0.9 as a FAILURE,
		// and its own asymmetry gives it away: 1.0 passed while 0.99 failed,
		// which is an artifact of the threshold rather than a judgement anyone
		// made. Only a threshold wrapper (assertion, guardrail) or a handler
		// that judged for itself produces a verdict; this result did neither.
		if data.Passed != nil {
			t.Errorf("bare scored eval must carry no verdict, got passed=%v", *data.Passed)
		}
		if data.Score == nil || *data.Score != 1.0 {
			t.Error("the score itself must still be reported")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

// TestEvalRunner_EmitResult_CopiesPassedRatherThanDeriving is the guard on
// #1861.
//
// emitResult used to compute the pass/fail itself: first from `score >= 1.0`,
// then — after that reported an llm_judge scoring 0.9 as FAILED — from
// `result.Value.(bool)`, which was only ever true because the assertion wrapper
// overwrote Value with its own boolean. Both were the runner re-deciding a
// question the wrapper had already answered.
//
// The two cases below are the two ways that goes wrong, and each fails if the
// copy is replaced by any derivation from score or value.
func TestEvalRunner_EmitResult_CopiesPassedRatherThanDeriving(t *testing.T) {
	reg := NewEvalTypeRegistry()
	bus := events.NewEventBus()
	defer bus.Close()

	received := make(chan *events.Event, 10)
	bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) { received <- e })

	emitter := events.NewEmitter(bus, "", "", "")
	r := NewEvalRunner(reg, WithEmitter(emitter))

	nextEvent := func(t *testing.T) *events.EvalCompletedData {
		t.Helper()
		select {
		case e := <-received:
			return e.Data.(*events.EvalCompletedData)
		case <-time.After(2 * time.Second):
			t.Fatal("timed out")
			return nil
		}
	}

	t.Run("a passing assertion below 1.0 is not overruled by its score", func(t *testing.T) {
		score := 0.7
		r.emitResult(nil, &EvalResult{
			EvalID: "a1", Type: WrapperTypeAssertion, Kind: events.EvalKindAssertion,
			Score: &score, Passed: boolPtr(true),
		})
		data := nextEvent(t)
		if data.Passed == nil {
			t.Fatal("the assertion stated a pass/fail and it did not reach the event")
		}
		if !*data.Passed {
			t.Error("score 0.7 overruled the assertion's own pass — the derivation is back")
		}
	})

	t.Run("a high-scoring eval still states nothing", func(t *testing.T) {
		score := 0.9
		r.emitResult(nil, &EvalResult{
			EvalID: "judge", Type: "llm_judge", Kind: events.EvalKindEval, Score: &score,
		})
		data := nextEvent(t)
		if data.Passed != nil {
			t.Errorf("an eval was reported as passed=%v. Evals measure; only a wrapper judges",
				*data.Passed)
		}
	})
}

// TestEvalRunner_EmitResult_Lossless1028 pins the #1028 contract: the event
// payload carries Trigger, Details, and full structured Violations, not the
// previously-flattened []string. Removing any of these would have to update
// this test (and the consumer-facing payload).
func TestEvalRunner_EmitResult_Lossless1028(t *testing.T) {
	reg := NewEvalTypeRegistry()
	bus := events.NewEventBus()
	defer bus.Close()

	received := make(chan *events.Event, 10)
	bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) {
		received <- e
	})

	emitter := events.NewEmitter(bus, "", "", "")
	r := NewEvalRunner(reg, WithEmitter(emitter))

	def := &EvalDef{ID: "e1", Type: "test", Trigger: TriggerEveryTurn}
	r.emitResult(def, &EvalResult{
		EvalID:  "e1",
		Type:    "test",
		Score:   func() *float64 { v := 1.0; return &v }(),
		Details: map[string]any{"per_criterion": []float64{0.9, 0.95}, "model": "judge-v1"},
		Violations: []EvalViolation{
			{
				TurnIndex:   2,
				Description: "tool args drifted",
				Evidence:    map[string]any{"expected": "x", "actual": "y"},
			},
			{TurnIndex: 5, Description: "format mismatch"},
		},
	})

	select {
	case e := <-received:
		data := e.Data.(*events.EvalCompletedData)
		if data.Trigger != string(TriggerEveryTurn) {
			t.Errorf("Trigger = %q, want %q", data.Trigger, TriggerEveryTurn)
		}
		if data.Details["model"] != "judge-v1" {
			t.Errorf("Details[\"model\"] = %v, want %q", data.Details["model"], "judge-v1")
		}
		if len(data.Violations) != 2 {
			t.Fatalf("Violations len = %d, want 2", len(data.Violations))
		}
		v0 := data.Violations[0]
		if v0.TurnIndex != 2 || v0.Description != "tool args drifted" {
			t.Errorf("Violation[0] = %+v, want TurnIndex=2 Description=\"tool args drifted\"", v0)
		}
		if v0.Evidence["expected"] != "x" || v0.Evidence["actual"] != "y" {
			t.Errorf("Violation[0].Evidence lost evidence map")
		}
		v1 := data.Violations[1]
		if v1.TurnIndex != 5 || v1.Description != "format mismatch" {
			t.Errorf("Violation[1] = %+v, want TurnIndex=5 Description=\"format mismatch\"", v1)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for EvalCompleted event")
	}
}

// TestEvalRunner_EmitResult_TriggerOmittedWhenNilDef sanity-checks the
// nil-def fallback (used by the existing test scaffolding above).
func TestEvalRunner_EmitResult_TriggerOmittedWhenNilDef(t *testing.T) {
	reg := NewEvalTypeRegistry()
	bus := events.NewEventBus()
	defer bus.Close()

	received := make(chan *events.Event, 10)
	bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) {
		received <- e
	})

	emitter := events.NewEmitter(bus, "", "", "")
	r := NewEvalRunner(reg, WithEmitter(emitter))

	r.emitResult(nil, &EvalResult{
		EvalID: "e1", Type: "test",
		Score: func() *float64 { v := 1.0; return &v }(),
	})

	select {
	case e := <-received:
		data := e.Data.(*events.EvalCompletedData)
		if data.Trigger != "" {
			t.Errorf("Trigger should be empty when def is nil, got %q", data.Trigger)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

// TestEvalRunner_EmitResult_GradedEvalIsNotAVerdict is the #1858 reproduction:
// an llm_judge scoring 0.9 must not be emitted as a FAILURE.
//
// llm_judge returns a Score and leaves Value non-bool, exactly as modelled
// here. The old fallback derived pass/fail from `score >= 1.0`, so 0.9 emitted
// passed=false — and 0.99 did too. Only a perfect score passed.
//
// An eval returns a value; it does not pass or fail. Only an assertion — an
// eval whose value IS a bool — carries a verdict. So the correct emission for a
// graded eval carries the score and no verdict at all.
func TestEvalRunner_EmitResult_GradedEvalIsNotAVerdict(t *testing.T) {
	reg := NewEvalTypeRegistry()
	bus := events.NewEventBus()
	defer bus.Close()

	received := make(chan *events.Event, 10)
	bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) { received <- e })

	emitter := events.NewEmitter(bus, "run1", "sess1", "conv1")
	r := NewEvalRunner(reg, WithEmitter(emitter))

	score := 0.9
	r.emitResult(nil, &EvalResult{
		EvalID: "response-quality",
		Type:   "llm_judge",
		Score:  &score,
		// The judge's own note, as llm_judge actually returns it: a detail,
		// not a verdict.
		Details: map[string]any{"passed": true, "reasoning": "clear and correct"},
	})

	select {
	case e := <-received:
		data := e.Data.(*events.EvalCompletedData)
		if data.Passed != nil {
			t.Errorf("a judge scoring 0.9 was emitted with a verdict (passed=%v); "+
				"evals score, they do not pass or fail", *data.Passed)
		}
		if data.Score == nil || *data.Score != 0.9 {
			t.Error("the score must still be carried")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

// TestEvalRunner_EmitResult_KindStatesTheRole pins the point of the enum: a
// consumer is TOLD what a result is, rather than inferring it.
//
// The role used to be inferred here, by string-matching result.Type against the
// two wrapper names. That is a coincidence, not a fact: Type carries the
// handler name, and any third wrapper a consumer registers reported as a plain
// eval. The last case below is the one that catches the inference coming back —
// its Type says "assertion" while its Kind says eval, so a string match and a
// copy give different answers.
func TestEvalRunner_EmitResult_KindStatesTheRole(t *testing.T) {
	reg := NewEvalTypeRegistry()
	bus := events.NewEventBus()
	defer bus.Close()

	received := make(chan *events.Event, 10)
	bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) { received <- e })

	emitter := events.NewEmitter(bus, "run1", "sess1", "conv1")
	r := NewEvalRunner(reg, WithEmitter(emitter))

	score := 0.9
	for _, tc := range []struct {
		name       string
		result     *EvalResult
		wantKind   events.EvalKind
		wantPassed *bool
	}{
		{
			name: "bare eval scores and does not judge",
			result: &EvalResult{
				EvalID: "judge", Type: "llm_judge", Kind: events.EvalKindEval, Score: &score,
			},
			wantKind: events.EvalKindEval,
		},
		{
			name: "assertion carries the pass it decided",
			result: &EvalResult{
				EvalID: "a1", Type: WrapperTypeAssertion, Kind: events.EvalKindAssertion,
				Score: &score, Passed: boolPtr(true),
			},
			wantKind: events.EvalKindAssertion, wantPassed: boolPtr(true),
		},
		{
			name: "guardrail that fired carries the fail",
			result: &EvalResult{
				EvalID: "g1", Type: WrapperTypeGuardrail, Kind: events.EvalKindGuardrail,
				Score: &score, Passed: boolPtr(false),
			},
			wantKind: events.EvalKindGuardrail, wantPassed: boolPtr(false),
		},
		{
			name: "the stated role wins over a handler type that looks like a wrapper",
			result: &EvalResult{
				EvalID: "odd", Type: WrapperTypeAssertion, Kind: events.EvalKindEval, Score: &score,
			},
			wantKind: events.EvalKindEval,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r.emitResult(nil, tc.result)
			select {
			case e := <-received:
				data := e.Data.(*events.EvalCompletedData)
				if data.Kind != tc.wantKind {
					t.Errorf("kind = %q, want %q — the role must be copied from the result, "+
						"not matched off its handler type", data.Kind, tc.wantKind)
				}
				switch {
				case tc.wantPassed == nil && data.Passed != nil:
					t.Errorf("kind %q carried passed=%v; only a coercing role states one",
						data.Kind, *data.Passed)
				case tc.wantPassed != nil && data.Passed == nil:
					t.Errorf("kind %q stated a pass/fail that never reached the event", data.Kind)
				case tc.wantPassed != nil && *data.Passed != *tc.wantPassed:
					t.Errorf("passed = %v, want %v", *data.Passed, *tc.wantPassed)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("timed out")
			}
		})
	}
}
