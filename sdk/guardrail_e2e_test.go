package sdk

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// End-to-end guardrail behavior through the public SDK surface.
//
// These deliberately go through sdk.Open + Send rather than exercising the
// runtime in isolation. Verifying the hook plumbing separately from the eval
// handlers is exactly how a silently-non-firing input guardrail shipped
// (#1679): each half passed its own tests.

const guardrailTestPack = "./testdata/packs/guardrail-test.pack.json"

// TestGuardrail_InputBlockIsVisibleToCaller pins that a caller can detect a
// guardrail-blocked turn through the public API. The runtime marks the canned
// turn types.FinishReasonSafety; before the fix, buildResponse rebuilt the
// message from a narrower struct and dropped the field, so an application had
// no reliable way to tell a policy block from a real model reply (#1681).
func TestGuardrail_InputBlockIsVisibleToCaller(t *testing.T) {
	conv, err := Open(guardrailTestPack, "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(),
		WithGuardrail(
			guardrails.Input("banned_words", map[string]any{
				"words": []any{"wire transfer"},
			}, guardrails.WithMessage("I can't help with transfers.")),
		),
	)
	require.NoError(t, err)
	defer conv.Close()

	resp, err := conv.Send(context.Background(), "please arrange a wire transfer")

	require.NoError(t, err, "an enforcing input guardrail must not error the send")
	assert.Equal(t, "I can't help with transfers.", resp.Text(),
		"the canned message must reach the caller")
	require.NotNil(t, resp.Message())
	assert.Equal(t, types.FinishReasonSafety, resp.Message().FinishReason,
		"a blocked turn must be detectable via FinishReason, not by matching text")
}

// TestGuardrail_NormalTurnIsNotMarkedBlocked is the discriminating half: an
// unblocked turn must NOT report the safety finish reason. Without this, a fix
// that hardcoded FinishReasonSafety everywhere would pass the test above.
func TestGuardrail_NormalTurnIsNotMarkedBlocked(t *testing.T) {
	conv, err := Open(guardrailTestPack, "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(),
		WithGuardrail(
			guardrails.Input("banned_words", map[string]any{
				"words": []any{"wire transfer"},
			}),
		),
	)
	require.NoError(t, err)
	defer conv.Close()

	resp, err := conv.Send(context.Background(), "what is the capital of France?")

	require.NoError(t, err)
	require.NotNil(t, resp.Message())
	assert.NotEqual(t, types.FinishReasonSafety, resp.Message().FinishReason,
		"a clean turn must not be reported as safety-blocked")
	assert.NotEmpty(t, resp.Text(), "a clean turn must carry the provider's reply")
}

// collectStreamDone drains a Stream channel and returns the Response carried by
// the terminal ChunkDone, failing the test if the stream errored or never
// finished.
func collectStreamDone(t *testing.T, ch <-chan StreamChunk) *Response {
	t.Helper()

	var done *Response
	for chunk := range ch {
		require.NoError(t, chunk.Error, "the stream must not error")
		if chunk.Type == ChunkDone {
			done = chunk.Message
		}
	}
	require.NotNil(t, done, "the stream must terminate with a ChunkDone carrying a Response")
	return done
}

// TestGuardrail_InputBlockIsVisibleToStreamCaller mirrors
// TestGuardrail_InputBlockIsVisibleToCaller on the streaming path. Send and
// Stream are different code paths — executePipeline/buildResponse versus
// executeStreamingPipeline/buildStreamingResponse — and only the latter dropped
// FinishReason, so a guardrail-blocked turn was invisible to a streaming caller
// short of matching the canned text (#1715).
func TestGuardrail_InputBlockIsVisibleToStreamCaller(t *testing.T) {
	conv, err := Open(guardrailTestPack, "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(),
		WithGuardrail(
			guardrails.Input("banned_words", map[string]any{
				"words": []any{"wire transfer"},
			}, guardrails.WithMessage("I can't help with transfers.")),
		),
	)
	require.NoError(t, err)
	defer conv.Close()

	resp := collectStreamDone(t, conv.Stream(context.Background(), "please arrange a wire transfer"))

	assert.Equal(t, "I can't help with transfers.", resp.Text(),
		"the canned message must reach the streaming caller")
	require.NotNil(t, resp.Message())
	assert.Equal(t, types.FinishReasonSafety, resp.Message().FinishReason,
		"a blocked streaming turn must be detectable via FinishReason, not by matching text")
}

// TestGuardrail_StreamNormalTurnIsNotMarkedBlocked is the discriminating half of
// the test above: a clean streaming turn must NOT report the safety finish
// reason. Without it, a fix that hardcoded types.FinishReasonSafety in
// buildStreamingResponse would pass.
func TestGuardrail_StreamNormalTurnIsNotMarkedBlocked(t *testing.T) {
	conv, err := Open(guardrailTestPack, "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(),
		WithGuardrail(
			guardrails.Input("banned_words", map[string]any{
				"words": []any{"wire transfer"},
			}),
		),
	)
	require.NoError(t, err)
	defer conv.Close()

	resp := collectStreamDone(t, conv.Stream(context.Background(), "what is the capital of France?"))

	require.NotNil(t, resp.Message())
	assert.NotEqual(t, types.FinishReasonSafety, resp.Message().FinishReason,
		"a clean streaming turn must not be reported as safety-blocked")
	assert.NotEmpty(t, resp.Text(), "a clean streaming turn must carry the provider's reply")
}

// TestGuardrail_InputBlockRecordsValidation pins the other observable signal —
// the ValidationResult naming the guardrail that fired — so an application can
// report *which* policy blocked the turn, not merely that one did.
func TestGuardrail_InputBlockRecordsValidation(t *testing.T) {
	conv, err := Open(guardrailTestPack, "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(),
		WithGuardrail(
			guardrails.Input("banned_words", map[string]any{
				"words": []any{"wire transfer"},
			}),
		),
	)
	require.NoError(t, err)
	defer conv.Close()

	resp, err := conv.Send(context.Background(), "please arrange a wire transfer")
	require.NoError(t, err)

	validations := resp.Validations()
	require.NotEmpty(t, validations, "a guardrail firing must be recorded")
	assert.Equal(t, "banned_words", validations[0].ValidatorType)
	assert.False(t, validations[0].Passed)
	assert.Equal(t, "input", validations[0].Details["direction"])
}

// TestGuardrail_StreamingOutputBlockDoesNotLeakBannedContent pins that an
// output guardrail replaces the response on the STREAMING path, not just the
// unary one.
//
// GuardrailHookAdapter.OnChunk only rewrote content for length validators; a
// content blocker modified nothing yet still returned Enforced, and the stage
// took that text verbatim. The caller therefore received the model's words —
// including the very pattern the guardrail was configured to block — and
// WithMessage was silently ignored (#1697).
//
// mock.NewProvider advertises streaming, so Send takes the streaming path here,
// which is what most real providers do too. Asserting on the absence of the
// banned word is the point: a fix that returned some other placeholder while
// still leaking the pattern would not satisfy this.
func TestGuardrail_StreamingOutputBlockDoesNotLeakBannedContent(t *testing.T) {
	conv, err := Open(guardrailTestPack, "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(),
		WithGuardrail(
			// "respons" matches the mock's canned reply ("Mock response from
			// mock model mock-model") under the STREAMING check, which uses
			// substring mode deliberately to avoid false negatives on partial
			// words — but not under the final check, which uses banned_words'
			// default word_boundary mode. That divergence is what let the
			// content survive: the chunk check aborted the stream, then the
			// final check judged the same text clean and never replaced it.
			guardrails.Output("banned_words", map[string]any{
				"words": []any{"respons"},
			}, guardrails.WithMessage("I can't share that.")),
		),
	)
	require.NoError(t, err)
	defer conv.Close()

	resp, err := conv.Send(context.Background(), "hello")
	require.NoError(t, err, "an Enforced output guardrail must not surface as an error")

	assert.NotContains(t, resp.Text(), "respons",
		"the banned pattern must not reach the caller")
	assert.Equal(t, "I can't share that.", resp.Text(),
		"the configured guardrail message must replace the model text")
}

// TestGuardrail_StreamingOutputAllowsCleanResponse is the discriminating half:
// a guardrail whose pattern does not appear must leave the model's reply alone,
// so a fix that always substituted the message would fail here.
func TestGuardrail_StreamingOutputAllowsCleanResponse(t *testing.T) {
	conv, err := Open(guardrailTestPack, "chat",
		WithProvider(mock.NewProvider("mock", "mock-model", false)),
		WithSkipSchemaValidation(),
		WithGuardrail(
			guardrails.Output("banned_words", map[string]any{
				"words": []any{"nonexistent-phrase-xyzzy"},
			}, guardrails.WithMessage("I can't share that.")),
		),
	)
	require.NoError(t, err)
	defer conv.Close()

	resp, err := conv.Send(context.Background(), "hello")
	require.NoError(t, err)

	assert.NotEqual(t, "I can't share that.", resp.Text(),
		"a clean response must not be replaced")
	assert.Contains(t, resp.Text(), "Mock response",
		"the model's reply must pass through untouched")
}
