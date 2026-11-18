package sdk

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// A streaming guardrail that replaces a reply with its message has judged the
// reply: the message is the guardrail's text, not the model's, so it is not
// judged again when the reply completes. A truncation keeps the model's own
// text, which the completed-reply checks still judge.

// streamReply opens a conversation whose model answers reply, streams one
// turn and returns the completed response.
func streamReply(t *testing.T, reply string, opts ...Option) *Response {
	t.Helper()
	repo := &sequenceRepo{turns: []mock.Turn{{Type: "text", Content: reply}}}
	conv, err := Open(writeGatePack(t), "assist", append([]Option{
		WithProvider(mock.NewToolProviderWithRepository("mock", "mock-model", false, repo)),
		WithSkipSchemaValidation(),
	}, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	var done *Response
	for c := range conv.Stream(context.Background(), "who is that?") {
		require.NoError(t, c.Error)
		if c.Type == ChunkDone {
			done = c.Message
		}
	}
	require.NotNil(t, done, "the stream ended without its response")
	return done
}

// A reply cut mid-stream carries one failed validation, the cut's, even when
// the guardrail's message names what it bans.
func TestStream_GuardrailReplacementIsJudgedOnce(t *testing.T) {
	resp := streamReply(t, "Let me tell you about Bruno.", WithGuardrail(
		guardrails.Output("content_excludes", map[string]any{"patterns": []any{"Bruno"}},
			guardrails.WithMessage("We don't talk about Bruno."))))

	assert.Equal(t, "We don't talk about Bruno.", resp.Text())
	var got []string
	for _, v := range resp.Validations() {
		got = append(got, v.ValidatorType+"|"+boolWord(v.Passed))
	}
	assert.Equal(t, []string{"content_excludes|failed"}, got)
}

func boolWord(passed bool) string {
	if passed {
		return "passed"
	}
	return "failed"
}

// afterCallSeen is an output hook that judges only completed replies,
// recording the text of each.
type afterCallSeen struct {
	mu   sync.Mutex
	seen []string
}

func (*afterCallSeen) Name() string { return "after_call_seen" }

func (*afterCallSeen) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (h *afterCallSeen) AfterCall(_ context.Context, _ *hooks.ProviderRequest, r *hooks.ProviderResponse) hooks.Decision {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seen = append(h.seen, r.Message.Content)
	return hooks.Allow
}

// A truncated reply is still the model's text: the completed-reply checks
// judge it.
func TestStream_TruncatedReplyIsStillJudged(t *testing.T) {
	seen := &afterCallSeen{}
	limit, err := guardrails.NewGuardrailHook("length", map[string]any{"max_characters": 10})
	require.NoError(t, err)

	resp := streamReply(t, "secret plans are here", WithProviderHook(limit), WithProviderHook(seen))

	assert.Equal(t, "secret pla", resp.Text())
	assert.Equal(t, []string{"secret pla"}, seen.seen)
}
