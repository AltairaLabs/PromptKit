package session

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	mock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
)

// Sends that race Close must return nil (the send won) or the session-closed
// error (Close won). They must never panic sending on the closed input
// channel (#2606). Senders and Close start on one barrier so the window
// between a send's closed check and its channel send is hit, and the test runs
// under -race in CI.
func TestDuplexSession_SendsRacingCloseNeverPanic(t *testing.T) {
	const (
		rounds  = 200
		senders = 8
	)
	builder := func(context.Context, providers.Provider, providers.StreamInputSupport,
		*providers.StreamingInputConfig, string, statestore.Store) (*stage.StreamPipeline, error) {
		return stage.NewPipelineBuilder().Chain(&testNoOpStage{}).Build()
	}
	for range rounds {
		sess, err := NewDuplexSession(context.Background(), &DuplexSessionConfig{
			Provider:        mock.NewProvider("mock", "mock-model", false),
			PipelineBuilder: builder,
		})
		require.NoError(t, err)
		resp := sess.Response()
		go func() {
			for range resp { //nolint:revive // drain
			}
		}()
		require.NoError(t, sess.SendText(context.Background(), "start"))

		start := make(chan struct{})
		errs := make(chan error, senders)
		var wg sync.WaitGroup
		for range senders {
			wg.Go(func() {
				<-start
				errs <- sess.SendText(context.Background(), "racing")
			})
		}
		wg.Go(func() {
			<-start
			_ = sess.Close()
		})
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				require.EqualError(t, err, errSessionClosed)
			}
		}
	}
}
