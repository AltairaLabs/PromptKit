package stage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// A call to a registered tool the session was never offered is refused, not
// run, and the same tool runs once a grant has put it on the session (#2713).
func TestDuplexToolOffer_NeverOfferedToolIsRefusedUntilGranted(t *testing.T) {
	exec := &grantingExec{grant: []string{"refund"}}
	reg := registryWithTools(t, "weather", "activate", "refund")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		cfg:       DuplexToolConfig{ToolGrants: exec.grants},
		turnState: &TurnState{AllowedTools: []string{"weather", "activate"}},
		updatable: true,
	})

	h.emit(toolCallTurn(namedCall("c1", "refund"), namedCall("c2", "weather")))
	h.awaitSent(1)
	h.emit(toolCallTurn(namedCall("c3", "activate")))
	h.awaitSent(2)
	h.emit(toolCallTurn(namedCall("c4", "refund")))
	sent := h.awaitSent(3)
	elems := h.finish()

	const refusal = "Tool refund was not run: it is not available this turn"
	assert.Equal(t, []providers.ToolResponse{
		{ToolCallID: "c1", Result: refusal, IsError: true},
		{ToolCallID: "c2", Result: "ok"},
	}, sent[0], "the results keep the calls' order")
	assert.Equal(t, []providers.ToolResponse{{ToolCallID: "c4", Result: "ok"}}, sent[2])
	assert.Equal(t, []string{"weather", "activate", "refund"}, exec.calls(), "refund runs only once granted")
	results := toolResultMessages(elems)
	require.Len(t, results, 4)
	assert.Equal(t, refusal, results[0].Error)
	assert.Equal(t, types.ToolErrorApproval, results[0].ErrorType)
}

// A grant revoked mid-turn makes its tool ineligible from the next round on,
// though the session has not been brought in line yet.
func TestDuplexToolOffer_RevokedGrantIsRefusedFromTheNextRound(t *testing.T) {
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
	h.awaitSent(2)
	exec.revoke()
	h.emit(toolCallTurn(namedCall("c3", "refund")))
	sent := h.awaitSent(3)
	h.finish()

	assert.Equal(t, []providers.ToolResponse{{ToolCallID: "c2", Result: "ok"}}, sent[1])
	assert.Equal(t, []providers.ToolResponse{{
		ToolCallID: "c3", Result: "Tool refund was not run: it is not available this turn", IsError: true,
	}}, sent[2])
	assert.Equal(t, []string{"activate", "refund"}, exec.calls(), "the revoked tool does not run again")
}

// A session the stage runs tools for is gated even when its offer is empty:
// a call to a registered tool it was never given is refused.
func TestDuplexToolOffer_EmptyOfferRefusesTheCall(t *testing.T) {
	exec := &grantingExec{}
	reg := registryWithTools(t, "weather")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{registry: reg, turnState: &TurnState{}})

	h.emit(toolCallTurn(namedCall("c1", "weather")))
	sent := h.awaitSent(1)
	h.finish()

	assert.Equal(t, []providers.ToolResponse{{
		ToolCallID: "c1", Result: "Tool weather was not run: it is not available this turn", IsError: true,
	}}, sent[0])
	assert.Empty(t, exec.calls())
}

// A host tool_choice of none offers the session nothing, the conversation's
// tools included, so a call to one is refused.
func TestDuplexToolOffer_HostToolChoiceNoneRefusesTheCall(t *testing.T) {
	exec := &grantingExec{}
	reg := registryWithTools(t, "weather")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{
		registry: reg, turnState: &TurnState{AllowedTools: []string{"weather"}}, hostToolChoice: toolChoiceNone,
	})

	h.emit(toolCallTurn(namedCall("c1", "weather")))
	sent := h.awaitSent(1)
	h.finish()

	assert.Equal(t, []providers.ToolResponse{{
		ToolCallID: "c1", Result: "Tool weather was not run: it is not available this turn", IsError: true,
	}}, sent[0])
	assert.Empty(t, exec.calls())
}

// A policy tool_choice of none offers the session nothing, the host's tools
// included, so a call to a host tool is refused.
func TestDuplexToolOffer_PolicyToolChoiceNoneRefusesAHostTool(t *testing.T) {
	exec := &grantingExec{}
	reg := registryWithTools(t, "host_only")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{
		registry:     reg,
		policy:       &pipeline.ToolPolicy{ToolChoice: toolChoiceNone},
		turnState:    &TurnState{},
		sessionTools: []providers.StreamingToolDefinition{{Name: "host_only", Description: "host tool"}},
	})

	h.emit(toolCallTurn(namedCall("c1", "host_only")))
	sent := h.awaitSent(1)
	h.finish()

	assert.Equal(t, []providers.ToolResponse{{
		ToolCallID: "c1", Result: "Tool host_only was not run: it is not available this turn", IsError: true,
	}}, sent[0])
	assert.Empty(t, exec.calls())
}

// The blocklist is checked before the session's tools are: a blocklisted
// tool the session lacks is refused as blocklisted.
func TestDuplexToolOffer_BlocklistPrecedesTheUnsyncedRefusal(t *testing.T) {
	exec := &grantingExec{grant: []string{"refund"}}
	reg := registryWithTools(t, "weather", "activate", "refund")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		policy:    &pipeline.ToolPolicy{Blocklist: []string{"refund"}},
		cfg:       DuplexToolConfig{ToolGrants: exec.grants},
		turnState: &TurnState{AllowedTools: []string{"weather", "activate"}},
	})

	h.emit(toolCallTurn(namedCall("c1", "activate")))
	h.awaitSent(1)
	h.emit(toolCallTurn(namedCall("c2", "refund")))
	sent := h.awaitSent(2)
	h.finish()

	assert.Equal(t, []providers.ToolResponse{{
		ToolCallID: "c2", Result: "Tool refund is blocked by policy", IsError: true,
	}}, sent[1])
}

// A tool the host declares on the session alone is offered: a call to it
// runs, though the conversation's offer does not name it.
func TestDuplexToolOffer_HostDeclaredToolRuns(t *testing.T) {
	exec := &grantingExec{}
	reg := registryWithTools(t, "weather", "host_only")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{
		registry:     reg,
		turnState:    &TurnState{AllowedTools: []string{"weather"}},
		sessionTools: []providers.StreamingToolDefinition{{Name: "host_only", Description: "host tool"}},
	})

	h.emit(toolCallTurn(namedCall("c1", "host_only")))
	sent := h.awaitSent(1)
	h.finish()

	assert.Equal(t, []providers.ToolResponse{{ToolCallID: "c1", Result: "ok"}}, sent[0])
	assert.Equal(t, []string{"host_only"}, exec.calls())
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
		{ToolCallID: "c2", Result: refusal, IsError: true},
		{ToolCallID: "c3", Result: "ok"},
	}, sent[1], "the results keep the calls' order")
	assert.Equal(t, []string{"activate", "weather"}, exec.calls(), "the refused call never runs")
	results := toolResultMessages(elems)
	require.Len(t, results, 3)
	assert.Equal(t, refusal, results[1].Error)

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
	// The scripted reply to the tool responses must end the tool turn first:
	// user input that arrives while that provider call is still open only
	// marks a new turn due at the next generation, so its sync (the second
	// update) would wait for a reply this test sends only after it.
	h.awaitToolTurnEnded()
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
	h.awaitNewToolTurn(earlier)
	// The round finishes once the new turn owns the offer: the turn is
	// current before it brings the session's tools in line, and a grant
	// landing before that is the new turn's to offer.
	require.Eventually(t, func() bool {
		turn := h.toolTurn()
		h.stage.calls.mu.Lock()
		defer h.stage.calls.mu.Unlock()
		return turn.offered["activate"]
	}, 2*time.Second, time.Millisecond, "the new turn never brought the session's tools in line")
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
// cannot be given it, gets the refusal each time (a refused call never uses
// up the repeated-call nudge). Its batches of refused calls count toward the
// stopped turn's refusal cap: past it the turn is stopped and the session
// closed, with an error naming the tool.
func TestDuplexToolOffer_RepeatedCallsToAToolTheSessionLacksCloseTheSession(t *testing.T) {
	exec := &grantingExec{grant: []string{"refund"}}
	reg := registryWithTools(t, "weather", "activate", "refund")
	reg.RegisterExecutor(exec)
	script := make([][]providers.StreamChunk, 0, 7)
	for i := 2; i <= 8; i++ {
		script = append(script, toolCallTurn(namedCall(fmt.Sprintf("c%d", i), "refund")))
	}
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		cfg:       DuplexToolConfig{ToolGrants: exec.grants},
		turnState: &TurnState{AllowedTools: []string{"weather", "activate"}},
		script:    script,
	})

	h.emit(toolCallTurn(namedCall("c1", "activate")))
	elems := drainUntilClosed(t, h.output)
	require.NoError(t, <-h.done)

	refused := func(id, text string) []providers.ToolResponse {
		return []providers.ToolResponse{{ToolCallID: id, IsError: true, Result: text}}
	}
	const unsynced = "Tool refund was not run: it became available after this realtime session started, " +
		"and the provider cannot add tools to a live session"
	assert.Equal(t, [][]providers.ToolResponse{
		{{ToolCallID: "c1", Result: "ok"}},
		refused("c2", unsynced),
		refused("c3", unsynced),
	}, h.sess().sentResponses(), "two refused batches, then the session closes")
	assert.True(t, h.sess().isClosed())
	errs := streamErrors(elems)
	require.Len(t, errs, 1)
	assert.Equal(t, "duplex provider stage: closing the session: the model kept calling tools after its "+
		"tool turn was stopped: tool refund is not available in this realtime session", errs[0].Error())
	assert.Equal(t, []string{"activate"}, exec.calls())
}

// A model that keeps calling a tool it was never offered, with new
// arguments each time so the repeated-call breaker never trips, is bounded:
// each batch of refused calls counts toward the stopped turn's refusal cap,
// so with no round limit near, the third batch stops the turn and closes the
// session, with an error naming the tool.
func TestDuplexToolOffer_UnofferedCallsAreBoundedByTheRefusalCap(t *testing.T) {
	exec := &grantingExec{}
	reg := registryWithTools(t, "weather", "refund")
	reg.RegisterExecutor(exec)
	call := func(i int) types.MessageToolCall {
		return types.MessageToolCall{ID: fmt.Sprintf("c%d", i), Name: "refund", Args: json.RawMessage(fmt.Sprintf(`{"n":%d}`, i))}
	}
	script := make([][]providers.StreamChunk, 0, 4)
	for i := 2; i <= 5; i++ {
		script = append(script, toolCallTurn(call(i)))
	}
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		turnState: &TurnState{AllowedTools: []string{"weather"}},
		script:    script,
	})

	h.emit(toolCallTurn(call(1)))
	elems := drainUntilClosed(t, h.output)
	require.NoError(t, <-h.done)

	const refusal = "Tool refund was not run: it is not available this turn"
	assert.Equal(t, [][]providers.ToolResponse{
		{{ToolCallID: "c1", Result: refusal, IsError: true}},
		{{ToolCallID: "c2", Result: refusal, IsError: true}},
	}, h.sess().sentResponses(), "two refused batches, then the session closes")
	assert.True(t, h.sess().isClosed())
	errs := streamErrors(elems)
	require.Len(t, errs, 1)
	assert.Equal(t, "duplex provider stage: closing the session: the model kept calling tools after its "+
		"tool turn was stopped: tool refund is not available in this realtime session", errs[0].Error())
	assert.Empty(t, exec.calls())
}

// A batch in which some call runs is not a refused batch: a model that mixes
// an unavailable tool into rounds that also call an offered one is answered
// every round, and the session stays open.
func TestDuplexToolOffer_MixedRoundsDoNotCountTowardTheRefusalCap(t *testing.T) {
	exec := &grantingExec{}
	reg := registryWithTools(t, "weather", "refund")
	reg.RegisterExecutor(exec)
	round := func(i int) []providers.StreamChunk {
		args := json.RawMessage(fmt.Sprintf(`{"n":%d}`, i))
		return toolCallTurn(
			types.MessageToolCall{ID: fmt.Sprintf("r%d", i), Name: "refund", Args: args},
			types.MessageToolCall{ID: fmt.Sprintf("w%d", i), Name: "weather", Args: args})
	}
	script := [][]providers.StreamChunk{round(2), round(3), round(4), textTurn("done", nil)}
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		turnState: &TurnState{AllowedTools: []string{"weather"}},
		script:    script,
	})

	h.emit(round(1))
	sent := h.awaitSent(4)
	elems := h.finish()

	assert.Len(t, sent, 4, "every round was answered")
	assert.Empty(t, streamErrors(elems))
	assert.Equal(t, []string{"weather", "weather", "weather", "weather"}, exec.calls())
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
				{ToolCallID: "c2", Result: refusal, IsError: true},
				{ToolCallID: "c3", Result: "ok"},
			}, sent[1], "the results keep the calls' order")
			assert.Equal(t, []string{"weather", "weather"}, exec.calls(), "the withdrawn tool never runs")
			results := toolResultMessages(elems)
			require.Len(t, results, 3)
			assert.Equal(t, refusal, results[1].Error)
		})
	}
}

// A tool excluded for this tool turn after repeated failures is not
// withdrawn from the conversation: on a session that cannot drop it, and on
// one that dropped it for the exclusion, a further call runs as it would on
// a text turn, and is not refused.
func TestDuplexToolOffer_TurnExclusionIsNotAWithdrawal(t *testing.T) {
	for name, updatable := range map[string]bool{"no updater": false, "updater": true} {
		t.Run(name, func(t *testing.T) {
			call := func(id string) types.MessageToolCall {
				return types.MessageToolCall{ID: id, Name: "flaky", Args: json.RawMessage(`{"n":"` + id + `"}`)}
			}
			reg := registryWithTools(t, "flaky")
			reg.RegisterExecutor(failingExec{})
			h := startRealtime(t, realtimeOpts{
				registry:  reg,
				turnState: &TurnState{AllowedTools: []string{"flaky"}},
				script:    [][]providers.StreamChunk{toolCallTurn(call("c3"))},
				updatable: updatable,
			})

			h.emit(toolCallTurn(call("c1"), call("c2")))
			sent := h.awaitSent(2)
			h.finish()

			if updatable {
				updates, _ := h.sess().toolUpdates()
				require.NotEmpty(t, updates)
				assert.Empty(t, updates[0], "the session dropped the excluded tool")
			}
			require.Len(t, sent[1], 1)
			assert.Equal(t, "c3", sent[1][0].ToolCallID)
			assert.True(t, sent[1][0].IsError)
			assert.Contains(t, sent[1][0].Result, "backend down", "the call ran")
			assert.NotContains(t, sent[1][0].Result, "not run")
		})
	}
}

// grantNow replaces the live grants, as a host granting a tool between
// rounds does.
func (e *grantingExec) grantNow(names ...string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.granted = append([]string(nil), names...)
}

// A grant or a revocation made after the session's tools were last brought
// in line, and before a round, is judged against what the session holds when
// the round begins: on a session that cannot change its tools, a newly
// granted tool it lacks is refused as having become available, and a revoked
// tool it still holds as withdrawn. Neither runs.
func TestDuplexToolOffer_GrantChangeSinceTheLastSyncIsJudgedAtTheRound(t *testing.T) {
	for name, tc := range map[string]struct {
		before, after []string
		refusal       string
	}{
		"granted": {after: []string{"refund"}, refusal: "Tool refund was not run: it became available after this " +
			"realtime session started, and the provider cannot add tools to a live session"},
		"revoked": {before: []string{"refund"}, refusal: "Tool refund was not run: it was withdrawn after this " +
			"realtime session started, and the provider cannot remove tools from a live session"},
	} {
		t.Run(name, func(t *testing.T) {
			exec := &grantingExec{granted: tc.before}
			reg := registryWithTools(t, "weather", "refund")
			reg.RegisterExecutor(exec)
			h := startRealtime(t, realtimeOpts{
				registry:  reg,
				cfg:       DuplexToolConfig{ToolGrants: exec.grants},
				turnState: &TurnState{AllowedTools: []string{"weather"}},
			})

			h.emit(toolCallTurn(namedCall("c1", "weather")))
			h.awaitSent(1)
			exec.grantNow(tc.after...)
			h.emit(toolCallTurn(namedCall("c2", "refund"), weatherCall("c3")))
			sent := h.awaitSent(2)
			h.finish()

			assert.Equal(t, []providers.ToolResponse{
				{ToolCallID: "c2", Result: tc.refusal, IsError: true},
				{ToolCallID: "c3", Result: "ok"},
			}, sent[1])
			assert.Equal(t, []string{"weather", "weather"}, exec.calls(), "refund never runs")
		})
	}
}

// Deciding a round's refusals takes no lock the session's tool sync holds
// (across the provider's UpdateTools): a sync in progress does not hold up
// a round, and no host code (ToolGrants, DescribeTool) runs under that lock.
func TestDuplexToolOffer_RoundRefusalsDoNotWaitOnTheSessionSync(t *testing.T) {
	exec := &grantingExec{}
	reg := registryWithTools(t, "weather", "refund")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		cfg:       DuplexToolConfig{ToolGrants: exec.grants},
		turnState: &TurnState{AllowedTools: []string{"weather"}},
	})
	t.Cleanup(func() { h.finish() })
	// One round first: the stage has set up its session by the time it answers.
	h.emit(toolCallTurn(namedCall("c0", "weather")))
	h.awaitSent(1)

	o := &h.stage.tools.offer
	o.mu.Lock()
	decided := make(chan roundRefusals, 1)
	go func() {
		decided <- h.stage.toolRunner().classifyRound([]types.MessageToolCall{
			namedCall("c1", "refund"), namedCall("c2", "weather"),
		}, nil).refused
	}()
	var got roundRefusals
	select {
	case got = <-decided:
	case <-time.After(2 * time.Second):
	}
	o.mu.Unlock()

	assert.Equal(t, roundRefusals{"refund": "Tool refund was not run: it is not available this turn"}, got)
}

// recoveringUpdateSession is an updating session whose first update fails.
type recoveringUpdateSession struct {
	*updatingToolSession
	failedOnce bool
}

func (s *recoveringUpdateSession) UpdateTools(ctx context.Context, defs []providers.StreamingToolDefinition) error {
	if !s.failedOnce {
		s.failedOnce = true
		return errors.New("socket write failed")
	}
	return s.updatingToolSession.UpdateTools(ctx, defs)
}

// A session whose tool update failed and a later one succeeded holds the
// offer again: the tool it now holds runs, and a call to a tool granted
// since is refused as unavailable, not as if the session's tools still could
// not be updated.
func TestDuplexToolOffer_RecoveredUpdateNoLongerReportsTheFailure(t *testing.T) {
	exec := &grantingExec{grant: []string{"refund"}}
	reg := registryWithTools(t, "weather", "activate", "refund", "extra")
	reg.RegisterExecutor(exec)
	h := startRealtime(t, realtimeOpts{
		registry:        reg,
		cfg:             DuplexToolConfig{ToolGrants: exec.grants},
		turnState:       &TurnState{AllowedTools: []string{"weather", "activate"}},
		failFirstUpdate: true,
	})

	h.emit(toolCallTurn(namedCall("c1", "activate"))) // grants refund; the update fails
	h.awaitSent(1)
	h.emit(toolCallTurn(namedCall("c2", "refund"))) // the update after it succeeds
	h.awaitSent(2)
	exec.grantNow("refund", "extra")
	h.emit(toolCallTurn(weatherCall("c3"), namedCall("c4", "refund"), namedCall("c5", "extra")))
	sent := h.awaitSent(3)
	h.finish()

	assert.Equal(t, []providers.ToolResponse{{ToolCallID: "c2", IsError: true,
		Result: "Tool refund was not run: it became available after this realtime session started, " +
			"and the session's tools could not be updated"}}, sent[1], "refused while the update had failed")
	assert.Equal(t, []providers.ToolResponse{
		{ToolCallID: "c3", Result: "ok"},
		{ToolCallID: "c4", Result: "ok"},
		{ToolCallID: "c5", Result: "Tool extra was not run: it is not available this turn", IsError: true},
	}, sent[2])
}

// describeCounter is a DescribeTool that counts the tools it describes.
type describeCounter struct {
	mu sync.Mutex
	n  map[string]int
}

func (c *describeCounter) describe(d *tools.ToolDescriptor) *tools.ToolDescriptor {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n == nil {
		c.n = map[string]int{}
	}
	c.n[d.Name]++
	return d
}

func (c *describeCounter) count(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n[name]
}

// A round's check takes DescribeTool's outcome for a tool from the session's
// last sync, which described it: deciding the round does not call the
// host's DescribeTool again.
func TestDuplexToolOffer_RoundReusesTheSyncsDescription(t *testing.T) {
	exec := &grantingExec{}
	reg := registryWithTools(t, "weather")
	reg.RegisterExecutor(exec)
	described := &describeCounter{}
	h := startRealtime(t, realtimeOpts{
		registry:  reg,
		cfg:       DuplexToolConfig{DescribeTool: described.describe},
		turnState: &TurnState{AllowedTools: []string{"weather"}},
	})
	t.Cleanup(func() { h.finish() })
	h.emit(toolCallTurn(namedCall("c0", "weather")))
	h.awaitSent(1)
	before := described.count("weather")
	require.Positive(t, before, "the session's syncs describe the tool")

	got := h.stage.toolRunner().classifyRound([]types.MessageToolCall{namedCall("c1", "weather")}, nil)

	assert.Empty(t, got.refused)
	assert.Equal(t, before, described.count("weather"), "the round described the tool again")
}
