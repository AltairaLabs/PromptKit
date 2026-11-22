package stage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// guardedStage is a streaming provider stage reading the test's stream with
// hs as its provider hooks, in order.
func guardedStage(stream chan providers.StreamChunk, hs ...hooks.ProviderHook) *ProviderStage {
	opts := make([]hooks.Option, 0, len(hs))
	for _, h := range hs {
		opts = append(opts, hooks.WithProviderHook(h))
	}
	prov := &scriptedChannelProvider{Provider: mock.NewProvider("p", "m", false), stream: stream}
	return NewProviderStageWithHooks(prov, nil, nil, &ProviderConfig{MaxTokens: 100}, nil, hooks.NewRegistry(opts...))
}

func guardrailHook(t *testing.T, typ string, params map[string]any, opts ...guardrails.GuardrailOption) hooks.ProviderHook {
	t.Helper()
	h, err := guardrails.NewGuardrailHook(typ, params, opts...)
	require.NoError(t, err)
	return h
}

// A guardrail declared after one that truncates the reply still judges it: a
// banned word the truncation keeps never reaches the caller, and the reply is
// the second guardrail's message, marked as replacing what was shown.
func TestProviderStage_EveryStreamingGuardrailJudgesATruncatedReply(t *testing.T) {
	stream := make(chan providers.StreamChunk, 8)
	sendScriptedDeltas(stream, "hello ba", "d world foo")
	close(stream)
	ps := guardedStage(stream,
		guardrailHook(t, "length", map[string]any{"max_characters": 12}),
		guardrailHook(t, "content_excludes", map[string]any{"patterns": []any{"bad"}}, guardrails.WithMessage("Blocked.")))

	elems, err := runProviderStage(t, ps, "hi")
	require.NoError(t, err)

	assert.Equal(t, []string{"hello ", "replace:Blocked."}, liveText(elems))
	assert.Equal(t, "Blocked.", lastAssistantOf(t, elems).Content)
}

// A stream that fails keeps, and has shown, the same text: what was held back
// when it failed is in neither.
func TestProviderStage_FailedStreamKeepsOnlyTheTextItShowed(t *testing.T) {
	stream := make(chan providers.StreamChunk, 8)
	sendScriptedDeltas(stream, "Say P")
	stream <- providers.StreamChunk{Error: errors.New("connection reset")}
	close(stream)

	elems, err := runProviderStage(t, pineappleGuardedStage(t, stream), "hi")
	require.Error(t, err)

	assert.Equal(t, []string{"Say "}, liveText(elems))
	assert.Equal(t, "Say ", lastAssistantOf(t, elems).Content)
}

// A cut does not wait for the provider to finish the reply it blocked: the
// guardrail's message and the turn's end go out at once, and the usage the
// provider sends when it finishes is reported for the call as
// provider.call.usage.
func TestProviderStage_CutTurnEndsAtOnceAndItsUsageFollows(t *testing.T) {
	stream := make(chan providers.StreamChunk, 8)
	sendScriptedDeltas(stream, "P", "ine", "apple")
	ps := pineappleGuardedStage(t, stream)
	ps.cutUsageWait = 10 * time.Millisecond
	bus := events.NewEventBus()
	t.Cleanup(bus.Close)
	ps.emitter = events.NewEmitter(bus, "run", "sess", "conv")
	snapshot := collectEvents(bus, events.EventProviderCallCompleted, events.EventProviderCallUsage)

	type result struct {
		elems []StreamElement
		err   error
	}
	done := make(chan result, 1)
	go func() {
		elems, err := runProviderStage(t, ps, "what goes on pizza?")
		done <- result{elems, err}
	}()
	var r result
	select {
	case r = <-done:
	case <-time.After(5 * time.Second):
		require.FailNow(t, "the cut turn waited for the provider to finish the blocked reply")
	}
	require.NoError(t, r.err)
	assert.Equal(t, []string{"replace:" + noPineappleMsg}, liveText(r.elems))
	assert.Equal(t, noPineappleMsg, lastAssistantOf(t, r.elems).Content)

	// The provider finishes the reply after the turn has ended.
	stream <- providers.StreamChunk{CostInfo: &types.CostInfo{InputTokens: 12, OutputTokens: 40, TotalCost: 0.5}}
	close(stream)

	// The bus delivers through a worker pool, so delivery order is not publish
	// order: find each event by type.
	got := waitForEvents(t, snapshot, 2)
	var completed *events.ProviderCallCompletedData
	var usage *events.ProviderCallUsageData
	for _, e := range got {
		switch d := e.Data.(type) {
		case *events.ProviderCallCompletedData:
			completed = d
		case *events.ProviderCallUsageData:
			usage = d
		}
	}
	require.NotNil(t, completed, "no provider.call.completed")
	require.NotNil(t, usage, "no provider.call.usage")
	assert.Equal(t, completed.CallID, usage.CallID)
	assert.NotEmpty(t, usage.CallID)
	assert.Equal(t, []any{12, 40, 0.5, "p", "m"},
		[]any{usage.InputTokens, usage.OutputTokens, usage.Cost, usage.Provider, usage.Model})
}

// A final chunk that carries text the guardrails held releases all of it,
// with the finish reason on that one element only.
func TestProviderStage_FinalChunkReleasesHeldTextWithOneFinishReason(t *testing.T) {
	stream := make(chan providers.StreamChunk, 8)
	sendScriptedDeltas(stream, "I like P")
	stop := types.FinishReasonStop
	stream <- providers.StreamChunk{Delta: "ine Pine", Content: "I like Pine Pine", FinishReason: &stop}
	close(stream)

	elems, err := runProviderStage(t, pineappleGuardedStage(t, stream), "hi")
	require.NoError(t, err)

	texts, finishes := textDeltas(elems)
	assert.Equal(t, []string{"I like ", "Pine Pine"}, texts)
	assert.Equal(t, []string{"", types.FinishReasonStop}, finishes)
}

// Media a chunk carries goes out after the chunk's text, even when the end of
// that text was held back.
func TestProviderStage_MediaFollowsTheTextHeldBeforeIt(t *testing.T) {
	stream := make(chan providers.StreamChunk, 8)
	stream <- providers.StreamChunk{Delta: "Say P", Content: "Say P",
		MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: []byte{1}}}
	stream <- providers.StreamChunk{Delta: "ine cones", Content: "Say Pine cones",
		MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: []byte{2}}}
	close(stream)

	elems, err := runProviderStage(t, pineappleGuardedStage(t, stream), "hi")
	require.NoError(t, err)

	var order []string
	for _, e := range elems {
		switch {
		case e.Text != nil && e.Message == nil:
			order = append(order, "text:"+*e.Text)
		case e.Audio != nil:
			order = append(order, "audio:"+string(rune('0'+e.Audio.Samples[0])))
		}
	}
	assert.Equal(t, []string{"text:Say ", "text:Pine cones", "audio:1", "audio:2"}, order)
}

// A realtime reply's live text holds back what could be the start of a banned
// word: a word split across transcript deltas never reaches the caller in
// part, and held text that is not the word is released.
func TestDuplexGuardrail_LiveTextHoldsTheStartOfABannedWord(t *testing.T) {
	guard := guardrailHook(t, "content_excludes", map[string]any{"patterns": []any{"pineapple"}},
		guardrails.WithMessage(noPineappleMsg))
	for name, tc := range map[string]struct {
		chunks []string
		want   []string
	}{
		"split banned word": {[]string{"Say P", "ine", "apple"}, []string{"Say ", "replace:" + noPineappleMsg}},
		"held then cleared": {[]string{"Pine", " cones"}, []string{"Pine cones"}},
		"held at turn end":  {[]string{"I like ", "Pine"}, []string{"I like ", "Pine"}},
	} {
		t.Run(name, func(t *testing.T) {
			s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(guard)))
			chunks := make([]providers.StreamChunk, 0, len(tc.chunks)+1)
			for _, c := range tc.chunks {
				chunks = append(chunks, transcriptChunk(c, pcm(10)))
			}
			elems, err := feedChunks(t, s, append(chunks, stopChunk())...)
			require.NoError(t, err)
			assert.Equal(t, tc.want, liveTextOf(elems))
		})
	}
}

// liveTextOf is liveText counting the text an element that also completes the
// turn carries.
func liveTextOf(elems []StreamElement) []string {
	var out []string
	for _, e := range elems {
		if e.Text == nil || (e.Message != nil && e.Message.Role != core.RoleAssistant) {
			continue
		}
		if e.Meta.ReplacesText {
			out = append(out, "replace:"+*e.Text)
		} else {
			out = append(out, *e.Text)
		}
	}
	return out
}

var _ = context.Background

// A truncation that keeps text the caller has not had yet, because it was
// held, sends that text as the rest of the reply, not as a replacement.
func TestTruncationSendsTheHeldTextItKeeps(t *testing.T) {
	limit := func() hooks.ProviderHook {
		return guardrailHook(t, "length", map[string]any{"max_characters": 6})
	}
	excludes := func() hooks.ProviderHook {
		return guardrailHook(t, "content_excludes", map[string]any{"patterns": []any{"pineapple"}})
	}

	t.Run("text path", func(t *testing.T) {
		stream := make(chan providers.StreamChunk, 8)
		sendScriptedDeltas(stream, "Say P", "ine cones")
		close(stream)
		elems, err := runProviderStage(t, guardedStage(stream, limit(), excludes()), "hi")
		require.NoError(t, err)
		assert.Equal(t, []string{"Say ", "Pi"}, liveText(elems))
		assert.Equal(t, "Say Pi", lastAssistantOf(t, elems).Content)
	})

	t.Run("realtime", func(t *testing.T) {
		s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(limit()), hooks.WithProviderHook(excludes())))
		elems, err := feedChunks(t, s,
			transcriptChunk("Say P", pcm(10)), transcriptChunk("ine cones", pcm(10)), stopChunk())
		require.NoError(t, err)
		assert.Equal(t, []string{"Say ", "Pi"}, liveText(elems))
	})
}

// afterCallTexts is an output hook recording the text of each completed reply
// it judges.
type afterCallTexts struct{ seen []string }

func (*afterCallTexts) Name() string { return "after_call_texts" }

func (*afterCallTexts) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}

func (h *afterCallTexts) AfterCall(_ context.Context, _ *hooks.ProviderRequest, r *hooks.ProviderResponse) hooks.Decision {
	h.seen = append(h.seen, r.Message.Content)
	return hooks.Allow
}

// A realtime truncation that keeps held text is still a truncation: the
// truncated reply is the model's own text, which the completed-turn checks
// judge.
func TestDuplexGuardrail_TruncationKeepingHeldTextIsStillJudged(t *testing.T) {
	seen := &afterCallTexts{}
	s, _ := cutStage(t, hooks.NewRegistry(
		hooks.WithProviderHook(guardrailHook(t, "length", map[string]any{"max_characters": 6})),
		hooks.WithProviderHook(guardrailHook(t, "content_excludes", map[string]any{"patterns": []any{"pineapple"}})),
		hooks.WithProviderHook(seen)))

	_, err := feedChunks(t, s, transcriptChunk("Say P", pcm(10)), transcriptChunk("ine cones", pcm(10)), stopChunk())
	require.NoError(t, err)
	assert.Equal(t, []string{"Say Pi"}, seen.seen)
}

// Text held when the caller barges in is dropped with the cut reply: the next
// reply's live text does not start with it.
func TestDuplexGuardrail_HeldTextIsDroppedOnBargeIn(t *testing.T) {
	s, _ := cutStage(t, hooks.NewRegistry(hooks.WithProviderHook(
		guardrailHook(t, "content_excludes", map[string]any{"patterns": []any{"pineapple"}}))))

	elems, err := feedChunks(t, s,
		transcriptChunk("Say P", pcm(10)), providers.StreamChunk{Interrupted: true},
		transcriptChunk("Hello", pcm(10)), stopChunk())
	require.NoError(t, err)
	assert.Equal(t, []string{"Say ", "Hello"}, liveText(elems))
}

// Media that waited behind held text is dropped with it when a retry restarts
// the stream.
func TestProviderStage_StreamResetDropsMediaHeldBehindText(t *testing.T) {
	stream := make(chan providers.StreamChunk, 8)
	stream <- providers.StreamChunk{Delta: "Say P", Content: "Say P",
		MediaData: &providers.StreamMediaData{MIMEType: "audio/pcm", Data: []byte{1}}}
	stream <- providers.StreamChunk{Reset: true}
	sendScriptedDeltas(stream, "Hello")
	close(stream)

	elems, err := runProviderStage(t, pineappleGuardedStage(t, stream), "hi")
	require.NoError(t, err)

	var audio int
	for _, e := range elems {
		if e.Audio != nil {
			audio++
		}
	}
	assert.Equal(t, 0, audio, "the failed attempt's audio is not delivered")
	assert.Equal(t, []string{"Say ", "Hello"}, liveText(elems))
}

// A stage with no event emitter discards a cut call's late usage.
func TestProviderStage_LateUsageWithoutAnEmitterIsDiscarded(t *testing.T) {
	stream := make(chan providers.StreamChunk, 8)
	sendScriptedDeltas(stream, "P", "ine", "apple")
	ps := pineappleGuardedStage(t, stream)
	ps.cutUsageWait = time.Millisecond

	elems, err := runProviderStage(t, ps, "hi")
	require.NoError(t, err)
	stream <- providers.StreamChunk{CostInfo: &types.CostInfo{OutputTokens: 4}}
	close(stream)
	time.Sleep(20 * time.Millisecond) // a report on a nil emitter would panic here
	assert.Equal(t, noPineappleMsg, lastAssistantOf(t, elems).Content)
}
