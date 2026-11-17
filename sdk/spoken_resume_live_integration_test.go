//go:build integration

package sdk_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// Live: a conversation spoken on OpenAI Realtime (its replies stored with
// their audio and transcript) resumes on a text model, on both OpenAI APIs.
//
//	go -C sdk test -tags integration -run 'TestLiveSpokenConversationResumesOnText' -count=1 .
func TestLiveSpokenConversationResumesOnText(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	store := statestore.NewMemoryStore()
	pack := rtlWritePack(t, `{"id":"spoken","version":"1.0.0","description":"spoken resume",
		"prompts":{"chat":{"id":"chat","name":"Chat","system_template":"You are terse. Answer in one short sentence."}}}`)
	rt := openai.NewProvider("openai-realtime", rtlEnvOr("OPENAI_REALTIME_MODEL", "gpt-realtime"),
		"https://api.openai.com", providers.ProviderDefaults{}, false)
	conv, err := sdk.OpenDuplex(pack, "chat", sdk.WithProvider(rt), sdk.WithSkipSchemaValidation(),
		sdk.WithStateStore(store), sdk.WithStreamingConfig(&providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio, ChunkSize: 4800,
				SampleRate: 24000, Channels: 1, BitDepth: 16, Encoding: "pcm16"},
		}))
	require.NoError(t, err)
	ch, err := conv.Response()
	require.NoError(t, err)
	go func() {
		for range ch { //nolint:revive // drained
		}
	}()
	require.NoError(t, conv.SendText(context.Background(), "Say hello to Ada."))

	spoken := func() bool {
		state, err := store.Load(context.Background(), conv.ID())
		if err != nil || state == nil {
			return false
		}
		for _, m := range state.Messages {
			if m.Role == "assistant" && m.GetContent() != "" {
				for _, p := range m.Parts {
					if p.Type == types.ContentTypeAudio {
						return true
					}
				}
			}
		}
		return false
	}
	require.Eventually(t, spoken, 60*time.Second, 100*time.Millisecond,
		"no spoken reply with audio and transcript was stored")
	require.NoError(t, conv.Close())

	for _, mode := range []string{"responses", "chat_completions"} {
		t.Run(mode, func(t *testing.T) {
			text, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
				ID: "openai-text", Type: "openai", Model: rtlEnvOr("OPENAI_TEXT_MODEL", "gpt-4o-mini"),
				BaseURL: "https://api.openai.com/v1", Defaults: providers.ProviderDefaults{MaxTokens: 60},
				AdditionalConfig: map[string]any{"api_mode": mode},
			})
			require.NoError(t, err)
			resumed, err := sdk.Resume(conv.ID(), pack, "chat", sdk.WithProvider(text), sdk.WithSkipSchemaValidation(),
				sdk.WithStateStore(store))
			require.NoError(t, err)
			defer func() { _ = resumed.Close() }()
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			resp, err := resumed.Send(ctx, "Whom did you greet? One word.")
			require.NoError(t, err)
			assert.Contains(t, resp.Text(), "Ada")
		})
	}
}
