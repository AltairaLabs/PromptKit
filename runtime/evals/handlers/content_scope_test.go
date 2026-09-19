package handlers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Content-matching handlers serve two callers with different scopes, and the
// scope must be explicit rather than inferred.
//
// evals.BuildEvalContext ALWAYS populates CurrentOutput (context.go), so
// "CurrentOutput is set" cannot mean "examine only this message" — every eval
// and assertion would silently collapse to the last assistant turn.
// The guardrail adapter, which evaluates one specific message, says so via
// EvalContext.ContentScope.

// TestContentExcludes_TranscriptScopeScansEveryAssistantTurn pins the eval and
// assertion contract: a banned word anywhere in the conversation is found, even
// when CurrentOutput points at a later, clean turn.
func TestContentExcludes_TranscriptScopeScansEveryAssistantTurn(t *testing.T) {
	h := &ContentExcludesHandler{}

	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "assistant", Content: "here is the forbidden content"},
			{Role: "user", Content: "thanks"},
			{Role: "assistant", Content: "you are welcome"},
		},
		// As BuildEvalContext would set it: the latest assistant turn.
		CurrentOutput: "you are welcome",
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"patterns": []string{"forbidden"},
	})
	require.NoError(t, err)
	require.NotNil(t, result.Score)

	assert.Less(t, *result.Score, 1.0,
		"a transcript-scope eval must still find a banned word in an earlier assistant turn")
}

// TestContainsAny_TranscriptScopeScansEveryAssistantTurn is the same contract
// for the inverse handler: "did the assistant mention X at any point".
func TestContainsAny_TranscriptScopeScansEveryAssistantTurn(t *testing.T) {
	h := &ContainsAnyHandler{}

	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "assistant", Content: "the answer is 42"},
			{Role: "user", Content: "thanks"},
			{Role: "assistant", Content: "you are welcome"},
		},
		CurrentOutput: "you are welcome",
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"patterns": []string{"42"},
	})
	require.NoError(t, err)
	require.NotNil(t, result.Score)

	assert.GreaterOrEqual(t, *result.Score, 1.0,
		"a transcript-scope eval must find a pattern from an earlier assistant turn")
}

// TestContentExcludes_CurrentScopeExaminesOnlyContentUnderTest pins the
// guardrail contract: when the caller declares current-message scope, an
// earlier turn must NOT taint the verdict, or an output guardrail would
// re-block every subsequent turn forever once one turn tripped it.
func TestContentExcludes_CurrentScopeExaminesOnlyContentUnderTest(t *testing.T) {
	h := &ContentExcludesHandler{}

	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "assistant", Content: "here is the forbidden content"},
			{Role: "user", Content: "thanks"},
			{Role: "assistant", Content: "you are welcome"},
		},
		CurrentOutput: "you are welcome",
		ContentScope:  evals.ContentScopeCurrent,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"patterns": []string{"forbidden"},
	})
	require.NoError(t, err)
	require.NotNil(t, result.Score)

	assert.GreaterOrEqual(t, *result.Score, 1.0,
		"current-message scope must ignore an earlier turn's banned word")
}

// TestContentExcludes_CurrentScopeSeesUserInput pins the input-guardrail case:
// with current scope the content under test is the user's message, which has no
// assistant role and would otherwise be invisible.
func TestContentExcludes_CurrentScopeSeesUserInput(t *testing.T) {
	h := &ContentExcludesHandler{}

	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "user", Content: "please arrange a wire transfer"},
		},
		CurrentOutput: "please arrange a wire transfer",
		ContentScope:  evals.ContentScopeCurrent,
	}

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"patterns": []string{"wire transfer"},
	})
	require.NoError(t, err)
	require.NotNil(t, result.Score)

	assert.Less(t, *result.Score, 1.0,
		"current-message scope must examine the user's message for an input guardrail")
}
