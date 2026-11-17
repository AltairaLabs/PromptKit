package hooks

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/sandbox"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

var execCaller = types.Caller{ID: "member@example.com", Claims: map[string]any{"tenant": "acme"}}

// execCallerJSON is how execCaller appears in an exec hook's stdin payload.
var execCallerJSON = map[string]any{"ID": "member@example.com", "Claims": map[string]any{"tenant": "acme"}}

func stdinPayload(t *testing.T, sb *recordingSandbox) map[string]any {
	t.Helper()
	var p map[string]any
	require.NoError(t, json.Unmarshal(sb.lastReq.Stdin, &p))
	return p
}

// An exec tool hook is given the call's caller on stdin, before and after
// execution, under request.Caller.
func TestExecToolHook_StdinCarriesTheCaller(t *testing.T) {
	sb := &recordingSandbox{resp: sandbox.Response{Stdout: []byte(`{"allow":true}`)}}
	hook := NewExecToolHook(&ExecHookConfig{
		Name: "policy", Command: "policy", Phases: []string{"before_execution", "after_execution"}, Sandbox: sb,
	})
	req := ToolRequest{Name: "lookup", CallID: "c1", Caller: execCaller}

	require.True(t, hook.BeforeExecution(context.Background(), req).Allow)
	p := stdinPayload(t, sb)
	assert.Equal(t, "before_execution", p["phase"])
	assert.Equal(t, execCallerJSON, p["request"].(map[string]any)["Caller"])

	require.True(t, hook.AfterExecution(context.Background(), req, ToolResponse{Name: "lookup", CallID: "c1"}).Allow)
	p = stdinPayload(t, sb)
	assert.Equal(t, "after_execution", p["phase"])
	assert.Equal(t, execCallerJSON, p["request"].(map[string]any)["Caller"])
}

// An exec session hook is given the conversation's caller on stdin under
// event.Caller.
func TestExecSessionHook_StdinCarriesTheCaller(t *testing.T) {
	sb := &recordingSandbox{resp: sandbox.Response{Stdout: []byte(`{"ack":true}`)}}
	hook := NewExecSessionHook(&ExecHookConfig{
		Name: "audit", Command: "audit", Phases: []string{"session_start"}, Sandbox: sb,
	})

	require.NoError(t, hook.OnSessionStart(context.Background(), SessionEvent{SessionID: "s1", Caller: execCaller}))
	p := stdinPayload(t, sb)
	assert.Equal(t, "session_start", p["phase"])
	assert.Equal(t, execCallerJSON, p["event"].(map[string]any)["Caller"])
}

// An exec provider hook is given the conversation's caller on stdin, before
// and after the call, under request.Caller.
func TestExecProviderHook_StdinCarriesTheCaller(t *testing.T) {
	sb := &recordingSandbox{resp: sandbox.Response{Stdout: []byte(`{"allow":true}`)}}
	hook := NewExecProviderHook(&ExecHookConfig{
		Name: "policy", Command: "policy", Phases: []string{"before_call", "after_call"}, Sandbox: sb,
	})
	req := &ProviderRequest{ProviderID: "p", Caller: execCaller}

	require.True(t, hook.BeforeCall(context.Background(), req).Allow)
	p := stdinPayload(t, sb)
	assert.Equal(t, "before_call", p["phase"])
	assert.Equal(t, execCallerJSON, p["request"].(map[string]any)["Caller"])

	require.True(t, hook.AfterCall(context.Background(), req, &ProviderResponse{ProviderID: "p"}).Allow)
	p = stdinPayload(t, sb)
	assert.Equal(t, "after_call", p["phase"])
	assert.Equal(t, execCallerJSON, p["request"].(map[string]any)["Caller"])
}
