package stage

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
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
// An enforced turn is marked types.FinishReasonSafety and its tool calls are
// dropped, so they are not executed. A hook that denies without enforcing fails
// the session the way it fails a text turn: an error element, and the stage
// returns the error.
//
// Whatever the provider streamed before a check fired has already reached the
// listener. Enforcement corrects the turn as recorded and stops what is still
// to come; it cannot unsay what was played.

// SetHookRegistry installs the provider hooks (guardrails) the stage runs. A
// nil registry, the default, runs none. The registry is handed the stage's
// emitter, as ProviderStage does, so its guardrails report through it. Must be
// called before the stage runs.
func (s *DuplexProviderStage) SetHookRegistry(r *hooks.Registry) {
	r.SetEmitter(s.emitter)
	s.hookRegistry = r
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
}

// duplexHistory is the conversation the session has seen, which the hooks'
// ProviderRequest carries. Both the input and the response goroutines add to
// it.
type duplexHistory struct {
	mu   sync.Mutex
	msgs []types.Message
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
// pipeline's turn plus the assistant turns this session has completed.
func (s *DuplexProviderStage) hookTurnIndex() int {
	return s.turnState.TurnIndex() + int(s.assistantTurns.Load())
}

// hookRequest builds the ProviderRequest for one hook call.
func (s *DuplexProviderStage) hookRequest(messages []types.Message) *hooks.ProviderRequest {
	req := &hooks.ProviderRequest{
		Messages:  messages,
		TurnIndex: s.hookTurnIndex(),
	}
	if s.provider != nil {
		req.ProviderID = s.provider.ID()
		req.Model = s.provider.Model()
	}
	if s.turnState != nil {
		req.SystemPrompt = s.turnState.SystemPrompt
	}
	return req
}

// guardInputText runs the BeforeCall chain on text bound for the session.
// handled reports that the element must not be sent: an enforcing hook
// blocked it and the blocked turn has been queued for the response loop, or a
// hook denied it and err is the HookDeniedError. Tool responses the element
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
	// (forwardInputMessages); bare text is the user's turn as sent.
	user := &types.Message{Role: roleUser, Content: text}
	userInHistory := elem.Message != nil && elem.Message.Role == roleUser
	messages := s.history.snapshot()
	if !userInHistory {
		messages = append(messages, *user)
	}

	req := s.hookRequest(messages)
	start := time.Now()
	d := s.hookRegistry.RunBeforeProviderCall(ctx, req)
	if !userInHistory {
		s.history.add(user)
	}
	if d.Allow {
		return false, nil
	}
	if !d.Enforced {
		return true, &hooks.HookDeniedError{
			HookName: providerHookName,
			HookType: providerHookTypeBefore,
			Reason:   d.Reason,
			Metadata: d.Metadata,
		}
	}

	blocked := blockedTurnMessage(req, d)
	recordGuardrailFiring(s.emitter, &blocked, d, hooks.DirectionInput, time.Since(start), req.TurnIndex)
	s.dropTurnID(elem)
	s.history.add(&blocked)
	s.assistantTurns.Add(1)
	if toolResponses := elem.Meta.ToolResponses; len(toolResponses) > 0 {
		s.sendToolResponses(ctx, toolResponses)
	}
	// The response loop emits it, between assistant turns, so it cannot land
	// inside a reply that is still streaming.
	select {
	case s.blockedTurns <- &blocked:
		return true, nil
	case <-ctx.Done():
		return true, ctx.Err()
	}
}

// pendingBlockedTurns returns the queue of blocked input turns while no
// assistant turn is in progress, and nil (never ready) while one is.
func (s *DuplexProviderStage) pendingBlockedTurns() <-chan *types.Message {
	if s.accumulatedText.Len() > 0 || len(s.accumulatedMedia) > 0 ||
		len(s.accumulatedToolCalls) > 0 || s.turnGuard.blocked {
		return nil
	}
	return s.blockedTurns
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

// emitBlockedTurn emits a blocked turn the way the stage emits any assistant
// turn: its text as a live text element, then the Message on an EndOfStream
// element carrying the finish reason.
func (s *DuplexProviderStage) emitBlockedTurn(
	ctx context.Context, blocked *types.Message, output chan<- StreamElement,
) error {
	fr := blocked.FinishReason
	text := blocked.Content
	textElem := StreamElement{Text: &text, Timestamp: timeNow()}
	textElem.Meta.FinishReason = &fr
	if err := sendStreamElement(ctx, textElem, output); err != nil {
		return err
	}
	turn := StreamElement{Message: blocked, EndOfStream: true, Timestamp: timeNow()}
	turn.Meta.FinishReason = &fr
	return sendStreamElement(ctx, turn, output)
}

// failInput reports a hook denial on the input side: the error goes
// downstream as an element and the session is closed, so the response loop
// ends and Process returns err.
//
// The error is the session's last output: a consumer stops reading at it, so
// the response loop must not then block emitting a partial reply.
func (s *DuplexProviderStage) failInput(ctx context.Context, err error, output chan<- StreamElement) {
	s.inputFailed.Store(true)
	_ = sendStreamElement(ctx, NewErrorElement(err), output)
	s.closeSessionIfOpen()
}

// guardOutputChunk runs the chunk interceptors on a response chunk's assistant
// text before it is accumulated or forwarded, and drops the chunk's text and
// audio once the turn is blocked. A non-nil error is the ValidationAbortError
// for an interceptor that denied without enforcing.
func (s *DuplexProviderStage) guardOutputChunk(
	ctx context.Context, chunk *providers.StreamChunk, isOutputTranscription bool,
) error {
	if s.turnGuard.blocked {
		dropAssistantOutput(chunk, isOutputTranscription)
		return nil
	}
	text := assistantChunkText(chunk, isOutputTranscription)
	if text == "" || !s.hookRegistry.HasChunkInterceptors() {
		return nil
	}

	probe := &providers.StreamChunk{Content: s.accumulatedText.String() + text, Delta: text}
	d := s.hookRegistry.RunOnChunk(ctx, probe)
	if d.Allow {
		return nil
	}
	recordGuardrailFiring(s.emitter, nil, d, hooks.DirectionOutput, 0, s.hookTurnIndex())
	if v, ok := guardrailValidation(d, hooks.DirectionOutput); ok {
		s.turnGuard.validations = append(s.turnGuard.validations, v)
	}
	if !d.Enforced {
		return &providers.ValidationAbortError{Reason: d.Reason, Chunk: *probe}
	}

	// The interceptor rewrote probe.Content (truncated or replaced): that is
	// the turn's text from here on.
	s.accumulatedText.Reset()
	s.accumulatedText.WriteString(probe.Content)
	s.turnGuard.blocked = true
	dropAssistantOutput(chunk, isOutputTranscription)
	return nil
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

// abortTurn fails the turn for a chunk interceptor that denied: as in text,
// none of the blocked output is kept, the error goes downstream, and the
// stage returns it.
func (s *DuplexProviderStage) abortTurn(ctx context.Context, err error, output chan<- StreamElement) error {
	s.resetTurnAccumulation()
	s.turnGuard = duplexTurnGuard{}
	return s.failTurn(ctx, err, output)
}

// failTurn sends err downstream as an error element and returns it, or the
// context's error when canceled first. The input turns a guardrail blocked
// are emitted first: their user messages were already forwarded, and the
// error is a consumer's last read.
func (s *DuplexProviderStage) failTurn(ctx context.Context, err error, output chan<- StreamElement) error {
	if flushErr := s.flushBlockedTurns(ctx, output); flushErr != nil {
		return flushErr
	}
	if sendErr := sendStreamElement(ctx, NewErrorElement(err), output); sendErr != nil {
		return sendErr
	}
	return err
}

// guardCompletedTurn applies the turn's guardrails to the element that
// completes it: the chunk firings are folded into its Message, a turn a chunk
// interceptor blocked is marked enforced, and the AfterCall chain runs. A
// non-nil error is the HookDeniedError for a hook that denied; the turn's
// Message must then not be emitted.
func (s *DuplexProviderStage) guardCompletedTurn(ctx context.Context, elem *StreamElement) error {
	guard := s.turnGuard
	s.turnGuard = duplexTurnGuard{}

	msg := elem.Message
	if msg == nil || msg.Role != roleAssistant || !s.hooksActive() {
		return nil
	}
	if tr := elem.Meta.Transcription; tr != nil && tr.Text != "" {
		s.history.fillUserTranscript(tr.Text)
	}
	msg.Validations = append(msg.Validations, guard.validations...)
	if guard.blocked {
		markEnforcedTurn(msg, elem)
	}
	if err := s.runTurnAfterCallHooks(ctx, msg, elem); err != nil {
		return err
	}
	s.history.add(msg)
	s.assistantTurns.Add(1)
	return nil
}

// runTurnAfterCallHooks runs the AfterCall chain on a completed turn,
// mirroring ProviderStage.runAfterCallHooks.
func (s *DuplexProviderStage) runTurnAfterCallHooks(
	ctx context.Context, msg *types.Message, elem *StreamElement,
) error {
	defer keepIdleAlive(ctx)()

	req := s.hookRequest(s.history.snapshot())
	resp := &hooks.ProviderResponse{
		ProviderID: req.ProviderID,
		Model:      req.Model,
		Message:    *msg,
		LatencyMs:  msg.LatencyMs,
	}
	resp.Message.Validations = slices.Clone(msg.Validations)
	known := len(msg.Validations)

	start := time.Now()
	d := s.hookRegistry.RunAfterProviderCall(ctx, req, resp)
	if d.Allow {
		return nil
	}
	recordGuardrailFiring(s.emitter, msg, d, hooks.DirectionOutput, time.Since(start), req.TurnIndex)
	if !d.Enforced {
		return &hooks.HookDeniedError{
			HookName: providerHookName,
			HookType: providerHookTypeAfter,
			Reason:   d.Reason,
			Metadata: d.Metadata,
		}
	}
	var dropped []types.MessageToolCall
	applyEnforcedResponse(msg, &dropped, resp, d, known)
	markEnforcedTurn(msg, elem)
	return nil
}

// markEnforcedTurn marks a duplex turn's Message enforced, as
// applyEnforcedResponse marks a text one: tool calls dropped, finish reason
// safety. The duplex Message also carries its text as a part and its finish
// reason in Meta and on the element, which are brought in line here: the text
// part follows Content so no reader of Parts sees the original text, while
// audio parts, which are what the session played, are kept.
func markEnforcedTurn(msg *types.Message, elem *StreamElement) {
	msg.ToolCalls = nil
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
