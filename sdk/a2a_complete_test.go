package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	a2aserver "github.com/AltairaLabs/PromptKit/server/a2a/v2"
)

// openA2AForEvals opens a conversation through A2AOpener, as the A2A server
// does, on a pack with one session eval and one conversation eval, and sends
// it one message.
func openA2AForEvals(t *testing.T) (a2aserver.Conversation, *completeEvalLog) {
	t.Helper()
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&stepRecordingHandler{typeName: "sess_stub"})
	registry.Register(&stepRecordingHandler{typeName: "conv_stub"})
	log := &completeEvalLog{}
	open := A2AOpener(writeWorkflowTestPack(t, lifecyclePackJSON), "chat",
		WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithEvalRegistry(registry),
		WithEvalHook(log),
	)
	conv, err := open("ctx-1")
	require.NoError(t, err)
	_, err = conv.Send(context.Background(), "hello")
	require.NoError(t, err)
	return conv, log
}

// The A2A server completes an evicted conversation through
// a2aserver.CompletableConversation (WithCompleteOnEviction). An SDK
// conversation completed that way runs its conversation evals once.
func TestA2AOpener_ConversationCompletesWithItsConversationEvals(t *testing.T) {
	conv, log := openA2AForEvals(t)
	completable, ok := conv.(a2aserver.CompletableConversation)
	require.True(t, ok, "the server can complete an SDK conversation")

	require.NoError(t, completable.Complete())
	assert.Equal(t, map[string]int{"sess": 1, "conv": 1}, log.counts())
	assert.ErrorIs(t, completable.Complete(), ErrConversationClosed)
	assert.Equal(t, map[string]int{"sess": 1, "conv": 1}, log.counts(), "a second Complete runs nothing")
}

// Close, which the server calls on Shutdown and on eviction by default, ends
// the session only.
func TestA2AOpener_ConversationCloseRunsSessionEvalsOnly(t *testing.T) {
	conv, log := openA2AForEvals(t)
	require.NoError(t, conv.Close())
	assert.Equal(t, map[string]int{"sess": 1}, log.counts())
}
