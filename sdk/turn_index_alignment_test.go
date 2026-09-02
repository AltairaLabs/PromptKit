package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/pipeline/stage"
)

// An eval reports the turn the pipeline derived from the transcript, not the
// middleware's own dispatch count.
//
// The two differ exactly where it matters. The dispatch count is per-process,
// so a conversation resumed with prior history restarts it at 1 while the
// transcript says otherwise — and the transcript is what these events exist to
// be lined up against.
func TestReportedTurn_PrefersThePipelineTurnOverDispatchCount(t *testing.T) {
	ts := stage.NewTurnState()
	ts.SetTurnIndex(9)
	conv := &Conversation{turnState: ts}
	em := &evalMiddleware{conv: conv}

	assert.Equal(t, 9, em.reportedTurn(1),
		"a resumed conversation must report the transcript's turn, not the first dispatch")
}

// With no pipeline turn state there is nothing derived to prefer, so the
// dispatch count stands in.
func TestReportedTurn_FallsBackToDispatchCount(t *testing.T) {
	em := &evalMiddleware{conv: &Conversation{}}
	assert.Equal(t, 3, em.reportedTurn(3))

	em = &evalMiddleware{}
	assert.Equal(t, 2, em.reportedTurn(2), "no conversation at all is still safe")
}

// A turn state that was never populated (no state-store load stage ran) is
// "unknown", not turn zero, so the dispatch count is used rather than reporting
// a turn the pipeline never established.
func TestReportedTurn_UnsetTurnStateFallsBack(t *testing.T) {
	conv := &Conversation{turnState: stage.NewTurnState()}
	em := &evalMiddleware{conv: conv}
	assert.Equal(t, 5, em.reportedTurn(5))
}
