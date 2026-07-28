package evals

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildGuardrailEvalContext_EnrichesFromHistory pins the fields the
// guardrail adapter used to leave zeroed (#1704). Every one is derivable from
// the message history, so a handler must see the same data whether it runs as
// an eval or as a guardrail.
func TestBuildGuardrailEvalContext_EnrichesFromHistory(t *testing.T) {
	toolResult := types.NewTextToolResult("c1", "lookup", "shipped")
	messages := []types.Message{
		types.NewUserMessage("where is my order"),
		{
			Role:      "assistant",
			ToolCalls: []types.MessageToolCall{{ID: "c1", Name: "lookup", Args: []byte(`{"id":"A1"}`)}},
			Meta:      map[string]any{"_workflow_current_state": "tracking"},
		},
		{Role: "tool", Content: "shipped", ToolResult: &toolResult},
		{
			Role:        "assistant",
			Content:     "it shipped",
			Validations: []types.ValidationResult{{ValidatorType: "banned_words", Passed: false}},
		},
	}
	metadata := map[string]any{"total_cost": 1.25, "workflow_complete": true}

	ctx := BuildGuardrailEvalContext(messages, "the message under test", metadata)

	require.NotNil(t, ctx)
	assert.Equal(t, metadata, ctx.Metadata, "Metadata must pass through untouched")

	require.Len(t, ctx.ToolCalls, 1, "ToolCalls must be derived from the transcript")
	assert.Equal(t, "lookup", ctx.ToolCalls[0].ToolName)
	assert.Equal(t, map[string]any{"id": "A1"}, ctx.ToolCalls[0].Arguments,
		"the call's JSON arguments must be parsed")
	parts, ok := ctx.ToolCalls[0].Result.([]types.ContentPart)
	require.True(t, ok, "a NewTextToolResult carries Parts, so Result is the parts slice")
	require.Len(t, parts, 1, "the tool-role result message must be paired in by call ID")
	require.NotNil(t, parts[0].Text)
	assert.Equal(t, "shipped", *parts[0].Text)

	assert.Equal(t, "tracking", ctx.Extras["workflow_current_state"],
		"workflow state on message Meta must reach Extras")
	assert.Equal(t, true, ctx.Extras["workflow_complete"],
		"workflow keys on the metadata map must also reach Extras")

	require.Len(t, ctx.PriorResults, 1, "failed validations must seed PriorResults")
	assert.Equal(t, "banned_words", ctx.PriorResults[0].Type)
	require.NotNil(t, ctx.PriorResults[0].Score)
	assert.Equal(t, 0.0, *ctx.PriorResults[0].Score, "a failed validation scores 0")
}

// TestBuildGuardrailEvalContext_StatesContentInsteadOfInferringIt is the
// difference from BuildEvalContext, and it is load-bearing: an input guardrail
// judges the USER's message. Inferring the content under test from the last
// assistant turn is what made banned_words a silent no-op on input (#1679).
func TestBuildGuardrailEvalContext_StatesContentInsteadOfInferringIt(t *testing.T) {
	messages := []types.Message{
		types.NewAssistantMessage("how can I help?"),
		types.NewUserMessage("please arrange a wire transfer"),
	}

	ctx := BuildGuardrailEvalContext(messages, "please arrange a wire transfer", nil)

	assert.Equal(t, "please arrange a wire transfer", ctx.CurrentOutput,
		"the caller states the content; it must not be re-derived from assistant turns")
	assert.Equal(t, ContentScopeCurrent, ctx.ContentScope,
		"a guardrail judges one message, so scope must be pinned to current")

	// Contrast: the eval path legitimately infers the last assistant turn.
	evalCtx := BuildEvalContext(messages, 0, "", "", nil)
	assert.Equal(t, "how can I help?", evalCtx.CurrentOutput)
	assert.Equal(t, ContentScopeTranscript, evalCtx.ContentScope)
}

// TestBuildGuardrailEvalContext_ToleratesNilMetadata covers the nil-map path —
// indexing a nil map for the workflow keys is safe, and a guardrail declared in
// a pack often has no metadata at all.
func TestBuildGuardrailEvalContext_ToleratesNilMetadata(t *testing.T) {
	ctx := BuildGuardrailEvalContext(nil, "text", nil)

	require.NotNil(t, ctx)
	assert.Nil(t, ctx.Metadata)
	assert.Nil(t, ctx.Extras, "no workflow state anywhere means no Extras map")
	assert.Empty(t, ctx.ToolCalls)
	assert.Empty(t, ctx.PriorResults)
	assert.Equal(t, "text", ctx.CurrentOutput)
}

func TestBuildEvalContext_ExtractsCurrentOutput(t *testing.T) {
	messages := []types.Message{
		types.NewUserMessage("hello"),
		types.NewAssistantMessage("first"),
		types.NewUserMessage("followup"),
		types.NewAssistantMessage("second"),
	}

	ctx := BuildEvalContext(messages, 3, "sess-1", "chat", nil)

	assert.Equal(t, "second", ctx.CurrentOutput)
	assert.Equal(t, 3, ctx.TurnIndex)
	assert.Equal(t, "sess-1", ctx.SessionID)
	assert.Equal(t, "chat", ctx.PromptID)
	assert.Len(t, ctx.Messages, 4)
}

func TestBuildEvalContext_NoMessages(t *testing.T) {
	ctx := BuildEvalContext(nil, 0, "sess-1", "chat", nil)

	assert.Empty(t, ctx.CurrentOutput)
	assert.Empty(t, ctx.ToolCalls)
	assert.Nil(t, ctx.Extras)
}

func TestBuildEvalContext_WithMetadata(t *testing.T) {
	metadata := map[string]any{"judge_provider": "mock"}
	ctx := BuildEvalContext(nil, 0, "s1", "p1", metadata)

	assert.Equal(t, "mock", ctx.Metadata["judge_provider"])
}

func TestExtractToolCalls_MatchesResults(t *testing.T) {
	messages := []types.Message{
		types.NewUserMessage("search"),
		{
			Role:    "assistant",
			Content: "searching...",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc-1", Name: "search", Args: []byte(`{"q":"cats"}`)},
			},
		},
		{
			Role:    "tool",
			Content: "found 3",
			ToolResult: &types.MessageToolResult{
				ID: "tc-1",
			},
		},
	}

	calls := ExtractToolCalls(messages)
	require.Len(t, calls, 1)
	assert.Equal(t, "search", calls[0].ToolName)
	assert.Equal(t, 1, calls[0].TurnIndex)
	assert.Equal(t, "cats", calls[0].Arguments["q"])
	assert.Equal(t, "found 3", calls[0].Result)
}

func TestExtractToolCalls_WithError(t *testing.T) {
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc-1", Name: "fail"},
			},
		},
		{
			Role:       "tool",
			ToolResult: &types.MessageToolResult{ID: "tc-1", Error: "boom"},
		},
	}

	calls := ExtractToolCalls(messages)
	require.Len(t, calls, 1)
	assert.Equal(t, "boom", calls[0].Error)
}

func TestExtractToolCalls_NoResult(t *testing.T) {
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc-1", Name: "search"},
			},
		},
	}

	calls := ExtractToolCalls(messages)
	require.Len(t, calls, 1)
	assert.Nil(t, calls[0].Result)
}

func TestExtractToolCalls_MultipartResult(t *testing.T) {
	txt := "generated image"
	messages := []types.Message{
		{
			Role: "assistant",
			ToolCalls: []types.MessageToolCall{
				{ID: "tc-1", Name: "image_gen"},
			},
		},
		{
			Role: "tool",
			ToolResult: &types.MessageToolResult{
				ID: "tc-1",
				Parts: []types.ContentPart{
					{Type: "text", Text: &txt},
				},
			},
		},
	}

	calls := ExtractToolCalls(messages)
	require.Len(t, calls, 1)
	parts, ok := calls[0].Result.([]types.ContentPart)
	require.True(t, ok)
	assert.Equal(t, &txt, parts[0].Text)
}

func TestExtractToolCalls_Empty(t *testing.T) {
	calls := ExtractToolCalls(nil)
	assert.Empty(t, calls)

	calls = ExtractToolCalls([]types.Message{types.NewUserMessage("hi")})
	assert.Empty(t, calls)
}

func TestExtractWorkflowExtras_AllFields(t *testing.T) {
	messages := []types.Message{
		{
			Role: "assistant",
			Meta: map[string]any{
				"_workflow_state":       "greeting",
				"_workflow_transitions": []string{"init", "greeting"},
				"_workflow_complete":    true,
			},
		},
	}

	extras := ExtractWorkflowExtras(messages)
	require.NotNil(t, extras)
	assert.Equal(t, "greeting", extras["workflow_state"])
	assert.Equal(t, true, extras["workflow_complete"])
}

func TestExtractWorkflowExtras_NoWorkflow(t *testing.T) {
	messages := []types.Message{
		{Role: "assistant", Meta: map[string]any{"other": "data"}},
	}
	assert.Nil(t, ExtractWorkflowExtras(messages))
}

func TestExtractWorkflowExtras_NilMeta(t *testing.T) {
	messages := []types.Message{{Role: "assistant"}}
	assert.Nil(t, ExtractWorkflowExtras(messages))
}

func TestExtractWorkflowExtras_Empty(t *testing.T) {
	assert.Nil(t, ExtractWorkflowExtras(nil))
}

func TestParseJSONArgs_Valid(t *testing.T) {
	result := parseJSONArgs([]byte(`{"key":"value","num":42}`))
	assert.Equal(t, "value", result["key"])
	assert.Equal(t, float64(42), result["num"])
}

func TestParseJSONArgs_Invalid(t *testing.T) {
	assert.Nil(t, parseJSONArgs([]byte(`not json`)))
}

func TestParseJSONArgs_Empty(t *testing.T) {
	result := parseJSONArgs([]byte(`{}`))
	assert.NotNil(t, result)
	assert.Empty(t, result)
}

func TestValidationsToPriorResults_SeedsFromLastAssistant(t *testing.T) {
	messages := []types.Message{
		types.NewUserMessage("hello"),
		{
			Role: "assistant",
			Validations: []types.ValidationResult{
				{ValidatorType: "content_excludes", Passed: false},
				{ValidatorType: "max_length", Passed: true},
			},
		},
	}

	results := validationsToPriorResults(messages)
	require.Len(t, results, 2)

	assert.Equal(t, "content_excludes", results[0].Type)
	assert.Equal(t, 0.0, *results[0].Score)

	assert.Equal(t, "max_length", results[1].Type)
	assert.Equal(t, 1.0, *results[1].Score)
}

// TestValidationsToPriorResults_CarriesDetails pins the half of the eval bridge
// that was missing: the guardrail recorder stamps which side it judged into
// ValidationResult.Details, and dropping the map made that unreadable to
// guardrail_triggered (#1718).
func TestValidationsToPriorResults_CarriesDetails(t *testing.T) {
	messages := []types.Message{
		types.NewUserMessage("hello"),
		{
			Role: "assistant",
			Validations: []types.ValidationResult{{
				ValidatorType: "pii_leakage",
				Passed:        false,
				Details:       map[string]any{"direction": "input", "reason": "ssn"},
			}},
		},
	}

	results := validationsToPriorResults(messages)
	require.Len(t, results, 1)
	assert.Equal(t, "input", results[0].Details["direction"])
	assert.Equal(t, "ssn", results[0].Details["reason"],
		"the whole detail map rides across, not just the direction key")
}

// TestValidationsToPriorResults_DetailsAreCopied pins that PriorResults does not
// alias message state. PriorResults is handed to arbitrary eval handlers; an
// aliased map lets any of them rewrite the recorded validation on the message.
func TestValidationsToPriorResults_DetailsAreCopied(t *testing.T) {
	messages := []types.Message{
		{
			Role: "assistant",
			Validations: []types.ValidationResult{{
				ValidatorType: "pii_leakage",
				Details:       map[string]any{"direction": "input"},
			}},
		},
	}

	results := validationsToPriorResults(messages)
	require.Len(t, results, 1)
	results[0].Details["direction"] = "output"

	assert.Equal(t, "input", messages[0].Validations[0].Details["direction"],
		"mutating a prior result must not rewrite the message's validation")
}

// TestValidationsToPriorResults_NoDetailsStaysNil keeps a validation without
// details from gaining an empty map, which would marshal a "details" key onto
// every result that never had one.
func TestValidationsToPriorResults_NoDetailsStaysNil(t *testing.T) {
	messages := []types.Message{
		{
			Role:        "assistant",
			Validations: []types.ValidationResult{{ValidatorType: "max_length", Passed: true}},
		},
	}

	results := validationsToPriorResults(messages)
	require.Len(t, results, 1)
	assert.Nil(t, results[0].Details)
}

func TestValidationsToPriorResults_NoValidations(t *testing.T) {
	messages := []types.Message{
		types.NewAssistantMessage("hi"),
	}
	assert.Nil(t, validationsToPriorResults(messages))
}

func TestValidationsToPriorResults_NoAssistantMessage(t *testing.T) {
	messages := []types.Message{
		types.NewUserMessage("hello"),
	}
	assert.Nil(t, validationsToPriorResults(messages))
}

func TestValidationsToPriorResults_Empty(t *testing.T) {
	assert.Nil(t, validationsToPriorResults(nil))
}

func TestBuildEvalContext_SeedsPriorResultsFromValidations(t *testing.T) {
	messages := []types.Message{
		types.NewUserMessage("test"),
		{
			Role: "assistant",
			Validations: []types.ValidationResult{
				{ValidatorType: "banned_words", Passed: false},
			},
		},
	}

	ctx := BuildEvalContext(messages, 1, "s1", "p1", nil)
	require.Len(t, ctx.PriorResults, 1)
	assert.Equal(t, "banned_words", ctx.PriorResults[0].Type)
	assert.Equal(t, 0.0, *ctx.PriorResults[0].Score)
}
