package stage

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/core"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage/internal/history"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The input guardrails judge the caller's spoken turn once its transcript
// arrives, which on OpenAI Realtime is usually after the reply to it began
// streaming, or after it completed. A guardrail can take a while (an LLM judge
// is a model round trip), and the response loop judging it in line stopped
// forwarding the reply meanwhile: the caller heard the reply stall
// mid-sentence, and its end, the cut and the spoken message all waited behind
// the judge.
//
// So the verdict is judged off the response loop and acted on when it
// arrives: a block then cuts the reply, as a block in line would have. While
// a reply is streaming the loop does not wait for it at all; otherwise it
// waits up to defaultInputVerdictInline, so a fast guardrail (any that runs in
// the process) is acted on in line. The loop waits for the verdicts still out
// only where the turn's outcome needs them: before anything of a reply is
// forwarded (its text, audio, reasoning or tool calls), so a blocked turn's
// reply is never heard; when a barge-in cuts a reply short; and before a
// reply's turn ends, so its Message carries the verdict.

// defaultInputVerdictInline is how long the response loop waits in line for
// the input guardrails' verdict on a spoken turn, when no reply is streaming,
// before it goes on while the verdict is out.
const defaultInputVerdictInline = 200 * time.Millisecond

// spokenVerdict is the input guardrails' verdict on a caller's spoken turn.
// The goroutine judging it sets d and dur, then closes done.
type spokenVerdict struct {
	req     *hooks.ProviderRequest
	inputID string
	done    chan struct{}
	d       hooks.Decision
	dur     time.Duration
	// late is set for a transcript that arrived after its turn's reply
	// (emitLateUserTurn).
	late *lateVerdict
}

// lateVerdict is what acting on a late transcript's verdict needs, taken when
// the transcript arrived: its user message, emitted once the verdict is in
// and marked when it blocks; whether the turn's reply completed (not a tool
// turn, which goes on); and that reply's generation and whether it was
// already cut.
type lateVerdict struct {
	user       *types.Message
	replyDone  bool
	cutGen     int64
	alreadyCut bool
}

// judgeSpokenInput runs the input hooks on the caller's spoken turn: messages
// is the conversation up to and including it, inputID the session's ID for
// it, and late set for a transcript that arrived after its turn's reply. A
// verdict in at once (or within inputVerdictInline when no reply is
// streaming) is acted on at once; a later one when it arrives (nextVerdict).
// Response goroutine only.
func (s *DuplexProviderStage) judgeSpokenInput(
	ctx context.Context, messages []types.Message, inputID string, late *lateVerdict, output chan<- StreamElement,
) error {
	if !s.hooksActive() {
		if late != nil {
			return s.emitLateUser(ctx, late.user, output)
		}
		return nil
	}
	req := s.hookRequest(messages)
	// The transcript is judged at the turn of the speech it transcribes, which
	// may no longer be the current one.
	req.TurnIndex = s.callers.transcriptTurn(inputID, late != nil)
	v := &spokenVerdict{req: req, inputID: inputID, done: make(chan struct{}), late: late}
	go func() {
		defer close(v.done)
		defer core.KeepIdleAlive(ctx)()
		start := time.Now()
		v.d = s.hookRegistry.RunBeforeProviderCall(ctx, v.req)
		v.dur = time.Since(start)
	}()
	if !s.replyOut {
		inline := time.NewTimer(s.inputVerdictInline)
		defer inline.Stop()
		select {
		case <-v.done:
			return s.applyVerdict(ctx, v, output)
		case <-inline.C:
		case <-ctx.Done():
		}
	}
	logger.Debug("DuplexProviderStage: the session goes on while the caller's input is judged")
	s.pendingVerdicts = append(s.pendingVerdicts, v)
	return nil
}

// applyVerdict acts on a verdict that is in: a block marks the turn and has
// it cut (cutReply). A late transcript's user message goes out now, marked
// when blocked, ahead of the cut.
func (s *DuplexProviderStage) applyVerdict(ctx context.Context, v *spokenVerdict, output chan<- StreamElement) error {
	if v.late == nil {
		s.applySpokenInputDecision(ctx, v.req, v.d, v.dur, v.inputID, nil)
		return s.cutReply(ctx, output)
	}
	if s.applySpokenInputDecision(ctx, v.req, v.d, v.dur, v.inputID, v.late) {
		history.MarkBlockedInput(v.late.user)
		v.late.user.Meta[history.BlockedReplyBeforeMetaKey] = true
	}
	if err := s.emitLateUser(ctx, v.late.user, output); err != nil {
		return err
	}
	return s.cutReply(ctx, output)
}

// nextVerdict is closed once the oldest verdict still out is in; nil (never
// ready) when none is out.
func (s *DuplexProviderStage) nextVerdict() <-chan struct{} {
	if len(s.pendingVerdicts) == 0 {
		return nil
	}
	return s.pendingVerdicts[0].done
}

// onVerdict acts on the oldest verdict still out, which came in while the
// session went on.
func (s *DuplexProviderStage) onVerdict(ctx context.Context, output chan<- StreamElement) error {
	v := s.pendingVerdicts[0]
	s.pendingVerdicts = s.pendingVerdicts[1:]
	return s.applyVerdict(ctx, v, output)
}

// awaitVerdicts waits for every verdict still out and acts on each, in the
// order the turns were spoken. It returns ctx's error when ctx ends first.
func (s *DuplexProviderStage) awaitVerdicts(ctx context.Context, output chan<- StreamElement) error {
	for len(s.pendingVerdicts) > 0 {
		select {
		case <-s.pendingVerdicts[0].done:
			if err := s.onVerdict(ctx, output); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// replyBearing reports whether a reply's chunk carries anything the caller
// gets: text, audio, an output transcript, reasoning or tool calls.
func replyBearing(chunk *providers.StreamChunk, isOutputTranscription bool) bool {
	return isAssistantContentChunk(chunk, isOutputTranscription) || len(chunk.ToolCalls) > 0 ||
		chunk.Reasoning != "" || len(chunk.OpaqueReasoning) > 0
}

// awaitVerdictsFor waits for the verdicts still out (awaitVerdicts) when the
// chunk needs them: it is the first of a reply to go out, or it cuts a reply
// short (an interruption). A turn's completion waits for them once its own
// user turn is out (settleTurnInput). It then records that the chunk's reply
// is going out.
func (s *DuplexProviderStage) awaitVerdictsFor(
	ctx context.Context, chunk *providers.StreamChunk, isOutputTranscription bool, output chan<- StreamElement,
) error {
	bearing := replyBearing(chunk, isOutputTranscription)
	first := bearing && !s.replyGoingOut(chunk)
	if len(s.pendingVerdicts) > 0 && (chunk.Interrupted || first) {
		if err := s.awaitVerdicts(ctx, output); err != nil {
			return err
		}
	}
	if bearing {
		s.replyOut, s.replyOutGen = true, chunk.Generation
	}
	return nil
}

// replyGoingOut reports whether output of the reply the chunk belongs to has
// gone out already: the reply streaming now, when the chunk names no other
// generation.
func (s *DuplexProviderStage) replyGoingOut(chunk *providers.StreamChunk) bool {
	return s.replyOut && (chunk.Generation == 0 || chunk.Generation == s.replyOutGen)
}

// endReplyOut records that the reply streaming ended (its turn completed, or
// a barge-in cut it short).
func (s *DuplexProviderStage) endReplyOut() {
	s.replyOut, s.replyOutGen = false, 0
}
