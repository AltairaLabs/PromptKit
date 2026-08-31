package sdk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk/internal/pack"
	"github.com/AltairaLabs/PromptKit/sdk/session"

	"github.com/AltairaLabs/PromptKit/runtime/packspec"
)

// evalTestConversation builds a conversation backed by a real store, seeded
// with the given transcript, so the eval middleware reads it the way it would
// in production.
func evalTestConversation(t *testing.T, msgs []types.Message) *Conversation {
	t.Helper()

	p := &pack.Pack{Pack: packspec.Pack{
		ID:      "eval-pack",
		Prompts: map[string]*pack.Prompt{"chat": {ID: "chat", SystemTemplate: "sys"}},
	}}
	promptRegistry := pack.ToPromptRegistry(p)
	pipeline, err := stage.NewPipelineBuilder().
		Chain(stage.NewPromptAssemblyStage(promptRegistry, "chat", nil)).
		Build()
	require.NoError(t, err)

	store := statestore.NewMemoryStore()
	const convID = "conv-evals"
	require.NoError(t, store.AppendMessages(context.Background(), convID, msgs))

	sess, err := session.NewUnarySession(session.UnarySessionConfig{
		ConversationID: convID,
		StateStore:     store,
		Pipeline:       pipeline,
		Variables:      map[string]string{},
	})
	require.NoError(t, err)

	return &Conversation{
		pack:           p,
		prompt:         p.Prompts["chat"],
		promptName:     "chat",
		promptRegistry: promptRegistry,
		config:         &config{},
		mode:           UnaryMode,
		unarySession:   sess,
	}
}

// toolCallTranscript is a completed tool exchange: the model called a tool, the
// tool answered, and the model summarised.
func toolCallTranscript(toolName string) []types.Message {
	return []types.Message{
		{Role: "user", Content: "run the check"},
		{Role: roleAssistant, ToolCalls: []types.MessageToolCall{{
			ID: "call_1", Name: toolName, Args: json.RawMessage(`{"case_id":"C-1042"}`),
		}}},
		types.NewToolResultMessage(types.NewTextToolResult("call_1", toolName, `{"ok":true}`)),
		{Role: roleAssistant, Content: "done"},
	}
}

// TestEvalMiddleware_SessionToolEvalSeesTheToolCalls is the regression test
// #1857 asks for: a session with a tool call, a tools_called_session eval
// naming it, asserting a pass.
//
// It failed before the middleware delegated to evals.BuildEvalContext. The
// middleware assembled an EvalContext literal and set four of its fields,
// leaving ToolCalls nil while holding the very messages it derives from — so
// the handler reported "got 0" for tools the transcript shows succeeding.
//
// The failure direction is what makes this worth pinning: handlers do not error
// on a nil slice, they report ABSENCE. An eval added to catch "the agent
// answered without doing the work" instead fired on every run that did it.
func TestEvalMiddleware_SessionToolEvalSeesTheToolCalls(t *testing.T) {
	conv := evalTestConversation(t, toolCallTranscript("case_fetch"))

	mw := &evalMiddleware{conv: conv}
	evalCtx := mw.buildEvalContext(context.Background())

	require.NotEmpty(t, evalCtx.ToolCalls,
		"the eval context carried no tool calls for a session that made one — every "+
			"ToolCalls-reading handler will report absence")

	h, err := evals.NewEvalTypeRegistry().Get("tools_called_session")
	require.NoError(t, err)

	result, err := h.Eval(context.Background(), evalCtx, map[string]any{
		"tool_names": []any{"case_fetch"},
	})
	require.NoError(t, err)
	require.NotNil(t, result.Score)
	assert.Equalf(t, 1.0, *result.Score,
		"tools_called_session scored %v on a session that called case_fetch: %s",
		*result.Score, result.Explanation)
}

// TestEvalMiddleware_ContextCarriesTheDerivedFields guards the delegation
// itself rather than one handler's symptom.
//
// The inline literal set 4 of EvalContext's fields; BuildEvalContext derives 9
// from the same messages. Asserting on the derived ones is what stops a future
// edit from quietly reverting to a hand-rolled struct — the same mistake
// already fixed once in BuildGuardrailEvalContext (#1704).
func TestEvalMiddleware_ContextCarriesTheDerivedFields(t *testing.T) {
	conv := evalTestConversation(t, toolCallTranscript("credit_report"))

	mw := &evalMiddleware{conv: conv}
	evalCtx := mw.buildEvalContext(context.Background())

	assert.NotEmpty(t, evalCtx.Messages, "messages must reach the context")
	assert.NotEmpty(t, evalCtx.ToolCalls, "ToolCalls is derived from those messages")
	assert.Equal(t, "credit_report", evalCtx.ToolCalls[0].ToolName)
	assert.Equal(t, "done", evalCtx.CurrentOutput,
		"CurrentOutput must be the last assistant turn")
}

// TestEvalMiddleware_MultimodalCurrentOutput covers the bug delegation exposed
// in the shared builder.
//
// BuildEvalContext read message.Content directly, which is EMPTY on a
// multimodal message whose text lives in Parts — so every content-matching eval
// saw nothing to match. GetContent resolves Parts and falls back to Content.
func TestEvalMiddleware_MultimodalCurrentOutput(t *testing.T) {
	text := "the multimodal answer"
	conv := evalTestConversation(t, []types.Message{{
		Role:  roleAssistant,
		Parts: []types.ContentPart{{Type: types.ContentTypeText, Text: &text}},
	}})

	mw := &evalMiddleware{conv: conv}
	evalCtx := mw.buildEvalContext(context.Background())

	assert.Equal(t, text, evalCtx.CurrentOutput,
		"a Parts-only assistant message left CurrentOutput empty, so content evals "+
			"had nothing to match against")
}
