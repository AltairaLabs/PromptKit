package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/sdk"
)

// These cover #1959: SetVar wrote to the session's variable map while the
// renderer read a copy taken at Open(), so a variable set after Open never
// reached the system prompt. Every assertion is on the rendered prompt the
// provider received — the pre-existing tests asserted a SetVar/GetVar round
// trip through the orphaned map and passed throughout.

func TestSetVar_ReachesSystemPrompt(t *testing.T) {
	conv, rec := openRecordingConv(t, "topic={{topic}}")
	conv.SetVar("topic", "batteries")

	_, err := conv.Send(context.Background(), "hello")
	require.NoError(t, err)

	assert.Contains(t, rec.system(), "topic=batteries")
	assert.NotContains(t, rec.system(), "{{topic}}")
}

func TestSetVars_ReachesSystemPrompt(t *testing.T) {
	conv, rec := openRecordingConv(t, "a={{alpha}} b={{beta}}")
	conv.SetVars(map[string]any{"alpha": "one", "beta": 2})

	_, err := conv.Send(context.Background(), "hello")
	require.NoError(t, err)

	assert.Contains(t, rec.system(), "a=one b=2")
}

// TestSetVar_LatestValueWinsAcrossSends pins that the renderer reads the live
// map rather than a snapshot taken at any single point: a value changed
// between sends must change the prompt.
func TestSetVar_LatestValueWinsAcrossSends(t *testing.T) {
	conv, rec := openRecordingConv(t, "topic={{topic}}")

	conv.SetVar("topic", "first")
	_, err := conv.Send(context.Background(), "hello")
	require.NoError(t, err)
	assert.Contains(t, rec.system(), "topic=first")

	conv.SetVar("topic", "second")
	_, err = conv.Send(context.Background(), "hello again")
	require.NoError(t, err)
	assert.Contains(t, rec.system(), "topic=second")
	assert.NotContains(t, rec.system(), "topic=first")
}

// TestSetVar_OverridesOpenTimeVariables pins precedence over WithVariables:
// the later, more specific declaration wins.
func TestSetVar_OverridesOpenTimeVariables(t *testing.T) {
	conv, rec := openRecordingConv(t, "topic={{topic}}",
		sdk.WithVariables(map[string]string{"topic": "default"}))
	conv.SetVar("topic", "explicit")

	_, err := conv.Send(context.Background(), "hello")
	require.NoError(t, err)

	assert.Contains(t, rec.system(), "topic=explicit")
	assert.NotContains(t, rec.system(), "topic=default")
}

// TestJSONInput_OverridesSetVar pins the other end of the precedence chain: a
// per-send binding is more specific than a conversation-level variable.
func TestJSONInput_OverridesSetVar(t *testing.T) {
	conv, rec := openRecordingConv(t, "topic={{topic}}")
	conv.SetVar("topic", "sticky")

	_, err := conv.Send(context.Background(), "", sdk.WithJSONInput(map[string]any{"topic": "per-send"}))
	require.NoError(t, err)

	assert.Contains(t, rec.system(), "topic=per-send")
	assert.NotContains(t, rec.system(), "topic=sticky")
}

// TestSetVar_SubstitutesInUserMessage covers the other consumer of the merged
// variable map: message text, not just the system template.
func TestSetVar_SubstitutesInUserMessage(t *testing.T) {
	conv, rec := openRecordingConv(t, "static prompt")
	conv.SetVar("product", "drill")

	_, err := conv.Send(context.Background(), "my {{product}} is broken")
	require.NoError(t, err)

	assert.Equal(t, "my drill is broken", rec.userText())
}

// TestJSONInput_SecondSendRebindsSystemPrompt covers the same freeze from the
// per-send binding side: the system prompt was rendered once per conversation,
// so every turn after the first rendered turn one's values.
func TestJSONInput_SecondSendRebindsSystemPrompt(t *testing.T) {
	conv, rec := openRecordingConv(t, "topic={{topic}}")

	_, err := conv.Send(context.Background(), "", sdk.WithJSONInput(map[string]any{"topic": "first"}))
	require.NoError(t, err)
	assert.Contains(t, rec.system(), "topic=first")

	_, err = conv.Send(context.Background(), "", sdk.WithJSONInput(map[string]any{"topic": "second"}))
	require.NoError(t, err)
	assert.Contains(t, rec.system(), "topic=second")
}
