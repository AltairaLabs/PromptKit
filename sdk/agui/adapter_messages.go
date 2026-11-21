package agui

import (
	"context"
	"strconv"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	aguitypes "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// historyReader is implemented by conversations that expose their history
// (*sdk.Conversation does).
type historyReader interface {
	Messages(ctx context.Context) []types.Message
}

// reasoningIDSuffix names the reasoning message restated before the
// assistant message it led to.
const reasoningIDSuffix = "-reasoning"

// emitMessagesSnapshot restates the conversation's history as a
// MESSAGES_SNAPSHOT (WithMessagesSnapshot).
//
// AG-UI reconciles a snapshot by message id and drops the messages a client
// holds that it does not name, so the snapshot is the agent's history
// outright. PromptKit's messages carry no id; each is keyed by the thread and
// its position in the history, which a later snapshot of the same thread
// gives it again, so each run's snapshot replaces the last one's messages in
// place. The messages streamed during the run had ids of their own, and the
// snapshot replaces them too.
//
// The history keeps no reasoning, so the snapshot restates the reasoning of
// this run's turns, unless WithReasoning(false) hides it. AG-UI reads a
// snapshot that carries reasoning messages as the complete set of them.
func (st *runState) emitMessagesSnapshot() {
	if !st.a.cfg.messagesSnapshot {
		return
	}
	hr, ok := st.a.sender.(historyReader)
	if !ok {
		return
	}
	history := hr.Messages(st.ctx)
	reasoning := st.turnReasoning(history)
	msgs := make([]aguitypes.Message, 0, len(history))
	for i := range history {
		id := st.a.cfg.threadID + "-" + strconv.Itoa(i)
		if text := reasoning[i]; text != "" {
			msgs = append(msgs, aguitypes.Message{ID: id + reasoningIDSuffix, Role: aguitypes.RoleReasoning, Content: text})
		}
		if m, ok := snapshotMessage(id, &history[i]); ok {
			msgs = append(msgs, m)
		}
	}
	st.emit(aguievents.NewMessagesSnapshotEvent(msgs))
}

// turnReasoning maps the history positions of this run's assistant messages
// to their reasoning text. The run's turns are the tail of the history.
func (st *runState) turnReasoning(history []types.Message) map[int]string {
	out := map[int]string{}
	offset := len(history) - len(st.turn)
	if st.a.cfg.hideReasoning || offset < 0 {
		return out
	}
	for i := range st.turn {
		r := st.turn[i].Reasoning
		if r == nil || r.Text == "" || history[offset+i].Role != st.turn[i].Role {
			continue
		}
		out[offset+i] = r.Text
	}
	return out
}

// snapshotMessage is msg as a snapshot message with id, or false for a
// message AG-UI cannot restate (a tool message with no result). A tool
// result is restated as text, the form AG-UI's Go SDK accepts for it.
func snapshotMessage(id string, msg *types.Message) (aguitypes.Message, bool) {
	if msg.Role == roleTool {
		if msg.ToolResult == nil || msg.ToolResult.ID == "" {
			return aguitypes.Message{}, false
		}
		return aguitypes.Message{
			ID: id, Role: aguitypes.RoleTool, ToolCallID: msg.ToolResult.ID,
			Content: toolResultText(msg.ToolResult), Error: msg.ToolResult.Error,
		}, true
	}
	m := MessageToAGUI(msg)
	m.ID = id
	return m, true
}
