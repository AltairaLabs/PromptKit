package handlers

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// TopicPolicyHandler confines a conversation to a declared subject scope,
// decided by a classify.TopicClassifier rather than by the model being
// governed. Declared as a guardrail, it gates input by default and a denied
// turn never reaches the primary provider.
//
// Pack validator (the normal declaration site):
//
//	validators:
//	  - type: topic_policy
//	    message: "I can help with questions about AltairaLabs products."
//	    params:
//	      description: Helps users evaluate and operate AltairaLabs products.
//	      allowed: [Omnia and PromptKit, licensing and support]
//	      disallowed: [politics]
//
// Host side, an ordinary provider file:
//
//	id: topic-control
//	role: inference
//	type: nvidia-topic-control
//	base_url: http://topic-control:8000/v1
//
// Params: description (required), allowed (required, non-empty), disallowed,
// small_talk (allow|deny), examples.{allowed,disallowed}, on_deny
// (block|respond), on_unknown (deny|allow), on_error (deny|allow),
// recent_turns (>= 0), classifier_id.
type TopicPolicyHandler struct {
	// warnedGuardrails remembers which misconfigured guardrails have already
	// been reported, so the unbound-classifier warning is loud once rather than
	// once per turn.
	//
	// The key is the policy digest plus the classifier id, NOT a session id.
	// That is deliberate: the guardrail path's EvalContext carries no SessionID
	// (BuildGuardrailEvalContext does not set one), so a session-keyed tracker
	// degenerates to the single key "" and warns once for the whole process —
	// every later conversation then blocks every turn with nothing in the log.
	// Keying on what the handler actually holds gives one warning per distinct
	// misconfigured guardrail per process, which is the granularity an operator
	// needs: it distinguishes "policy A's classifier is missing" from "policy B's
	// is too". Per-turn visibility is not lost either way — every denied turn
	// still emits a guardrail firing carrying the reason.
	//
	// Bounded by maxWarnedGuardrails; guarded by a mutex rather than sync.Map so
	// the cap-and-clear logic in markWarned is a single atomic check.
	warnedGuardrailsMu sync.Mutex
	warnedGuardrails   map[string]struct{}
}

// maxWarnedGuardrails bounds the memory warnedGuardrails can use.
// TopicPolicyHandler is registered once as a process-wide singleton, so without
// a cap the map would grow by one entry per distinct policy digest for the life
// of the process — a multi-tenant server loading many packs, each with its own
// policy, against a persistently missing classifier. 1024 is comfortably larger
// than the number of distinct topic policies a single process would plausibly
// have loaded at once; when the cap is hit the tracker is cleared before the new
// key is inserted (see markWarned), so the worst case is an occasional repeat
// warning for a guardrail already reported — never unbounded growth.
const maxWarnedGuardrails = 1024

// Compile-time checks.
var (
	_ evals.EvalTypeHandler = (*TopicPolicyHandler)(nil)
	_ evals.ParamValidator  = (*TopicPolicyHandler)(nil)
)

// topicPolicyType is the eval type identifier, shared between Type() and the
// result builder so it is written down once.
const topicPolicyType = "topic_policy"

// roleUser is a role literal; runtime/types exports no role constants.
// roleAssistant already exists in helpers.go.
const roleUser = "user"

// Type returns the eval type identifier.
func (h *TopicPolicyHandler) Type() string { return topicPolicyType }

// ValidateParams rejects a malformed policy at pack load rather than letting it
// degrade silently on every turn.
func (h *TopicPolicyHandler) ValidateParams(params map[string]any) error {
	_, err := parseTopicPolicyParams(params)
	return err
}

// Eval classifies the current user message against the policy.
func (h *TopicPolicyHandler) Eval(
	ctx context.Context, evalCtx *evals.EvalContext, params map[string]any,
) (*evals.EvalResult, error) {
	cfg, err := parseTopicPolicyParams(params)
	if err != nil {
		return errorResult(h.Type(), err.Error()), nil
	}

	// A turn with no judgable text — an image-, audio- or video-only user
	// message — is not an in-scope message, it is one this check cannot judge.
	// Sending "" to the classifier asks a meaningless question and gets a
	// meaningless answer (most likely "on-topic"), which would no-op the
	// guardrail on exactly the traffic no text check can see. Route it through
	// on_unknown instead, which defaults to deny.
	message := judgedMessage(evalCtx)
	if strings.TrimSpace(message) == "" {
		return h.outcomeResult(cfg, cfg.onUnknown,
			"turn carried no judgable text (media-only or empty message); topic_policy classifies text", ""), nil
	}

	classifier, err := h.resolveClassifier(ctx, cfg)
	if err != nil {
		return h.outcomeResult(cfg, cfg.onError, err.Error(), ""), nil
	}

	res, err := classifier.ClassifyTopic(ctx, classify.TopicRequest{
		Policy:  cfg.policy,
		History: recentTopicTurns(evalCtx, cfg.recentTurns),
		Message: message,
	})
	if err != nil {
		return h.outcomeResult(cfg, cfg.onError, err.Error(), ""), nil
	}

	switch res.Decision {
	case classify.TopicAllow:
		return h.decisionResult(cfg, classify.TopicAllow, 1.0, res), nil
	case classify.TopicDeny:
		return h.decisionResult(cfg, classify.TopicDeny, 0.0, res), nil
	case classify.TopicUnknown:
		// Handled below, along with any decision value the classifier package
		// hasn't defined yet — the classifier didn't decide, so policy does.
	}
	return h.outcomeResult(cfg, cfg.onUnknown,
		"classifier returned no usable label", res.Raw), nil
}

// resolveClassifier deliberately does NOT return a skipped result when nothing
// is bound — the convention the other classify-backed handlers follow. Skipped
// scores 1.0 and passes, which for a safety control means it silently does not
// run (#1996). Here an unbound classifier is an error and obeys on_error, which
// defaults to deny.
func (h *TopicPolicyHandler) resolveClassifier(
	ctx context.Context, cfg topicPolicyConfig,
) (classify.TopicClassifier, error) {
	reg := classify.FromContext(ctx)
	if reg == nil {
		return nil, h.warnUnbound(cfg, "no classify registry configured")
	}
	classifier, err := reg.TopicClassifier(cfg.classifierID)
	if err != nil {
		return nil, h.warnUnbound(cfg, err.Error())
	}
	return classifier, nil
}

func (h *TopicPolicyHandler) warnUnbound(cfg topicPolicyConfig, reason string) error {
	err := fmt.Errorf(
		"topic_policy: %s; declare a provider with role: inference whose backend implements "+
			"topic classification (e.g. type: nvidia-topic-control), or set classifier_id", reason)

	digest := topicPolicyDigest(cfg.policy)
	if h.markWarned(warnKey(digest, cfg.classifierID)) {
		logger.Warn("topic_policy guardrail has no classifier bound; it is blocking every turn",
			"policy_digest", digest, "classifier_id", cfg.classifierID, "reason", reason)
	}
	return err
}

// warnKey identifies one misconfigured guardrail: the policy it enforces plus
// the classifier it asked for. Two packs with different policies each warn; the
// same pack warning on every turn does not. See TopicPolicyHandler.
func warnKey(policyDigest, classifierID string) string {
	return policyDigest + "\x1f" + classifierID
}

// markWarned records key as having been warned and reports whether this is the
// first time — the caller logs only then, so the warning stays loud once per
// misconfigured guardrail rather than once per turn. Bounded by
// maxWarnedGuardrails: at capacity the tracker is cleared before the new key is
// recorded, so a very long-lived process may warn twice for some guardrail
// rather than grow without bound.
func (h *TopicPolicyHandler) markWarned(key string) bool {
	h.warnedGuardrailsMu.Lock()
	defer h.warnedGuardrailsMu.Unlock()
	if h.warnedGuardrails == nil {
		h.warnedGuardrails = make(map[string]struct{})
	}
	if _, seen := h.warnedGuardrails[key]; seen {
		return false
	}
	if len(h.warnedGuardrails) >= maxWarnedGuardrails {
		h.warnedGuardrails = make(map[string]struct{})
	}
	h.warnedGuardrails[key] = struct{}{}
	return true
}

// Details keys shared between decisionResult and outcomeResult.
const (
	detailDecision     = "decision"
	detailRaw          = "raw"
	detailPolicyDigest = "policy_digest"
)

// decisionResult builds the result for a decision the backend actually made.
func (h *TopicPolicyHandler) decisionResult(
	cfg topicPolicyConfig, decision classify.TopicDecision, score float64, res classify.TopicResult,
) *evals.EvalResult {
	details := map[string]any{
		detailDecision:     string(decision),
		detailRaw:          res.Raw,
		paramClassifierID:  cfg.classifierID,
		paramOnDeny:        cfg.onDeny,
		detailPolicyDigest: topicPolicyDigest(cfg.policy),
	}
	if res.Reason != "" {
		details["reason"] = res.Reason
	}
	// Never manufacture a confidence: a label-emitting model has none, and an
	// absent key is honest where a zero would look like certainty of denial.
	if res.Confidence != nil {
		details["confidence"] = *res.Confidence
	}
	return scoredTopicResult(score, details)
}

// outcomeResult builds the result for unknown or error, where the score comes
// from policy rather than from the classifier. The decision is always
// TopicUnknown: every path here is one where the classifier did not decide, so
// the recorded decision is fixed and only the outcome (deny or allow, chosen by
// on_unknown / on_error) and the reason vary.
func (h *TopicPolicyHandler) outcomeResult(
	cfg topicPolicyConfig, outcome, reason, raw string,
) *evals.EvalResult {
	score := 0.0
	if outcome == outcomeAllow {
		score = 1.0
	}
	return scoredTopicResult(score, map[string]any{
		detailDecision:     string(classify.TopicUnknown),
		"reason":           reason,
		detailRaw:          raw,
		paramClassifierID:  cfg.classifierID,
		"outcome":          outcome,
		detailPolicyDigest: topicPolicyDigest(cfg.policy),
	})
}

func scoredTopicResult(score float64, details map[string]any) *evals.EvalResult {
	s := score
	m := score
	return &evals.EvalResult{
		Type:        topicPolicyType,
		Score:       &s,
		MetricValue: &m,
		Details:     details,
	}
}

// judgedMessage returns the message under judgment. The guardrail adapter puts
// the user's text in CurrentOutput for an input-direction check; the transcript
// fallback covers direct eval invocation.
func judgedMessage(evalCtx *evals.EvalContext) string {
	if evalCtx == nil {
		return ""
	}
	if evalCtx.CurrentOutput != "" {
		return evalCtx.CurrentOutput
	}
	for i := len(evalCtx.Messages) - 1; i >= 0; i-- {
		if evalCtx.Messages[i].Role == roleUser {
			return evalCtx.Messages[i].GetContent()
		}
	}
	return ""
}

// recentTopicTurns flattens the last n conversational turns before the judged
// message. GetContent() rather than .Content: user text can live in Parts while
// assistant text lives on Content.
//
// Filter first, then slice — never the other way round. A transcript carries
// tool-result messages (role "tool") alongside the conversation, so slicing the
// raw tail first lets a single tool-heavy prior turn evict every real turn: four
// tool results with recent_turns: 4 leave the classifier zero history, and an
// anaphoric but perfectly in-scope follow-up ("What about Azure?") is judged bare
// and denied.
func recentTopicTurns(evalCtx *evals.EvalContext, n int) []classify.TopicTurn {
	if evalCtx == nil || n <= 0 || len(evalCtx.Messages) == 0 {
		return nil
	}
	// The judged message is the trailing user turn; history is everything
	// before it.
	history := evalCtx.Messages
	if last := len(history) - 1; last >= 0 && history[last].Role == roleUser {
		history = history[:last]
	}
	out := make([]classify.TopicTurn, 0, len(history))
	for i := range history {
		if !isTopicConversationRole(history[i].Role) {
			continue
		}
		text := history[i].GetContent()
		if text == "" {
			continue
		}
		out = append(out, classify.TopicTurn{Role: history[i].Role, Text: text})
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

func isTopicConversationRole(role string) bool {
	return role == roleUser || role == roleAssistant
}
