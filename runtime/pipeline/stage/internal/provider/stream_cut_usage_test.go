package provider

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// scriptedChannelProvider streams whatever the test sends on its channel.
type scriptedChannelProvider struct {
	*mock.Provider
	stream chan providers.StreamChunk
}

func (p *scriptedChannelProvider) SupportsStreaming() bool { return true }

func (p *scriptedChannelProvider) PredictStream(
	context.Context, providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	return p.stream, nil
}

const noPineappleMsg = "Let's talk about something else."

// pineappleGuardedStage is a streaming provider stage with a no-pineapple output guardrail
// reading the test's stream.
func pineappleGuardedStage(t *testing.T, stream chan providers.StreamChunk) *ProviderStage {
	t.Helper()
	guard, err := guardrails.NewGuardrailHook("content_excludes",
		map[string]any{"patterns": []any{"pineapple"}}, guardrails.WithMessage(noPineappleMsg))
	require.NoError(t, err)
	prov := &scriptedChannelProvider{Provider: mock.NewProvider("p", "m", false), stream: stream}
	return NewProviderStageWithHooks(prov, nil, nil, &ProviderConfig{MaxTokens: 100}, nil,
		hooks.NewRegistry(hooks.WithProviderHook(guard)))
}

// sendScriptedDeltas sends each delta as a chunk carrying the reply so far.
func sendScriptedDeltas(stream chan<- providers.StreamChunk, deltas ...string) {
	sofar := ""
	for _, d := range deltas {
		sofar += d
		stream <- providers.StreamChunk{Delta: d, Content: sofar}
	}
}

// lastAssistantOf is the last assistant message among elems.
func lastAssistantOf(t *testing.T, elems []core.StreamElement) *types.Message {
	t.Helper()
	for i := len(elems) - 1; i >= 0; i-- {
		if m := elems[i].Message; m != nil && m.Role == core.RoleAssistant {
			return m
		}
	}
	require.FailNow(t, "no assistant message")
	return nil
}

// A provider that sends its usage on a chunk of its own after the finish
// reason (as OpenAI does) still has it recorded on a call the guardrail cut.
func TestProviderStage_CutStreamRecordsUsageSentAfterTheCut(t *testing.T) {
	stream := make(chan providers.StreamChunk, 8)
	sendScriptedDeltas(stream, "P", "ine", "apple is", " the best topping.")
	stop := types.FinishReasonStop
	stream <- providers.StreamChunk{Content: "Pineapple is the best topping.", FinishReason: &stop}
	stream <- providers.StreamChunk{CostInfo: &types.CostInfo{InputTokens: 12, OutputTokens: 7, TotalCost: 0.25}}
	close(stream)

	elems, err := runProviderStage(t, pineappleGuardedStage(t, stream), "what goes on pizza?")
	require.NoError(t, err)

	msg := lastAssistantOf(t, elems)
	assert.Equal(t, noPineappleMsg, msg.Content)
	require.NotNil(t, msg.CostInfo)
	assert.Equal(t, 12, msg.CostInfo.InputTokens)
	assert.Equal(t, 7, msg.CostInfo.OutputTokens)
	assert.InDelta(t, 0.25, msg.CostInfo.TotalCost, 1e-12)
}

// A stream that never ends after the cut does not hold the turn: the stage
// waits for the usage only so long, then completes the blocked turn without it.
func TestProviderStage_CutStreamWaitsForUsageOnlySoLong(t *testing.T) {
	stream := make(chan providers.StreamChunk, 8)
	t.Cleanup(func() { close(stream) })
	sendScriptedDeltas(stream, "P", "ine", "apple")
	ps := pineappleGuardedStage(t, stream)
	ps.cutUsageWait = 20 * time.Millisecond

	type result struct {
		elems []core.StreamElement
		err   error
	}
	done := make(chan result, 1)
	go func() {
		elems, err := runProviderStage(t, ps, "what goes on pizza?")
		done <- result{elems, err}
	}()
	select {
	case r := <-done:
		require.NoError(t, r.err)
		msg := lastAssistantOf(t, r.elems)
		assert.Equal(t, noPineappleMsg, msg.Content)
		assert.Equal(t, types.FinishReasonSafety, msg.FinishReason)
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the cut turn waited on a stream that never ends")
	}
}

// textDeltas lists the text elements among elems, each with the finish reason
// it carries ("" for none).
func textDeltas(elems []core.StreamElement) (texts, finishes []string) {
	for _, e := range elems {
		if e.Text == nil {
			continue
		}
		texts = append(texts, *e.Text)
		fr := ""
		if e.Meta.FinishReason != nil {
			fr = *e.Meta.FinishReason
		}
		finishes = append(finishes, fr)
	}
	return texts, finishes
}

// Text held at the end of a reply is released when the reply ends, carrying
// the reply's finish reason, as its last chunk's text would have.
func TestProviderStage_HeldTailIsReleasedWithTheFinishReason(t *testing.T) {
	stream := make(chan providers.StreamChunk, 8)
	sendScriptedDeltas(stream, "I like ", "Pine")
	stop := types.FinishReasonStop
	stream <- providers.StreamChunk{Content: "I like Pine", FinishReason: &stop}
	close(stream)

	elems, err := runProviderStage(t, pineappleGuardedStage(t, stream), "what goes on pizza?")
	require.NoError(t, err)

	texts, finishes := textDeltas(elems)
	assert.Equal(t, []string{"I like ", "Pine"}, texts)
	assert.Equal(t, []string{"", types.FinishReasonStop}, finishes)
	assert.Equal(t, "I like Pine", lastAssistantOf(t, elems).Content)
}

// A guardrail that truncates the reply keeps the model's text: the caller gets
// what follows what it already had, up to the cut, marked safety.
func TestProviderStage_TruncatedStreamSendsTheRestUpToTheCut(t *testing.T) {
	limit, err := guardrails.NewGuardrailHook("length", map[string]any{"max_characters": 10})
	require.NoError(t, err)
	stream := make(chan providers.StreamChunk, 8)
	sendScriptedDeltas(stream, "secret ", "plans are here")
	close(stream)
	prov := &scriptedChannelProvider{Provider: mock.NewProvider("p", "m", false), stream: stream}
	ps := NewProviderStageWithHooks(prov, nil, nil, &ProviderConfig{MaxTokens: 100}, nil,
		hooks.NewRegistry(hooks.WithProviderHook(limit)))

	elems, err := runProviderStage(t, ps, "tell me")
	require.NoError(t, err)

	texts, finishes := textDeltas(elems)
	assert.Equal(t, []string{"secret ", "pla"}, texts)
	assert.Equal(t, []string{"", types.FinishReasonSafety}, finishes)
	assert.Equal(t, "secret pla", lastAssistantOf(t, elems).Content)
}

// A retry that restarts the stream discards what the failed attempt left held:
// the caller gets the retry's text, not the failed attempt's held tail joined
// to it.
func TestProviderStage_StreamResetDropsHeldText(t *testing.T) {
	stream := make(chan providers.StreamChunk, 8)
	sendScriptedDeltas(stream, "Say P")
	stream <- providers.StreamChunk{Reset: true}
	sendScriptedDeltas(stream, "Hello there")
	stop := types.FinishReasonStop
	stream <- providers.StreamChunk{Content: "Hello there", FinishReason: &stop}
	close(stream)

	elems, err := runProviderStage(t, pineappleGuardedStage(t, stream), "hi")
	require.NoError(t, err)

	texts, _ := textDeltas(elems)
	assert.Equal(t, []string{"Say ", "Hello there"}, texts)
	assert.Equal(t, "Hello there", lastAssistantOf(t, elems).Content)
}

// A truncation that cuts exactly where the caller's text already ends sends
// nothing more: the caller already has the whole truncated reply.
func TestProviderStage_TruncationAtTheSentTextSendsNothingMore(t *testing.T) {
	limit, err := guardrails.NewGuardrailHook("length", map[string]any{"max_characters": 10})
	require.NoError(t, err)
	stream := make(chan providers.StreamChunk, 8)
	sendScriptedDeltas(stream, "secret pla", "ns are here")
	close(stream)
	prov := &scriptedChannelProvider{Provider: mock.NewProvider("p", "m", false), stream: stream}
	ps := NewProviderStageWithHooks(prov, nil, nil, &ProviderConfig{MaxTokens: 100}, nil,
		hooks.NewRegistry(hooks.WithProviderHook(limit)))

	elems, err := runProviderStage(t, ps, "tell me")
	require.NoError(t, err)

	texts, _ := textDeltas(elems)
	assert.Equal(t, []string{"secret pla"}, texts)
	assert.Equal(t, "secret pla", lastAssistantOf(t, elems).Content)
}
