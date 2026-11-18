package stage

import (
	"context"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The input guardrails judge the caller's spoken turn once its transcript
// arrives, which on OpenAI Realtime is usually after the reply to it began
// streaming. A guardrail can take a while (an LLM judge is a model round
// trip), and the response loop judging it in line stopped forwarding the
// reply meanwhile: the caller heard the reply stall mid-sentence, and its
// end, the cut and the spoken message all waited behind the judge.
//
// So a verdict that is not in within defaultInputVerdictInline is left to
// arrive while the reply goes on streaming, and is acted on when it does: a
// block then cuts the reply, as a block in line would have. The loop waits
// for the verdicts still out only where the turn's outcome needs them: before
// the first output of a reply, so a blocked turn's reply is never heard, and
// before the reply's turn ends, so its Message carries the verdict. A verdict
// fast enough (any guardrail that runs in the process) is acted on in line,
// exactly as before.

// defaultInputVerdictInline is how long the response loop waits in line for
// the input guardrails' verdict on a spoken turn before it lets the reply go
// on while the verdict is out.
const defaultInputVerdictInline = 200 * time.Millisecond

// spokenVerdict is the input guardrails' verdict on a caller's spoken turn.
// The goroutine judging it sets d and dur, then closes done.
type spokenVerdict struct {
	req     *hooks.ProviderRequest
	inputID string
	done    chan struct{}
	d       hooks.Decision
	dur     time.Duration
}

// judgeSpokenInput runs the input hooks on the caller's spoken turn, whose
// reply has not completed (guardSpokenInput, without replyDone): messages is
// the conversation up to and including it, and inputID the session's ID for
// it. A verdict in within inputVerdictInline is acted on at once; a later one is
// acted on when it arrives (nextVerdict), the reply streaming meanwhile.
// Response goroutine only.
func (s *DuplexProviderStage) judgeSpokenInput(ctx context.Context, messages []types.Message, inputID string) {
	if !s.hooksActive() {
		return
	}
	v := &spokenVerdict{req: s.hookRequest(messages), inputID: inputID, done: make(chan struct{})}
	go func() {
		defer close(v.done)
		defer keepIdleAlive(ctx)()
		start := time.Now()
		v.d = s.hookRegistry.RunBeforeProviderCall(ctx, v.req)
		v.dur = time.Since(start)
	}()
	inline := time.NewTimer(s.inputVerdictInline)
	defer inline.Stop()
	select {
	case <-v.done:
		s.applySpokenInputDecision(v.req, v.d, v.dur, false, v.inputID)
		return
	case <-inline.C:
	case <-ctx.Done():
	}
	logger.Debug("DuplexProviderStage: the reply goes on while the caller's input is judged")
	s.pendingVerdicts = append(s.pendingVerdicts, v)
}

// nextVerdict is closed once the oldest verdict still out is in; nil (never
// ready) when none is out.
func (s *DuplexProviderStage) nextVerdict() <-chan struct{} {
	if len(s.pendingVerdicts) == 0 {
		return nil
	}
	return s.pendingVerdicts[0].done
}

// takeVerdict acts on the oldest verdict still out, which is in. Response
// goroutine only.
func (s *DuplexProviderStage) takeVerdict() {
	v := s.pendingVerdicts[0]
	s.pendingVerdicts = s.pendingVerdicts[1:]
	s.applySpokenInputDecision(v.req, v.d, v.dur, false, v.inputID)
}

// onVerdict acts on a verdict that came in while the reply streamed: a block
// cuts the reply now (cutReply).
func (s *DuplexProviderStage) onVerdict(ctx context.Context, output chan<- StreamElement) error {
	defer s.syncReplyStreaming()
	s.takeVerdict()
	return s.cutReply(ctx, output)
}

// awaitVerdicts waits for every verdict still out and acts on each, in the
// order the turns were spoken. It returns ctx's error when ctx ends first.
func (s *DuplexProviderStage) awaitVerdicts(ctx context.Context) error {
	for len(s.pendingVerdicts) > 0 {
		select {
		case <-s.pendingVerdicts[0].done:
			s.takeVerdict()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// awaitVerdictsFor waits for the verdicts still out (awaitVerdicts) when the
// chunk needs them: it begins a reply (nothing of the turn streamed yet), or
// cuts one short (an interruption). A turn's completion waits for them once
// its own user turn is out (handleResponseChunk).
func (s *DuplexProviderStage) awaitVerdictsFor(
	ctx context.Context, chunk *providers.StreamChunk, isOutputTranscription bool,
) error {
	if len(s.pendingVerdicts) == 0 {
		return nil
	}
	begins := isAssistantContentChunk(chunk, isOutputTranscription) && !s.assistantTurnInProgress()
	if !chunk.Interrupted && !begins {
		return nil
	}
	return s.awaitVerdicts(ctx)
}
