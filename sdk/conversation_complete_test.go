package sdk

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// completeEvalLog records each eval result the conversation reports, by eval
// id, with the texts of the transcript the eval saw.
type completeEvalLog struct {
	mu   sync.Mutex
	seen map[string][][]string
}

func (l *completeEvalLog) Name() string { return "complete-eval-log" }

func (l *completeEvalLog) OnEvalResult(_ context.Context, def *evals.EvalDef, ec *evals.EvalContext, _ *evals.EvalResult) {
	var texts []string
	for i := range ec.Messages {
		texts = append(texts, ec.Messages[i].GetContent())
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.seen == nil {
		l.seen = map[string][][]string{}
	}
	l.seen[def.ID] = append(l.seen[def.ID], texts)
}

// counts reports how many times each eval ran.
func (l *completeEvalLog) counts() map[string]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]int{}
	for id, runs := range l.seen {
		out[id] = len(runs)
	}
	return out
}

// transcripts returns the transcripts eval id saw, one per run.
func (l *completeEvalLog) transcripts(id string) [][]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([][]string(nil), l.seen[id]...)
}

// writeApprovalEvalPack writes a pack whose prompt can call send_message and
// that declares one session eval and one conversation eval.
func writeApprovalEvalPack(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "approval.pack.json")
	body := `{
      "$schema": "https://promptpack.org/schema/latest/promptpack.schema.json",
      "id": "approval", "name": "approval", "version": "1.0.0",
      "template_engine": {"version": "v1", "syntax": "{{variable}}"},
      "evals": [
        {"id": "sess", "type": "sess_stub", "trigger": "on_session_complete"},
        {"id": "conv", "type": "conv_stub", "trigger": "on_conversation_complete"}
      ],
      "prompts": {"assist": {"id": "assist", "name": "a", "version": "1.0.0",
        "system_template": "You help.",
        "tools": ["send_message"]}},
      "tools": {"send_message": {"name": "send_message", "description": "send",
        "parameters": {"type": "object", "properties": {"body": {"type": "string"}}}}}
    }`
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

// A conversation that suspends while a held tool call waits for a human, and
// is resumed in another conversation when the approval arrives, runs its
// session evals at each close and its conversation evals once, when the host
// completes it, over the whole transcript (#2657).
func TestComplete_SuspendedForApprovalScoresConversationOnce(t *testing.T) {
	repo := newGateTurnRepo()
	repo.add(1, mock.Turn{
		Type:      "tool_calls",
		Content:   "Sending.",
		ToolCalls: []mock.ToolCall{{Name: "send_message", Arguments: map[string]any{"body": "hi"}}},
	})
	repo.add(2, mock.Turn{Type: "text", Content: "Sent it."})
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&stepRecordingHandler{typeName: "sess_stub"})
	registry.Register(&stepRecordingHandler{typeName: "conv_stub"})
	pending := sdktools.NewMemoryPendingStore()
	t.Cleanup(func() { _ = pending.Close() })
	state := statestore.NewMemoryStore()
	packPath := writeApprovalEvalPack(t)
	log := &completeEvalLog{}
	opts := []Option{
		WithProvider(mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)),
		WithSkipSchemaValidation(),
		WithEvalRegistry(registry),
		WithEvalHook(log),
		WithPendingStore(pending),
		WithStateStore(state),
	}
	ctx := context.Background()

	// The turn holds the call for approval; the host suspends the conversation.
	held, err := Open(packPath, "assist", append(opts, WithConversationID("refund-1"))...)
	require.NoError(t, err)
	holdEverySendMessage(held)
	resp, err := held.Send(ctx, "message the customer")
	require.NoError(t, err)
	require.Len(t, resp.PendingTools(), 1)
	require.NoError(t, held.Close())
	assert.Equal(t, map[string]int{"sess": 1}, log.counts(),
		"a suspension ends a session, not the conversation")

	// The approval arrives: the host resumes, settles the call and finishes.
	resumed, err := Resume("refund-1", packPath, "assist", opts...)
	require.NoError(t, err)
	_, ran := holdEverySendMessage(resumed)
	calls, err := resumed.PendingTools(ctx)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	_, err = resumed.ResolveTool(ctx, calls[0].ID)
	require.NoError(t, err)
	final, err := resumed.Continue(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Sent it.", final.Text())
	assert.EqualValues(t, 1, ran.Load())
	require.NoError(t, resumed.Complete())

	assert.Equal(t, map[string]int{"sess": 2, "conv": 1}, log.counts(),
		"each session is scored, the conversation once")
	conv := log.transcripts("conv")[0]
	assert.Contains(t, conv, "message the customer", "the conversation eval sees the suspended session's turn")
	assert.Contains(t, conv, "Sent it.", "and the resumed session's reply")

	assert.ErrorIs(t, resumed.Complete(), ErrConversationClosed, "a completed conversation cannot complete again")
	assert.NoError(t, resumed.Close(), "Close stays a no-op on a closed conversation")
	assert.Equal(t, map[string]int{"sess": 2, "conv": 1}, log.counts(), "neither call ran the evals again")
}

// Complete on a conversation that was closed runs nothing and says so: the
// session evals ran on the close, and the conversation evals cannot run on a
// released conversation.
func TestComplete_AfterCloseRunsNoEvals(t *testing.T) {
	registry := evals.NewEvalTypeRegistry()
	registry.Register(&stepRecordingHandler{typeName: "sess_stub"})
	registry.Register(&stepRecordingHandler{typeName: "conv_stub"})
	log := &completeEvalLog{}
	c, err := Open(writeWorkflowTestPack(t, lifecyclePackJSON), "chat",
		WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithEvalRegistry(registry),
		WithEvalHook(log),
	)
	require.NoError(t, err)
	_, err = c.Send(context.Background(), "hello")
	require.NoError(t, err)

	require.NoError(t, c.Close())
	assert.ErrorIs(t, c.Complete(), ErrConversationClosed)
	assert.Equal(t, map[string]int{"sess": 1}, log.counts())
}
