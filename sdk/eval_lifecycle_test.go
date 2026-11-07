package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// lifecyclePackJSON declares one eval per close-time trigger, each with its own
// recording stub type so a test can tell which one ran.
const lifecyclePackJSON = `{
	"$schema": "https://promptpack.org/schema/2025.1/promptpack.schema.json",
	"schema_version": "2025.1",
	"id": "eval-lifecycle-pack",
	"version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "handlebars", "features": []},
	"evals": [
		{"id": "conv", "type": "conv_stub", "trigger": "on_conversation_complete"},
		{"id": "sess", "type": "sess_stub", "trigger": "on_session_complete"}
	],
	"prompts": {
		"chat": {
			"id": "chat", "name": "Chat", "description": "Chat",
			"version": "1.0.0", "system_template": "You chat."
		}
	}
}`

// Closing a conversation runs its on_conversation_complete evals once, against
// the whole transcript, alongside its on_session_complete evals. Nothing runs
// them before Close.
func TestConversationEvals_RunOnClose(t *testing.T) {
	bus, col, registry, _ := newStepEvalHarness(t)
	conv := &stepRecordingHandler{typeName: "conv_stub"}
	registry.Register(conv)
	registry.Register(&stepRecordingHandler{typeName: "sess_stub"})
	packPath := writeWorkflowTestPack(t, lifecyclePackJSON)

	c, err := Open(packPath, "chat",
		WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithEvalRegistry(registry),
		WithEventBus(bus),
	)
	require.NoError(t, err)

	_, err = c.Send(context.Background(), "hello")
	require.NoError(t, err)
	assert.Empty(t, conv.transcripts(), "a conversation eval must not run before the conversation ends")

	require.NoError(t, c.Close())
	require.NoError(t, c.Close(), "a second Close is a no-op")

	require.Eventually(t, func() bool {
		return len(col.byTrigger(evals.TriggerOnConversationComplete)) >= 1
	}, 5*time.Second, 5*time.Millisecond, "Close must run the on_conversation_complete evals")
	time.Sleep(20 * time.Millisecond)

	got := col.byTrigger(evals.TriggerOnConversationComplete)
	require.Len(t, got, 1, "one Close runs the conversation evals once")
	assert.Equal(t, "conv", got[0].EvalID)
	assert.Equal(t, 1, got[0].TurnIndex, "the result carries the conversation's last turn")
	assert.Len(t, col.byTrigger(evals.TriggerOnSessionComplete), 1, "session evals still run on Close")

	seen := conv.transcripts()
	require.Len(t, seen, 1)
	assert.Contains(t, seen[0], "hello", "the conversation eval sees the whole transcript")
}

// A conversation closed with nothing said — after a failed open, or never
// used — has no transcript, so Close runs no conversation evals on it.
func TestConversationEvals_SkippedOnEmptyTranscript(t *testing.T) {
	bus, col, registry, _ := newStepEvalHarness(t)
	conv := &stepRecordingHandler{typeName: "conv_stub"}
	registry.Register(conv)
	registry.Register(&stepRecordingHandler{typeName: "sess_stub"})
	packPath := writeWorkflowTestPack(t, lifecyclePackJSON)

	c, err := Open(packPath, "chat",
		WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithEvalRegistry(registry),
		WithEventBus(bus),
	)
	require.NoError(t, err)
	require.NoError(t, c.Close())

	require.Eventually(t, func() bool { return len(col.byTrigger(evals.TriggerOnSessionComplete)) >= 1 },
		5*time.Second, 5*time.Millisecond, "the session evals still ran, so the close was observed")
	time.Sleep(20 * time.Millisecond)
	assert.Empty(t, conv.transcripts())
	assert.Empty(t, col.byTrigger(evals.TriggerOnConversationComplete))
}

// slowJudgeHandler signals when its eval starts and blocks until released,
// standing in for a slow judge.
type slowJudgeHandler struct {
	typeName string
	started  chan struct{}
	release  chan struct{}
}

func (h *slowJudgeHandler) Type() string { return h.typeName }

func (h *slowJudgeHandler) Eval(context.Context, *evals.EvalContext, map[string]any) (*evals.EvalResult, error) {
	close(h.started)
	<-h.release
	score := 1.0
	return &evals.EvalResult{Score: &score}, nil
}

// A slow on_conversation_complete eval runs without the conversation's lock
// held, so it does not block other calls on the Conversation while Close waits
// for it. The closed conversation answers them at once.
func TestConversationEvals_DoNotHoldConversationLock(t *testing.T) {
	bus, _, registry, _ := newStepEvalHarness(t)
	slow := &slowJudgeHandler{typeName: "conv_stub", started: make(chan struct{}), release: make(chan struct{})}
	registry.Register(slow)
	registry.Register(&stepRecordingHandler{typeName: "sess_stub"})
	packPath := writeWorkflowTestPack(t, lifecyclePackJSON)

	c, err := Open(packPath, "chat",
		WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithEvalRegistry(registry),
		WithEventBus(bus),
	)
	require.NoError(t, err)
	_, err = c.Send(context.Background(), "hello")
	require.NoError(t, err)

	closed := make(chan error, 1)
	go func() { closed <- c.Close() }()
	<-slow.started

	answered := make(chan struct{})
	go func() {
		c.GetVar("anything")
		close(answered)
	}()
	select {
	case <-answered:
	case <-time.After(2 * time.Second):
		close(slow.release)
		t.Fatal("GetVar blocked while a conversation eval ran: Close held the conversation lock")
	}
	close(slow.release)
	require.NoError(t, <-closed)
}
