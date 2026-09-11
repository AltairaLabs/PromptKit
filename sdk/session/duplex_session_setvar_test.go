package session

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	mock "github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// TestDuplexSession_SetVarAfterStart_Warns covers #1962: a duplex pipeline
// renders the system prompt once, when the first input starts it, so a
// variable set afterwards can never reach the provider. The value is still
// stored, but the first such call must be reported rather than dropped
// silently — and a variable set before the first input must not be.
func TestDuplexSession_SetVarAfterStart_Warns(t *testing.T) {
	var buf bytes.Buffer
	logger.SetLogger(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { logger.SetLogger(nil) })

	ctx := context.Background()
	provider := mock.NewStreamingProvider("mock-provider", "mock-model", false)
	sess, err := NewDuplexSession(ctx, &DuplexSessionConfig{
		Provider:        provider,
		PipelineBuilder: testPipelineBuilder,
		Config: &providers.StreamingInputConfig{
			Config: types.StreamingMediaConfig{Type: types.ContentTypeAudio},
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = sess.Close() })

	sess.SetVar("before", "x")
	assert.Empty(t, buf.String(), "a variable set before the first input is rendered and must not warn")

	require.NoError(t, sess.SendText(ctx, "hello"))

	sess.SetVar("after", "y")
	sess.SetVar("again", "z")

	logs := buf.String()
	assert.Contains(t, logs, "will not reach the provider")
	assert.Contains(t, logs, `"variable":"after"`, "the warning names the first late variable")
	assert.Equal(t, 1, strings.Count(logs, "will not reach the provider"), "warn once per session, not per call")

	got, ok := sess.GetVar("after")
	assert.True(t, ok)
	assert.Equal(t, "y", got, "the value is stored even though it cannot be rendered")
}
