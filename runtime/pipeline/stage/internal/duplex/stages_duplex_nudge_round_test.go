package duplex

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// These cover a realtime round the repeated-identical-call breaker nudges:
// its other calls run as any round's do, under the per-turn call budget,
// counted for rejection and changing the session's tools.

func argsCall(id, name, args string) types.MessageToolCall {
	return types.MessageToolCall{ID: id, Name: name, Args: json.RawMessage(args)}
}

// nudgeText is the breaker's result for a call to name repeated twice.
func nudgeText(name string) string {
	return "Repeated call ignored: \"" + name + "\" has been called 2 times with identical arguments and was " +
		"NOT executed again — the result will not change. Stop repeating this exact call: change the " +
		"arguments, use a different tool, or finish the task."
}

// With max_tool_calls_per_turn spent, a nudged round's new call is refused as
// over the budget, not run.
func TestDuplexNudgeRound_NewCallOverTheSpentBudgetIsRefused(t *testing.T) {
	exec := &grantingExec{}
	reg := registryWithTools(t, "weather")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		policy:    &pipeline.ToolPolicy{MaxToolCallsPerTurn: 1, MaxIdenticalToolCalls: 2},
		turnState: &core.TurnState{AllowedTools: []string{"weather"}},
	})

	h.emit(toolCallTurn(argsCall("c1", "weather", `{"city":"Paris"}`)))
	h.awaitSent(1)
	h.emit(toolCallTurn(argsCall("c2", "weather", `{"city":"Paris"}`), argsCall("c3", "weather", `{"city":"Rome"}`)))
	sent := h.awaitSent(2)
	h.finish()

	assert.Equal(t, []providers.ToolResponse{
		{ToolCallID: "c2", Result: nudgeText("weather")},
		{ToolCallID: "c3", Result: "Tool weather not executed: the tool call limit for this turn is reached", IsError: true},
	}, sent[1])
	assert.Equal(t, []string{"weather"}, exec.calls(), "only the call within the budget ran")
}

// A grant made by a call in a nudged round puts its tool on the session, and
// the next round's call to it runs.
func TestDuplexNudgeRound_GrantExtendsTheSessionsTools(t *testing.T) {
	exec := &grantingExec{grant: []string{"refund"}}
	reg := registryWithTools(t, "weather", "activate", "refund")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		policy:    &pipeline.ToolPolicy{MaxIdenticalToolCalls: 2},
		cfg:       DuplexToolConfig{ToolGrants: exec.grants},
		turnState: &core.TurnState{AllowedTools: []string{"weather", "activate"}},
		updatable: true,
	})

	h.emit(toolCallTurn(namedCall("c1", "weather")))
	h.awaitSent(1)
	h.emit(toolCallTurn(namedCall("c2", "weather"), namedCall("c3", "activate")))
	h.awaitSent(2)
	h.emit(toolCallTurn(namedCall("c4", "refund")))
	sent := h.awaitSent(3)
	h.finish()

	updates, _ := h.sess().toolUpdates()
	assert.Equal(t, [][]providers.StreamingToolDefinition{
		{convTool("activate"), convTool("refund"), convTool("weather")},
	}, updates)
	assert.Equal(t, []providers.ToolResponse{{ToolCallID: "c4", Result: "ok"}}, sent[2])
	assert.Equal(t, []string{"weather", "activate", "refund"}, exec.calls())
}

// flakyOrOKExec fails every call to "flaky" and answers the rest with ok,
// recording the tools it ran.
type flakyOrOKExec struct{ grantingExec }

func (e *flakyOrOKExec) Execute(ctx context.Context, d *tools.ToolDescriptor, args json.RawMessage) (
	json.RawMessage, error,
) {
	out, _ := e.grantingExec.Execute(ctx, d, args)
	if d.Name == "flaky" {
		return nil, errors.New("backend down")
	}
	return out, nil
}

// A nudged round's rejected call counts toward the rejection rule: rejected
// twice in the turn, the tool is taken off the session before the round's
// responses are sent.
func TestDuplexNudgeRound_RejectionCountsTowardExclusion(t *testing.T) {
	exec := &flakyOrOKExec{}
	reg := registryWithTools(t, "weather", "flaky")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		policy:    &pipeline.ToolPolicy{MaxIdenticalToolCalls: 2},
		turnState: &core.TurnState{AllowedTools: []string{"weather", "flaky"}},
		updatable: true,
	})

	h.emit(toolCallTurn(namedCall("c1", "weather"), argsCall("c2", "flaky", `{"n":1}`)))
	h.awaitSent(1)
	h.emit(toolCallTurn(namedCall("c3", "weather"), argsCall("c4", "flaky", `{"n":2}`)))
	sent := h.awaitSent(2)
	h.finish()

	updates, log := h.sess().toolUpdates()
	assert.Equal(t, [][]providers.StreamingToolDefinition{{convTool("weather")}}, updates,
		"the second rejection, in the nudged round, takes flaky off the session")
	assert.Equal(t, []string{"responses", "update", "responses"}, log, "the update precedes the round's responses")
	assert.Equal(t, []providers.ToolResponse{
		{ToolCallID: "c3", Result: nudgeText("weather")},
		{ToolCallID: "c4", Result: "Tool execution failed: backend down", IsError: true},
	}, sent[1])
	assert.ElementsMatch(t, []string{"weather", "flaky", "flaky"}, exec.calls())
}

// A call refused as over the budget did not run, so it does not count toward
// the repeated-call breaker: its one repeat is refused as over the budget
// again, not nudged.
func TestDuplexNudgeRound_OverBudgetCallDoesNotCountTowardTheBreaker(t *testing.T) {
	exec := &grantingExec{}
	reg := registryWithTools(t, "weather")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		policy:    &pipeline.ToolPolicy{MaxToolCallsPerTurn: 1, MaxIdenticalToolCalls: 2},
		turnState: &core.TurnState{AllowedTools: []string{"weather"}},
	})

	h.emit(toolCallTurn(argsCall("c1", "weather", `{"city":"Paris"}`), argsCall("c2", "weather", `{"city":"Rome"}`)))
	h.awaitSent(1)
	h.emit(toolCallTurn(argsCall("c3", "weather", `{"city":"Rome"}`)))
	sent := h.awaitSent(2)
	h.finish()

	const overBudget = "Tool weather not executed: the tool call limit for this turn is reached"
	assert.Equal(t, [][]providers.ToolResponse{
		{{ToolCallID: "c1", Result: "ok"}, {ToolCallID: "c2", Result: overBudget, IsError: true}},
		{{ToolCallID: "c3", Result: overBudget, IsError: true}},
	}, sent)
	assert.Equal(t, []string{"weather"}, exec.calls())
}

// A looping call ahead of a new one keeps its place in the round's responses.
func TestDuplexNudgeRound_ResponsesAreInCallOrder(t *testing.T) {
	exec := &grantingExec{}
	reg := registryWithTools(t, "weather")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		policy:    &pipeline.ToolPolicy{MaxIdenticalToolCalls: 2},
		turnState: &core.TurnState{AllowedTools: []string{"weather"}},
	})

	h.emit(toolCallTurn(argsCall("c1", "weather", `{"city":"Paris"}`)))
	h.awaitSent(1)
	h.emit(toolCallTurn(argsCall("c2", "weather", `{"city":"Paris"}`), argsCall("c3", "weather", `{"city":"Rome"}`)))
	sent := h.awaitSent(2)
	h.finish()

	assert.Equal(t, []providers.ToolResponse{
		{ToolCallID: "c2", Result: nudgeText("weather")},
		{ToolCallID: "c3", Result: "ok"},
	}, sent[1])
}
