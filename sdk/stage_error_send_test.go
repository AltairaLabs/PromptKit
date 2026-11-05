package sdk

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
)

// unresolvedVarPack's system template uses a variable it never declares, the
// reproduction from #2312.
const unresolvedVarPack = `{
  "id": "repro",
  "name": "Repro",
  "version": "1.0.0",
  "template_engine": {"version": "v1", "syntax": "{{variable}}"},
  "prompts": {
    "chat": {
      "id": "chat",
      "name": "Chat",
      "version": "1.0.0",
      "system_template": "The user's name is {{user_name}}."
    }
  }
}`

func openUnresolvedVarConversation(t *testing.T) (*Conversation, *countingProvider) {
	t.Helper()
	packPath := filepath.Join(t.TempDir(), "p.pack.json")
	require.NoError(t, os.WriteFile(packPath, []byte(unresolvedVarPack), 0o600))

	provider := newCountingMockProvider()
	conv, err := Open(packPath, "chat", WithProvider(provider))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	return conv, provider
}

// TestSend_UnresolvedTemplateVariableFailsTheTurn is #2312: Send returned a nil
// error and the model's reply, having called the model without its system
// prompt. The template stage's error must end the turn.
func TestSend_UnresolvedTemplateVariableFailsTheTurn(t *testing.T) {
	conv, provider := openUnresolvedVarConversation(t)

	_, err := conv.Send(context.Background(), "Hello!")

	require.Error(t, err)
	assert.ErrorIs(t, err, stage.ErrTemplateUnresolved)
	assert.Zero(t, provider.callCount(), "the model must not be called after the template failed")
}

// TestStream_UnresolvedTemplateVariableFailsTheTurn is the streaming half.
func TestStream_UnresolvedTemplateVariableFailsTheTurn(t *testing.T) {
	conv, provider := openUnresolvedVarConversation(t)

	var streamErr error
	for chunk := range conv.Stream(context.Background(), "Hello!") {
		if chunk.Error != nil && streamErr == nil {
			streamErr = chunk.Error
		}
	}

	require.Error(t, streamErr)
	assert.ErrorIs(t, streamErr, stage.ErrTemplateUnresolved)
	assert.Zero(t, provider.callCount(), "the model must not be called after the template failed")
}
