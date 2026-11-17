//go:build integration

package sdk_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

const liveIdlePackJSON = `{
	"id": "live-idle", "version": "1.0.0",
	"prompts": {"chat": {"id": "chat", "name": "Chat", "version": "1.0.0",
		"system_template": "You are terse. Answer in at most five words."}}
}`

// A live Gemini Live session left idle past its idle timeout ends, and a
// SendText after that returns the idle-timeout error at once instead of
// blocking.
//
//	go -C sdk test -tags integration -run 'TestLiveDuplex_SendAfterIdleTimeout' -count=1 -v .
func TestLiveDuplex_SendAfterIdleTimeout(t *testing.T) {
	rtlEachRealtime(t, func(t *testing.T, rt rtlRealtime) {
		if rt.name != "gemini-live" {
			t.Skip("Gemini Live only")
		}
		conv := rtlOpenDuplex(t, rt, liveIdlePackJSON, sdk.WithIdleTimeout(3*time.Second))
		stream := rtlWatch(t, conv)
		require.NoError(t, conv.SendText(context.Background(), "Say hello."))

		done, err := conv.Done()
		require.NoError(t, err)
		select {
		case <-done:
		case <-time.After(rtlTimeout):
			t.Fatal("the idle session never ended")
		}
		select {
		case <-stream.closed:
		case <-time.After(5 * time.Second):
			t.Fatal("Response() did not close when the session ended")
		}

		got := make(chan error, 1)
		go func() { got <- conv.SendText(context.Background(), "Are you there?") }()
		select {
		case err := <-got:
			assert.ErrorIs(t, err, stage.ErrIdleTimeout)
		case <-time.After(5 * time.Second):
			t.Fatal("SendText blocked on the ended session")
		}
		assert.ErrorIs(t, conv.SessionError(), stage.ErrIdleTimeout)
	})
}
