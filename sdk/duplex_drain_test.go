package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// closeDuration reports how long Close took and what it returned.
func closeDuration(t *testing.T, conv *Conversation) (time.Duration, error) {
	t.Helper()
	start := time.Now()
	err := conv.Close()
	return time.Since(start), err
}

// drainBudget is well under session.DefaultDrainTimeout (30s): a graceful drain
// completes in milliseconds, so anything near the timeout means it deadlocked.
const drainBudget = 5 * time.Second

// TestDuplexCloseDrainsPromptly covers #1638: closing a duplex conversation
// used to block for the full 30s drain timeout and return
// "drain timed out: context deadline exceeded", regardless of whether the
// caller drained Response().
//
// Drain sends an EndOfStream element and waits for the pipeline before closing
// the input channel, but the non-streaming ProviderStage only ended on channel
// close — so the wait could never succeed. The session did shut down correctly
// afterwards via the hard-close fallback, which is why the failure showed up as
// a 30s pause rather than broken behavior.
//
// The subtests cover the shapes that all previously timed out, including the
// undrained case the issue originally blamed. Draining was never the variable.
func TestDuplexCloseDrainsPromptly(t *testing.T) {
	newConv := func(t *testing.T) *Conversation {
		t.Helper()
		conv, err := OpenDuplex(writeIngestionTestPack(t), "main",
			WithProvider(mock.NewStreamingProvider("mock", "mock-model", false).
				WithAutoRespond("hi")),
			WithSkipSchemaValidation(),
		)
		require.NoError(t, err)
		return conv
	}

	sendOne := func(t *testing.T, conv *Conversation) {
		t.Helper()
		require.NoError(t, conv.SendChunk(context.Background(),
			&providers.StreamChunk{Content: "hello"}))
		time.Sleep(200 * time.Millisecond)
	}

	t.Run("idle session that never received input", func(t *testing.T) {
		d, err := closeDuration(t, newConv(t))
		assert.NoError(t, err)
		assert.Less(t, d, drainBudget, "Close blocked on the drain timeout")
	})

	t.Run("response channel drained", func(t *testing.T) {
		conv := newConv(t)
		ch, err := conv.Response()
		require.NoError(t, err)
		go func() {
			for range ch {
			}
		}()
		sendOne(t, conv)

		d, cerr := closeDuration(t, conv)
		assert.NoError(t, cerr)
		assert.Less(t, d, drainBudget, "Close blocked on the drain timeout")
	})

	t.Run("response channel taken but never read", func(t *testing.T) {
		conv := newConv(t)
		_, err := conv.Response()
		require.NoError(t, err)
		sendOne(t, conv)

		d, cerr := closeDuration(t, conv)
		assert.NoError(t, cerr)
		assert.Less(t, d, drainBudget, "Close blocked on the drain timeout")
	})

	t.Run("response never requested", func(t *testing.T) {
		conv := newConv(t)
		sendOne(t, conv)

		d, cerr := closeDuration(t, conv)
		assert.NoError(t, cerr)
		assert.Less(t, d, drainBudget, "Close blocked on the drain timeout")
	})
}
