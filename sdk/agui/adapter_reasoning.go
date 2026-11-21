package agui

import (
	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A round's reasoning is sent as AG-UI reasoning: a span (REASONING_START /
// REASONING_END) holding one reasoning message (REASONING_MESSAGE_START,
// _CONTENT, _END). Only the reasoning's text is sent. Opaque reasoning
// (signatures, encrypted or redacted blocks) is a token the provider needs
// back, which the conversation keeps in its own history; it is not sent as
// REASONING_ENCRYPTED_VALUE, since the adapter would never read it back.

// liveReasoning sends a delta of the current round's reasoning, opening the
// round's reasoning when none is open. Reasoning after the round's calls is
// the next round's.
func (st *runState) liveReasoning(delta, messageID string) {
	if delta == "" || !st.a.cfg.showReasoning {
		return
	}
	r := st.lastRound()
	if r == nil || r.called {
		r = st.newRound(messageID)
	}
	st.openReasoning(r)
	st.emit(aguievents.NewReasoningMessageContentEvent(r.reasoningMsgID, delta))
}

// settleReasoning sends a round's reasoning whole, from its message, when
// none of it streamed.
func (st *runState) settleReasoning(r *liveRound, trace *types.ReasoningTrace) {
	if r.reasoned || !st.a.cfg.showReasoning || trace == nil || trace.Text == "" {
		return
	}
	st.openReasoning(r)
	st.emit(aguievents.NewReasoningMessageContentEvent(r.reasoningMsgID, trace.Text))
	st.closeReasoning(r)
}

// openReasoning opens a span and its reasoning message for the round, unless
// one is open.
func (st *runState) openReasoning(r *liveRound) {
	if r.reasoningOpen {
		return
	}
	r.reasoningSpanID = aguievents.GenerateMessageID()
	r.reasoningMsgID = aguievents.GenerateMessageID()
	st.emit(aguievents.NewReasoningStartEvent(r.reasoningSpanID))
	st.emit(aguievents.NewReasoningMessageStartEvent(r.reasoningMsgID, string(aguitypes.RoleReasoning)))
	r.reasoningOpen, r.reasoned = true, true
}

// closeReasoning ends the round's open reasoning message and its span.
func (st *runState) closeReasoning(r *liveRound) {
	if !r.reasoningOpen {
		return
	}
	st.emit(aguievents.NewReasoningMessageEndEvent(r.reasoningMsgID))
	st.emit(aguievents.NewReasoningEndEvent(r.reasoningSpanID))
	r.reasoningOpen = false
}
