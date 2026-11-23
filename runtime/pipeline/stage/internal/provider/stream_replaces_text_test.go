package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
)

// liveText renders the live text elements the caller receives, each as
// "<text>" or, when it replaces the turn's text so far, "replace:<text>".
func liveText(elems []core.StreamElement) []string {
	var out []string
	for _, e := range elems {
		if e.Text == nil || e.Message != nil {
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

// A guardrail message that replaces a streamed reply is marked as replacing
// the text already streamed, on the text path, whether or not any was.
func TestProviderStage_CutReplacementIsMarkedAsReplacingTheText(t *testing.T) {
	for name, tc := range map[string]struct {
		deltas []string
		want   []string
	}{
		"after released text": {[]string{"Say P", "ine", "apple."}, []string{"Say ", "replace:" + noPineappleMsg}},
		"before any text":     {[]string{"P", "ine", "apple."}, []string{"replace:" + noPineappleMsg}},
		"a clean reply":       {[]string{"Pine", " cones"}, []string{"Pine cones"}},
	} {
		t.Run(name, func(t *testing.T) {
			stream := make(chan providers.StreamChunk, 8)
			sendScriptedDeltas(stream, tc.deltas...)
			close(stream)

			elems, err := runProviderStage(t, pineappleGuardedStage(t, stream), "what goes on pizza?")
			require.NoError(t, err)
			assert.Equal(t, tc.want, liveText(elems))
		})
	}
}

// The rest of a truncated reply follows what was sent: it replaces nothing.
func TestProviderStage_TruncationRestDoesNotReplaceTheText(t *testing.T) {
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
	assert.Equal(t, []string{"secret ", "pla"}, liveText(elems))
}
