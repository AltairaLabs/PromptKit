package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TestCaller_TransitionAuthorizerSeesTheCaller — the host's authorizer is
// asked about a model-fired transition and a host-fired one; both requests
// carry the caller set with WithCaller, claims included.
func TestCaller_TransitionAuthorizerSeesTheCaller(t *testing.T) {
	member := types.Caller{ID: "member@example.com", Claims: map[string]any{"tier": "gold"}}
	policy := &recordingPolicy{}
	provider := &eventScriptProvider{
		ToolProvider: mock.NewToolProvider("mock", "mock-model", false, nil),
		events:       []string{"InfoComplete"},
	}
	wc, err := OpenWorkflow(writeWorkflowTestPack(t, workflowPackJSON),
		WithProvider(provider), WithSkipSchemaValidation(),
		WithTransitionAuthorizer(policy), WithCaller(member))
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })

	_, err = wc.Send(context.Background(), "hello")
	require.NoError(t, err)
	require.Equal(t, "processing", wc.CurrentState())

	_, err = wc.Transition("Done")
	require.NoError(t, err)
	require.Equal(t, "confirmation", wc.CurrentState())

	require.Len(t, policy.seen, 2)
	require.Equal(t, "InfoComplete", policy.seen[0].Event)
	require.Equal(t, member, policy.seen[0].Caller, "model-fired transition")
	require.Equal(t, "Done", policy.seen[1].Event)
	require.Equal(t, member, policy.seen[1].Caller, "host-fired transition")
}

// TestCaller_TransitionAuthorizerSeesNoCallerWhenUnset pins the default.
func TestCaller_TransitionAuthorizerSeesNoCallerWhenUnset(t *testing.T) {
	policy := &recordingPolicy{}
	wc, _ := openAuthorizedWorkflow(t, policy)

	_, err := wc.Transition("InfoComplete")
	require.NoError(t, err)

	require.Len(t, policy.seen, 1)
	require.Equal(t, types.Caller{}, policy.seen[0].Caller)
}
