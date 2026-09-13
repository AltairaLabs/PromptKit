package handlers

import (
	"context"
	"fmt"
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
	// warnedSessions remembers which conversations have already been told
	// that no classifier is bound. The failure is loud once per conversation
	// rather than once per turn: silent fail-closed is only marginally better
	// than silent fail-open, but a warning on every turn is noise.
	warnedSessions sync.Map
}

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

	classifier, err := h.resolveClassifier(ctx, evalCtx, cfg)
	if err != nil {
		return h.outcomeResult(cfg, classify.TopicUnknown, cfg.onError, err.Error(), ""), nil
	}

	res, err := classifier.ClassifyTopic(ctx, classify.TopicRequest{
		Policy:  cfg.policy,
		History: recentTopicTurns(evalCtx, cfg.recentTurns),
		Message: judgedMessage(evalCtx),
	})
	if err != nil {
		return h.outcomeResult(cfg, classify.TopicUnknown, cfg.onError, err.Error(), ""), nil
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
	return h.outcomeResult(cfg, classify.TopicUnknown, cfg.onUnknown,
		"classifier returned no usable label", res.Raw), nil
}

// resolveClassifier deliberately does NOT return a skipped result when nothing
// is bound — the convention the other classify-backed handlers follow. Skipped
// scores 1.0 and passes, which for a safety control means it silently does not
// run (#1996). Here an unbound classifier is an error and obeys on_error, which
// defaults to deny.
func (h *TopicPolicyHandler) resolveClassifier(
	ctx context.Context, evalCtx *evals.EvalContext, cfg topicPolicyConfig,
) (classify.TopicClassifier, error) {
	reg := classify.FromContext(ctx)
	if reg == nil {
		return nil, h.warnUnbound(evalCtx, "no classify registry configured")
	}
	classifier, err := reg.TopicClassifier(cfg.classifierID)
	if err != nil {
		return nil, h.warnUnbound(evalCtx, err.Error())
	}
	return classifier, nil
}

func (h *TopicPolicyHandler) warnUnbound(evalCtx *evals.EvalContext, reason string) error {
	err := fmt.Errorf(
		"topic_policy: %s; declare a provider with role: inference whose backend implements "+
			"topic classification (e.g. type: nvidia-topic-control), or set classifier_id", reason)

	sessionID := ""
	if evalCtx != nil {
		sessionID = evalCtx.SessionID
	}
	if _, seen := h.warnedSessions.LoadOrStore(sessionID, struct{}{}); !seen {
		logger.Warn("topic_policy guardrail has no classifier bound; it is blocking every turn",
			"session_id", sessionID, "reason", reason)
	}
	return err
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
// from policy rather than from the classifier.
func (h *TopicPolicyHandler) outcomeResult(
	cfg topicPolicyConfig, decision classify.TopicDecision, outcome, reason, raw string,
) *evals.EvalResult {
	score := 0.0
	if outcome == outcomeAllow {
		score = 1.0
	}
	return scoredTopicResult(score, map[string]any{
		detailDecision:     string(decision),
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

// recentTopicTurns flattens the last n turns before the judged message.
// GetContent() rather than .Content: user text can live in Parts while
// assistant text lives on Content.
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
	if len(history) > n {
		history = history[len(history)-n:]
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
	return out
}

func isTopicConversationRole(role string) bool {
	return role == roleUser || role == roleAssistant
}
