package sdk

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
)

// testDispatcher records dispatch calls.
type testDispatcher struct {
	mu           sync.Mutex
	turnCalls    int
	sessionCalls int
	turnCh       chan struct{}
	sessionCh    chan struct{}
}

func newTestDispatcher() *testDispatcher {
	return &testDispatcher{
		turnCh:    make(chan struct{}, 100),
		sessionCh: make(chan struct{}, 100),
	}
}

func (d *testDispatcher) DispatchTurnEvals(
	_ context.Context, _ []evals.EvalDef, _ *evals.EvalContext,
) ([]evals.EvalResult, error) {
	d.mu.Lock()
	d.turnCalls++
	d.mu.Unlock()
	d.turnCh <- struct{}{}
	return nil, nil
}

func (d *testDispatcher) DispatchSessionEvals(
	_ context.Context, _ []evals.EvalDef, _ *evals.EvalContext,
) ([]evals.EvalResult, error) {
	d.mu.Lock()
	d.sessionCalls++
	d.mu.Unlock()
	d.sessionCh <- struct{}{}
	return nil, nil
}

func (d *testDispatcher) TurnCalls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.turnCalls
}

func (d *testDispatcher) SessionCalls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sessionCalls
}

// testResultWriter records written results.
type testResultWriter struct {
	mu      sync.Mutex
	results []evals.EvalResult
	calls   int
}

func (w *testResultWriter) WriteResults(_ context.Context, results []evals.EvalResult) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls++
	w.results = append(w.results, results...)
	return nil
}

func TestNewEvalMiddleware_NilDispatcherReturnsNil(t *testing.T) {
	conv := &Conversation{
		config: &config{},
		pack:   &pack.Pack{},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw != nil {
		t.Error("expected nil middleware when no dispatcher configured")
	}
}

func TestNewEvalMiddleware_NoDefsReturnsNil(t *testing.T) {
	conv := &Conversation{
		config: &config{
			evalDispatcher: &evals.NoOpDispatcher{},
		},
		pack:   &pack.Pack{}, // No evals
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw != nil {
		t.Error("expected nil middleware when no eval defs")
	}
}

func TestNewEvalMiddleware_WithDefs(t *testing.T) {
	conv := &Conversation{
		config: &config{
			evalDispatcher: &evals.NoOpDispatcher{},
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware when defs exist")
	}
	if len(mw.defs) != 1 {
		t.Errorf("expected 1 def, got %d", len(mw.defs))
	}
}

func TestEvalMiddleware_NilMiddlewareSafeNoOp(t *testing.T) {
	// Should not panic
	var mw *evalMiddleware
	mw.dispatchTurnEvals(context.Background())
	mw.dispatchSessionEvals(context.Background())
}

func TestEvalMiddleware_ResolvesPackAndPromptEvals(t *testing.T) {
	conv := &Conversation{
		config: &config{
			evalDispatcher: &evals.NoOpDispatcher{},
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "a", Type: "contains", Trigger: evals.TriggerEveryTurn},
				{ID: "b", Type: "regex", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{
			Evals: []evals.EvalDef{
				{ID: "b", Type: "regex_override", Trigger: evals.TriggerEveryTurn}, // Override
				{ID: "c", Type: "length", Trigger: evals.TriggerOnSessionComplete},
			},
		},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	// Should be: a (from pack), b_override (from prompt), c (from prompt)
	if len(mw.defs) != 3 {
		t.Fatalf("expected 3 resolved defs, got %d", len(mw.defs))
	}
	if mw.defs[0].ID != "a" {
		t.Errorf("expected first def ID 'a', got %q", mw.defs[0].ID)
	}
	if mw.defs[1].Type != "regex_override" {
		t.Errorf("expected second def type 'regex_override', got %q", mw.defs[1].Type)
	}
	if mw.defs[2].ID != "c" {
		t.Errorf("expected third def ID 'c', got %q", mw.defs[2].ID)
	}
}

func TestEvalMiddleware_MultipleResultWritersComposed(t *testing.T) {
	w1 := &testResultWriter{}
	w2 := &testResultWriter{}

	conv := &Conversation{
		config: &config{
			evalDispatcher:    &evals.NoOpDispatcher{},
			evalResultWriters: []evals.ResultWriter{w1, w2},
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	// Verify it's a composite writer
	if _, ok := mw.resultWriter.(*evals.CompositeResultWriter); !ok {
		t.Error("expected CompositeResultWriter when multiple writers provided")
	}
}

func TestJudgeProvider_Interface(t *testing.T) {
	// Verify JudgeProvider satisfies the interface
	var _ handlers.JudgeProvider = (*JudgeProvider)(nil)
}

func TestParseJudgeResponse_ValidJSON(t *testing.T) {
	raw := `{"passed": true, "score": 0.85, "reasoning": "Good response"}`
	result, err := parseJudgeResponse(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Error("expected passed")
	}
	if result.Score != 0.85 {
		t.Errorf("expected score 0.85, got %f", result.Score)
	}
}

func TestParseJudgeResponse_MarkdownWrapped(t *testing.T) {
	raw := "```json\n{\"passed\": false, \"score\": 0.2, \"reasoning\": \"Bad\"}\n```"
	result, err := parseJudgeResponse(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Error("expected not passed")
	}
}

func TestParseJudgeResponse_MinScoreOverride(t *testing.T) {
	raw := `{"score": 0.7, "reasoning": "OK"}`
	minScore := 0.8
	result, err := parseJudgeResponse(raw, &minScore)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Error("expected not passed when score < minScore")
	}
}

func TestParseJudgeResponse_InvalidJSON(t *testing.T) {
	raw := "This is not JSON at all"
	result, err := parseJudgeResponse(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Fallback behavior
	if !result.Passed {
		t.Error("expected fallback to passed=true")
	}
	if result.Score != defaultPassThreshold {
		t.Errorf("expected fallback score %f, got %f", defaultPassThreshold, result.Score)
	}
}

func TestParseJudgeResponse_DefaultThresholdPassed(t *testing.T) {
	raw := `{"score": 0.7, "reasoning": "OK"}`
	result, err := parseJudgeResponse(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Error("expected passed when score >= default threshold")
	}
}

func TestParseJudgeResponse_DefaultThresholdFailed(t *testing.T) {
	raw := `{"score": 0.3, "reasoning": "Bad"}`
	result, err := parseJudgeResponse(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Error("expected not passed when score < default threshold")
	}
}

func TestParseJudgeResponse_MinScorePassesAbove(t *testing.T) {
	raw := `{"score": 0.9, "reasoning": "Great"}`
	minScore := 0.8
	result, err := parseJudgeResponse(raw, &minScore)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Error("expected passed when score >= minScore")
	}
}

// evalMockProvider implements providers.Provider for testing Judge.
type evalMockProvider struct {
	predictResp providers.PredictionResponse
	predictErr  error
}

func (m *evalMockProvider) ID() string    { return "mock" }
func (m *evalMockProvider) Model() string { return "mock-model" }
func (m *evalMockProvider) Predict(_ context.Context, _ providers.PredictionRequest) (providers.PredictionResponse, error) {
	return m.predictResp, m.predictErr
}
func (m *evalMockProvider) PredictStream(_ context.Context, _ providers.PredictionRequest) (<-chan providers.StreamChunk, error) {
	return nil, errors.New("not implemented")
}
func (m *evalMockProvider) SupportsStreaming() bool       { return false }
func (m *evalMockProvider) ShouldIncludeRawOutput() bool  { return false }
func (m *evalMockProvider) Close() error                  { return nil }
func (m *evalMockProvider) CalculateCost(_, _, _ int) types.CostInfo {
	return types.CostInfo{}
}

func TestNewJudgeProvider(t *testing.T) {
	mp := &evalMockProvider{}
	jp := NewJudgeProvider(mp)
	if jp == nil {
		t.Fatal("expected non-nil JudgeProvider")
	}
	if jp.provider == nil {
		t.Error("expected provider to be set")
	}
}

func TestJudgeProvider_Judge_Success(t *testing.T) {
	mp := &evalMockProvider{
		predictResp: providers.PredictionResponse{
			Content: `{"passed": true, "score": 0.9, "reasoning": "Excellent"}`,
		},
	}
	jp := NewJudgeProvider(mp)

	result, err := jp.Judge(context.Background(), handlers.JudgeOpts{
		Content:  "Hello world",
		Criteria: "Is it a greeting?",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Error("expected passed")
	}
	if result.Score != 0.9 {
		t.Errorf("expected score 0.9, got %f", result.Score)
	}
}

func TestJudgeProvider_Judge_WithRubricAndSystemPrompt(t *testing.T) {
	mp := &evalMockProvider{
		predictResp: providers.PredictionResponse{
			Content: `{"passed": true, "score": 1.0, "reasoning": "Perfect"}`,
		},
	}
	jp := NewJudgeProvider(mp)

	result, err := jp.Judge(context.Background(), handlers.JudgeOpts{
		Content:      "Hello world",
		Criteria:     "Is it a greeting?",
		Rubric:       "Must say hello",
		SystemPrompt: "Custom system prompt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Passed {
		t.Error("expected passed")
	}
}

func TestJudgeProvider_Judge_ProviderError(t *testing.T) {
	mp := &evalMockProvider{
		predictErr: errors.New("provider failure"),
	}
	jp := NewJudgeProvider(mp)

	_, err := jp.Judge(context.Background(), handlers.JudgeOpts{
		Content:  "test",
		Criteria: "test",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, mp.predictErr) {
		t.Errorf("expected wrapped provider error, got: %v", err)
	}
}

func TestEvalMiddleware_SingleResultWriter(t *testing.T) {
	w := &testResultWriter{}

	conv := &Conversation{
		config: &config{
			evalDispatcher:    &evals.NoOpDispatcher{},
			evalResultWriters: []evals.ResultWriter{w},
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	// Single writer should be used directly, not wrapped
	if mw.resultWriter != w {
		t.Error("expected single writer to be used directly")
	}
}

func TestEvalMiddleware_NoResultWriters(t *testing.T) {
	conv := &Conversation{
		config: &config{
			evalDispatcher: &evals.NoOpDispatcher{},
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}
	if mw.resultWriter != nil {
		t.Error("expected nil result writer")
	}
}

func TestEvalMiddleware_NilConfig(t *testing.T) {
	conv := &Conversation{
		config: nil,
	}

	mw := newEvalMiddleware(conv)
	if mw != nil {
		t.Error("expected nil middleware when config is nil")
	}
}

func TestEvalMiddleware_NilPack(t *testing.T) {
	conv := &Conversation{
		config: &config{
			evalDispatcher: &evals.NoOpDispatcher{},
		},
		pack:   nil,
		prompt: nil,
	}

	mw := newEvalMiddleware(conv)
	if mw != nil {
		t.Error("expected nil middleware when pack and prompt are nil")
	}
}

func TestEvalMiddleware_BuildEvalContext_NoSession(t *testing.T) {
	conv := &Conversation{
		config: &config{
			evalDispatcher: &evals.NoOpDispatcher{},
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt:     &pack.Prompt{},
		promptName: "my-prompt",
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	mw.turnIndex = 3
	ctx := mw.buildEvalContext()

	if ctx.TurnIndex != 3 {
		t.Errorf("expected TurnIndex 3, got %d", ctx.TurnIndex)
	}
	if ctx.PromptID != "my-prompt" {
		t.Errorf("expected PromptID 'my-prompt', got %q", ctx.PromptID)
	}
	if ctx.SessionID != "" {
		t.Errorf("expected empty SessionID, got %q", ctx.SessionID)
	}
	if len(ctx.Messages) != 0 {
		t.Errorf("expected no messages, got %d", len(ctx.Messages))
	}
}

// errorDispatcher returns errors on dispatch.
type errorDispatcher struct {
	turnErr    error
	sessionErr error
}

func (d *errorDispatcher) DispatchTurnEvals(
	_ context.Context, _ []evals.EvalDef, _ *evals.EvalContext,
) ([]evals.EvalResult, error) {
	return nil, d.turnErr
}

func (d *errorDispatcher) DispatchSessionEvals(
	_ context.Context, _ []evals.EvalDef, _ *evals.EvalContext,
) ([]evals.EvalResult, error) {
	return nil, d.sessionErr
}

func TestEvalMiddleware_DispatchTurnEvalsError(t *testing.T) {
	conv := &Conversation{
		config: &config{
			evalDispatcher: &errorDispatcher{turnErr: errors.New("turn error")},
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	// Should not panic on error — runs async so we can't easily check,
	// but at least verify it doesn't crash
	mw.dispatchTurnEvals(context.Background())
}

func TestEvalMiddleware_DispatchSessionEvalsError(t *testing.T) {
	conv := &Conversation{
		config: &config{
			evalDispatcher: &errorDispatcher{sessionErr: errors.New("session error")},
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	// Should not panic on error — runs synchronously
	mw.dispatchSessionEvals(context.Background())
}

// errorResultWriter returns an error on WriteResults.
type errorResultWriter struct{}

func (w *errorResultWriter) WriteResults(_ context.Context, _ []evals.EvalResult) error {
	return errors.New("write error")
}

// returningDispatcher always returns results.
type returningDispatcher struct {
	results []evals.EvalResult
}

func (d *returningDispatcher) DispatchTurnEvals(
	_ context.Context, _ []evals.EvalDef, _ *evals.EvalContext,
) ([]evals.EvalResult, error) {
	return d.results, nil
}

func (d *returningDispatcher) DispatchSessionEvals(
	_ context.Context, _ []evals.EvalDef, _ *evals.EvalContext,
) ([]evals.EvalResult, error) {
	return d.results, nil
}

func TestEvalMiddleware_SessionEvalsResultWriterError(t *testing.T) {
	conv := &Conversation{
		config: &config{
			evalDispatcher: &returningDispatcher{
				results: []evals.EvalResult{{EvalID: "e1", Passed: true}},
			},
			evalResultWriters: []evals.ResultWriter{&errorResultWriter{}},
		},
		pack: &pack.Pack{
			Evals: []evals.EvalDef{
				{ID: "e1", Type: "contains", Trigger: evals.TriggerEveryTurn},
			},
		},
		prompt: &pack.Prompt{},
	}

	mw := newEvalMiddleware(conv)
	if mw == nil {
		t.Fatal("expected non-nil middleware")
	}

	// Should not panic even when result writer errors
	mw.dispatchSessionEvals(context.Background())
}

