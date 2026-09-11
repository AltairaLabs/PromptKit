package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// These cover the duplex half of #1962. A duplex pipeline runs ONCE for the
// life of the session: VariableProviderStage resolves providers at the top of
// its single Process run, TemplateStage renders once, and DuplexProviderStage
// creates the provider session with that render as its system instruction. So
// the system prompt is fixed at the first input and nothing that changes
// afterwards — SetVar included — can reach the provider. The unary path renders
// per Send (#1959); duplex renders per session. Every assertion here is on what
// the provider session was actually handed, not on a SetVar/GetVar round trip.

// recordingStreamingProvider records the system instruction each created
// session was configured with. The duplex stage mutates its base config in
// place before every CreateStreamSession, so the value is copied at call time
// rather than kept by pointer.
type recordingStreamingProvider struct {
	*mock.StreamingProvider
	mu           sync.Mutex
	instructions []string
}

func newRecordingStreamingProvider() *recordingStreamingProvider {
	return &recordingStreamingProvider{
		StreamingProvider: mock.NewStreamingProvider("rec-stream", "rec-model", false).
			WithAutoRespond("ok"),
	}
}

func (p *recordingStreamingProvider) CreateStreamSession(
	ctx context.Context, req *providers.StreamingInputConfig,
) (providers.StreamInputSession, error) {
	instruction := ""
	if req != nil {
		instruction = req.SystemInstruction
	}
	p.mu.Lock()
	p.instructions = append(p.instructions, instruction)
	p.mu.Unlock()
	return p.StreamingProvider.CreateStreamSession(ctx, req)
}

// sessionInstructions returns the SystemInstruction of every session created.
func (p *recordingStreamingProvider) sessionInstructions() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.instructions))
	copy(out, p.instructions)
	return out
}

// sessionTexts returns every text the single provider session received —
// user turns and any SendSystemContext payloads (prefixed "[CONTEXT] ").
func (p *recordingStreamingProvider) sessionTexts(t *testing.T) []string {
	t.Helper()
	sessions := p.GetSessions()
	require.Len(t, sessions, 1, "duplex should create exactly one provider session")
	return sessions[0].GetTexts()
}

func openRecordingDuplexConv(
	t *testing.T, systemTemplate string, opts ...sdk.Option,
) (*sdk.Conversation, *recordingStreamingProvider) {
	t.Helper()
	rec := newRecordingStreamingProvider()
	packPath := writePackFile(t, jsonInputPack(systemTemplate))

	allOpts := append([]sdk.Option{
		sdk.WithProvider(rec),
		sdk.WithSkipSchemaValidation(),
		sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio},
		}),
	}, opts...)

	conv, err := sdk.OpenDuplex(packPath, "fn", allOpts...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv, rec
}

// sendDuplexTurn sends one text input and waits for the turn's reply. The
// duplex output surfaces the mock's reply as a single content chunk with no
// FinishReason, so the wait is for content rather than for a finish marker.
func sendDuplexTurn(t *testing.T, conv *sdk.Conversation, responseCh <-chan providers.StreamChunk, text string) {
	t.Helper()
	require.NoError(t, conv.SendText(context.Background(), text))

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case chunk, ok := <-responseCh:
			require.True(t, ok, "response channel closed before a reply to %q", text)
			require.NoError(t, chunk.Error)
			if chunk.Delta != "" || chunk.Content != "" {
				return
			}
		case <-timer.C:
			t.Fatalf("no reply to %q within timeout", text)
		}
	}
}

// TestDuplexSetVar_BeforeFirstInput_ReachesSessionInstruction pins the
// working half: a variable set between OpenDuplex and the first input is
// rendered into the system instruction the provider session is created with.
// (A lazily created session gets the prompt only that way — the stage marks it
// sent at creation and never calls SendSystemContext.)
func TestDuplexSetVar_BeforeFirstInput_ReachesSessionInstruction(t *testing.T) {
	conv, rec := openRecordingDuplexConv(t, "topic={{topic}}")
	conv.SetVar("topic", "batteries")

	responseCh, err := conv.Response()
	require.NoError(t, err)
	sendDuplexTurn(t, conv, responseCh, "hello")

	instructions := rec.sessionInstructions()
	require.Len(t, instructions, 1)
	assert.Equal(t, "topic=batteries", instructions[0])
	for _, text := range rec.sessionTexts(t) {
		assert.NotContains(t, text, "{{topic}}", "the placeholder must not reach the provider")
	}
}

// TestDuplexSetVar_OverridesOpenTimeVariables pins precedence on the duplex
// path: SetVar beats WithVariables, as it does for unary.
func TestDuplexSetVar_OverridesOpenTimeVariables(t *testing.T) {
	conv, rec := openRecordingDuplexConv(t, "topic={{topic}}",
		sdk.WithVariables(map[string]string{"topic": "default"}))
	conv.SetVar("topic", "explicit")

	responseCh, err := conv.Response()
	require.NoError(t, err)
	sendDuplexTurn(t, conv, responseCh, "hello")

	instructions := rec.sessionInstructions()
	require.Len(t, instructions, 1)
	assert.Equal(t, "topic=explicit", instructions[0])
}

// TestDuplexSetVar_AfterSessionStart_IsFrozen pins the documented limit: once
// the first input has started the pipeline, the provider session's system
// prompt is fixed for the life of the session. A later SetVar is stored (GetVar
// still round-trips) but is never rendered and never reaches the provider — no
// second session is created and no text carrying the new value is sent.
func TestDuplexSetVar_AfterSessionStart_IsFrozen(t *testing.T) {
	conv, rec := openRecordingDuplexConv(t, "topic={{topic}}")
	conv.SetVar("topic", "first")

	responseCh, err := conv.Response()
	require.NoError(t, err)
	sendDuplexTurn(t, conv, responseCh, "hello")

	conv.SetVar("topic", "second")
	sendDuplexTurn(t, conv, responseCh, "hello again")

	got, ok := conv.GetVar("topic")
	require.True(t, ok)
	assert.Equal(t, "second", got, "the value is stored even though it cannot be rendered")

	instructions := rec.sessionInstructions()
	require.Len(t, instructions, 1, "duplex must not recreate the provider session on a variable change")
	assert.Equal(t, "topic=first", instructions[0])

	for _, text := range rec.sessionTexts(t) {
		assert.NotContains(t, text, "topic=second", "a post-start SetVar must not leak into the session")
	}
}
