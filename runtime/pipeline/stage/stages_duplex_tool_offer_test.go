package stage

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// grantingExec is a "local" executor whose "activate" tool grants the tools
// named by grant, as skill__activate does, and whose other tools return ok.
type grantingExec struct {
	grant []string

	mu      sync.Mutex
	granted []string
	ran     []string
}

func (e *grantingExec) Name() string { return "local" }

func (e *grantingExec) Execute(_ context.Context, d *tools.ToolDescriptor, _ json.RawMessage) (json.RawMessage, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ran = append(e.ran, d.Name)
	if d.Name == "activate" {
		e.granted = append([]string(nil), e.grant...)
	}
	return json.RawMessage(`"ok"`), nil
}

// grants is the live grant accessor (DuplexToolConfig.ToolGrants).
func (e *grantingExec) grants() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.granted...)
}

func (e *grantingExec) revoke() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.granted = nil
}

func (e *grantingExec) calls() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.ran...)
}

func objectSchema() map[string]interface{} { return map[string]interface{}{"type": "object"} }

// convTool is the session definition of a registryWithTools tool.
func convTool(name string) providers.StreamingToolDefinition {
	return providers.StreamingToolDefinition{Name: name, Description: "desc-" + name, Parameters: objectSchema()}
}

func toolNamesOf(defs []providers.StreamingToolDefinition) []string {
	names := make([]string, 0, len(defs))
	for i := range defs {
		names = append(names, defs[i].Name)
	}
	return names
}

// assistantMessages returns the assistant messages in elems, in order.
func realtimeAssistantMessages(elems []StreamElement) []*types.Message {
	var out []*types.Message
	for _, e := range elems {
		if e.Message != nil && e.Message.Role == roleAssistant {
			out = append(out, e.Message)
		}
	}
	return out
}

func namedCall(id, name string) types.MessageToolCall {
	return types.MessageToolCall{ID: id, Name: name, Args: json.RawMessage(`{}`)}
}

// The session is created with the conversation's tools (the prompt's allowed
// tools and the capability tools the registry holds) merged with the host's:
// on a name clash the host's definition wins, and a host-only tool is kept.
func TestDuplexToolOffer_SessionIsCreatedWithTheConversationsTools(t *testing.T) {
	reg := registryWithTools(t, "weather", "refund", "memory__recall")
	hostWeather := providers.StreamingToolDefinition{
		Name: "weather", Description: "host weather",
		Parameters: map[string]interface{}{"type": "object", "required": []interface{}{"city"}},
	}
	hostOnly := providers.StreamingToolDefinition{Name: "host_only", Description: "host tool"}
	h := startRealtime(t, realtimeOpts{
		registry:     reg,
		turnState:    &TurnState{AllowedTools: []string{"weather"}},
		sessionTools: []providers.StreamingToolDefinition{hostWeather, hostOnly},
	})
	h.finish()

	created := h.provider.createdWith()
	require.Len(t, created, 1)
	assert.Equal(t, []providers.StreamingToolDefinition{convTool("memory__recall"), hostWeather, hostOnly},
		created[0].Tools)
	assert.Empty(t, created[0].ToolChoice)
}

// A tool_choice of "none", the policy's or the host's, offers the session no
// tools at all, the host's included. The policy's choice itself is not sent:
// no tools say it.
func TestDuplexToolOffer_ToolChoiceNoneOffersNoTools(t *testing.T) {
	for name, tc := range map[string]struct{ policy, host string }{
		"policy": {policy: "none"},
		"host":   {host: "none"},
	} {
		t.Run(name, func(t *testing.T) {
			h := startRealtime(t, realtimeOpts{
				registry:       registryWithTools(t, "weather", "memory__recall"),
				policy:         &pipeline.ToolPolicy{ToolChoice: tc.policy},
				turnState:      &TurnState{AllowedTools: []string{"weather"}},
				sessionTools:   []providers.StreamingToolDefinition{{Name: "host_only"}},
				hostToolChoice: tc.host,
			})
			h.finish()

			created := h.provider.createdWith()
			require.Len(t, created, 1)
			assert.Empty(t, created[0].Tools)
			assert.Equal(t, tc.host, created[0].ToolChoice)
		})
	}
}

// The host's tool_choice reaches the session, whatever the policy says.
func TestDuplexToolOffer_ToolChoiceIsPassedOn(t *testing.T) {
	for name, tc := range map[string]struct{ policy, host, want string }{
		"host":         {host: "weather", want: "weather"},
		"host over it": {policy: "required", host: "auto", want: "auto"},
	} {
		t.Run(name, func(t *testing.T) {
			h := startRealtime(t, realtimeOpts{
				registry:       registryWithTools(t, "weather"),
				policy:         &pipeline.ToolPolicy{ToolChoice: tc.policy},
				turnState:      &TurnState{AllowedTools: []string{"weather"}},
				hostToolChoice: tc.host,
			})
			h.finish()
			created := h.provider.createdWith()
			require.Len(t, created, 1)
			assert.Equal(t, tc.want, created[0].ToolChoice)
			assert.Equal(t, []string{"weather"}, toolNamesOf(created[0].Tools))
		})
	}
}

// A tool granted mid-session is added to a session that can change its tools,
// before the round's responses are sent, so the model can call it at once;
// the call then runs, and the turn reports the tool offered.
func TestDuplexToolOffer_GrantMidSessionUpdatesTheSession(t *testing.T) {
	exec := &grantingExec{grant: []string{"refund"}}
	reg := registryWithTools(t, "weather", "activate", "refund")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		cfg:       DuplexToolConfig{ToolGrants: exec.grants},
		turnState: &TurnState{AllowedTools: []string{"weather", "activate"}},
		updatable: true,
	})

	h.emit(toolCallTurn(namedCall("c1", "activate")))
	h.awaitSent(1)
	h.emit(toolCallTurn(namedCall("c2", "refund")))
	sent := h.awaitSent(2)
	elems := h.finish()

	updates, log := h.sess().toolUpdates()
	require.Equal(t, [][]providers.StreamingToolDefinition{
		{convTool("activate"), convTool("refund"), convTool("weather")},
	}, updates)
	assert.Equal(t, []string{"update", "responses", "responses"}, log, "the update precedes the round's responses")
	assert.Equal(t, []providers.ToolResponse{{ToolCallID: "c2", Result: "ok"}}, sent[1])
	assert.Equal(t, []string{"activate", "refund"}, exec.calls())

	msgs := realtimeAssistantMessages(elems)
	require.Len(t, msgs, 2)
	assert.Equal(t, []string{"activate", "weather"}, msgs[0].Meta[types.MetaToolsOffered])
	assert.Equal(t, []string{"activate", "refund", "weather"}, msgs[1].Meta[types.MetaToolsOffered])
}

// A session that cannot change its tools keeps the ones it was created with,
// and a call to a tool granted after that is refused, not run.
func TestDuplexToolOffer_WithoutUpdaterAGrantedToolIsRefused(t *testing.T) {
	exec := &grantingExec{grant: []string{"refund"}}
	reg := registryWithTools(t, "weather", "activate", "refund")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		cfg:       DuplexToolConfig{ToolGrants: exec.grants},
		turnState: &TurnState{AllowedTools: []string{"weather", "activate"}},
	})

	h.emit(toolCallTurn(namedCall("c1", "activate")))
	h.awaitSent(1)
	h.emit(toolCallTurn(namedCall("c2", "refund"), namedCall("c3", "weather")))
	sent := h.awaitSent(2)
	elems := h.finish()

	const refusal = "Tool refund was not run: it became available after this realtime session started, " +
		"and the provider cannot add tools to a live session"
	assert.Equal(t, []providers.ToolResponse{
		{ToolCallID: "c3", Result: "ok"},
		{ToolCallID: "c2", Result: refusal, IsError: true},
	}, sent[1])
	assert.Equal(t, []string{"activate", "weather"}, exec.calls(), "the refused call never runs")
	results := toolResultMessages(elems)
	require.Len(t, results, 3)
	assert.Equal(t, refusal, results[2].Error)

	msgs := realtimeAssistantMessages(elems)
	require.Len(t, msgs, 2)
	assert.Equal(t, []string{"activate", "weather"}, msgs[1].Meta[types.MetaToolsOffered],
		"the session was never offered the granted tool")
}

// tools_offered reports each turn's own offer: a tool offered on one turn and
// withdrawn before the next is not reported on the next.
func TestDuplexToolOffer_ToolsOfferedIsPerTurn(t *testing.T) {
	exec := &grantingExec{grant: []string{"refund"}}
	reg := registryWithTools(t, "weather", "activate", "refund")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		cfg:       DuplexToolConfig{ToolGrants: exec.grants},
		turnState: &TurnState{AllowedTools: []string{"weather", "activate"}},
		updatable: true,
		script:    [][]providers.StreamChunk{textTurn("activated", nil)},
	})

	h.emit(toolCallTurn(namedCall("c1", "activate")))
	h.awaitSent(1)
	exec.revoke()
	user := types.Message{Role: roleUser, Content: "next"}
	h.input <- StreamElement{Message: &user}
	require.Eventually(t, func() bool {
		updates, _ := h.sess().toolUpdates()
		return len(updates) == 2
	}, 2*time.Second, time.Millisecond, "the new turn withdraws the revoked tool")
	h.emit(textTurn("done", nil))
	require.Eventually(t, func() bool { return len(h.output) >= 5 }, 2*time.Second, time.Millisecond)
	elems := h.finish()

	updates, _ := h.sess().toolUpdates()
	assert.Equal(t, []string{"activate", "weather"}, toolNamesOf(updates[1]))
	msgs := realtimeAssistantMessages(elems)
	require.Len(t, msgs, 3)
	assert.Equal(t, []string{"activate", "weather"}, msgs[0].Meta[types.MetaToolsOffered])
	assert.Equal(t, []string{"activate", "refund", "weather"}, msgs[1].Meta[types.MetaToolsOffered])
	assert.Equal(t, "done", msgs[2].Content)
	assert.Equal(t, []string{"activate", "weather"}, msgs[2].Meta[types.MetaToolsOffered])
}

// gatedGrantingExec is a grantingExec whose calls wait for gate.
type gatedGrantingExec struct {
	*grantingExec
	gate    chan struct{}
	started chan struct{}
}

func (e *gatedGrantingExec) Execute(ctx context.Context, d *tools.ToolDescriptor, a json.RawMessage) (json.RawMessage, error) {
	e.started <- struct{}{}
	<-e.gate
	return e.grantingExec.Execute(ctx, d, a)
}

// A round of an earlier tool turn that finishes once a user turn has started a
// new one leaves the session's tools alone: the new turn owns the offer, and
// its tools_offered names only what the session held during it.
func TestDuplexToolOffer_EarlierTurnsRoundDoesNotChangeTheNewTurnsOffer(t *testing.T) {
	exec := &gatedGrantingExec{
		grantingExec: &grantingExec{grant: []string{"refund"}},
		gate:         make(chan struct{}), started: make(chan struct{}, 1),
	}
	reg := registryWithTools(t, "weather", "activate", "refund")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		cfg:       DuplexToolConfig{ToolGrants: exec.grants},
		turnState: &TurnState{AllowedTools: []string{"weather", "activate"}},
		updatable: true,
	})

	h.emit(toolCallTurn(namedCall("c1", "activate")))
	<-exec.started
	earlier := h.toolTurn()
	user := types.Message{Role: roleUser, Content: "never mind"}
	h.input <- StreamElement{Message: &user}
	h.awaitNewToolTurn(earlier) // the round finishes once the new turn owns the offer
	close(exec.gate)
	h.awaitSent(1)
	h.emit(textTurn("ok", nil))
	require.Eventually(t, func() bool { return len(h.output) >= 4 }, 2*time.Second, time.Millisecond)
	elems := h.finish()

	updates, _ := h.sess().toolUpdates()
	assert.Empty(t, updates, "the earlier turn's round changed the session's tools")
	msgs := realtimeAssistantMessages(elems)
	require.Len(t, msgs, 2)
	assert.Equal(t, "ok", msgs[1].Content)
	assert.Equal(t, []string{"activate", "weather"}, msgs[1].Meta[types.MetaToolsOffered])
}

// A host tool the turn excluded is withheld like a conversation tool.
func TestMergeSessionTools_ExcludedHostToolIsWithheld(t *testing.T) {
	host := []providers.StreamingToolDefinition{{Name: "lookup_order"}, {Name: "cancel_order"}}
	got := mergeSessionTools(nil, host, map[string]bool{"cancel_order": true})
	assert.Equal(t, []providers.StreamingToolDefinition{{Name: "lookup_order"}}, got)
}

// A policy tool_choice of "required" (or a tool name) governs a text turn's
// first round only. A realtime session has no rounds to scope it to, and
// keeping it for the session would force a tool call after every tool
// result. It is not sent: the reply after the tool results ends the turn.
func TestDuplexToolOffer_PolicyToolChoiceIsNotSentToTheSession(t *testing.T) {
	for _, choice := range []string{"required", "weather"} {
		t.Run(choice, func(t *testing.T) {
			exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
			h := startRealtime(t, realtimeOpts{
				registry:  newWeatherRegistry(t, exec),
				policy:    &pipeline.ToolPolicy{ToolChoice: choice},
				turnState: &TurnState{AllowedTools: []string{"weather"}},
				updatable: true,
				script:    [][]providers.StreamChunk{textTurn("It is 21 degrees", nil)},
			})

			h.emit(toolCallTurn(weatherCall("c1")))
			h.awaitSent(1)
			require.Eventually(t, func() bool { return len(h.output) >= 3 }, 2*time.Second, time.Millisecond)
			elems := h.finish()

			created := h.provider.createdWith()
			require.Len(t, created, 1)
			assert.Empty(t, created[0].ToolChoice)
			assert.Equal(t, []string{"weather"}, toolNamesOf(created[0].Tools))
			assert.Empty(t, streamErrors(elems))
			msgs := realtimeAssistantMessages(elems)
			require.Len(t, msgs, 2)
			assert.Equal(t, "It is 21 degrees", msgs[1].Content)
			updates, _ := h.sess().toolUpdates()
			assert.Empty(t, updates)
		})
	}
}

// A spent max_tool_calls_per_turn budget refuses the calls over it; it does
// not take the session's tools away, and the next turn's calls run.
func TestDuplexToolOffer_SpentBudgetKeepsTheSessionsTools(t *testing.T) {
	exec := &realtimeToolExec{result: json.RawMessage(`{"temp":21}`)}
	h := startRealtime(t, realtimeOpts{
		registry:  newWeatherRegistry(t, exec),
		policy:    &pipeline.ToolPolicy{MaxToolCallsPerTurn: 1},
		turnState: &TurnState{AllowedTools: []string{"weather"}},
		updatable: true,
		script: [][]providers.StreamChunk{
			toolCallTurn(weatherCall("c2")),
			textTurn("I cannot check again", nil),
		},
	})

	h.emit(toolCallTurn(weatherCall("c1")))
	h.awaitSent(2)
	h.awaitToolTurnEnded() // by "I cannot check again"
	user := types.Message{Role: roleUser, Content: "and now?"}
	h.input <- StreamElement{Message: &user}
	h.awaitNewToolTurn(nil)
	h.emit(toolCallTurn(weatherCall("c3")))
	sent := h.awaitSent(3)
	h.finish()

	const overBudget = "Tool weather not executed: the tool call limit for this turn is reached"
	assert.Equal(t, [][]providers.ToolResponse{
		{{ToolCallID: "c1", Result: weatherResult}},
		{{ToolCallID: "c2", Result: overBudget, IsError: true}},
		{{ToolCallID: "c3", Result: weatherResult}},
	}, sent)
	updates, _ := h.sess().toolUpdates()
	assert.Empty(t, updates, "the spent budget changed the session's tools")
	assert.Equal(t, []string{"weather", "weather"}, exec.calls())
}

// failingExec fails every call to "flaky" and answers the rest with ok.
type failingExec struct{}

func (failingExec) Name() string { return "local" }

func (failingExec) Execute(_ context.Context, d *tools.ToolDescriptor, _ json.RawMessage) (json.RawMessage, error) {
	if d.Name == "flaky" {
		return nil, errors.New("backend down")
	}
	return json.RawMessage(`"ok"`), nil
}

// A tool excluded for repeated failures on one tool turn is offered again on
// the next, also when that turn starts without a user turn the stage can see
// (audio the provider does not transcribe). With every tool excluded the
// model can call none, so no tool round would ever restore them: the next
// turn's reply restores them as it begins.
func TestDuplexToolOffer_ExclusionIsLiftedAtTheNextTurnWithoutAUserTurn(t *testing.T) {
	reg := registryWithTools(t, "flaky")
	reg.RegisterExecutor(failingExec{})
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		turnState: &TurnState{AllowedTools: []string{"flaky"}},
		updatable: true,
		script:    [][]providers.StreamChunk{textTurn("the backend is down", nil)},
	})

	h.emit(toolCallTurn(namedCall("c1", "flaky"), namedCall("c2", "flaky")))
	h.awaitSent(1)
	require.Eventually(t, func() bool { return len(h.output) >= 4 }, 2*time.Second, time.Millisecond)
	h.emit(textTurn("anything else?", nil))
	require.Eventually(t, func() bool { return len(h.output) >= 5 }, 2*time.Second, time.Millisecond)
	elems := h.finish()

	updates, log := h.sess().toolUpdates()
	require.Len(t, updates, 2)
	assert.Empty(t, updates[0], "excluded after two failures")
	assert.Equal(t, []string{"flaky"}, toolNamesOf(updates[1]), "offered again on the next turn")
	assert.Equal(t, []string{"update", "responses", "update"}, log)
	msgs := realtimeAssistantMessages(elems)
	require.Len(t, msgs, 3)
	assert.Equal(t, "anything else?", msgs[2].Content)
	assert.Nil(t, msgs[2].Meta[types.MetaToolsOffered],
		"the reply began before the update, when the session held no tools")
}

// failingUpdateSession is an updating session whose updates fail.
type failingUpdateSession struct {
	*realtimeToolSession
}

func (s *failingUpdateSession) UpdateTools(context.Context, []providers.StreamingToolDefinition) error {
	return errors.New("socket write failed")
}

// A session whose tool update failed keeps its tools, and a call to a tool
// granted since is refused, saying the update failed.
func TestDuplexToolOffer_FailedUpdateRefusesTheGrantedTool(t *testing.T) {
	exec := &grantingExec{grant: []string{"refund"}}
	reg := registryWithTools(t, "weather", "activate", "refund")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{
		registry:    reg,
		cfg:         DuplexToolConfig{ToolGrants: exec.grants},
		turnState:   &TurnState{AllowedTools: []string{"weather", "activate"}},
		failUpdates: true,
	})

	h.emit(toolCallTurn(namedCall("c1", "activate")))
	h.awaitSent(1)
	h.emit(toolCallTurn(namedCall("c2", "refund")))
	sent := h.awaitSent(2)
	h.finish()

	assert.Equal(t, []providers.ToolResponse{{ToolCallID: "c2", IsError: true,
		Result: "Tool refund was not run: it became available after this realtime session started, " +
			"and the session's tools could not be updated"}}, sent[1])
	assert.Equal(t, []string{"activate"}, exec.calls())
}

// A model that keeps calling a tool granted after setup, on a session that
// cannot be given it, gets two refused batches; the next closes the session
// with an error naming the tool, as a stopped turn's refusals do.
func TestDuplexToolOffer_RepeatedCallsToAToolTheSessionLacksCloseTheSession(t *testing.T) {
	exec := &grantingExec{grant: []string{"refund"}}
	reg := registryWithTools(t, "weather", "activate", "refund")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		cfg:       DuplexToolConfig{ToolGrants: exec.grants},
		turnState: &TurnState{AllowedTools: []string{"weather", "activate"}},
		script: [][]providers.StreamChunk{
			toolCallTurn(namedCall("c2", "refund")),
			toolCallTurn(namedCall("c3", "refund")),
			toolCallTurn(namedCall("c4", "refund")),
			toolCallTurn(namedCall("c5", "refund")),
		},
	})

	h.emit(toolCallTurn(namedCall("c1", "activate")))
	elems := drainUntilClosed(t, h.output)
	require.NoError(t, <-h.done)

	refusal := func(id string) []providers.ToolResponse {
		return []providers.ToolResponse{{ToolCallID: id, IsError: true,
			Result: "Tool refund was not run: it became available after this realtime session started, " +
				"and the provider cannot add tools to a live session"}}
	}
	assert.Equal(t, [][]providers.ToolResponse{
		{{ToolCallID: "c1", Result: "ok"}},
		refusal("c2"),
		refusal("c3"),
	}, h.sess().sentResponses(), "two refusal batches, then nothing")
	assert.True(t, h.sess().isClosed())
	errs := streamErrors(elems)
	require.Len(t, errs, 1)
	assert.Equal(t, "duplex provider stage: closing the session: the model kept calling tools after its "+
		"tool turn was stopped: tool refund is not available in this realtime session", errs[0].Error())
	assert.Equal(t, []string{"activate"}, exec.calls())
}

// A turn completion with no content (only its cost), and no user turn
// before it, starts a tool turn too, and that turn lifts the last turn's
// exclusion.
func TestDuplexToolOffer_ContentlessCompletionLiftsTheExclusion(t *testing.T) {
	reg := registryWithTools(t, "flaky")
	reg.RegisterExecutor(failingExec{})
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		turnState: &TurnState{AllowedTools: []string{"flaky"}},
		updatable: true,
		script:    [][]providers.StreamChunk{textTurn("the backend is down", nil)},
	})

	h.emit(toolCallTurn(namedCall("c1", "flaky"), namedCall("c2", "flaky")))
	h.awaitSent(1)
	require.Eventually(t, func() bool { return len(h.output) >= 4 }, 2*time.Second, time.Millisecond)
	fr := "stop"
	h.emit([]providers.StreamChunk{{
		FinishReason: &fr, CostInfo: &types.CostInfo{InputTokens: 12, OutputTokens: 1, TotalCost: 0.001},
	}})
	require.Eventually(t, func() bool {
		updates, _ := h.sess().toolUpdates()
		return len(updates) == 2
	}, 2*time.Second, time.Millisecond, "the content-less turn never lifted the exclusion")
	h.finish()

	updates, _ := h.sess().toolUpdates()
	assert.Empty(t, updates[0])
	assert.Equal(t, []string{"flaky"}, toolNamesOf(updates[1]))
}

// A tool the conversation withdraws (here a revoked skill grant) from a
// session that cannot drop it, or whose update failed, is still held by the
// session; a call to it is refused, not run.
func TestDuplexToolOffer_WithdrawnToolTheSessionStillHoldsIsRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		failUpdates bool
		reason      string
	}{
		"no updater":    {reason: "the provider cannot remove tools from a live session"},
		"failed update": {failUpdates: true, reason: "the session's tools could not be updated"},
	} {
		t.Run(name, func(t *testing.T) {
			exec := &grantingExec{granted: []string{"refund"}}
			reg := registryWithTools(t, "weather", "refund")
			reg.RegisterExecutor(exec)
			h := startRealtime(t, realtimeOpts{
				registry:    reg,
				cfg:         DuplexToolConfig{ToolGrants: exec.grants},
				turnState:   &TurnState{AllowedTools: []string{"weather"}},
				failUpdates: tc.failUpdates,
			})
			require.Equal(t, []string{"refund", "weather"}, toolNamesOf(h.provider.createdWith()[0].Tools))

			exec.revoke()
			h.emit(toolCallTurn(namedCall("c1", "weather")))
			h.awaitSent(1)
			h.emit(toolCallTurn(namedCall("c2", "refund"), namedCall("c3", "weather")))
			sent := h.awaitSent(2)
			elems := h.finish()

			refusal := "Tool refund was not run: it was withdrawn after this realtime session started, and " + tc.reason
			assert.Equal(t, []providers.ToolResponse{
				{ToolCallID: "c3", Result: "ok"},
				{ToolCallID: "c2", Result: refusal, IsError: true},
			}, sent[1])
			assert.Equal(t, []string{"weather", "weather"}, exec.calls(), "the withdrawn tool never runs")
			results := toolResultMessages(elems)
			require.Len(t, results, 3)
			assert.Equal(t, refusal, results[2].Error)
		})
	}
}

// A tool excluded for this tool turn after repeated failures is not
// withdrawn from the conversation: on a session that cannot drop it, a
// further call runs as it would on a text turn, and is not refused as
// withdrawn.
func TestDuplexToolOffer_TurnExclusionIsNotAWithdrawal(t *testing.T) {
	call := func(id string) types.MessageToolCall {
		return types.MessageToolCall{ID: id, Name: "flaky", Args: json.RawMessage(`{"n":"` + id + `"}`)}
	}
	reg := registryWithTools(t, "flaky")
	reg.RegisterExecutor(failingExec{})
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		turnState: &TurnState{AllowedTools: []string{"flaky"}},
		script:    [][]providers.StreamChunk{toolCallTurn(call("c3"))},
	})

	h.emit(toolCallTurn(call("c1"), call("c2")))
	sent := h.awaitSent(2)
	h.finish()

	require.Len(t, sent[1], 1)
	assert.Equal(t, "c3", sent[1][0].ToolCallID)
	assert.True(t, sent[1][0].IsError)
	assert.Contains(t, sent[1][0].Result, "backend down", "the call ran")
	assert.NotContains(t, sent[1][0].Result, "withdrawn")
}
