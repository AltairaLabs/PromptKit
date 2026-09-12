package sdk

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// budgetTurnRepo scripts the LLM by call order rather than turn number: the
// first provider round asks for two tools, every later round replies with text
// so the loop terminates.
type budgetTurnRepo struct {
	mu    sync.Mutex
	calls int
}

func (r *budgetTurnRepo) GetResponse(ctx context.Context, p mock.ResponseParams) (string, error) {
	t, err := r.GetTurn(ctx, p)
	if err != nil {
		return "", err
	}
	return t.Content, nil
}

func (r *budgetTurnRepo) GetTurn(_ context.Context, _ mock.ResponseParams) (*mock.Turn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.calls == 1 {
		return &mock.Turn{
			Type:    "tool_calls",
			Content: "Noting both.",
			ToolCalls: []mock.ToolCall{
				{Name: "note", Arguments: map[string]any{"text": "one"}},
				{Name: "note", Arguments: map[string]any{"text": "two"}},
			},
		}, nil
	}
	return &mock.Turn{Type: "text", Content: "Done."}, nil
}

// A workflow whose engine.budget caps tool calls at 2.
const toolBudgetPackJSON = `{
	"$schema": "https://promptpack.org/schema/2025.1/promptpack.schema.json",
	"schema_version": "2025.1",
	"id": "tool-budget-pack",
	"version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "handlebars", "features": []},
	"prompts": {
		"work_prompt": {
			"id": "work_prompt", "name": "Work", "description": "does work",
			"version": "1.0.0", "system_template": "You work.", "tools": ["note"]
		},
		"done_prompt": {
			"id": "done_prompt", "name": "Done", "description": "finishes",
			"version": "1.0.0", "system_template": "You are done."
		}
	},
	"tools": {
		"note": {"name": "note", "description": "record a note",
			"parameters": {"type": "object", "properties": {"text": {"type": "string"}}}}
	},
	"workflow": {
		"version": 1,
		"entry": "work",
		"engine": {"budget": {"max_tool_calls": 2}},
		"states": {
			"work": {"prompt_task": "work_prompt", "on_event": {"Finish": "done"}},
			"done": {"prompt_task": "done_prompt"}
		}
	}
}`

func writeToolBudgetPack(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "budget.pack.json")
	require.NoError(t, os.WriteFile(p, []byte(toolBudgetPackJSON), 0o600))
	return p
}

// RFC 0009's max_tool_calls, end to end. This is the seam the old unit tests
// could not see: machine_test.go called IncrementToolCalls directly, so the
// budget check was covered while the counter had no production caller at all
// and the limit could never fire.
func TestWorkflow_ToolCallsCountTowardBudget(t *testing.T) {
	packPath := writeToolBudgetPack(t)
	// NewToolProvider, not NewProvider: the plain mock never emits tool calls,
	// so a budget test built on it would pass by measuring nothing.
	provider := mock.NewToolProviderWithRepository("mock", "mock-model", false, &budgetTurnRepo{})

	wc, err := OpenWorkflow(packPath, WithSkipSchemaValidation(), WithProvider(provider))
	require.NoError(t, err)
	defer wc.Close()

	var noted []string
	var mu sync.Mutex
	wc.ActiveConversation().OnTool("note", func(args map[string]any) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		noted = append(noted, args["text"].(string))
		return map[string]any{"ok": true}, nil
	})

	_, err = wc.Send(context.Background(), "note two things")
	require.NoError(t, err)

	mu.Lock()
	require.Len(t, noted, 2, "both tools must actually run — otherwise the count proves nothing")
	mu.Unlock()

	assert.Equal(t, 2, wc.Context().TotalToolCalls,
		"executed tool calls must reach the workflow context")

	// Budget is 2 and 2 have run, so the next transition must refuse.
	_, err = wc.Transition("Finish")
	require.Error(t, err)
	require.ErrorIs(t, err, workflow.ErrBudgetExhausted)

	var budgetErr *workflow.BudgetExhaustedError
	require.ErrorAs(t, err, &budgetErr)
	assert.Equal(t, workflow.BudgetLimitToolCalls, budgetErr.Limit)
	assert.Equal(t, 2, budgetErr.Current)
	assert.Equal(t, 2, budgetErr.Max)
	assert.Equal(t, "work", wc.CurrentState(), "an exhausted budget must not advance the state")
}

// recordingInner counts forwarded calls for the holder delegation test.
type recordingInner struct {
	workflowStateResolver
	got []int
}

func (r *recordingInner) RecordToolCalls(n int) { r.got = append(r.got, n) }

// The holder is what the pipeline installs, so a RecordToolCalls that stops at
// the holder reaches no workflow at all — the exact shape of the bug being
// fixed here, one layer up.
func TestWorkflowResolverHolder_RecordToolCallsDelegates(t *testing.T) {
	inner := &recordingInner{}
	holder := &workflowResolverHolder{}
	holder.set(inner)

	holder.RecordToolCalls(3)

	assert.Equal(t, []int{3}, inner.got, "the holder must forward the count to the inner resolver")
}

func TestWorkflowResolverHolder_RecordToolCallsIsSafeWhenUnset(t *testing.T) {
	var nilHolder *workflowResolverHolder
	empty := &workflowResolverHolder{}
	plain := &workflowResolverHolder{}
	plain.set(&workflowStateResolver{}) // inner without a RecordToolCalls method

	require.NotPanics(t, func() {
		nilHolder.RecordToolCalls(1)
		empty.RecordToolCalls(1)
		plain.RecordToolCalls(1)
	})
}
