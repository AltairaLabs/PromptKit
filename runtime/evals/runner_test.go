package evals

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/runtime/logger"
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
	r := NewEvalRunner(reg, WithTimeout(5*time.Second))

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

	r.emitResult(&EvalResult{
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
		if !data.Passed {
			t.Error("expected passed=true for score 1.0")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}

func TestEvalRunner_EmitResult_UsesValueForPassed(t *testing.T) {
	reg := NewEvalTypeRegistry()
	bus := events.NewEventBus()
	defer bus.Close()

	received := make(chan *events.Event, 10)
	bus.Subscribe(events.EventEvalCompleted, func(e *events.Event) {
		received <- e
	})

	emitter := events.NewEmitter(bus, "", "", "")
	r := NewEvalRunner(reg, WithEmitter(emitter))

	// Score is 0.7 (below 1.0) but Value is true (threshold passed)
	r.emitResult(&EvalResult{
		EvalID: "e1",
		Type:   "test",
		Score:  func() *float64 { v := 0.7; return &v }(),
		Value:  true,
	})

	select {
	case e := <-received:
		data := e.Data.(*events.EvalCompletedData)
		if !data.Passed {
			t.Error("expected passed=true from Value, not score")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out")
	}
}
