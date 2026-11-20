package sdk

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
)

// writeStepValidatorPack writes a pack whose composition "flow" runs one
// prompt step on "main", which has the one validator given as JSON.
func writeStepValidatorPack(t *testing.T, validator string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "step-validator.pack.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"name": "step-validator",
		"version": "v1",
		"prompts": {
			"main": {"system_template": "You are a helpful assistant.", "validators": [`+validator+`]}
		},
		"compositions": {
			"flow": {"version": 1, "output": "s",
				"steps": [{"id": "s", "kind": "prompt", "prompt_task": "main", "input": "${input}"}]}
		}
	}`), 0o600))
	return path
}

// A composition step runs its own prompt's validators, as Open runs the
// opened prompt's: an output validator blocks the step's reply with its
// message, and an input validator blocks the step's input before the call.
func TestComposition_StepRunsItsPromptsValidators(t *testing.T) {
	t.Run("output", func(t *testing.T) {
		path := writeStepValidatorPack(t, `{"type": "banned_words", "id": "no-bruno", "enabled": true,
			"message": "We don't talk about Bruno.", "params": {"words": ["Bruno"]}}`)
		conv, err := OpenComposition(path, "flow", WithProvider(brunoReplyProvider()), WithSkipSchemaValidation())
		require.NoError(t, err)
		t.Cleanup(func() { _ = conv.Close() })

		resp, err := conv.Send(context.Background(), "who is in the family?")
		require.NoError(t, err)
		assert.Equal(t, `"`+brunoMessage+`"`, resp.Text())
	})
	t.Run("input", func(t *testing.T) {
		path := writeStepValidatorPack(t, `{"type": "banned_words", "id": "no-bruno-in", "enabled": true,
			"message": "We don't talk about Bruno.", "params": {"words": ["Bruno"], "direction": "input"}}`)
		prov := &recordingTextProvider{}
		conv, err := OpenComposition(path, "flow", WithProvider(prov), WithSkipSchemaValidation())
		require.NoError(t, err)
		t.Cleanup(func() { _ = conv.Close() })

		resp, err := conv.Send(context.Background(), "tell me about Bruno")
		require.NoError(t, err)
		assert.Equal(t, `"`+brunoMessage+`"`, resp.Text())
		assert.Empty(t, prov.received, "the blocked input never reached the model")
	})
	t.Run("host text", func(t *testing.T) {
		path := writeStepValidatorPack(t, `{"type": "banned_words", "id": "no-bruno", "enabled": true,
			"message": "We don't talk about Bruno.", "params": {"words": ["Bruno"]}}`)
		host := &blockedCalls{reply: localizedBruno}
		conv, err := OpenComposition(path, "flow", WithProvider(brunoReplyProvider()),
			WithSkipSchemaValidation(), WithBlockedMessage(host.fn))
		require.NoError(t, err)
		t.Cleanup(func() { _ = conv.Close() })

		resp, err := conv.Send(context.Background(), "who is in the family?")
		require.NoError(t, err)
		assert.Equal(t, `"`+localizedBruno+`"`, resp.Text())
		require.Len(t, host.calls(), 1)
		assert.Equal(t, "no-bruno", host.calls()[0].ValidatorID)
	})
}

// stepCallCounter counts the provider calls it sees after the fact.
type stepCallCounter struct {
	mu    sync.Mutex
	after int
}

func (h *stepCallCounter) Name() string { return "counting" }
func (h *stepCallCounter) BeforeCall(context.Context, *hooks.ProviderRequest) hooks.Decision {
	return hooks.Allow
}
func (h *stepCallCounter) AfterCall(context.Context, *hooks.ProviderRequest, *hooks.ProviderResponse) hooks.Decision {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.after++
	return hooks.Allow
}

// A host's provider hook runs once per step call beside the step prompt's
// validators, not once more for being the conversation's.
func TestComposition_HostHookRunsOnceBesideStepValidators(t *testing.T) {
	path := writeStepValidatorPack(t, `{"type": "banned_words", "id": "no-bruno", "enabled": true,
		"message": "We don't talk about Bruno.", "params": {"words": ["Bruno"]}}`)
	counter := &stepCallCounter{}
	conv, err := OpenComposition(path, "flow", WithProvider(&recordingTextProvider{}),
		WithSkipSchemaValidation(), WithProviderHook(counter))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	resp, err := conv.Send(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, `"ack"`, resp.Text())
	assert.Equal(t, 1, counter.after)
}

// A prompt step inside a parallel step's branches runs its prompt's
// validators too.
func TestComposition_ParallelBranchStepRunsItsPromptsValidators(t *testing.T) {
	path := filepath.Join(t.TempDir(), "branch-validator.pack.json")
	require.NoError(t, os.WriteFile(path, []byte(`{
		"name": "branch-validator",
		"version": "v1",
		"prompts": {
			"main": {"system_template": "You are a helpful assistant.", "validators": [
				{"type": "banned_words", "id": "no-bruno", "enabled": true,
				 "message": "We don't talk about Bruno.", "params": {"words": ["Bruno"]}}]}
		},
		"compositions": {
			"flow": {"version": 1, "output": "p",
				"steps": [{"id": "p", "kind": "parallel",
					"branches": [{"id": "s", "kind": "prompt", "prompt_task": "main", "input": "${input}"}],
					"reduce": {"strategy": "barrier", "into": "out"}}]}
		}
	}`), 0o600))
	conv, err := OpenComposition(path, "flow", WithProvider(brunoReplyProvider()), WithSkipSchemaValidation())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	resp, err := conv.Send(context.Background(), "who is in the family?")
	require.NoError(t, err)
	assert.Contains(t, resp.Text(), brunoMessage)
	assert.NotContains(t, resp.Text(), "Let me tell you")
}

// A step prompt's validator that cannot be built fails the open, as one on
// an opened prompt does, rather than leaving the step unguarded.
func TestComposition_UnbuildableStepValidatorFailsTheOpen(t *testing.T) {
	path := writeStepValidatorPack(t, `{"type": "no_such_check", "id": "bad", "enabled": true}`)
	_, err := OpenComposition(path, "flow", WithProvider(brunoReplyProvider()), WithSkipSchemaValidation())
	require.Error(t, err)
	assert.Contains(t, err.Error(), `composition step "s": prompt "main"`)
}
