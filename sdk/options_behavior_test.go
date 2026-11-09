package sdk

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// WithCredential's sources (an explicit key, an environment variable, a file)
// are what the auto-detected provider authenticates with.
func TestConv_WithCredentialAuthenticatesTheRequest(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "key.txt")
	if err := os.WriteFile(keyFile, []byte("file-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]CredentialOption{
		"api key": WithCredentialAPIKey("explicit-key"),
		"env":     WithCredentialEnv("CONV_TEST_KEY"),
		"file":    WithCredentialFile(keyFile),
	}
	want := map[string]string{"api key": "explicit-key", "env": "env-key", "file": "file-key"}
	for name, opt := range cases {
		t.Run(name, func(t *testing.T) { assertConvCredentialOnWire(t, opt, "Bearer "+want[name]) })
	}
}

// assertConvCredentialOnWire opens an auto-detected conversation with opt and
// checks the Authorization header its request carries.
func assertConvCredentialOnWire(t *testing.T, opt CredentialOption, wantAuth string) {
	t.Helper()
	convClearProviderEnv(t)
	t.Setenv("CONV_TEST_KEY", "env-key")
	w := newConvWire(t)
	conv, err := Open(createTestPackFile(t, convChatPack), "chat", WithSkipSchemaValidation(), WithCredential(opt))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	convRedirectAgent(t, conv, w)
	_, _ = conv.Send(context.Background(), "hi") // the endpoint's answer is not under test
	assert.Equal(t, wantAuth, w.last(t).Header.Get("Authorization"))
}

// WithVariables values render into the first turn's system prompt and win
// over the pack's defaults.
func TestConv_WithVariablesRenderIntoTheSystemPrompt(t *testing.T) {
	w := newConvWire(t)
	conv := openConv(t, w, WithVariables(map[string]string{"name": "Lin"}))
	_, err := conv.Send(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "You help Lin.", w.lastChat(t).system(t))
}

// WithResponseFormat and WithJSONMode put the format in the provider request.
func TestConv_ResponseFormatReachesTheRequest(t *testing.T) {
	t.Run("JSON mode", func(t *testing.T) {
		w := newConvWire(t, convWireReply{Text: `{"ok":true}`})
		conv := openConv(t, w, WithJSONMode())
		_, err := conv.Send(context.Background(), "hi")
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"type": "json_object"}, w.lastChat(t).Body["response_format"])
	})
	t.Run("JSON schema", func(t *testing.T) {
		w := newConvWire(t, convWireReply{Text: `{"name":"x"}`})
		conv := openConv(t, w, WithResponseFormat(&providers.ResponseFormat{
			Type: providers.ResponseFormatJSONSchema, SchemaName: "person", Strict: true,
			JSONSchema: []byte(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`),
		}))
		_, err := conv.Send(context.Background(), "hi")
		require.NoError(t, err)
		rf, _ := w.lastChat(t).Body["response_format"].(map[string]any)
		require.NotNil(t, rf, "the request carries no response_format")
		assert.Equal(t, "json_schema", rf["type"])
		js, _ := rf["json_schema"].(map[string]any)
		assert.Equal(t, "person", js["name"])
		assert.Equal(t, true, js["strict"])
	})
}

// WithUserID and WithSessionMetadata are persisted with the conversation, in
// every store, and a resumed conversation keeps the user who started it.
func TestConv_UserIDAndSessionMetadataArePersisted(t *testing.T) {
	for name, newStore := range convStores(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			store := newStore()
			w := newConvWire(t)
			conv := openConv(t, w, WithStateStore(store), WithConversationID("meta"),
				WithUserID("user-7"), WithSessionMetadata(map[string]any{"tenant": "acme"}))
			_, err := conv.Send(ctx, "hi")
			require.NoError(t, err)

			state, err := store.Load(ctx, "meta")
			require.NoError(t, err)
			assert.Equal(t, "user-7", state.UserID)
			assert.Equal(t, "acme", state.Metadata["tenant"])
			assert.Len(t, state.Messages, 2)

			resumed, err := Resume("meta", createTestPackFile(t, convChatPack), "chat", WithSkipSchemaValidation(),
				WithLLMProvider(convWireSpec(w)), WithStateStore(store), WithUserID("someone-else"))
			require.NoError(t, err)
			t.Cleanup(func() { _ = resumed.Close() })
			state, err = store.Load(ctx, "meta")
			require.NoError(t, err)
			assert.Equal(t, "user-7", state.UserID)
			assert.Len(t, state.Messages, 2, "recording the user must not drop the history")
		})
	}
}

// WithMaxMessageSize rejects a larger message before it reaches the provider.
func TestConv_WithMaxMessageSizeRejectsBeforeTheProvider(t *testing.T) {
	w := newConvWire(t)
	conv := openConv(t, w, WithMaxMessageSize(10))
	_, err := conv.Send(context.Background(), strings.Repeat("x", 11))
	require.ErrorIs(t, err, ErrMessageTooLarge)
	assert.Empty(t, w.chatRequests())

	_, err = conv.Send(context.Background(), "short")
	require.NoError(t, err)
	assert.Len(t, w.chatRequests(), 1)
}
