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
// outright, and each run's snapshot replaces the last one's messages in
// place. Each message is keyed by its stable id (types.MetaMessageID), the
// same in every run's snapshot; a message stored before ids were assigned is
// keyed by the thread and its position in the history instead. The messages
// streamed during the run had ids of their own, and the snapshot replaces
// them too.
//
// A history that reads back empty or shorter than the run's own turn is not
// the conversation (a store failed, or does not keep it), and a snapshot of
// it would clear the client: none is sent.
//
// The history keeps no reasoning, so with WithReasoning the snapshot restates
// the reasoning of this run's turns. AG-UI reads a snapshot that carries
// reasoning messages as the complete set of them.
func (st *runState) emitMessagesSnapshot() {
	if !st.a.cfg.messagesSnapshot {
		return
	}
	hr, ok := st.a.sender.(historyReader)
	if !ok {
		return
	}
	history := hr.Messages(st.ctx)
	if len(history) == 0 || len(history) < len(st.turn) {
		return
	}
	reasoning := st.turnReasoning(history)
	msgs := make([]aguitypes.Message, 0, len(history))
	used := make(map[string]bool, len(history))
	for i := range history {
		id := snapshotID(history[i].ID(), st.a.cfg.threadID, i, used)
		if text := reasoning[i]; text != "" {
			msgs = append(msgs, aguitypes.Message{ID: id + reasoningIDSuffix, Role: aguitypes.RoleReasoning, Content: text})
		}
		if m, ok := snapshotMessage(id, &history[i]); ok {
			msgs = append(msgs, m)
		}
	}
	st.emit(aguievents.NewMessagesSnapshotEvent(msgs))
}

// turnReasoning maps history positions to the reasoning of this run's
// assistant messages that led to them. Each of the run's messages, last
// first, is matched to the latest history message it is (same role, text and
// tool calls), searching back from the previous match, so a store that trims,
// summarizes or appends to the history does not move the reasoning onto
// another message. A message with no match is left out.
func (st *runState) turnReasoning(history []types.Message) map[int]string {
	out := map[int]string{}
	if !st.a.cfg.showReasoning {
		return out
	}
	next := len(history) - 1
	for i := len(st.turn) - 1; i >= 0; i-- {
		m := &st.turn[i]
		j := lastMatch(history, next, m)
		if j < 0 {
			continue
		}
		next = j - 1
		if m.Reasoning != nil && m.Reasoning.Text != "" {
			out[j] = m.Reasoning.Text
		}
	}
	return out
}

// lastMatch is the position of the last message at or before from that is
// msg as the history holds it (its role, text and tool calls), or -1.
func lastMatch(history []types.Message, from int, msg *types.Message) int {
	for j := from; j >= 0; j-- {
		h := &history[j]
		if h.Role == msg.Role && h.GetContent() == msg.GetContent() && sameCalls(h.ToolCalls, msg.ToolCalls) {
			return j
		}
	}
	return -1
}

// sameCalls reports whether two messages made the same tool calls.
func sameCalls(a, b []types.MessageToolCall) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Name != b[i].Name {
			return false
		}
	}
	return true
}

// snapshotID is the id a snapshot gives the message at position i: its stable
// id, or, for a message stored before ids were assigned (or one whose id an
// earlier message of the snapshot already took), the thread id and its
// position, as "<thread>-<i>".
func snapshotID(stable, threadID string, i int, used map[string]bool) string {
	id := stable
	if id == "" || used[id] {
		id = threadID + "-" + strconv.Itoa(i)
	}
	used[id] = true
	return id
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
	m := MessageToAGUI(msg) // without a context block RunSend added
	m.ID = id
	return m, true
}
