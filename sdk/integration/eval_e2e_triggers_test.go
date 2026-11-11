package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	sdk "github.com/AltairaLabs/PromptKit/sdk/v2"
)

// costlyCloseEvals has a cheap per-turn check and close-time evals in the same
// (default, fast-running) group, so no group filter can separate them (#2518).
const costlyCloseEvals = lifecycleEvals + `,
	{"id": "sampled-session", "type": "min_length", "trigger": "sample_sessions", "sample_percentage": 100,
	 "params": {"min": 1}}`

// A host that opens a conversation per message, closes it when the message's
// stream ends and resumes it for the next one runs per-turn evals inline with
// WithEvalTriggers(TriggerEveryTurn), and no Close in the cycle runs a session
// or conversation eval.
func TestE2E_EvalTriggers_PerTurnHostRunsNoCloseEvals(t *testing.T) {
	packPath := writePackFile(t, e2ePack("", costlyCloseEvals, ""))
	store := statestore.NewMemoryStore()
	perTurn := sdk.WithEvalTriggers(evals.TriggerEveryTurn)

	total := map[string]int{}
	for i, msg := range []string{"one", "two", "three"} {
		conv, log := openStored(t, packPath, "per-message", store, i > 0, perTurn)
		sendAndClose(t, conv, msg)
		for id, n := range log.ids() {
			total[id] += n
		}
		assert.Equalf(t, map[string]int{"turn": 1}, log.ids(), "cycle %d", i+1)
	}
	assert.Equal(t, map[string]int{"turn": 3}, total)
}

// The same cycle without the option runs the session evals again on every
// Close; the conversation evals wait for Complete.
func TestE2E_EvalTriggers_UnsetKeepsSessionEvalsOnClose(t *testing.T) {
	packPath := writePackFile(t, e2ePack("", costlyCloseEvals, ""))
	store := statestore.NewMemoryStore()
	for i, msg := range []string{"one", "two"} {
		conv, log := openStored(t, packPath, "per-message", store, i > 0)
		sendAndClose(t, conv, msg)
		assert.Equalf(t, map[string]int{"turn": 1, "session": 1, "sampled-session": 1},
			log.ids(), "cycle %d", i+1)
	}
	conv, log := openStored(t, packPath, "per-message", store, true)
	sendAndComplete(t, conv, "three")
	assert.Equal(t, map[string]int{"turn": 1, "session": 1, "sampled-session": 1, "conversation": 1}, log.ids())
}

// Naming only close-time triggers runs those and no per-turn eval; naming
// several runs the union.
func TestE2E_EvalTriggers_SelectsByTrigger(t *testing.T) {
	packJSON := e2ePack("", costlyCloseEvals, "")
	for name, tc := range map[string]struct {
		triggers []evals.EvalTrigger
		want     map[string]int
	}{
		"conversation only": {
			[]evals.EvalTrigger{evals.TriggerOnConversationComplete}, map[string]int{"conversation": 1},
		},
		"turn and session": {
			[]evals.EvalTrigger{evals.TriggerEveryTurn, evals.TriggerOnSessionComplete},
			map[string]int{"turn": 2, "session": 1},
		},
		"session sampling": {
			[]evals.EvalTrigger{evals.TriggerSampleSessions}, map[string]int{"sampled-session": 1},
		},
	} {
		t.Run(name, func(t *testing.T) {
			conv, log := openE2E(t, packJSON, "hello", sdk.WithEvalTriggers(tc.triggers...))
			sendAndComplete(t, conv, "a", "b")
			assert.Equal(t, tc.want, log.ids())
		})
	}
}

// The filter composes with WithEvalGroups, as the issue's example uses them.
func TestE2E_EvalTriggers_ComposesWithGroups(t *testing.T) {
	judge := &countingJudge{}
	packJSON := e2ePack("", lifecycleEvals+`,
	{"id": "close-judge", "type": "llm_judge_session", "trigger": "on_conversation_complete",
	 "params": {"criteria": "helpful"}}`, "")
	conv, log := openE2E(t, packJSON, "hello",
		sdk.WithJudgeProvider(judge),
		sdk.WithEvalGroups(evals.GroupFastRunning),
		sdk.WithEvalTriggers(evals.TriggerEveryTurn))
	sendAndComplete(t, conv, "a")
	assert.Equal(t, map[string]int{"turn": 1}, log.ids())
	assert.Zero(t, judge.calls)
}

// A trigger the runtime does not know, or no trigger at all, fails Open rather
// than silently running nothing.
func TestE2E_EvalTriggers_RejectsUnknownAndEmpty(t *testing.T) {
	packPath := writePackFile(t, e2ePack("", lifecycleEvals, ""))
	_, err := sdk.Open(packPath, "chat", sdk.WithProvider(replyProvider("hi")),
		sdk.WithEvalTriggers("every_tunr"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), `"every_tunr"`)
	assert.Contains(t, err.Error(), evals.TriggerEveryTurn, "the error lists the valid triggers")

	_, err = sdk.Open(packPath, "chat", sdk.WithProvider(replyProvider("hi")), sdk.WithEvalTriggers())
	require.Error(t, err)
}

// In a workflow each state's conversation takes the filter, so a state change
// (which closes the outgoing state's conversation) runs no close-time evals.
func TestE2E_EvalTriggers_WorkflowStateChange(t *testing.T) {
	packJSON := workflowEvalPackWithCloseEvals
	log := &resultLog{}
	wc, err := sdk.OpenWorkflow(writePackFile(t, packJSON),
		sdk.WithProvider(replyProvider("noted")), sdk.WithEvalHook(log),
		sdk.WithEvalTriggers(evals.TriggerEveryTurn, evals.TriggerOnWorkflowStep))
	require.NoError(t, err)
	t.Cleanup(func() { _ = wc.Close() })

	_, err = wc.Send(context.Background(), "help")
	require.NoError(t, err)
	_, err = wc.Transition("Escalate")
	require.NoError(t, err)
	require.NoError(t, wc.Complete())
	assert.Equal(t, map[string]int{"turn": 1, "step": 1}, log.ids())
}

// A workflow's state change and Close end sessions; Complete finishes the
// workflow and runs the conversation evals once, for the state it finished in.
func TestE2E_WorkflowComplete_RunsConversationEvalsOnce(t *testing.T) {
	for name, tc := range map[string]struct {
		finish func(*sdk.WorkflowConversation) error
		want   int
	}{
		"close":    {(*sdk.WorkflowConversation).Close, 0},
		"complete": {(*sdk.WorkflowConversation).Complete, 1},
	} {
		t.Run(name, func(t *testing.T) {
			log := &resultLog{}
			wc, err := sdk.OpenWorkflow(writePackFile(t, workflowEvalPackWithCloseEvals),
				sdk.WithProvider(replyProvider("noted")), sdk.WithEvalHook(log))
			require.NoError(t, err)
			t.Cleanup(func() { _ = wc.Close() })

			_, err = wc.Send(context.Background(), "help")
			require.NoError(t, err)
			_, err = wc.Transition("Escalate")
			require.NoError(t, err)
			_, err = wc.Send(context.Background(), "still there?")
			require.NoError(t, err)
			require.NoError(t, tc.finish(wc))
			assert.Len(t, log.byID("conversation"), tc.want)
			if tc.want > 0 {
				assert.Equal(t, []int{4}, log.messagesSeen("conversation"),
					"the conversation eval scores both states' turns")
			}
			assert.ErrorIs(t, wc.Complete(), sdk.ErrWorkflowClosed, "a finished workflow cannot complete again")
		})
	}
}

// workflowEvalPackWithCloseEvals is workflowEvalPack plus a conversation eval.
var workflowEvalPackWithCloseEvals = replaceOnce(workflowEvalPack,
	`{"id": "step",`,
	`{"id": "conversation", "type": "min_length", "trigger": "on_conversation_complete", "params": {"min": 1}},
		{"id": "step",`)
