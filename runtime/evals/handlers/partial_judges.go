package handlers

import "github.com/AltairaLabs/PromptKit/runtime/v2/evals"

// The handlers that can judge a reply still being written (evals.PartialJudge).
// A topic, policy or safety judge's verdict on what a prefix says holds
// whatever follows, so their output guardrails hold a streamed reply per
// segment by default. A free-form llm_judge can carry criteria a prefix fails
// for being incomplete ("answers the question"), so it holds the whole turn
// unless its guardrail sets stream_hold: segment. Relevancy, completeness,
// session and tool-call judges are not partial judges.
var (
	_ evals.PartialJudge = (*LLMJudgeHandler)(nil)
	_ evals.PartialJudge = (*TopicPolicyHandler)(nil)
	_ evals.PartialJudge = (*PIILeakageHandler)(nil)
	_ evals.PartialJudge = (*ToxicityHandler)(nil)
	_ evals.PartialJudge = (*BiasHandler)(nil)
	_ evals.PartialJudge = (*RoleViolationHandler)(nil)
	_ evals.PartialJudge = (*TextToxicityHandler)(nil)
)

// SegmentHoldByDefault implements evals.PartialJudge: a free-form judge holds
// the whole turn unless told otherwise.
func (h *LLMJudgeHandler) SegmentHoldByDefault() bool { return false }

// SegmentHoldByDefault implements evals.PartialJudge.
func (h *TopicPolicyHandler) SegmentHoldByDefault() bool { return true }

// SegmentHoldByDefault implements evals.PartialJudge.
func (h *PIILeakageHandler) SegmentHoldByDefault() bool { return true }

// SegmentHoldByDefault implements evals.PartialJudge.
func (h *ToxicityHandler) SegmentHoldByDefault() bool { return true }

// SegmentHoldByDefault implements evals.PartialJudge.
func (h *BiasHandler) SegmentHoldByDefault() bool { return true }

// SegmentHoldByDefault implements evals.PartialJudge.
func (h *RoleViolationHandler) SegmentHoldByDefault() bool { return true }

// SegmentHoldByDefault implements evals.PartialJudge.
func (h *TextToxicityHandler) SegmentHoldByDefault() bool { return true }

// partialReplyNote is added to an LLM judge's criteria when the content is a
// reply still being written (evals.EvalContext.Partial).
const partialReplyNote = "The content is a reply that is still being written and may be incomplete: " +
	"judge only what it says, not what it has not said yet."

// withPartialNote tells the judge the content is partial, when it is.
func withPartialNote(opts *JudgeOpts, evalCtx *evals.EvalContext) {
	if evalCtx == nil || !evalCtx.Partial {
		return
	}
	if opts.Criteria == "" {
		opts.Criteria = partialReplyNote
		return
	}
	opts.Criteria += "\n\n" + partialReplyNote
}
