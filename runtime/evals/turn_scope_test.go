package evals_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers" // registers the built-in handlers
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// turnWithCalls is a turn that calls lookup, which succeeds with text and an
// image, and fetch, which fails.
func turnWithCalls() []types.Message {
	return []types.Message{
		{Role: "user", Content: "find x"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{
			{ID: "c1", Name: "lookup", Args: json.RawMessage(`{"q":"x"}`)},
			{ID: "c2", Name: "fetch", Args: json.RawMessage(`{"url":"u"}`)},
		}},
		{Role: "tool", ToolResult: &types.MessageToolResult{
			ID: "c1", Name: "lookup",
			Parts: []types.ContentPart{
				types.NewTextPart("found x"),
				types.NewImagePartFromData("aGk=", "image/png", nil),
			},
		}},
		{Role: "tool", ToolResult: &types.MessageToolResult{
			ID: "c2", Name: "fetch", Error: "boom", ErrorType: types.ToolErrorExecution,
		}},
		{Role: "assistant", Content: "x is found"},
	}
}

// turnWithoutCalls is a turn that calls no tool.
func turnWithoutCalls() []types.Message {
	return []types.Message{
		{Role: "user", Content: "thanks"},
		{Role: "assistant", Content: "you're welcome"},
	}
}

func runTurnEval(t *testing.T, def evals.EvalDef, messages []types.Message) evals.EvalResult {
	t.Helper()
	if def.Trigger == "" {
		def.Trigger = evals.TriggerEveryTurn
	}
	runner := evals.NewEvalRunner(evals.NewEvalTypeRegistry())
	evalCtx := evals.BuildEvalContext(messages, 1, "s1", "p1", nil)
	results := runner.RunTurnEvals(context.Background(), []evals.EvalDef{def}, evalCtx)
	require.Len(t, results, 1)
	return results[0]
}

func runSessionEval(t *testing.T, def evals.EvalDef, messages []types.Message) evals.EvalResult {
	t.Helper()
	runner := evals.NewEvalRunner(evals.NewEvalTypeRegistry())
	evalCtx := evals.BuildEvalContext(messages, 1, "s1", "p1", nil)
	results := runner.RunSessionEvals(context.Background(), []evals.EvalDef{def}, evalCtx)
	require.Len(t, results, 1)
	return results[0]
}

func scoreOf(t *testing.T, r evals.EvalResult) float64 {
	t.Helper()
	require.Empty(t, r.Error)
	require.NotNil(t, r.Score, "result %+v has no score", r)
	return *r.Score
}

// verdict is what a caller reads off a result, without the timing.
type verdict struct {
	Score       *float64
	Explanation string
	Skipped     bool
	Error       string
}

func verdictOf(r evals.EvalResult) verdict {
	return verdict{Score: r.Score, Explanation: r.Explanation, Skipped: r.Skipped, Error: r.Error}
}

// A turn-level tool eval run on turn 2 judges turn 2 as if turn 1, and its
// calls, never happened (#2927). Each type is given params that turn 1's calls
// would change the verdict of, which the precondition checks, so the case
// fails if the calls of an earlier turn reach the handler.
func TestTurnEvals_TurnLevelToolTypesJudgeOnlyTheCurrentTurn(t *testing.T) {
	cases := []struct {
		evalType string
		params   map[string]any
	}{
		{"tools_called", map[string]any{"tool_names": []any{"lookup"}}},
		{"tools_not_called", map[string]any{"tool_names": []any{"lookup"}}},
		{"tool_call_count", map[string]any{"tool": "lookup", "min": 1}},
		{"tool_args", map[string]any{"tool_name": "lookup", "expected_args": map[string]any{"q": "x"}}},
		{"tool_calls_with_args", map[string]any{"tool_name": "lookup", "result_includes": []any{"found"}}},
		{"tool_result_includes", map[string]any{"tool": "lookup", "patterns": []any{"found"}}},
		{"tool_result_matches", map[string]any{"tool": "lookup", "pattern": "found"}},
		{"tool_result_has_media", map[string]any{"tool": "lookup", "media_type": "image"}},
		{"tool_result_media_type", map[string]any{"tool": "lookup", "mime_type": "image/png"}},
		{"no_tool_errors", map[string]any{"tools": []any{"fetch"}}},
	}
	for _, tc := range cases {
		t.Run(tc.evalType, func(t *testing.T) {
			def := evals.EvalDef{ID: tc.evalType, Type: tc.evalType, Params: tc.params}

			withCalls := verdictOf(runTurnEval(t, def, turnWithCalls()))
			alone := verdictOf(runTurnEval(t, def, turnWithoutCalls()))
			require.NotEqual(t, withCalls, alone,
				"precondition: turn 1's calls must change this eval's verdict, or the case proves nothing")

			history := append(turnWithCalls(), turnWithoutCalls()...)
			assert.Equal(t, alone, verdictOf(runTurnEval(t, def, history)),
				"turn 2 must be judged without turn 1's calls")
		})
	}
}

// The repro from #2927, stated as the pack author sees it.
func TestTurnEvals_ToolsNotCalledPassesOnALaterTurnThatMadeNoCall(t *testing.T) {
	def := evals.EvalDef{ID: "no-lookup", Type: "tools_not_called",
		Params: map[string]any{"tool_names": []any{"lookup"}}}
	history := append(turnWithCalls(), turnWithoutCalls()...)

	assert.Equal(t, 1.0, scoreOf(t, runTurnEval(t, def, history)))
	assert.Equal(t, 0.0, scoreOf(t, runTurnEval(t, def, turnWithCalls())),
		"the turn that called lookup still fails")
}

// A session-level type judges the whole conversation even when it runs on
// every turn.
func TestTurnEvals_SessionLevelToolTypesStillSeeEarlierTurns(t *testing.T) {
	def := evals.EvalDef{ID: "lookup-ever", Type: "tools_called_session",
		Params: map[string]any{"tool_names": []any{"lookup"}}}
	history := append(turnWithCalls(), turnWithoutCalls()...)

	assert.Equal(t, 1.0, scoreOf(t, runTurnEval(t, def, history)))
	assert.Equal(t, 0.0, scoreOf(t, runTurnEval(t, def, turnWithoutCalls())),
		"precondition: without turn 1 the session never called lookup")
}

// A wrapped turn-level type is scoped like a direct one.
func TestTurnEvals_AssertionWrappedTurnLevelTypeJudgesOnlyTheCurrentTurn(t *testing.T) {
	def := evals.EvalDef{ID: "wrapped", Type: evals.WrapperTypeAssertion, Params: map[string]any{
		"eval_type":   "tools_not_called",
		"eval_params": map[string]any{"tool_names": []any{"lookup"}},
	}}
	history := append(turnWithCalls(), turnWithoutCalls()...)

	result := runTurnEval(t, def, history)
	require.NotNil(t, result.Passed)
	assert.True(t, *result.Passed)

	result = runTurnEval(t, def, turnWithCalls())
	require.NotNil(t, result.Passed)
	assert.False(t, *result.Passed, "the turn that called lookup still fails")
}

// A when gate on a turn-triggered eval asks about the turn being judged, so a
// tool called on an earlier turn does not open it.
func TestTurnEvals_WhenGateSeesOnlyTheCurrentTurn(t *testing.T) {
	def := evals.EvalDef{ID: "after-lookup", Type: "contains",
		Params: map[string]any{"patterns": []any{"welcome"}},
		When:   map[string]any{"tool_called": "lookup"}}
	history := append(turnWithCalls(), turnWithoutCalls()...)

	result := runTurnEval(t, def, history)
	assert.True(t, result.Skipped, "turn 2 made no lookup call")
	assert.Equal(t, `tool "lookup" not called`, result.SkipReason)

	result = runTurnEval(t, def, turnWithCalls())
	assert.False(t, result.Skipped, "turn 1 called lookup, so the gate opens")
}

// A session-triggered eval keeps judging the whole conversation, whatever its
// type: only turn triggers narrow to the current turn.
func TestSessionEvals_TurnLevelToolTypeStillSeesTheWholeSession(t *testing.T) {
	def := evals.EvalDef{ID: "lookup", Type: "tools_called", Trigger: evals.TriggerOnSessionComplete,
		Params: map[string]any{"tool_names": []any{"lookup"}}}
	history := append(turnWithCalls(), turnWithoutCalls()...)

	assert.Equal(t, 1.0, scoreOf(t, runSessionEval(t, def, history)))
}

// A wrapper around a session-level type is not narrowed.
func TestTurnEvals_AssertionWrappedSessionLevelTypeStillSeesEarlierTurns(t *testing.T) {
	def := evals.EvalDef{ID: "wrapped", Type: evals.WrapperTypeAssertion, Params: map[string]any{
		"eval_type":   "tools_called_session",
		"eval_params": map[string]any{"tool_names": []any{"lookup"}},
	}}
	history := append(turnWithCalls(), turnWithoutCalls()...)

	result := runTurnEval(t, def, history)
	require.NotNil(t, result.Passed)
	assert.True(t, *result.Passed, "the session called lookup on turn 1")
}

// Without a user message the turn boundary cannot be seen, so every call
// counts as the current turn's.
func TestCurrentTurnToolCalls_NoUserMessageKeepsEveryCall(t *testing.T) {
	messages := turnWithCalls()[1:]
	evalCtx := evals.BuildEvalContext(messages, 0, "s1", "p1", nil)

	names := []string{}
	for _, tc := range evals.CurrentTurnToolCalls(evalCtx) {
		names = append(names, tc.ToolName)
	}
	assert.Equal(t, []string{"lookup", "fetch"}, names)
}

// ScopeToTurn hands back a narrowed copy and leaves the caller's context, which
// the runner reuses for the rest of the batch, as it was.
func TestScopeToTurn_LeavesTheCallersContextWhole(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()
	handler, err := registry.Get("tools_called")
	require.NoError(t, err)
	history := append(turnWithCalls(), turnWithoutCalls()...)
	evalCtx := evals.BuildEvalContext(history, 1, "s1", "p1", nil)

	scoped := evals.ScopeToTurn(handler, nil, evalCtx)

	assert.Empty(t, scoped.ToolCalls)
	assert.Len(t, evalCtx.ToolCalls, 2, "the caller's context keeps turn 1's calls")
}

// A provider that delivers the caller's transcript after the reply (OpenAI
// Realtime, without reordering) stores it after that reply, and the turn is
// reported once it arrives. The trailing user message is the judged turn's own,
// so the turn's calls before it still count.
func TestTurnEvals_LateTranscriptKeepsTheTurnsCalls(t *testing.T) {
	earlier := []types.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "c0", Name: "fetch"}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "c0", Name: "fetch"}},
		{Role: "assistant", Content: "hello"},
	}
	lateTurn := []types.Message{
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "lookup"}}},
		{Role: "tool", ToolResult: &types.MessageToolResult{ID: "c1", Name: "lookup", Parts: []types.ContentPart{
			types.NewTextPart("found x"),
		}}},
		{Role: "assistant", Content: "x is found"},
		{Role: "user", Content: "find x"},
	}
	history := append(earlier, lateTurn...)

	called := evals.EvalDef{ID: "lookup", Type: "tools_called",
		Params: map[string]any{"tool_names": []any{"lookup"}}}
	assert.Equal(t, 1.0, scoreOf(t, runTurnEval(t, called, history)), "the late turn called lookup")

	notFetch := evals.EvalDef{ID: "no-fetch", Type: "tools_not_called",
		Params: map[string]any{"tool_names": []any{"fetch"}}}
	assert.Equal(t, 1.0, scoreOf(t, runTurnEval(t, notFetch, history)), "fetch was the earlier turn's call")

	gated := evals.EvalDef{ID: "gated", Type: "contains",
		Params: map[string]any{"patterns": []any{"found"}},
		When:   map[string]any{"tool_called": "lookup"}}
	assert.False(t, runTurnEval(t, gated, history).Skipped, "the late turn called lookup, so the gate opens")
}

// scopeProbe is a custom handler that judges one turn only when its scope
// param says so, and scores 1 when it sees no tool calls.
type scopeProbe struct{}

func (scopeProbe) Type() string { return "scope_probe" }

func (scopeProbe) ScopedToTurn(params map[string]any) bool { return params["scope"] == "turn" }

func (scopeProbe) Eval(_ context.Context, evalCtx *evals.EvalContext, _ map[string]any) (*evals.EvalResult, error) {
	score := 1.0
	if len(evalCtx.ToolCalls) > 0 {
		score = 0
	}
	return &evals.EvalResult{Score: &score}, nil
}

// A wrapper asks the wrapped handler whether it judges one turn with the params
// it will call it with, its defaults applied, as a direct declaration does.
func TestTurnEvals_WrapperAppliesTheWrappedTypesDefaultsToItsScope(t *testing.T) {
	evals.ParamDefaults["scope_probe"] = map[string]any{"scope": "turn"}
	t.Cleanup(func() { delete(evals.ParamDefaults, "scope_probe") })
	registry := evals.NewEvalTypeRegistry()
	registry.Register(scopeProbe{})
	def := evals.EvalDef{ID: "wrapped", Type: evals.WrapperTypeAssertion, Trigger: evals.TriggerEveryTurn,
		Params: map[string]any{"eval_type": "scope_probe"}}
	history := append(turnWithCalls(), turnWithoutCalls()...)

	runner := evals.NewEvalRunner(registry)
	evalCtx := evals.BuildEvalContext(history, 1, "s1", "p1", nil)
	results := runner.RunTurnEvals(context.Background(), []evals.EvalDef{def}, evalCtx)

	require.Len(t, results, 1)
	require.NotNil(t, results[0].Passed)
	assert.True(t, *results[0].Passed, "the default scope: turn must hide turn 1's calls")
}
