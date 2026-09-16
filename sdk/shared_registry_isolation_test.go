package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// callOnceProvider calls one named tool on its first round, then answers with
// text, so a test can observe which conversation's handler ran.
type callOnceProvider struct {
	*toolGrantProvider
	tool string
}

func newCallOnceProvider(tool string) *callOnceProvider {
	return &callOnceProvider{toolGrantProvider: newToolGrantProvider(), tool: tool}
}

func (p *callOnceProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	p.mu.Lock()
	p.rounds++
	round := p.rounds
	p.mu.Unlock()

	if round == 1 {
		calls := []types.MessageToolCall{{ID: "c1", Name: p.tool, Args: []byte(`{}`)}}
		return providers.PredictionResponse{ToolCalls: calls}, calls, nil
	}
	return providers.PredictionResponse{Content: "done"}, nil, nil
}

// Two conversations sharing one tools.Registry must each run their OWN local
// tool handlers.
//
// WithToolRegistry is public, and capability/executor registration is guarded
// per conversation rather than per registry, so the second conversation's
// localExecutor overwrites the first's by mode key -- and it carries a live
// accessor pointing at its own conversation. The first conversation's OnTool
// handlers then dispatch through the second's. This is the core tool path, so
// it is the worst instance of the class #2011 describes.
func TestLocalHandlers_DoNotLeakAcrossASharedRegistry(t *testing.T) {
	shared := sharedPackRegistry(t)
	packPath := writeWorkflowTestPack(t, toolGrantPackJSON)

	open := func(answer string) *Conversation {
		conv, err := Open(packPath, "chat",
			WithProvider(newCallOnceProvider("lookup_order")),
			WithSkipSchemaValidation(),
			WithToolRegistry(shared),
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = conv.Close() })
		conv.OnTool("lookup_order", func(map[string]any) (any, error) { return answer, nil })
		return conv
	}

	alice := open("alice-result")
	bob := open("bob-result")

	aliceRan := make(chan string, 1)
	alice.OnTool("lookup_order", func(map[string]any) (any, error) {
		aliceRan <- "alice"
		return "alice-result", nil
	})
	bobRan := make(chan string, 1)
	bob.OnTool("lookup_order", func(map[string]any) (any, error) {
		bobRan <- "bob"
		return "bob-result", nil
	})

	_, err := alice.Send(context.Background(), "look it up")
	require.NoError(t, err)

	select {
	case who := <-aliceRan:
		assert.Equal(t, "alice", who)
	default:
		t.Fatal("alice's own handler never ran; her tool call went to another conversation")
	}
	assert.Empty(t, bobRan, "bob's handler must not run for alice's tool call")
}

// The reverse direction: the conversation that registered LAST must still run
// its own handlers. Without both directions the test passes on the accident of
// registration order rather than on isolation.
func TestLocalHandlers_LastRegisteredConversationKeepsItsOwn(t *testing.T) {
	shared := sharedPackRegistry(t)
	packPath := writeWorkflowTestPack(t, toolGrantPackJSON)

	open := func() *Conversation {
		conv, err := Open(packPath, "chat",
			WithProvider(newCallOnceProvider("lookup_order")),
			WithSkipSchemaValidation(),
			WithToolRegistry(shared),
		)
		require.NoError(t, err)
		t.Cleanup(func() { _ = conv.Close() })
		return conv
	}

	alice, bob := open(), open()

	ran := make(chan string, 2)
	alice.OnTool("lookup_order", func(map[string]any) (any, error) {
		ran <- "alice"
		return "ok", nil
	})
	bob.OnTool("lookup_order", func(map[string]any) (any, error) {
		ran <- "bob"
		return "ok", nil
	})

	_, err := bob.Send(context.Background(), "look it up")
	require.NoError(t, err)
	require.Len(t, ran, 1, "exactly one handler must have run")
	assert.Equal(t, "bob", <-ran, "bob's send must run bob's handler")
}

// sharedPackRegistry is the registry a host passes to WithToolRegistry: it
// already carries the pack's tool descriptors, because WithToolRegistry
// bypasses the pack-derived repository the SDK builds by default.
func sharedPackRegistry(t *testing.T) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	for _, name := range []string{"lookup_order", "refund"} {
		require.NoError(t, reg.Register(&tools.ToolDescriptor{
			Name:        name,
			Description: name,
			Mode:        "local",
			InputSchema: []byte(`{"type":"object"}`),
		}))
	}
	return reg
}

// The nil guards on the accessor context are reachable: a nil conversation
// attaches nothing, and a context with no accessors reports none, which is what
// keeps the pre-existing snapshot-then-live path alive for hosts that never
// go through Send (direct registry use, tests, embedded pipelines).
func TestConversationHandlersContext_NilIsANoOp(t *testing.T) {
	ctx := context.Background()

	assert.Nil(t, conversationHandlersFromContext(ctx))
	assert.Equal(t, ctx, withLocalHandlers(ctx, nil))
	//nolint:staticcheck // deliberately probing the nil-context guard
	assert.Nil(t, conversationHandlersFromContext(nil))
}

// An executor with accessors on the context but no matching handler still
// reports the tool as unhandled, rather than silently succeeding.
func TestLocalHandlers_UnhandledToolStillErrors(t *testing.T) {
	packPath := writeWorkflowTestPack(t, toolGrantPackJSON)
	conv, err := Open(packPath, "chat",
		WithProvider(newCallOnceProvider("lookup_order")),
		WithSkipSchemaValidation(),
		WithToolRegistry(sharedPackRegistry(t)),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	own := conversationHandlersFromContext(withLocalHandlers(context.Background(), conv))
	require.NotNil(t, own)

	_, ok := own.local.getHandler("lookup_order")
	assert.False(t, ok, "no handler was registered")
	_, ok = own.client.getHandler("lookup_order")
	assert.False(t, ok, "no client handler was registered")
}
