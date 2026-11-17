//go:build integration

package sdk_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Live verification that a text-only OpenAI Realtime reply streams: it
// reaches Response() as several deltas, before the turn ends, and the
// recorded reply is their text once.
//
//	go -C sdk test -tags integration -run 'TestLiveRealtimeTextOnlyReplyStreams' -count=1 -v .
func TestLiveRealtimeTextOnlyReplyStreams(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	rt := rtlRealtimeProviders()[0] // openai-realtime, text-only
	require.Equal(t, "openai-realtime", rt.name)
	conv := rtlOpenDuplex(t, rt, rtlWeatherPackJSON)
	ch, err := conv.Response()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), rtlTimeout)
	defer cancel()
	require.NoError(t, conv.SendText(ctx, "Without calling any tool, name the four seasons in one sentence."))

	var deltas []string
	for done := false; !done; {
		select {
		case c, ok := <-ch:
			require.True(t, ok, "Response() closed before the reply ended")
			require.NoError(t, c.Error)
			if c.Delta != "" {
				deltas = append(deltas, c.Delta)
			}
			done, _ = c.Metadata["assistant_turn_complete"].(bool)
		case <-ctx.Done():
			t.Fatalf("the reply never ended; deltas so far %q", deltas)
		}
	}

	assert.Greater(t, len(deltas), 1, "the reply arrived as one chunk: %q", deltas)
	msgs := conv.Messages(ctx)
	require.NotEmpty(t, msgs)
	last := msgs[len(msgs)-1]
	assert.Equal(t, "assistant", last.Role)
	assert.Equal(t, strings.Join(deltas, ""), last.GetContent())
	t.Logf("%d deltas: %q", len(deltas), deltas)
}
