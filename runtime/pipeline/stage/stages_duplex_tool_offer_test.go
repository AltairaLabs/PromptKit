package stage

import (
	"context"
	"encoding/json"
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

// tool_choice "none" offers the session no tools at all, the host's included,
// and the session is told so.
func TestDuplexToolOffer_ToolChoiceNoneOffersNoTools(t *testing.T) {
	h := startRealtime(t, realtimeOpts{
		registry:     registryWithTools(t, "weather", "memory__recall"),
		policy:       &pipeline.ToolPolicy{ToolChoice: "none"},
		turnState:    &TurnState{AllowedTools: []string{"weather"}},
		sessionTools: []providers.StreamingToolDefinition{{Name: "host_only"}},
	})
	h.finish()

	created := h.provider.createdWith()
	require.Len(t, created, 1)
	assert.Empty(t, created[0].Tools)
	assert.Equal(t, "none", created[0].ToolChoice)
}

// The policy's tool_choice reaches the session; a host's own wins over it.
func TestDuplexToolOffer_ToolChoiceIsPassedOn(t *testing.T) {
	for name, tc := range map[string]struct{ policy, host, want string }{
		"policy":       {policy: "required", want: "required"},
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
	user := types.Message{Role: roleUser, Content: "never mind"}
	h.input <- StreamElement{Message: &user}
	require.Eventually(t, func() bool { return len(h.output) >= 2 }, 2*time.Second, time.Millisecond)
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
