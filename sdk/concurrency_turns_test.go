package sdk

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Concurrent turns on one conversation (#2570): Conversation is documented as
// safe for concurrent use, so N goroutines calling Send and Stream at once must
// each get the reply to their own input, and the history must end as N
// complete user/assistant pairs, never interleaved. Run under -race.
func TestConv_ConcurrentTurnsAreSerialized(t *testing.T) {
	const n = 6
	ctx := context.Background()
	w := newConvWire(t, convWireReply{Echo: true})
	conv := openConv(t, w)

	start := make(chan struct{})
	var wg sync.WaitGroup
	replies := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			in := fmt.Sprintf("msg-%d", i)
			if i%2 == 0 {
				resp, err := conv.Send(ctx, in)
				errs[i] = err
				if err == nil {
					replies[i] = resp.Text()
				}
				return
			}
			for chunk := range conv.Stream(ctx, in) {
				if chunk.Error != nil {
					errs[i] = chunk.Error
				}
				if chunk.Type == ChunkDone && chunk.Message != nil {
					replies[i] = chunk.Message.Text()
				}
			}
		}(i)
	}
	close(start)
	wg.Wait()

	for i := 0; i < n; i++ {
		require.NoError(t, errs[i], "turn %d", i)
		assert.Equal(t, fmt.Sprintf("echo:msg-%d", i), replies[i], "turn %d got another turn's reply", i)
	}

	msgs := conv.Messages(ctx)
	require.Len(t, msgs, 2*n, "every turn must leave exactly one user and one assistant message")
	seen := map[string]bool{}
	for i := 0; i < len(msgs); i += 2 {
		require.Equal(t, "user", msgs[i].Role, "message %d", i)
		require.Equal(t, "assistant", msgs[i+1].Role, "message %d", i+1)
		in := msgs[i].GetContent()
		assert.Equal(t, "echo:"+in, msgs[i+1].GetContent(), "pair %d is interleaved", i/2)
		seen[in] = true
	}
	assert.Len(t, seen, n)

	// Each request saw only whole earlier turns: system, pairs, then its user.
	for _, r := range w.chatRequests() {
		m := r.messages(t)
		require.Equal(t, "system", m[0].Role)
		require.Equal(t, 0, (len(m)-2)%2, "request history is not whole pairs: %+v", m)
		for j := 1; j < len(m)-1; j += 2 {
			assert.Equal(t, "echo:"+m[j].Text, m[j+1].Text, "request history interleaves turns: %+v", m)
		}
	}
}

// A tool handler that starts a turn on the conversation running it gets
// ErrReentrantTurn instead of deadlocking, and the outer turn completes.
func TestConv_ReentrantTurnFailsInsteadOfDeadlocking(t *testing.T) {
	w := newConvWire(t,
		convWireReply{ToolName: "lookup", ToolArgs: `{"key":"k"}`},
		convWireReply{Text: "outer done"})
	conv := openConvPack(t, w, convToolPack)
	var nestedErr error
	conv.OnToolCtx("lookup", func(ctx context.Context, _ map[string]any) (any, error) {
		_, nestedErr = conv.Send(ctx, "nested")
		return "ok", nil
	})

	resp, err := conv.Send(context.Background(), "outer")
	require.NoError(t, err)
	assert.Equal(t, "outer done", resp.Text())
	assert.ErrorIs(t, nestedErr, ErrReentrantTurn)
}

// A turn waiting for the conversation gives up when its context ends, and a
// conversation closed while a turn runs rejects the turns that were waiting
// rather than running them on a closed conversation.
func TestConv_WaitingTurnHonorsContextAndClose(t *testing.T) {
	w := newConvWire(t,
		convWireReply{ToolName: "lookup", ToolArgs: `{"key":"k"}`},
		convWireReply{Text: "first done"})
	conv := openConvPack(t, w, convToolPack)
	inTool, unblock := make(chan struct{}), make(chan struct{})
	conv.OnTool("lookup", func(map[string]any) (any, error) {
		close(inTool)
		<-unblock
		return "ok", nil
	})

	first := make(chan error, 1)
	go func() {
		_, err := conv.Send(context.Background(), "first")
		first <- err
	}()
	<-inTool

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := conv.Send(cancelled, "gives up")
	assert.ErrorIs(t, err, context.Canceled)

	waiting := make(chan error, 1)
	go func() {
		_, err := conv.Send(context.Background(), "waits")
		waiting <- err
	}()
	closed := make(chan error, 1)
	go func() { closed <- conv.Close() }()
	close(unblock) // Close flushes the running turn's pipeline; let it finish
	require.NoError(t, <-closed)
	<-first
	assert.ErrorIs(t, <-waiting, ErrConversationClosed)
}
