package stage

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/duplex"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/persist"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The pipeline's completion totals count a held message once: the release
// repeats it.
func TestCompletionTotals_CountHeldMessageOnce(t *testing.T) {
	msg := &types.Message{Role: core.RoleAssistant, Content: "x",
		CostInfo: &types.CostInfo{InputTokens: 3, OutputTokens: 4, TotalCost: 0.5}}
	held := core.StreamElement{Message: msg, EndOfStream: true}
	held.Meta.PlayoutHeld = true
	release := core.StreamElement{Message: msg}
	release.Meta.PlayoutRelease = true
	totals := &completionTotals{}
	assert.True(t, totals.observe(&held))
	assert.True(t, totals.observe(&release))
	assert.Equal(t, 1, totals.messageCount)
	assert.Equal(t, 3, totals.inputTokens)
	assert.Equal(t, 4, totals.outputTokens)
	assert.InDelta(t, 0.5, totals.totalCost, 1e-9)
}

// eosCounter counts the turn ends and the messages a stage downstream of the
// hold observes.
type eosCounter struct {
	core.BaseStage
	mu       sync.Mutex
	ends     int
	messages []string
}

func (c *eosCounter) Process(ctx context.Context, input <-chan core.StreamElement, output chan<- core.StreamElement) error {
	defer close(output)
	for e := range input {
		c.mu.Lock()
		if e.EndOfStream {
			c.ends++
		}
		if e.Message != nil && !e.Meta.PlayoutHeld {
			c.messages = append(c.messages, e.Message.Content)
		}
		c.mu.Unlock()
		select {
		case output <- e:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Each held reply ends its turn exactly once for every stage after the hold
// (recording, save, the session's turn boundary), whether it played to the
// end or was cut, and its message reaches the transcript once.
func TestPlayoutHold_OneTurnEndPerReply(t *testing.T) {
	fc := &fakeHoldClock{t: holdT0}
	hold := NewPlayoutHoldStage()
	duplex.SetPlayoutClock(hold, core.HoldClock{Now: fc.now, Until: fc.until})
	rec := persist.NewRecordingStage(&fakeEventStore{}, persist.RecordingStageConfig{Position: persist.RecordingPositionOutput})
	counter := &eosCounter{BaseStage: core.NewBaseStage("eos-counter", core.StageTypeTransform)}
	p, err := NewPipelineBuilder().Chain(hold, rec, counter, core.NewPassthroughStage("sink")).Build()
	require.NoError(t, err)
	in := make(chan core.StreamElement, 16)
	out, err := p.Execute(context.Background(), in)
	require.NoError(t, err)
	next := func() core.StreamElement {
		select {
		case e := <-out:
			return e
		case <-time.After(3 * time.Second):
			require.FailNow(t, "no element came out")
			return core.StreamElement{}
		}
	}

	in <- replyAudio(100 * time.Millisecond) // played to the end
	in <- replyEnd("First.")
	next()
	next()
	fc.advance(100 * time.Millisecond)
	next()                        // released
	in <- replyAudio(time.Second) // cut
	in <- replyEnd("Second.")
	next()
	next()
	in <- bargeInElem()
	next()
	next() // released
	close(in)
	for range out { //nolint:revive // drain
	}

	counter.mu.Lock()
	defer counter.mu.Unlock()
	assert.Equal(t, 2, counter.ends, "one turn end per reply")
	assert.Equal(t, []string{"First.", "Second."}, counter.messages, "each reply reaches the transcript once")
}
