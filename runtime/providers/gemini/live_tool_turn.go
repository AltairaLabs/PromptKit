package gemini

import (
	"slices"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// Gemini Live ends a tool-call generation in one of two ways, told apart by
// the messages that follow the toolCall, never by the model's name:
//
//   - toolCall, generationComplete, then a turnComplete carrying that
//     generation's usage and nothing else (observed live on gemini-3.8-live:
//     all three read within 2ms, before the server has the tool response);
//   - toolCall and nothing more: the reply to the tool response follows, and
//     one turnComplete carries the usage of the whole server turn (observed
//     live on gemini-2.5-flash-native-audio).
//
// The tool calls are sent the moment the toolCall is read, so no tool waits.
// What has to be decided is what a content-free generationComplete or
// turnComplete read after them ends: the tool-call generation (its usage is
// then reported on its own, for that generation, and ends no turn) or the
// reply to the tool response, which can be empty (it ends the turn).
//
// The reply cannot begin before the server has the tool response, so one
// read before the client has sent the tool response for the toolCall is
// always the tool-call generation's: that rule needs no clock. Once the tool
// response is sent (a fast tool's can go out before the client reads the
// tool-call generation's end), a turnComplete is taken for the tool-call
// generation's only when a generationComplete came first, with no model
// output since, read within toolGenEndWindow of sending the response: the
// server cannot have the response, run the model on it and send its
// generationComplete back that soon. Anything else is the reply's end, which
// is a generation of its own.
const toolGenEndWindow = 100 * time.Millisecond

// toolAnswer tracks whether the tool response to the last toolCall has been
// sent. It is written by the receive goroutine (a toolCall) and by the
// caller's (SendToolResponses), so it has a lock of its own.
type toolAnswer struct {
	ids    []string  // the last toolCall's function call IDs
	sentAt time.Time // when a response to one of them was sent; zero: not yet
}

// noteToolCall records a toolCall's arrival: a new tool-call generation whose
// end is still to come, and whose response is still to be sent. Receive
// goroutine only.
func (s *StreamSession) noteToolCall(tc *ToolCallMsg) {
	s.toolCallTurn = true
	s.toolGenEnding = false
	ids := make([]string, len(tc.FunctionCalls))
	for i := range tc.FunctionCalls {
		ids[i] = tc.FunctionCalls[i].ID
	}
	s.toolMu.Lock()
	s.toolAnswer = toolAnswer{ids: ids}
	s.toolMu.Unlock()
}

// noteToolResponse records that responses are being sent; when one answers
// a call of the last toolCall, that toolCall is answered from now (from the
// first such response: the earliest the server could start the reply).
// Called before the response goes out, so the reply can never be read first.
func (s *StreamSession) noteToolResponse(responses []providers.ToolResponse) {
	now := s.clock()
	s.toolMu.Lock()
	defer s.toolMu.Unlock()
	if !s.toolAnswer.sentAt.IsZero() {
		return
	}
	for i := range responses {
		if slices.Contains(s.toolAnswer.ids, responses[i].ToolCallID) {
			s.toolAnswer.sentAt = now
			return
		}
	}
}

// toolResponseSentAt returns when the response to the last toolCall was
// sent, and whether it has been.
func (s *StreamSession) toolResponseSentAt() (time.Time, bool) {
	s.toolMu.Lock()
	defer s.toolMu.Unlock()
	return s.toolAnswer.sentAt, !s.toolAnswer.sentAt.IsZero()
}

// noteToolGenerationComplete marks the tool-call generation as ending when a
// generationComplete is read after its toolCall: before its tool response
// was sent, or within toolGenEndWindow after. Model output, in it or later,
// clears the mark (carryToolCallUsage). Receive goroutine only.
func (s *StreamSession) noteToolGenerationComplete(sc *ServerContent) {
	if !s.toolCallTurn || sc == nil || !sc.GenerationComplete {
		return
	}
	sentAt, sent := s.toolResponseSentAt()
	if !sent || s.clock().Sub(sentAt) < toolGenEndWindow {
		s.toolGenEnding = true
	}
}

// endsToolGeneration reports whether a content-free turnComplete read now
// ends the tool-call generation rather than the reply.
func (s *StreamSession) endsToolGeneration() bool {
	if s.toolGenEnding {
		return true
	}
	_, sent := s.toolResponseSentAt()
	return !sent
}

// hasModelOutput reports whether sc carries the model's output or an
// interruption of it.
func hasModelOutput(sc *ServerContent) bool {
	return sc.ModelTurn != nil || sc.Interrupted ||
		(sc.OutputTranscription != nil && sc.OutputTranscription.Text != "")
}
