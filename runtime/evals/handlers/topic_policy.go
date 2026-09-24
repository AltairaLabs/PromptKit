package handlers

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TopicPolicyHandler confines a conversation to a declared subject scope,
// decided by an inference provider rather than by the model being governed.
// Declared as a guardrail, it gates input by default and a denied turn never
// reaches the primary provider.
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
// recent_turns (>= 0), provider.
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

	provider, err := h.resolveProvider(ctx, cfg)
	if err != nil {
		return h.outcomeResult(cfg, cfg.onError, err.Error(), ""), nil
	}

	inputs := recentTopicTurns(evalCtx, cfg.recentTurns)
	inputs = append(inputs, types.Message{Role: roleUser, Content: message})
	resp, err := provider.Infer(ctx, inference.Request{
		Prompt: renderTopicPrompt(cfg.policy),
		Inputs: inputs,
		Labels: []string{labelOnTopic, labelOffTopic},
	})
	if err != nil {
		return h.outcomeResult(cfg, cfg.onError, err.Error(), ""), nil
	}

	// The backend returns a probability per label. A distribution that carries
	// neither label, or no mass on either, is not a decision — policy decides,
	// never an implicit allow.
	onTopic, _ := resp.Score(labelOnTopic)
	offTopic, _ := resp.Score(labelOffTopic)
	switch {
	case onTopic > offTopic:
		return h.decisionResult(cfg, topicAllow, 1.0, onTopic, resp.Raw), nil
	case offTopic > onTopic:
		return h.decisionResult(cfg, topicDeny, 0.0, offTopic, resp.Raw), nil
	}
	return h.outcomeResult(cfg, cfg.onUnknown,
		"classifier returned no usable label", resp.Raw), nil
}

// Labels every topic backend answers with. They are NemoGuard topic control's
// trained output labels; renderTopicPrompt's closing sentence names them.
const (
	labelOnTopic  = "on-topic"
	labelOffTopic = "off-topic"
)

// Recorded decisions.
const (
	topicAllow   = "allow"
	topicDeny    = "deny"
	topicUnknown = "unknown"
)

// resolveProvider deliberately does NOT return a skipped result when nothing
// is bound — the convention the other classify-backed handlers follow. Skipped
// scores 1.0 and passes, which for a safety control means it silently does not
// run (#1996). Here an unbound classifier is an error and obeys on_error, which
// defaults to deny.
func (h *TopicPolicyHandler) resolveProvider(
	ctx context.Context, cfg topicPolicyConfig,
) (inference.Provider, error) {
	provider, err := resolveInference(ctx, cfg.providerKey, "topic classifier")
	if err != nil {
		return nil, h.warnUnbound(cfg, err.Error())
	}
	return provider, nil
}

func (h *TopicPolicyHandler) warnUnbound(cfg topicPolicyConfig, reason string) error {
	err := fmt.Errorf(
		"topic_policy: %s; declare a provider with role: inference (e.g. type: openai, "+
			"systemone or nvidia-topic-control), and name it with "+
			"params.provider using the key the pack declares in requires", reason)

	digest := topicPolicyDigest(cfg.policy)
	if h.markWarned(warnKey(digest, cfg.providerKey)) {
		logger.Warn("topic_policy guardrail has no classifier bound; it is blocking every turn",
			"policy_digest", digest, "provider", cfg.providerKey, "reason", reason)
	}
	return err
}

// warnKey identifies one misconfigured guardrail: the policy it enforces plus
// the classifier it asked for. Two packs with different policies each warn; the
// same pack warning on every turn does not. See TopicPolicyHandler.
func warnKey(policyDigest, providerKey string) string {
	return policyDigest + "\x1f" + providerKey
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
// confidence is the probability of the chosen label, as the backend reported it.
func (h *TopicPolicyHandler) decisionResult(
	cfg topicPolicyConfig, decision string, score, confidence float64, raw string,
) *evals.EvalResult {
	return scoredTopicResult(score, map[string]any{
		detailDecision:     decision,
		detailRaw:          raw,
		paramProvider:      cfg.providerKey,
		paramOnDeny:        cfg.onDeny,
		detailPolicyDigest: topicPolicyDigest(cfg.policy),
		"confidence":       confidence,
	})
}

// outcomeResult builds the result for unknown or error, where the score comes
// from policy rather than from the classifier. The decision is always
// topicUnknown: every path here is one where the classifier did not decide, so
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
		detailDecision:     topicUnknown,
		"reason":           reason,
		detailRaw:          raw,
		paramProvider:      cfg.providerKey,
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

// recentTopicTurns returns the last n conversational turns before the judged
// message, as plain-text messages. GetContent() rather than .Content: user text
// can live in Parts while assistant text lives on Content.
//
// Filter first, then slice — never the other way round. A transcript carries
// tool-result messages (role "tool") alongside the conversation, so slicing the
// raw tail first lets a single tool-heavy prior turn evict every real turn: four
// tool results with recent_turns: 4 leave the classifier zero history, and an
// anaphoric but perfectly in-scope follow-up ("What about Azure?") is judged bare
// and denied.
func recentTopicTurns(evalCtx *evals.EvalContext, n int) []types.Message {
	if evalCtx == nil || n <= 0 || len(evalCtx.Messages) == 0 {
		return nil
	}
	// The judged message is the trailing user turn; history is everything
	// before it.
	history := evalCtx.Messages
	if last := len(history) - 1; last >= 0 && history[last].Role == roleUser {
		history = history[:last]
	}
	out := make([]types.Message, 0, len(history))
	for i := range history {
		if !isTopicConversationRole(history[i].Role) {
			continue
		}
		if isSubstitutedAssistantTurn(history[i]) {
			continue
		}
		text := history[i].GetContent()
		if text == "" {
			continue
		}
		out = append(out, types.Message{Role: history[i].Role, Content: text})
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

func isTopicConversationRole(role string) bool {
	return role == roleUser || role == roleAssistant
}

// isSubstitutedAssistantTurn reports whether an assistant message is a blocked
// turn's replacement text rather than something the agent generated.
//
// A denied turn is persisted with the validator's message as the assistant
// reply and FinishReason "safety", so on the NEXT turn it would otherwise be
// replayed to the classifier as the agent's own voice. Two reasons not to:
//
//   - It is not the agent's voice. The window that exists to resolve "what
//     about that one?" gets spent on a refusal with no subject in it, and with
//     a user who keeps trying, the whole window fills with identical refusals
//     while the real history is evicted.
//   - It discloses prior denials to the classifier, which the policy never
//     asked to convey.
//
// Providers set the same FinishReason for model-side content filtering, and
// those turns are excluded too. That is the same call for the same reason:
// whatever text a filtered turn carries, it is not a substantive answer, so it
// is not useful history for deciding what the conversation is about.
//
// The user's message that was denied is NOT filtered — it is genuinely what
// the user said, and it is what an anaphoric follow-up may refer back to.
func isSubstitutedAssistantTurn(m types.Message) bool {
	return m.Role == roleAssistant && m.FinishReason == types.FinishReasonSafety
}
