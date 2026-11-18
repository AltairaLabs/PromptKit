package gemini

import "time"

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
// What has to be decided is what a content-free turnComplete read after them
// ends: the tool-call generation (its usage is then reported on its own, for
// that generation, and ends no turn) or the reply to the tool response, which
// can be empty (it ends the turn). It is the tool-call generation's when,
// with no model output since the toolCall:
//
//   - a generationComplete came within toolGenEndWindow of the toolCall, or
//   - it came itself within toolGenEndWindow of the toolCall.
//
// The reply's generationComplete or turnComplete cannot come that soon: the
// server sends them only after it has the tool response and has run the
// model on it. Whether the client has sent the tool response yet does not
// enter into it (a fast tool's response can go out before the client reads
// the tool-call generation's end).
const toolGenEndWindow = 100 * time.Millisecond

// noteToolCall records a toolCall's arrival: a new tool-call generation whose
// end is still to come. Receive goroutine only.
func (s *StreamSession) noteToolCall() {
	s.toolCallTurn = true
	s.toolGenEnding = false
	s.toolCallAt = s.clock()
}

// soonAfterToolCall reports whether now is within toolGenEndWindow of the
// last toolCall.
func (s *StreamSession) soonAfterToolCall() bool {
	return s.clock().Sub(s.toolCallAt) < toolGenEndWindow
}

// noteToolGenerationComplete marks the tool-call generation as ending when a
// generationComplete comes soon after its toolCall, with no model output
// since. Receive goroutine only.
func (s *StreamSession) noteToolGenerationComplete(sc *ServerContent) {
	if s.toolCallTurn && sc != nil && sc.GenerationComplete && !sc.TurnComplete &&
		!hasModelOutput(sc) && s.soonAfterToolCall() {
		s.toolGenEnding = true
	}
}

// endsToolGeneration reports whether a content-free turnComplete read now
// ends the tool-call generation rather than the reply.
func (s *StreamSession) endsToolGeneration() bool {
	return s.toolGenEnding || s.soonAfterToolCall()
}

// hasModelOutput reports whether sc carries the model's output or an
// interruption of it.
func hasModelOutput(sc *ServerContent) bool {
	return sc.ModelTurn != nil || sc.Interrupted ||
		(sc.OutputTranscription != nil && sc.OutputTranscription.Text != "")
}
