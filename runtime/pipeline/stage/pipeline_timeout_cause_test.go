package stage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecuteSync_ExecutionTimeoutReportedOverEarlierDeadlineError: an earlier
// non-fatal error that wraps context.DeadlineExceeded (a tool timeout, say)
// must not stand in for the turn's execution-timeout failure.
func TestExecuteSync_ExecutionTimeoutReportedOverEarlierDeadlineError(t *testing.T) {
	config := DefaultPipelineConfig()
	config.ExecutionTimeout = 5 * time.Millisecond
	config.IdleTimeout = 0
	stage := NewStageFunc("slow", StageTypeTransform,
		func(ctx context.Context, in <-chan StreamElement, out chan<- StreamElement) error {
			defer close(out)
			for range in { //nolint:revive // drain
			}
			out <- NewErrorElement(fmt.Errorf("tool lookup timed out: %w", context.DeadlineExceeded))
			<-ctx.Done()
			return ctx.Err()
		})
	p, err := NewPipelineBuilderWithConfig(config).Chain(stage).Build()
	require.NoError(t, err)

	_, execErr := p.ExecuteSync(context.Background(), userElement("hi"))

	require.ErrorIs(t, execErr, ErrExecutionTimeout)
	assert.ErrorIs(t, execErr, context.DeadlineExceeded)
	var stageErr *StageError
	require.ErrorAs(t, execErr, &stageErr, "the reported error is the stage's timeout failure")
	assert.Equal(t, "slow", stageErr.StageName)
}
