//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// Live check that OpenAI accepts a realtime conversation resumed with Resume
// after its session ended during a tool round, and after a user turn arrived
// while the round ran, on both of its APIs. Chat Completions rejects a tool
// call whose results do not follow it at once.
//
// Run (keys loaded from .env into the test process only):
//
//	go -C sdk test -tags integration -run 'TestLiveDuplexToolExchange' -count=1 ./integration/

// liveOpenAIText is the OpenAI text provider (OPENAI_TEXT_MODEL, default
// gpt-4o-mini) on apiMode, or skips the test without a key.
func liveOpenAIText(t *testing.T, apiMode string) providers.Provider {
	t.Helper()
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	model := os.Getenv("OPENAI_TEXT_MODEL")
	if model == "" {
		model = "gpt-4o-mini"
	}
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "openai-text", Type: "openai", Model: model, BaseURL: "https://api.openai.com/v1",
		Defaults:         providers.ProviderDefaults{MaxTokens: 60},
		AdditionalConfig: map[string]any{"api_mode": apiMode},
	})
	require.NoError(t, err)
	return p
}

// liveAPIModes are the OpenAI APIs a resumed conversation is checked on.
var liveAPIModes = []string{"chat_completions", "responses"}

func TestLiveDuplexToolExchange_CloseDuringRoundResumesOnOpenAI(t *testing.T) {
	for _, mode := range liveAPIModes {
		t.Run(mode, func(t *testing.T) {
			prov := liveOpenAIText(t, mode)
			c := openToolExchangeCall(t)
			c.ask("where is order 7?")
			c.callTool("call_1")
			require.NoError(t, c.conv.Close())
			c.saved(3)

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			resp, err := c.resume(prov).Send(ctx, "Reply with one word: ok")
			require.NoError(t, err, "OpenAI accepts the resumed conversation")
			assert.NotEmpty(t, resp.Text())
		})
	}
}

func TestLiveDuplexToolExchange_TurnDuringRoundResumesOnOpenAI(t *testing.T) {
	for _, mode := range liveAPIModes {
		t.Run(mode, func(t *testing.T) {
			prov := liveOpenAIText(t, mode)
			c := openToolExchangeCall(t)
			c.ask("where is order 7?")
			c.callTool("call_1")
			c.ask("and order 8?")
			close(c.release)
			select {
			case <-c.sess.toolResponses:
			case <-time.After(3 * time.Second):
				t.Fatal("the tool's result never reached the session")
			}
			reason := "stop"
			c.sess.EmitChunk(&providers.StreamChunk{Content: "Both have shipped.", FinishReason: &reason})
			c.saved(5)
			require.NoError(t, c.conv.Close())

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			resp, err := c.resume(prov).Send(ctx, "Reply with one word: ok")
			require.NoError(t, err, "OpenAI accepts the resumed conversation")
			assert.NotEmpty(t, resp.Text())
		})
	}
}
