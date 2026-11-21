package agui

import (
	"errors"
	"strings"

	aguievents "github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// errStreamIncomplete is returned when a turn's stream ends without its
// response or an error.
var errStreamIncomplete = errors.New("agui: the turn's stream ended without a response")

// liveRound is one round of a streamed turn, as the run has emitted it so far:
// its text message, its tool calls and the results answering them.
type liveRound struct {
	msgID    string
	round    int             // the stream's round number (sdk.StreamChunk.Round), once its calls arrive
	text     strings.Builder // the text streamed into the open or last text message
	textOpen bool            // a text message is open
	texted   bool            // a text message was emitted for the round
	called   bool            // the round's tool calls were emitted
	resulted bool            // a result of the round was emitted
	answered map[string]bool // the round's calls that have their result

	reasoningSpanID string // the open or last reasoning span
	reasoningMsgID  string // the reasoning message inside it
	reasoningOpen   bool   // a reasoning span is open
	reasoned        bool   // reasoning was emitted for the round
}

// consume emits a streamed turn as its chunks arrive (text, each round's tool
// calls and results) and returns the turn's response. The channel is read to
// its end, which releases the conversation's turn.
func (st *runState) consume(chunks <-chan sdk.StreamChunk) (*sdk.Response, error) {
	var resp *sdk.Response
	var err error
	for chunk := range chunks {
		if err != nil {
			continue
		}
		switch {
		case chunk.Error != nil:
			err = chunk.Error
			// The partial reply of a stream that failed part-way carries the
			// usage of the calls the model made before it failed.
			st.usage.addResponse(chunk.Message)
		case chunk.Type == sdk.ChunkReasoning:
			st.liveReasoning(chunk.Reasoning)
		case chunk.Type == sdk.ChunkText:
			st.liveText(chunk.Text, chunk.ReplacesText)
		case chunk.Type == sdk.ChunkToolCall && chunk.ToolCall != nil:
			st.liveCall(chunk.ToolCall, chunk.Round)
		case chunk.Type == sdk.ChunkToolResult && chunk.ToolResult != nil:
			st.liveResult(chunk.ToolResult)
		case chunk.Type == sdk.ChunkDone:
			resp = chunk.Message
		}
	}
	switch {
	case err != nil:
		return nil, err
	case resp != nil:
		return resp, nil
	case st.ctx.Err() != nil:
		return nil, st.ctx.Err()
	default:
		return nil, errStreamIncomplete
	}
}

// newRound starts the turn's next round, whose results are deduplicated on
// their own.
func (st *runState) newRound() *liveRound {
	r := &liveRound{msgID: aguievents.GenerateMessageID(), answered: map[string]bool{}}
	st.rounds = append(st.rounds, r)
	st.answered = r.answered
	return r
}

func (st *runState) lastRound() *liveRound {
	if len(st.rounds) == 0 {
		return nil
	}
	return st.rounds[len(st.rounds)-1]
}

// liveText emits a text delta. Text after a round's tool calls is the next
// round's. A delta that replaces the text streamed before it (an output
// guardrail cut the reply) closes the streamed message and starts a new one,
// since AG-UI cannot take back text the client has shown.
func (st *runState) liveText(delta string, replaces bool) {
	if delta == "" {
		return
	}
	r := st.lastRound()
	if r == nil || r.called {
		r = st.newRound()
	}
	st.closeReasoning(r)
	if replaces && r.texted {
		st.closeText(r)
		r.msgID = aguievents.GenerateMessageID()
		r.text.Reset()
	}
	if !r.textOpen {
		st.emit(aguievents.NewTextMessageStartEvent(r.msgID, aguievents.WithRole(roleAssistant)))
		r.textOpen, r.texted = true, true
	}
	st.emit(aguievents.NewTextMessageContentEvent(r.msgID, delta))
	r.text.WriteString(delta)
}

// liveCall emits a tool call once its round's reply has streamed. A call of
// another round than the current round's calls starts the next round, whether
// or not results came between them; so does a call after the round's results.
func (st *runState) liveCall(tc *types.MessageToolCall, round int) {
	r := st.lastRound()
	if r == nil || (r.called && (r.round != round || r.resulted)) {
		r = st.newRound()
	}
	st.closeReasoning(r)
	st.closeText(r)
	st.emitCall(r.msgID, tc)
	r.called, r.round = true, round
}

// liveResult emits the result of one of the current round's calls.
func (st *runState) liveResult(res *types.MessageToolResult) {
	r := st.lastRound()
	if r == nil {
		return
	}
	r.resulted = true
	st.emitResult(res.ID, toolResultText(res))
}

// settleText completes a round's text from final, its message's text: it
// sends what has not streamed and closes the message. A round that streamed
// none sends final whole, and so does one with no tool calls (noCalls) and no
// text, as an empty message. A round whose text ended differently from what
// streamed sends final as a new message.
func (st *runState) settleText(r *liveRound, final string, noCalls bool) {
	streamed := r.text.String()
	if r.textOpen && strings.HasPrefix(final, streamed) {
		if rest := final[len(streamed):]; rest != "" {
			st.emit(aguievents.NewTextMessageContentEvent(r.msgID, rest))
			r.text.WriteString(rest)
		}
		st.closeText(r)
		return
	}
	st.closeText(r)
	switch {
	case r.texted && final == streamed:
	case r.texted:
		if final != "" {
			st.emitText(aguievents.GenerateMessageID(), final)
		}
	case final != "" || (noCalls && !r.called):
		st.emitText(r.msgID, final)
		r.texted = true
	}
}

// emitText emits a whole text message.
func (st *runState) emitText(msgID, text string) {
	st.emit(aguievents.NewTextMessageStartEvent(msgID, aguievents.WithRole(roleAssistant)))
	if text != "" {
		st.emit(aguievents.NewTextMessageContentEvent(msgID, text))
	}
	st.emit(aguievents.NewTextMessageEndEvent(msgID))
}

// closeText ends a round's open text message.
func (st *runState) closeText(r *liveRound) {
	if !r.textOpen {
		return
	}
	st.emit(aguievents.NewTextMessageEndEvent(r.msgID))
	r.textOpen = false
}

// emitCall emits one tool call, naming parent as its message.
func (st *runState) emitCall(parent string, tc *types.MessageToolCall) {
	st.emit(aguievents.NewToolCallStartEvent(tc.ID, tc.Name, aguievents.WithParentMessageID(parent)))
	if len(tc.Args) > 0 {
		st.emit(aguievents.NewToolCallArgsEvent(tc.ID, string(tc.Args)))
	}
	st.emit(aguievents.NewToolCallEndEvent(tc.ID))
}
