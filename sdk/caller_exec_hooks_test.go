package sdk

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/sandbox"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// callerSandbox is an exec-hook sandbox that records the stdin of every spawn
// and answers allow.
type callerSandbox struct {
	mu     sync.Mutex
	stdins [][]byte
}

func (s *callerSandbox) Name() string { return "caller-sandbox" }

func (s *callerSandbox) Spawn(_ context.Context, req sandbox.Request) (sandbox.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stdins = append(s.stdins, append([]byte(nil), req.Stdin...))
	return sandbox.Response{Stdout: []byte(`{"allow":true}`)}, nil
}

// payloads decodes every recorded stdin as an exec hook request.
func (s *callerSandbox) payloads(t *testing.T) []map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, 0, len(s.stdins))
	for _, b := range s.stdins {
		var p map[string]any
		require.NoError(t, json.Unmarshal(b, &p))
		out = append(out, p)
	}
	return out
}

// Exec hooks are host policy code run out of process: PromptKit hands them the
// caller on stdin, as it does in-process hooks. The tool hook's before and
// after requests and the session hook's start and end events each carry the
// caller's exact ID and Claims.
func TestCaller_ExecHooksReceiveTheCallerOnStdin(t *testing.T) {
	member := types.Caller{ID: "member@example.com", Claims: map[string]any{"tenant": "acme", "roles": []any{"admin"}}}
	wantCaller := map[string]any{
		"ID":     "member@example.com",
		"Claims": map[string]any{"tenant": "acme", "roles": []any{"admin"}},
	}
	toolBox, sessionBox := &callerSandbox{}, &callerSandbox{}
	toolHook := hooks.NewExecToolHook(&hooks.ExecHookConfig{
		Name: "policy", Command: "policy", Phases: []string{"before_execution", "after_execution"}, Sandbox: toolBox,
	})
	sessionHook := hooks.NewExecSessionHook(&hooks.ExecHookConfig{
		Name: "audit", Command: "audit", Phases: []string{"session_start", "session_end"}, Sandbox: sessionBox,
	})

	conv, err := Open(createTestPackFile(t, callerToolsPack), "chat",
		WithProvider(newCallerToolProvider()), WithSkipSchemaValidation(),
		WithToolHook(toolHook), WithSessionHook(sessionHook), WithCaller(member))
	require.NoError(t, err)
	conv.OnToolExecutor("lookup", &callerExecutor{})
	_, err = conv.Send(context.Background(), "ada")
	require.NoError(t, err)
	require.NoError(t, conv.Close())

	toolPayloads := toolBox.payloads(t)
	require.Len(t, toolPayloads, 2)
	for i, phase := range []string{"before_execution", "after_execution"} {
		p := toolPayloads[i]
		assert.Equal(t, "tool", p["hook"])
		assert.Equal(t, phase, p["phase"])
		req, ok := p["request"].(map[string]any)
		require.True(t, ok, "phase %s carries the request", phase)
		assert.Equal(t, callerCallID, req["CallID"])
		assert.Equal(t, wantCaller, req["Caller"], "phase %s", phase)
	}

	var sessionPhases []string
	for _, p := range sessionBox.payloads(t) {
		sessionPhases = append(sessionPhases, p["phase"].(string))
		event, ok := p["event"].(map[string]any)
		require.True(t, ok, "phase %v carries the event", p["phase"])
		assert.Equal(t, wantCaller, event["Caller"], "phase %v", p["phase"])
	}
	assert.Equal(t, []string{"session_start", "session_end"}, sessionPhases)
}
