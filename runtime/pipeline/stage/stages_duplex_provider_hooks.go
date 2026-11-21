package stage

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// This file holds the duplex provider stage's guardrail (provider hook)
// support. The phases match ProviderStage's, adapted to a session that streams
// instead of making one call per round:
//
//   - Input (BeforeCall): text bound for the session is checked before it is
//     sent. An enforcing hook keeps it from the session and the blocked turn is
//     emitted in its place. Audio cannot be checked before it is sent: its
//     transcript only comes back from the provider afterwards.
//   - Streaming (OnChunk): each chunk of assistant text, whether model text or
//     the transcript of spoken audio, is checked against the turn's text so far
//     before it is forwarded. An enforcing interceptor replaces the turn's text
//     and the rest of the turn's text and audio is dropped, the realtime
//     equivalent of ProviderStage no longer reading the stream.
//   - Turn completion (AfterCall): the completed assistant Message is checked.
//     An enforcing hook rewrites it, as in text.
//
//   - Spoken input (BeforeCall, after the fact): the caller's transcript is
//     checked once it is materialized as their user turn. The model heard the
//     audio before the transcript existed, so a block can only stop the reply
//     to it: that reply becomes the blocked turn. A session that can is then
//     asked to take the turn and that reply out of the model's conversation
//     (providers.TurnForgetter); one that cannot (Gemini Live) remembers it.
//     A slow check does not hold the reply back (stages_duplex_input_verdict.go).
//
// A turn is cut once and its guardrail message shown and spoken once, however
// many guardrails fire on it: a later block only adds its firing and marks,
// and has the input forgotten.
//
// An enforced turn is marked types.FinishReasonSafety and its tool calls are
// dropped, so they are not executed. A hook that denies without enforcing
// blocks the turn the same way, with the default blocked message: the turn
// ends, not the session, whether it denied typed text, spoken input or the
// reply.
//
// A blocked reply is cut (cutReply): the provider is asked to stop generating
// it and to cut its own copy to the audio the caller heard
// (providers.ResponseInterrupter), and an Interrupt element tells the caller
// to stop playing what it buffered, as a barge-in does. The blocked turn's
// text then goes out as live output, as a blocked typed turn's does, carrying
// a cue (StreamElement.Cue) that has the cue stage after this stage speak it,
// so the caller hears why the reply stopped. The model never says it: the
// message is not model output, the output guardrails never see it, and it
// never enters the model's conversation. A chain with no cue stage shows the
// text only. Whatever the caller heard before the check fired cannot be
// unsaid.

// SetHookRegistry installs the provider hooks (guardrails) the stage runs. A
// nil registry, the default, runs none. The registry is handed the stage's
// emitter, as ProviderStage does, so its guardrails report through it. Must be
// called before the stage runs.
func (s *DuplexProviderStage) SetHookRegistry(r *hooks.Registry) {
	r.SetEmitter(s.emitter)
	s.hookRegistry = r
}

// SetPromptDeclarations sets the pack declarations of the prompts, keyed by
// task, so each hook call carries the declaration of the prompt the session
// runs (ProviderRequest.Prompt), as ProviderStage's do through
// ProviderConfig.PromptDeclarations. Must be called before the stage runs.
func (s *DuplexProviderStage) SetPromptDeclarations(decls map[string]*packspec.Prompt) {
	s.promptDeclarations = decls
}

// SetCaller sets who the conversation acts for, so each provider hook call
// and chunk interceptor call carries it (ProviderRequest.Caller,
// ChunkRequest.Caller), as ProviderStage's do through ProviderConfig.Caller.
// Must be called before the stage runs.
func (s *DuplexProviderStage) SetCaller(caller types.Caller) {
	s.caller = caller
}

// SetBlockedMessage sets the function that chooses the text a guardrail's
// block replaces a turn with, as ProviderConfig.BlockedMessage does for
// ProviderStage, so a blocked turn reads and sounds the same on both paths.
// The text it returns is shown, stored and spoken by the cue stage. Must be
// called before the stage runs.
func (s *DuplexProviderStage) SetBlockedMessage(fn BlockedMessageFunc) {
	s.blockedMessage = fn
}

// duplexTurnGuard is the response loop's guardrail state for the current
// assistant turn.
type duplexTurnGuard struct {
	// blocked is set once a chunk interceptor enforced: the turn's remaining
	// text and audio are dropped.
	blocked bool
	// validations are the chunk-interceptor firings, folded into the turn's
	// Message when it is built.
	validations []types.ValidationResult
	// blockedText, when set, is the blocked turn's text in place of the reply:
	// the blocked message for a deny or a blocked spoken input. blockedMeta is
	// the decision's metadata for its Message.
	blockedText *string
	blockedMeta map[string]interface{}
	// replaced is set when a block replaced the turn's text with a guardrail's
	// own (its message, or an enforcing check's replacement): the text is not
	// the model's, so the completed turn is not judged by the output
	// guardrails again. A message that names what it blocks ("We don't talk
	// about Bruno.") would otherwise block its own turn a second time.
	replaced bool
	// spoken is set once a block has shown and spoken a guardrail's message
	// for the turn: a turn gets one, however many guardrails fire on it.
	spoken bool
	// passed is the reply text the completed-reply check passed before the
	// turn completed (onGenerationComplete): a turn ending with that text and
	// no tool calls is not judged again.
	passed *string
}

// passedAsIs reports whether msg is the reply the completed-reply check
// already passed.
func (g *duplexTurnGuard) passedAsIs(msg *types.Message) bool {
	return g.passed != nil && *g.passed == msg.Content && len(msg.ToolCalls) == 0
}

// duplexHistory is the conversation the session has seen, which the hooks'
// ProviderRequest carries. Both the input and the response goroutines add to
// it.
type duplexHistory struct {
	mu   sync.Mutex
	msgs []types.Message
	// src is the message each entry of msgs was copied from, so an entry can
	// be taken out while its message is held (remove).
	src []*types.Message
}

// add keeps a text-only copy of m: the guardrails judge text, and a realtime
// turn's audio part would otherwise hold every turn's audio for the life of
// the session.
func (h *duplexHistory) add(m *types.Message) {
	c := *m
	c.Parts = textOnlyParts(m.Parts)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.msgs = append(h.msgs, c)
	h.src = append(h.src, m)
}

// remove takes out the entry copied from m, the latest one, and reports
// whether there was one.
func (h *duplexHistory) remove(m *types.Message) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.src) - 1; i >= 0; i-- {
		if h.src[i] == m {
			h.msgs = append(h.msgs[:i], h.msgs[i+1:]...)
			h.src = append(h.src[:i], h.src[i+1:]...)
			return true
		}
	}
	return false
}

// fillUserTranscript gives the latest user turn that has no text yet its
// transcript. On the scenario path the user Message is forwarded before its
// audio is transcribed, and the transcript arrives on the turn's completion.
func (h *duplexHistory) fillUserTranscript(text string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.msgs) - 1; i >= 0; i-- {
		if h.msgs[i].Role != roleUser {
			continue
		}
		if h.msgs[i].GetContent() == "" {
			h.msgs[i].Content = text
		}
		return
	}
}

// upTo returns the history's messages up to and including position i
// (clamped to the history).
func (h *duplexHistory) upTo(i int) []types.Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	i = max(0, min(i+1, len(h.msgs)))
	return append([]types.Message(nil), h.msgs[:i]...)
}

// length is the number of messages in the history.
func (h *duplexHistory) length() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.msgs)
}

// insert adds a text-only copy of m at position i (clamped to the history),
// ahead of the messages already there from i on.
func (h *duplexHistory) insert(i int, m *types.Message) {
	c := *m
	c.Parts = textOnlyParts(m.Parts)
	h.mu.Lock()
	defer h.mu.Unlock()
	i = max(0, min(i, len(h.msgs)))
	h.msgs = append(h.msgs[:i], append([]types.Message{c}, h.msgs[i:]...)...)
	h.src = append(h.src[:i], append([]*types.Message{m}, h.src[i:]...)...)
}

// textOnlyParts returns the text parts of parts.
func textOnlyParts(parts []types.ContentPart) []types.ContentPart {
	var out []types.ContentPart
	for _, p := range parts {
		if p.Type == types.ContentTypeText {
			out = append(out, p)
		}
	}
	return out
}

func (h *duplexHistory) snapshot() []types.Message {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]types.Message(nil), h.msgs...)
}

// hooksActive reports whether the stage runs provider hooks.
func (s *DuplexProviderStage) hooksActive() bool {
	return s.hookRegistry.HasProviderHooks()
}

// recordHistory adds m to the history the hooks see, when hooks run.
func (s *DuplexProviderStage) recordHistory(m *types.Message) {
	if s.hooksActive() {
		s.history.add(m)
	}
}

// hookTurnIndex is the turn index the hooks and their events report: the
// caller turn the reply in progress answers, or, between replies, the newest
// caller turn (callerTurns.current).
func (s *DuplexProviderStage) hookTurnIndex() int {
	return s.callers.current()
}

// completeAssistantTurn counts a completed assistant turn in the session's
// turns (TurnState.SessionTurns, which counts replies).
func (s *DuplexProviderStage) completeAssistantTurn() {
	s.turnState.completeSessionTurn()
}

// completeBlockedTurn counts an emitted blocked turn as a completed assistant
// turn.
func (s *DuplexProviderStage) completeBlockedTurn() {
	s.turnState.completeSessionTurn()
}

// beginReplyTurn places the reply chunk belongs to against the caller's
// turns (callerTurns.replyBegins), with the caller input the provider says it
// answers. Response goroutine only.
func (s *DuplexProviderStage) beginReplyTurn(chunk *providers.StreamChunk) {
	input, _ := chunk.Metadata[providers.ReplyToInputKey].(string)
	s.callers.replyBegins(input)
}

// stampCallerTurn marks the element that ends a reply with the caller turn the
// reply answers (ElementMetadata.CallerTurn), whether the turn's transcript is
// still to come (AwaitsTranscript), and ends the reply. An element already
// marked (a blocked turn's, with its own input's turn) is left as it is.
func (s *DuplexProviderStage) stampCallerTurn(elem *StreamElement) {
	if elem.Meta.CallerTurn == nil {
		if !endsReply(elem) {
			return
		}
		turn := s.callers.replyBegins("")
		s.callers.replyEnds()
		index := turn.index
		elem.Meta.CallerTurn = &index
		elem.Meta.AwaitsTranscript = turn.heard && !turn.transcribed
		s.turnState.setCallerTurn(index)
	}
	storeCallerTurn(elem.Message, *elem.Meta.CallerTurn)
}

// storeCallerTurn records caller turn index on reply m (types.MetaCallerTurn),
// for the transcript, on a Meta of its own. Nil-safe.
func storeCallerTurn(m *types.Message, index int) {
	if m == nil || m.Role != roleAssistant {
		return
	}
	meta := make(map[string]interface{}, len(m.Meta)+1)
	for k, v := range m.Meta {
		meta[k] = v
	}
	meta[types.MetaCallerTurn] = index
	m.Meta = meta
}

// endsReply reports whether elem ends a reply: an assistant Message on a turn
// end, or on the element of a reply barge-in cut short.
func endsReply(elem *StreamElement) bool {
	return elem.Message != nil && elem.Message.Role == roleAssistant && (elem.EndOfStream || elem.Meta.Interrupted)
}

// hookRequest builds the ProviderRequest for one hook call.
func (s *DuplexProviderStage) hookRequest(messages []types.Message) *hooks.ProviderRequest {
	req := &hooks.ProviderRequest{
		Messages:  messages,
		TurnIndex: s.hookTurnIndex(),
		Caller:    s.caller,
	}
	if s.provider != nil {
		req.ProviderID = s.provider.ID()
		req.Model = s.provider.Model()
	}
	if s.turnState != nil {
		req.SystemPrompt = s.turnState.SystemPrompt
		if s.turnState.Template != nil {
			req.Prompt = s.promptDeclarations[s.turnState.Template.TaskType]
		}
	}
	return req
}

// guardInputText runs the BeforeCall chain on text bound for the session.
// handled reports that the element must not be sent: a hook blocked it
// (enforcing, or denying, which gives the default blocked message) and the
// blocked turn has been queued for the response loop. err is set only when
// ctx ended first. Tool responses the element
// carries still reach the session; its end-of-input signal does not, since
// nothing was sent for the provider to answer.
func (s *DuplexProviderStage) guardInputText(
	ctx context.Context, elem *StreamElement,
) (handled bool, err error) {
	text := sessionBoundText(elem)
	if !s.hooksActive() || text == "" {
		return false, nil
	}
	defer keepIdleAlive(ctx)()

	// A user Message on the element is already in the history
	// (forwardInputMessages); bare text is the user's turn as sent. Either way
	// the user's turn enters the history when it is sent, blocked or not, so
	// user turns keep the order the user sent them in.
	user := &types.Message{Role: roleUser, Content: text}
	userInHistory := elem.Message != nil && elem.Message.Role == roleUser
	messages := s.history.snapshot()
	if !userInHistory {
		messages = append(messages, *user)
	}

	req := s.hookRequest(messages)
	req.TurnIndex = s.typedTurn
	start := time.Now()
	d := s.hookRegistry.RunBeforeProviderCall(ctx, req)
	if !userInHistory {
		s.history.add(user)
	}
	if d.Allow {
		return false, nil
	}

	// Enforced or denied, the turn is blocked; the session goes on. The text
	// is chosen first: the choice is recorded on the decision.
	msg := s.blockedTurnMessage(ctx, req, &d, hooks.DirectionInput)
	blocked := &duplexBlockedTurn{
		msg:      msg,
		decision: d,
		duration: time.Since(start),
		user:     s.pendingTypedUser,
		turn:     s.typedTurn,
	}
	markBlockedInput(&blocked.msg)
	s.dropTurnID(elem)
	if toolResponses := elem.Meta.ToolResponses; len(toolResponses) > 0 {
		s.sendToolResponses(ctx, toolResponses)
	}
	// The response loop emits it, between assistant turns, so it cannot land
	// inside a reply that is still streaming, and records it as it emits it.
	select {
	case s.blockedTurns <- blocked:
		return true, nil
	case <-ctx.Done():
		// Never emitted, but the guardrail fired: its event is still reported.
		s.recordBlockedFiring(blocked)
		return true, ctx.Err()
	}
}

// duplexBlockedTurn is an input turn a guardrail blocked, queued for the
// response loop. The blocked assistant turn and its firing are recorded only
// when the loop emits it, so the history and turn index follow the order the
// assistant turns reach the consumer.
type duplexBlockedTurn struct {
	msg      types.Message
	decision hooks.Decision
	duration time.Duration
	// user is the typed user turn the blocked turn answers, emitted ahead of
	// it. A userOnly entry carries just that turn: the input was not blocked,
	// and the entry only keeps the user's turn in order between assistant
	// turns.
	user     *types.Message
	userOnly bool
	// turn is the caller turn of the blocked input.
	turn int
}

// recordBlockedFiring records a blocked turn's guardrail firing (its
// validation on the Message, and the validation event) at the current turn
// index.
func (s *DuplexProviderStage) recordBlockedFiring(turn *duplexBlockedTurn) {
	s.guardrailOutcomeAt(turn.turn).record(
		&turn.msg.Validations, turn.decision, hooks.DirectionInput, turn.duration)
}

// dropBlockedTurns empties the queue of blocked turns when the session ends
// without emitting them (canceled, or after an input deny whose error was the
// last output). Each one's firing is still reported: the block happened.
func (s *DuplexProviderStage) dropBlockedTurns() {
	for {
		select {
		case turn := <-s.blockedTurns:
			if turn.userOnly {
				continue
			}
			s.recordBlockedFiring(turn)
			s.completeBlockedTurn()
		default:
			return
		}
	}
}

// pendingBlockedTurns returns the queue of blocked input turns while no
// assistant turn is in progress, and nil (never ready) while one is.
func (s *DuplexProviderStage) pendingBlockedTurns() <-chan *duplexBlockedTurn {
	if s.assistantTurnInProgress() {
		return nil
	}
	return s.blockedTurns
}

// nextBlockedTurn returns a queued blocked turn when one is waiting and no
// assistant turn is in progress, without blocking. The response loop takes it
// before reading the next chunk, so a turn blocked during a reply is emitted
// right after that reply, ahead of the next one.
func (s *DuplexProviderStage) nextBlockedTurn() *duplexBlockedTurn {
	select {
	case turn := <-s.pendingBlockedTurns():
		return turn
	default:
		return nil
	}
}

// emitWaitingBlockedTurns emits the queued blocked turns waiting between
// assistant turns. A canceled context leaves the queue to the loop's
// cancellation path, and after an input deny the error was the last output.
func (s *DuplexProviderStage) emitWaitingBlockedTurns(ctx context.Context, output chan<- StreamElement) error {
	for ctx.Err() == nil {
		turn := s.nextBlockedTurn()
		if turn == nil {
			return nil
		}
		if err := s.emitBlockedTurn(ctx, turn, output); err != nil {
			return err
		}
	}
	return nil
}

// assistantTurnInProgress reports whether the current assistant turn has
// streamed anything yet: text, reasoning, audio or tool calls, or a chunk
// interceptor has blocked it.
func (s *DuplexProviderStage) assistantTurnInProgress() bool {
	return s.accumulatedText.Len() > 0 || s.accumulatedReasoning.Len() > 0 ||
		len(s.accumulatedOpaqueReasoning) > 0 || len(s.accumulatedMedia) > 0 ||
		len(s.accumulatedToolCalls) > 0 || s.turnGuard.blocked
}

// flushBlockedTurns emits the blocked turns still queued when the session
// ends.
func (s *DuplexProviderStage) flushBlockedTurns(ctx context.Context, output chan<- StreamElement) error {
	for {
		select {
		case blocked := <-s.blockedTurns:
			if err := s.emitBlockedTurn(ctx, blocked, output); err != nil {
				return err
			}
		default:
			return nil
		}
	}
}

// sessionBoundText returns the text sendElementContent would send for elem,
// or "" when it sends media or nothing.
func sessionBoundText(elem *StreamElement) string {
	if elem.Text == nil || *elem.Text == "" {
		return ""
	}
	if (elem.Audio != nil && len(elem.Audio.Samples) > 0) ||
		(elem.Video != nil && len(elem.Video.Data) > 0) ||
		(elem.Image != nil && len(elem.Image.Data) > 0) {
		return ""
	}
	return *elem.Text
}

// dropTurnID removes the turn_id an input element queued: its turn was
// answered here, so no provider turn completion will pop it, and leaving it
// would correlate the next transcript with the wrong user turn.
func (s *DuplexProviderStage) dropTurnID(elem *StreamElement) {
	if elem.Meta.TurnID == nil || *elem.Meta.TurnID == "" {
		return
	}
	id := *elem.Meta.TurnID
	s.turnIDMu.Lock()
	defer s.turnIDMu.Unlock()
	for i, queued := range s.turnIDQueue {
		if queued == id {
			s.turnIDQueue = append(s.turnIDQueue[:i], s.turnIDQueue[i+1:]...)
			return
		}
	}
}

// emitBlockedTurn records a blocked turn at the point it is emitted (the
// firing at this turn index, the blocked assistant turn in the history, one
// more completed assistant turn), then emits it the way the stage emits any
// assistant turn: its text as a live text element, then the Message on an
// EndOfStream element carrying the finish reason. The text element carries the
// cue that has the cue stage speak the message in a spoken session
// (guardrailCue); no reply was playing, so no Interrupt goes ahead of it, and
// the blocked input never reached the model.
func (s *DuplexProviderStage) emitBlockedTurn(
	ctx context.Context, turn *duplexBlockedTurn, output chan<- StreamElement,
) error {
	if turn.user != nil {
		if err := s.emitTypedUserTurn(ctx, turn.user, output); err != nil {
			return err
		}
	}
	if turn.userOnly {
		return nil
	}
	blocked := &turn.msg
	s.recordBlockedFiring(turn)
	s.history.add(blocked)
	s.completeBlockedTurn()
	// The blocked message answers the blocked input's turn, not a reply.
	s.callers.answer(turn.turn)
	s.turnState.setCallerTurn(turn.turn)

	fr := blocked.FinishReason
	text := blocked.Content
	textElem := StreamElement{Text: &text, Timestamp: timeNow(), Cue: s.guardrailCue(text)}
	textElem.Meta.FinishReason = &fr
	if err := sendStreamElement(ctx, textElem, output); err != nil {
		return err
	}
	s.takeCarriedCost(blocked)
	end := StreamElement{Message: blocked, EndOfStream: true, Timestamp: timeNow()}
	end.Meta.FinishReason = &fr
	callerTurn := turn.turn
	end.Meta.CallerTurn = &callerTurn
	return s.emit(ctx, end, output)
}

// guardOutputChunk has the output guardrails judge a response chunk's
// assistant text (guardrailOutcome) before it is accumulated or forwarded, and
// drops the chunk's text and audio once the turn is blocked. A firing blocks
// the turn: a truncation keeps what it leaves of the model's text, and any
// other firing (a replacement, a deny, a failed check) replaces the turn's
// text with the guardrail's message.
func (s *DuplexProviderStage) guardOutputChunk(
	ctx context.Context, chunk *providers.StreamChunk, isOutputTranscription bool,
) {
	if s.turnGuard.blocked {
		dropAssistantOutput(chunk, isOutputTranscription)
		return
	}
	text := assistantChunkText(chunk, isOutputTranscription)
	if text == "" || !s.hookRegistry.HasChunkInterceptors() {
		return
	}

	probe := &providers.StreamChunk{Content: s.accumulatedText.String() + text, Delta: text}
	v := s.guardrailOutcomeAt(s.hookTurnIndex()).judgeChunk(ctx, probe)
	if !v.blocks() {
		s.liveHolds = v.holds
		return
	}
	// What the caller has of the turn's text: the held end never went out.
	sent := strings.TrimSuffix(s.accumulatedText.String(), s.liveHeld.pending)
	s.liveHeld.reset()
	for _, f := range v.firings {
		s.noteBlock(f, hooks.DirectionOutput, s.replyGeneration())
	}
	s.turnGuard.validations = append(s.turnGuard.validations, v.validations...)
	if v.replaces() {
		// The guardrail's text is the turn's, whatever a later block adds.
		s.blockTurnWithMessage(types.Message{Content: v.text, Meta: decisionMessageMeta(v.decision)})
	} else {
		// A truncation keeps a prefix of the model's own text: that is the
		// turn's text from here on. It is shown, not spoken, and the
		// completed turn is still judged. The caller already has what was
		// forwarded: only what follows it is sent. Under a hold the caller
		// has only what was released, with its audio: the turn keeps that,
		// and nothing they did not hear is shown.
		kept, held := v.text, s.replyHold != nil
		if heard := s.heardReply(); held && strings.HasPrefix(kept, heard) {
			kept = heard
		}
		s.accumulatedText.Reset()
		s.accumulatedText.WriteString(kept)
		s.blockTurn()
		if !held { // under a hold nothing beyond what was heard is shown
			s.showTruncatedText(v.text, sent)
		}
	}
	dropAssistantOutput(chunk, isOutputTranscription)
}

// showTruncatedText shows the caller a truncated reply's text, kept, given
// the text they already have, sent: what follows it, or kept in place of it
// when kept does not begin with it.
func (s *DuplexProviderStage) showTruncatedText(kept, sent string) {
	if rest, ok := strings.CutPrefix(kept, sent); !ok {
		s.showBlockedText(kept, false, true)
	} else if rest != "" {
		s.showBlockedText(rest, false, false)
	}
}

// blockTurn marks the current reply blocked, so the rest of its text and audio
// is dropped, and has it cut (cutReply) unless it already was.
func (s *DuplexProviderStage) blockTurn() {
	// What a hold still holds of the reply is never heard.
	s.dropReplyHold(false)
	if !s.turnGuard.blocked {
		s.cutReplyOf(s.replyGeneration())
	}
	s.turnGuard.blocked = true
}

// cutReplyOf has the reply numbered gen cut (cutReply).
func (s *DuplexProviderStage) cutReplyOf(gen int64) {
	s.cutPending = true
	s.cutGen = gen
}

// replyGeneration is the generation of the reply a block applies to now: the
// one being accumulated, or whose chunk is being handled, or, before any of
// it has arrived, the one after the latest reply seen. Zero when the provider
// does not number its replies.
func (s *DuplexProviderStage) replyGeneration() int64 {
	switch {
	case s.accGen != 0:
		return s.accGen
	case s.chunkIsReply && s.chunkGen != 0:
		return s.chunkGen
	case s.lastReplyGen == 0:
		return 0
	}
	return s.lastReplyGen + 1
}

// blockTurnWith blocks the current reply for a decision that supplies no text
// for it (a deny, or a blocked spoken input): its text is the blocked message,
// as a blocked typed input's is (blockedTurnMessage). The text is kept on the
// guard, not accumulated: the reply's first chunk may still be to come, and
// starting its generation clears the accumulation (turnText).
//
// A turn whose message a block already showed and spoke keeps it: a turn is
// cut once and gets one message, however many guardrails fire on it (an
// output guardrail on the streamed reply, then an input guardrail on the
// caller's transcript, which on OpenAI Realtime arrives after the reply
// began). Only the later block's marks are added, and the stage's
// BlockedMessageFunc is not asked again: the first firing's text stands.
func (s *DuplexProviderStage) blockTurnWith(
	ctx context.Context, req *hooks.ProviderRequest, d *hooks.Decision, direction string,
) {
	if s.turnGuard.spoken {
		s.turnGuard.blockedMeta = mergeMeta(s.turnGuard.blockedMeta, blockedTurnMessage(req, *d).Meta)
		return
	}
	s.blockTurnWithMessage(s.blockedTurnMessage(ctx, req, d, direction))
}

// blockTurnWithMessage is blockTurnWith with the blocked turn's message. In
// a reply the caller already heard the start of under a hold
// (stages_duplex_reply_hold.go), the turn's text is what was heard followed
// by the message, which then follows the shown text rather than replacing it.
func (s *DuplexProviderStage) blockTurnWithMessage(blocked types.Message) {
	content, live, replaces := heardAndMessage(s.heardReply(), blocked.Content)
	s.turnGuard.blockedText = &content
	// Merged, not replaced: a mark an earlier block on this turn set (a
	// blocked input's) must survive this one.
	s.turnGuard.blockedMeta = mergeMeta(s.turnGuard.blockedMeta, blocked.Meta)
	s.turnGuard.replaced = true
	s.turnGuard.spoken = true
	s.blockTurn()
	s.showBlockedText(live, true, replaces)
}

// blockedTurnMessage is blockedTurnMessage with its text chosen by the
// stage's BlockedMessageFunc for a firing in direction.
func (s *DuplexProviderStage) blockedTurnMessage(
	ctx context.Context, req *hooks.ProviderRequest, d *hooks.Decision, direction string,
) types.Message {
	turn := s.hookTurnIndex()
	if req != nil {
		turn = req.TurnIndex
	}
	return s.guardrailOutcomeAt(turn).blockedMessage(ctx, req, d, direction)
}

// guardrailOutcomeAt is the stage's judge of its guardrails' firings
// (guardrailOutcome) on turn, with the stage's choice of blocked text.
func (s *DuplexProviderStage) guardrailOutcomeAt(turn int) guardrailOutcome {
	return guardrailOutcome{
		registry: s.hookRegistry,
		emitter:  s.emitter,
		caller:   s.caller,
		turn:     turn,
		choose: func(ctx context.Context, d *hooks.Decision, direction, text string) string {
			return s.chooseBlocked(ctx, d, direction, text, turn)
		},
	}
}

// chooseBlocked is the text firing d blocks the turn with: the stage's
// BlockedMessageFunc's choice for text, given at most the cue stage's
// on-demand budget so a slow function cannot stall the session, else text.
func (s *DuplexProviderStage) chooseBlocked(
	ctx context.Context, d *hooks.Decision, direction, text string, turn int,
) string {
	timeout := defaultCueOnDemandBudget
	if s.cues != nil && s.cues.budget > 0 {
		timeout = s.cues.budget
	}
	return chooseBlockedText(ctx, s.blockedMessage, timeout, blockedFiring{
		d: d, direction: direction, text: text, turn: turn, caller: s.caller,
	})
}

// showBlockedText has a blocked turn's text sent as live output by the next
// cutReply, and, with speak, spoken (a cue, guardrailCue): a message that
// replaces the reply is, the rest of a truncated one is not. With replaces the
// text is marked as replacing what the turn already streamed
// (Meta.ReplacesText); without, it follows it.
func (s *DuplexProviderStage) showBlockedText(text string, speak, replaces bool) {
	s.blockedLive = &text
	s.blockedLiveSpoken = speak
	s.blockedLiveReplaces = replaces
}

// cutReply cuts a reply a guardrail blocked, once per block: the session is
// asked to stop it and cut its own copy to what the caller heard, when it can
// (providers.ResponseInterrupter), and an Interrupt element marked safety
// tells the caller to stop playing what it buffered, as a barge-in does. A
// session that fails to stop the reply is logged: the stage drops the rest of
// it anyway. Then the blocked turn's text, when it is new, goes out as a text
// element marked safety, shaped as a blocked typed turn's (emitBlockedTurn),
// carrying the cue that has the cue stage speak it when the block replaced
// the reply (guardrailCue). The clip plays after the Interrupt, so a later
// barge-in cuts it as it cuts a reply.
func (s *DuplexProviderStage) cutReply(ctx context.Context, output chan<- StreamElement) error {
	if s.cutPending {
		s.cutPending = false
		if ri, ok := s.session.(providers.ResponseInterrupter); ok {
			if err := ri.InterruptResponse(ctx, s.cutGen); err != nil {
				logger.Warn("DuplexProviderStage: the provider did not stop the blocked reply", "error", err)
			}
		}
		if err := sendStreamElement(ctx, interruptElement(types.FinishReasonSafety), output); err != nil {
			return err
		}
	}
	s.forgetBlockedTurn(ctx)
	s.sendBlockNotes(ctx)
	text := s.blockedLive
	if text == nil {
		return nil
	}
	s.blockedLive = nil
	fr := types.FinishReasonSafety
	elem := StreamElement{Text: text, Timestamp: timeNow()}
	elem.Meta.FinishReason = &fr
	elem.Meta.ReplacesText = s.blockedLiveReplaces
	if s.blockedLiveSpoken {
		elem.Cue = s.guardrailCue(*text)
	}
	return sendStreamElement(ctx, elem, output)
}

// guardrailCue is the cue that has the cue stage after s speak a guardrail's
// message, or nil when none will: no cue stage follows (NewDuplexCueStage), or
// the session's replies are not spoken. Without it the caller has the text.
func (s *DuplexProviderStage) guardrailCue(text string) *Cue {
	if s.cues == nil || !s.cues.voices(text) {
		return nil
	}
	return &Cue{Kind: CueGuardrailMessage, Text: text}
}

// forgetInputOf has the spoken input an input guardrail blocked forgotten,
// with the reply to it, by the next cutReply (forgetBlockedTurn). The input is
// named by the session's own ID for it (providers.InputItemIDKey); an input
// the session gave no ID is not forgotten.
func (s *DuplexProviderStage) forgetInputOf(inputID string) {
	if inputID == "" {
		return
	}
	s.forgetPending = true
	s.forgetInput = inputID
}

// forgetBlockedTurn asks the session to take the blocked spoken input and the
// reply to it out of the model's conversation (providers.TurnForgetter), once
// per block, after the reply was cut. A session that cannot keeps the turn; one
// that fails to is logged.
func (s *DuplexProviderStage) forgetBlockedTurn(ctx context.Context) {
	if !s.forgetPending {
		return
	}
	s.forgetPending = false
	f, ok := s.session.(providers.TurnForgetter)
	if !ok {
		return
	}
	if err := f.ForgetTurn(ctx, s.forgetInput); err != nil {
		logger.Warn("DuplexProviderStage: the provider did not forget the blocked turn", "error", err)
	}
}

// interruptElement is an Interrupt element carrying why the reply was cut:
// types.FinishReasonSafety for a guardrail, FinishReasonInterrupted for a
// barge-in.
func interruptElement(reason string) StreamElement {
	elem := NewInterruptElement()
	elem.Meta.FinishReason = &reason
	return elem
}

// guardSpokenInput runs the input hooks (BeforeCall) on the caller's spoken
// turn in line, on the scenario path, where the transcript arrives with the
// reply's end: messages is the conversation up to and including it. It runs
// after the fact: the model heard the audio before its transcript existed, so
// a block stops the reply to it (applySpokenInputDecision). The streaming path
// judges off the response loop (judgeSpokenInput).
func (s *DuplexProviderStage) guardSpokenInput(ctx context.Context, messages []types.Message) {
	if !s.hooksActive() {
		return
	}
	defer keepIdleAlive(ctx)()

	req := s.hookRequest(messages)
	start := time.Now()
	d := s.hookRegistry.RunBeforeProviderCall(ctx, req)
	s.applySpokenInputDecision(ctx, req, d, time.Since(start), "", nil)
}

// applySpokenInputDecision acts on the input hooks' decision d on a spoken
// turn, which took dur, and reports whether it blocked the turn. A block
// stops the reply to it (blockTurnWith, the firing recorded on the reply),
// or, for a late transcript whose reply already completed (late.replyDone),
// only cuts what is still playing, unless that reply was cut already. Either
// way the session is asked to forget the input, named by inputID, the
// session's ID for it, and the reply to it (forgetBlockedTurn), so the model
// does not remember what was blocked. Response goroutine only.
func (s *DuplexProviderStage) applySpokenInputDecision(
	ctx context.Context, req *hooks.ProviderRequest, d hooks.Decision, dur time.Duration, inputID string,
	late *lateVerdict,
) bool {
	if d.Allow {
		return false
	}
	g := s.guardrailOutcomeAt(req.TurnIndex)
	noteGen := s.replyGeneration()
	if late != nil && late.replyDone {
		noteGen = late.cutGen
	}
	s.noteBlock(d, hooks.DirectionInput, noteGen)
	s.forgetInputOf(inputID)
	if late != nil && late.replyDone {
		// A reply already cut (a guardrail's cut, or a barge-in) is not cut
		// again: what plays now is its guardrail message, or nothing of it.
		if !late.alreadyCut {
			s.cutReplyOf(late.cutGen)
		}
		g.record(nil, d, hooks.DirectionInput, dur)
		return true
	}
	// The blocked text is chosen before the firing is recorded, which records
	// the choice.
	s.blockTurnWith(ctx, req, &d, hooks.DirectionInput)
	g.record(&s.turnGuard.validations, d, hooks.DirectionInput, dur)
	// The reply that replaces the turn is a blocked input turn's canned reply.
	s.turnGuard.blockedMeta = withBlockedInputMark(s.turnGuard.blockedMeta)
	return true
}

// turnText is the current turn's text: the blocked text of a turn a block
// supplied one for, otherwise what the turn accumulated.
func (s *DuplexProviderStage) turnText() string {
	if t := s.turnGuard.blockedText; t != nil {
		return *t
	}
	return s.accumulatedText.String()
}

// holdLiveText keeps from what a chunk forwards the end of the turn's text the
// streaming guardrails have not cleared (hooks.Decision.Hold): text that could
// be the start of a word they block. It is released when a later chunk clears
// it or the turn ends, and dropped when the turn is blocked or cut short. The
// turn's text (accumulatedText) keeps all of it; only the live text waits. The
// audio of it cannot wait: it has already been generated and plays as it is.
func (s *DuplexProviderStage) holdLiveText(chunk *providers.StreamChunk, isOutputTranscription bool) {
	// A held reply's text is held whole (stages_duplex_reply_hold.go).
	if !s.hookRegistry.HasChunkInterceptors() || s.replyHold != nil {
		return
	}
	if chunk.Interrupted {
		s.liveHeld.reset()
		return
	}
	text := assistantChunkText(chunk, isOutputTranscription)
	s.liveHeld.add(text)
	var out string
	if chunk.FinishReason != nil {
		// The turn has ended, so nothing can complete what is held.
		s.liveHeld.clearAll()
		out = s.liveHeld.release()
		s.liveHeld.reset()
	} else {
		s.liveHeld.clearTails(s.liveHolds)
		out = s.liveHeld.release()
	}
	if isOutputTranscription {
		chunk.Delta = out
		return
	}
	chunk.Content = out
}

// assistantChunkText returns the assistant text a chunk adds to the turn,
// as accumulateResponseChunk reads it.
func assistantChunkText(chunk *providers.StreamChunk, isOutputTranscription bool) string {
	if isOutputTranscription && chunk.Delta != "" {
		return chunk.Delta
	}
	return chunk.Content
}

// dropAssistantOutput removes a chunk's assistant text and audio, leaving
// what it carries for the turn's bookkeeping (finish reason, tool calls, cost,
// transcripts of the user's input).
func dropAssistantOutput(chunk *providers.StreamChunk, isOutputTranscription bool) {
	chunk.Content = ""
	if isOutputTranscription {
		chunk.Delta = ""
	}
	chunk.MediaData = nil
}

// guardCompletedTurn applies the turn's guardrails to the element that
// completes it: on the scenario path the caller's transcript, which arrives
// with the turn, is checked (guardSpokenInput); the firings are folded into
// its Message; a blocked turn is marked enforced; and the AfterCall chain
// runs. A block found only now cuts the reply (cutReply). The turn is counted
// whether or not hooks run: the session's completed turns are also what its
// session evals report.
func (s *DuplexProviderStage) guardCompletedTurn(ctx context.Context, elem *StreamElement) {
	msg := elem.Message
	assistant := msg != nil && msg.Role == roleAssistant
	turn := assistant && s.hooksActive()
	if tr := elem.Meta.Transcription; turn && tr != nil && tr.Text != "" {
		s.history.fillUserTranscript(tr.Text)
		// The scenario path's transcript names no session input to forget.
		s.guardSpokenInput(ctx, s.history.snapshot())
	}
	guard := s.turnGuard
	s.turnGuard = duplexTurnGuard{}
	if !turn {
		if assistant {
			s.completeAssistantTurn()
		}
		return
	}
	msg.Validations = append(msg.Validations, guard.validations...)
	if guard.blocked {
		markBlockedTurn(msg, elem, &guard)
	}
	s.endedTurnCut = guard.blocked
	if !guard.replaced && !guard.passedAsIs(msg) && s.runTurnAfterCallHooks(ctx, msg, elem) {
		if !guard.blocked {
			s.cutReplyOf(s.replyGeneration())
		}
		s.endedTurnCut = true
		message := msg.Content
		content, live, replaces := heardAndMessage(s.heardReply(), message)
		if content != message {
			msg.Content = content
			msg.Parts = replaceTextParts(msg.Parts, content)
		}
		s.showBlockedText(live, true, replaces)
	}
	s.history.add(msg)
	s.completeAssistantTurn()
}

// markBlockedTurn marks a turn a guardrail blocked while it streamed: its text
// is the blocked text, when the block supplied one, and it is marked enforced.
func markBlockedTurn(msg *types.Message, elem *StreamElement, guard *duplexTurnGuard) {
	if guard.blockedText != nil {
		msg.Content = *guard.blockedText
		if guard.blockedMeta != nil {
			msg.Meta = guard.blockedMeta
		}
	}
	markEnforcedTurn(msg, elem)
}

// runTurnAfterCallHooks runs the AfterCall chain on a completed turn,
// mirroring ProviderStage.runAfterCallHooks, and reports whether a hook
// blocked it. An enforcing hook rewrites the turn; one that denies without
// enforcing replaces it with the blocked message. Either way the turn is
// marked enforced, and the session goes on.
func (s *DuplexProviderStage) runTurnAfterCallHooks(
	ctx context.Context, msg *types.Message, elem *StreamElement,
) bool {
	defer keepIdleAlive(ctx)()

	req := s.hookRequest(s.history.snapshot())
	v := s.guardrailOutcomeAt(req.TurnIndex).judgeResponse(ctx, req, msg, 0, msg.LatencyMs, false)
	if !v.blocks() {
		return false
	}
	s.noteBlock(v.decision, hooks.DirectionOutput, s.replyGeneration())
	// An output block replaces the turn's Meta with its own; a turn an input
	// block already replaced stays marked as one.
	blockedInput := isMarkedBlockedInput(msg)
	msg.Validations = append(msg.Validations, v.validations...)
	v.enforce(msg)
	if blockedInput {
		markBlockedInput(msg)
	}
	markEnforcedTurn(msg, elem)
	return true
}

// markEnforcedTurn marks a duplex turn's Message enforced, as
// outputVerdict.enforce marks a text one: tool calls and reasoning dropped,
// finish reason safety. The duplex Message also carries its text as a part and its finish
// reason in Meta and on the element, which are brought in line here: the text
// part follows Content so no reader of Parts sees the original text, while
// audio parts, which are what the session played, are kept.
func markEnforcedTurn(msg *types.Message, elem *StreamElement) {
	msg.ToolCalls = nil
	msg.Reasoning = nil
	msg.FinishReason = types.FinishReasonSafety
	msg.Parts = replaceTextParts(msg.Parts, msg.Content)
	if msg.Meta == nil {
		msg.Meta = map[string]interface{}{}
	}
	msg.Meta[finishReasonMetaKey] = types.FinishReasonSafety
	fr := types.FinishReasonSafety
	elem.Meta.FinishReason = &fr
}

// replaceTextParts swaps the text parts for one part holding content, at the
// position of the first, and keeps every other part in place. With no text
// part and non-empty content, the text part goes first, where
// buildAssistantParts puts it.
func replaceTextParts(parts []types.ContentPart, content string) []types.ContentPart {
	out := make([]types.ContentPart, 0, len(parts)+1)
	replaced := false
	for _, p := range parts {
		if p.Type != types.ContentTypeText {
			out = append(out, p)
			continue
		}
		if !replaced && content != "" {
			text := content
			out = append(out, types.ContentPart{Type: types.ContentTypeText, Text: &text})
		}
		replaced = true
	}
	if !replaced && content != "" {
		text := content
		out = append([]types.ContentPart{{Type: types.ContentTypeText, Text: &text}}, out...)
	}
	return out
}

// SetStreamHold sets how the stage holds a spoken reply for the output
// guardrails whose stream_hold holds it (StreamHoldConfig.Realtime), and how
// segments are sized and judged. Must be called before the stage runs.
func (s *DuplexProviderStage) SetStreamHold(cfg StreamHoldConfig) {
	s.streamHold = cfg
}
