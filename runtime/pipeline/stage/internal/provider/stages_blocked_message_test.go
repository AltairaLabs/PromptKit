package provider

import (
	"context"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// lengthGuard is the length guardrail at max characters: it truncates.
func lengthGuard(t *testing.T, max int) hooks.ProviderHook {
	t.Helper()
	guard, err := guardrails.NewGuardrailHook("length", map[string]any{"max_characters": max})
	require.NoError(t, err)
	return guard
}

// On the text path the function chooses a blocked turn's text, before the
// call, on a streamed chunk and on the completed reply, and is given the
// firing.
func TestBlockedMessage_TextPathTakesTheHostText(t *testing.T) {
	cases := []struct {
		name      string
		streaming bool
		hook      hooks.ProviderHook
		want      BlockedMessage
	}{
		{"before call", false, &enforceBeforeCallHook{reason: "input blocked", replacement: "Canned.",
			metadata: map[string]any{"validator_type": "test_input"}},
			BlockedMessage{ValidatorType: "test_input", Direction: hooks.DirectionInput, Reason: "input blocked",
				Text: "Canned."}},
		{"completed reply", false, &enforceAfterCallHook{replacement: "Replaced."},
			BlockedMessage{ValidatorType: "test_output", Direction: hooks.DirectionOutput, Reason: "output blocked",
				Text: "Replaced."}},
		{"streamed chunk", true, bannedWordsHook(t, "Mock"),
			BlockedMessage{ValidatorType: "banned_words", Direction: hooks.DirectionOutput,
				Reason: `forbidden content found in stream: "Mock"`, Text: "I can't say that."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host := &hostTexts{reply: "Host text."}
			elems, err := runProviderStage(t, textStage(tc.streaming, tc.hook, host.fn), "hi")
			require.NoError(t, err)
			msgs := assistantMessages(elems)
			require.Len(t, msgs, 1)
			assert.Equal(t, "Host text.", msgs[0].Content)
			assert.Equal(t, []BlockedMessage{tc.want}, host.calls())
		})
	}
}

// A streamed reply a guardrail replaced is not judged again on completion:
// the host's text, though it names what the guardrail bans, is the turn's,
// and the function is asked once. The turn index is the 0-based index of the
// turn (the third turn is 2), as on the realtime path.
func TestBlockedMessage_TextPathReplacementIsNotJudgedAgain(t *testing.T) {
	host := &hostTexts{reply: "Mock is not discussed."}
	turn := core.NewTurnState()
	turn.SetTurnIndex(3)
	provider := &redactionRecordingProvider{Provider: mock.NewProvider("p", "m", false), streaming: true}
	s := NewProviderStageWithTurnState(provider, nil, nil, &ProviderConfig{MaxTokens: 100, BlockedMessage: host.fn},
		nil, hooks.NewRegistry(hooks.WithProviderHook(bannedWordsHook(t, "Mock"))), turn)

	elems, err := runProviderStage(t, s, "hi")
	require.NoError(t, err)

	msgs := assistantMessages(elems)
	require.Len(t, msgs, 1)
	assert.Equal(t, "Mock is not discussed.", msgs[0].Content)
	assert.Len(t, msgs[0].Validations, 1, "one firing")
	require.Len(t, host.calls(), 1)
	assert.Equal(t, 2, host.calls()[0].TurnIndex)
}

// On the text path a truncation, streamed or on the completed reply, does not
// ask the function.
func TestBlockedMessage_TextPathTruncationDoesNotAsk(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		host := &hostTexts{reply: "Never shown."}
		elems, err := runProviderStage(t, textStage(streaming, lengthGuard(t, 8), host.fn), "hi")
		require.NoError(t, err)
		msgs := assistantMessages(elems)
		require.Len(t, msgs, 1)
		assert.Equal(t, "Mock res", msgs[0].Content, "streaming=%v", streaming)
		assert.Empty(t, host.calls(), "streaming=%v", streaming)
	}
}

// A firing whose decision carries the pack's declaration names its id.
func TestBlockedMessage_ValidatorIDFromTheDeclaration(t *testing.T) {
	host := &hostTexts{}
	d := hooks.Enforced("why", map[string]any{"validator_type": "t",
		hooks.MetadataKeyValidatorDeclaration: &packspec.Validator{ID: "v-1", Type: "t"}})
	got := ChooseBlockedText(context.Background(), host.fn, 0, BlockedFiring{Decision: &d, Direction: hooks.DirectionOutput,
		Text: "Text.", Turn: 2, Caller: types.Caller{}})
	assert.Equal(t, "Text.", got, "an empty choice keeps the text")
	assert.Equal(t, []BlockedMessage{{ValidatorType: "t", ValidatorID: "v-1", Direction: hooks.DirectionOutput,
		Reason: "why", Text: "Text.", TurnIndex: 2}}, host.calls())
}

// Only text that is not a prefix of the original replaces it: a truncation and
// unchanged content keep the model's text.
func TestReplacedText(t *testing.T) {
	assert.False(t, replacedText("abcdef", "abc"), "a truncation")
	assert.False(t, replacedText("abc", "abc"), "unchanged")
	assert.True(t, replacedText("abcdef", "xyz"), "a replacement")
	assert.True(t, replacedText("ab", "abc"), "a longer text")
}
