package sdk

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// shutdownProbe is a pipeline whose Shutdown records how much of its
// context's deadline was left on entry and, when hung, waits for that context
// to end, like a pipeline with an execution that outlives the timeout.
type shutdownProbe struct {
	hung bool
	left chan time.Duration
}

func newShutdownProbe(hung bool) *shutdownProbe {
	return &shutdownProbe{hung: hung, left: make(chan time.Duration, 1)}
}

func (p *shutdownProbe) Shutdown(ctx context.Context) error {
	deadline, _ := ctx.Deadline()
	p.left <- time.Until(deadline)
	if p.hung {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

// TestShutdownPipelines_EachGetsTheWholeTimeout: a pipeline shut down after
// one that used up the timeout must not start with an expired context, which
// abandons its in-flight execution with no grace at all.
func TestShutdownPipelines_EachGetsTheWholeTimeout(t *testing.T) {
	first, second := newShutdownProbe(true), newShutdownProbe(false)

	const timeout = 100 * time.Millisecond
	shutdownPipelines([]*shutdownProbe{first, second}, timeout)

	assert.Greater(t, <-first.left, timeout/2)
	assert.Greater(t, <-second.left, timeout/2,
		"the second pipeline started its shutdown with the timeout already spent")
}

// TestShutdownPipelines_BoundedByOneTimeout: however many pipelines hang,
// Close waits about one timeout, not one per pipeline.
func TestShutdownPipelines_BoundedByOneTimeout(t *testing.T) {
	const timeout = 50 * time.Millisecond
	probes := []*shutdownProbe{newShutdownProbe(true), newShutdownProbe(true), newShutdownProbe(true), newShutdownProbe(true)}

	start := time.Now()
	shutdownPipelines(probes, timeout)

	assert.Less(t, time.Since(start), 3*timeout, "hung pipelines must not add their timeouts up")
}
