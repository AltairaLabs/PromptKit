package stage

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Execute racing Shutdown must either register before Shutdown waits (and be
// waited for) or be refused. Checking the shutdown flag and adding to the
// WaitGroup separately let an Execute add while Shutdown was already in Wait,
// which sync.WaitGroup forbids; the SDK's Conversation.Close calls Shutdown,
// so a Send racing Close could hit it. Run with -race.
func TestStreamPipeline_ExecuteRacingShutdownIsWaitedForOrRefused(t *testing.T) {
	for range 500 {
		builder := NewPipelineBuilder()
		builder.AddStage(NewPassthroughStage("passthrough"))
		p, err := builder.Build()
		require.NoError(t, err)

		const executions = 4
		results := make(chan error, executions)
		var wg sync.WaitGroup
		for range executions {
			wg.Go(func() {
				input := make(chan StreamElement)
				close(input)
				output, err := p.Execute(context.Background(), input)
				if err == nil {
					for range output { //nolint:revive // drain until the execution finishes
					}
				}
				results <- err
			})
		}
		shutdownErr := p.Shutdown(context.Background())
		wg.Wait()
		close(results)

		assert.NoError(t, shutdownErr, "every registered execution finishes, so Shutdown must not time out")
		for err := range results {
			if err != nil {
				assert.ErrorIs(t, err, ErrPipelineShuttingDown)
			}
		}
	}
}
