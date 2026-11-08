package integration

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// requiredVarPackJSON declares a prompt with a required variable.
const requiredVarPackJSON = `{
	"id": "required-var", "version": "1.0.0", "description": "required variable",
	"prompts": {"chat": {"id": "chat", "name": "Chat", "version": "1.0.0",
		"system_template": "Topic: {{topic}}",
		"variables": [{"name": "topic", "type": "string", "required": true}]}}
}`

// staticVars is a VariableProvider with fixed values.
type staticVars map[string]string

func (staticVars) Name() string { return "static" }
func (s staticVars) Provide(context.Context) (map[string]string, error) {
	return s, nil
}

// TestRequiredVariable_FromEverySourceReachesThePrompt supplies a required
// variable from each source a caller has. The provider must see the pack's
// prompt, rendered, never a substitute.
func TestRequiredVariable_FromEverySourceReachesThePrompt(t *testing.T) {
	cases := []struct {
		name  string
		opts  []sdk.Option
		setup func(*sdk.Conversation)
	}{
		{name: "with_variables", opts: []sdk.Option{sdk.WithVariables(map[string]string{"topic": "billing"})}},
		{name: "set_var", setup: func(c *sdk.Conversation) { c.SetVar("topic", "billing") }},
		{name: "variable_provider", opts: []sdk.Option{sdk.WithVariableProvider(staticVars{"topic": "billing"})}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := newRecordingProvider()
			conv := openTestConvWithPack(t, requiredVarPackJSON, "chat",
				append([]sdk.Option{sdk.WithProvider(rec)}, tc.opts...)...)
			if tc.setup != nil {
				tc.setup(conv)
			}
			_, err := conv.Send(context.Background(), "hi")
			require.NoError(t, err)
			assert.Equal(t, "Topic: billing", rec.system(), "the pack's prompt did not reach the provider")
		})
	}
}

// TestRequiredVariable_MissingFailsTheTurn leaves the required variable unset.
// The turn fails with ErrTemplateUnresolved naming it, and the provider is
// never called with a substitute prompt.
func TestRequiredVariable_MissingFailsTheTurn(t *testing.T) {
	rec := newRecordingProvider()
	conv := openTestConvWithPack(t, requiredVarPackJSON, "chat", sdk.WithProvider(rec))
	_, err := conv.Send(context.Background(), "hi")
	require.Error(t, err)
	assert.True(t, errors.Is(err, stage.ErrTemplateUnresolved), "error: %v", err)
	assert.Contains(t, err.Error(), "topic")
	assert.Empty(t, rec.system(), "the provider was called without the pack's prompt")
}

// TestRequiredVariable_PerSendValueDoesNotCarryOver binds the required
// variable on one Send only. The next Send without it fails: a per-send value
// belongs to its own turn and must not satisfy, or render into, a later one.
func TestRequiredVariable_PerSendValueDoesNotCarryOver(t *testing.T) {
	rec := newRecordingProvider()
	conv := openTestConvWithPack(t, requiredVarPackJSON, "chat", sdk.WithProvider(rec))
	ctx := context.Background()

	_, err := conv.Send(ctx, "hi", sdk.WithJSONInput(map[string]any{"topic": "billing"}))
	require.NoError(t, err)
	require.Equal(t, "Topic: billing", rec.system())

	_, err = conv.Send(ctx, "again")
	require.Error(t, err, "the first Send's per-send value satisfied the second Send's required variable")
	assert.True(t, errors.Is(err, stage.ErrTemplateUnresolved), "error: %v", err)
}
