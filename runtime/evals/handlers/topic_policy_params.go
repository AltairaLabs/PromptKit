package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
)

// topicPolicyConfig is the handler's view of a validated policy declaration.
type topicPolicyConfig struct {
	policy       classify.TopicPolicy
	onDeny       string
	onUnknown    string
	onError      string
	recentTurns  int
	classifierID string
}

// Enum values for the decision params. Each is a closed set: a typo must be
// rejected rather than silently resolved to a policy nobody chose, because
// these are the knobs that decide whether uncertainty fails open.
const (
	onDenyBlock   = "block"
	onDenyRespond = "respond"
	outcomeDeny   = "deny"
	outcomeAllow  = "allow"
)

// Param name constants. Each backs at least one param name that is checked
// against the closed key set AND read from the map AND (for a couple)
// echoed back into EvalResult.Details — defining it once keeps those three
// sites from drifting apart, and keeps goconst happy about the repetition.
const (
	paramAllowed      = "allowed"
	paramDisallowed   = "disallowed"
	paramOnDeny       = "on_deny"
	paramClassifierID = "classifier_id"
	paramMessage      = "message"
)

// defaultRecentTurns is topic_policy's recent_turns default, mirrored in
// runtime/evals/normalize.go's ParamDefaults entry (that package can't
// import this one, so the value is duplicated rather than shared).
const defaultRecentTurns = 4

// topicPolicyKeys is the closed key set. Rejecting anything outside it is the
// single rule that makes an inline params map a defined config shape: without
// it a misspelled `dissallowed:` yields a policy with no exclusions and no
// signal that anything is wrong.
var topicPolicyKeys = map[string]struct{}{
	"description":     {},
	paramAllowed:      {},
	paramDisallowed:   {},
	"small_talk":      {},
	"examples":        {},
	paramOnDeny:       {},
	"on_unknown":      {},
	"on_error":        {},
	"recent_turns":    {},
	paramClassifierID: {},
	// direction is consumed by the guardrail factory, not by this handler,
	// but it arrives in the same map (and via ParamDefaults), so it must be
	// permitted here or every declaration fails validation.
	"direction": {},
	// message is the user-facing blocked text; the guardrail adapter reads
	// it from the validator entry, and packs may also set it in params.
	paramMessage: {},
}

// parseTopicPolicyParams validates and converts. It is the single source of
// truth for both ValidateParams (load time) and Eval (per turn).
func parseTopicPolicyParams(params map[string]any) (topicPolicyConfig, error) {
	var cfg topicPolicyConfig

	if err := rejectTopicThresholdParams(params); err != nil {
		return cfg, err
	}
	if err := rejectUnknownTopicKeys(params); err != nil {
		return cfg, err
	}

	description, err := requiredString(params, "description")
	if err != nil {
		return cfg, err
	}
	allowed, err := requiredStringList(params, paramAllowed)
	if err != nil {
		return cfg, err
	}
	disallowed, err := optionalStringList(params, paramDisallowed)
	if err != nil {
		return cfg, err
	}
	examples, err := parseTopicExamples(params)
	if err != nil {
		return cfg, err
	}

	smallTalk, err := enumParam(params, "small_talk", classify.SmallTalkAllow,
		classify.SmallTalkAllow, classify.SmallTalkDeny)
	if err != nil {
		return cfg, err
	}
	if cfg.onDeny, err = enumParam(params, paramOnDeny, onDenyBlock, onDenyBlock, onDenyRespond); err != nil {
		return cfg, err
	}
	if cfg.onUnknown, err = enumParam(params, "on_unknown", outcomeDeny, outcomeDeny, outcomeAllow); err != nil {
		return cfg, err
	}
	if cfg.onError, err = enumParam(params, "on_error", outcomeDeny, outcomeDeny, outcomeAllow); err != nil {
		return cfg, err
	}
	if cfg.recentTurns, err = nonNegativeIntParam(params, "recent_turns", defaultRecentTurns); err != nil {
		return cfg, err
	}
	if cfg.classifierID, err = optionalString(params, paramClassifierID); err != nil {
		return cfg, err
	}

	cfg.policy = classify.TopicPolicy{
		Description: description,
		Allowed:     allowed,
		Disallowed:  disallowed,
		SmallTalk:   smallTalk,
		Examples:    examples,
	}
	return cfg, nil
}

func rejectTopicThresholdParams(params map[string]any) error {
	for _, key := range thresholdParamNames {
		if _, ok := params[key]; ok {
			return fmt.Errorf(
				"topic_policy: %q is a threshold param and belongs on a type: assertion wrapper, "+
					"not on the eval; topic_policy is default-deny and needs no threshold", key)
		}
	}
	return nil
}

func rejectUnknownTopicKeys(params map[string]any) error {
	var unknown []string
	for key := range params {
		if _, ok := topicPolicyKeys[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	known := make([]string, 0, len(topicPolicyKeys))
	for key := range topicPolicyKeys {
		known = append(known, key)
	}
	sort.Strings(known)
	return fmt.Errorf("topic_policy: unknown param(s) %s (valid: %s)",
		strings.Join(unknown, ", "), strings.Join(known, ", "))
}

func requiredString(params map[string]any, key string) (string, error) {
	v, err := optionalString(params, key)
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", fmt.Errorf(
			"topic_policy: %q is required — an allow-list alone reads as keywords to a "+
				"semantic classifier; describe what the application is for", key)
	}
	return v, nil
}

func optionalString(params map[string]any, key string) (string, error) {
	raw, ok := params[key]
	if !ok || raw == nil {
		return "", nil
	}
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("topic_policy: %q must be a string, got %T", key, raw)
	}
	return strings.TrimSpace(s), nil
}

func requiredStringList(params map[string]any, key string) ([]string, error) {
	list, err := optionalStringList(params, key)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, fmt.Errorf(
			"topic_policy: %q is required and must list at least one subject — the policy is "+
				"default-deny, so an empty allow-list blocks every message", key)
	}
	return list, nil
}

func optionalStringList(params map[string]any, key string) ([]string, error) {
	raw, ok := params[key]
	if !ok || raw == nil {
		return nil, nil
	}
	items, err := toAnySlice(raw)
	if err != nil {
		return nil, fmt.Errorf("topic_policy: %q must be a list of strings, got %T", key, raw)
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("topic_policy: %q[%d] must be a string, got %T", key, i, item)
		}
		if strings.TrimSpace(s) == "" {
			return nil, fmt.Errorf("topic_policy: %q[%d] is blank; remove it or describe a subject", key, i)
		}
		out = append(out, strings.TrimSpace(s))
	}
	return out, nil
}

func toAnySlice(raw any) ([]any, error) {
	switch v := raw.(type) {
	case []any:
		return v, nil
	case []string:
		out := make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
		return out, nil
	default:
		return nil, errors.New("not a list")
	}
}

func parseTopicExamples(params map[string]any) (classify.TopicExamples, error) {
	var out classify.TopicExamples
	raw, ok := params["examples"]
	if !ok || raw == nil {
		return out, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return out, fmt.Errorf(
			"topic_policy: \"examples\" must be a map with optional \"allowed\" and "+
				"\"disallowed\" lists, got %T", raw)
	}
	for key := range m {
		if key != paramAllowed && key != paramDisallowed {
			return out, fmt.Errorf(
				"topic_policy: unknown key %q under \"examples\" (valid: allowed, disallowed)", key)
		}
	}
	allowed, err := optionalStringList(m, paramAllowed)
	if err != nil {
		return out, err
	}
	disallowed, err := optionalStringList(m, paramDisallowed)
	if err != nil {
		return out, err
	}
	out.Allowed = allowed
	out.Disallowed = disallowed
	return out, nil
}

func enumParam(params map[string]any, key, fallback string, valid ...string) (string, error) {
	v, err := optionalString(params, key)
	if err != nil {
		return "", err
	}
	if v == "" {
		return fallback, nil
	}
	for _, candidate := range valid {
		if v == candidate {
			return v, nil
		}
	}
	return "", fmt.Errorf("topic_policy: %q must be one of %s, got %q",
		key, strings.Join(valid, ", "), v)
}

func nonNegativeIntParam(params map[string]any, key string, fallback int) (int, error) {
	raw, ok := params[key]
	if !ok || raw == nil {
		return fallback, nil
	}
	var n int
	switch v := raw.(type) {
	case int:
		n = v
	case int64:
		n = int(v)
	case float64: // YAML and JSON both hand numbers over as float64 on some paths.
		n = int(v)
	default:
		return 0, fmt.Errorf("topic_policy: %q must be an integer, got %T", key, raw)
	}
	if n < 0 {
		return 0, fmt.Errorf("topic_policy: %q must be >= 0 (0 means the current message only), got %d", key, n)
	}
	return n, nil
}

// topicPolicyDigest identifies the policy that governed an interaction.
//
// It is a digest, not a version: inline params carry no policy id, so this is
// the best available answer to "which policy was in force?". Lists are sorted
// and entries trimmed before hashing so cosmetic reordering does not change it,
// but a wording change does — and the digest cannot say what changed.
func topicPolicyDigest(p classify.TopicPolicy) string {
	normalize := func(items []string) []string {
		out := make([]string, 0, len(items))
		for _, item := range items {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, s)
			}
		}
		sort.Strings(out)
		return out
	}
	parts := []string{
		strings.TrimSpace(p.Description),
		strings.Join(normalize(p.Allowed), "\x1f"),
		strings.Join(normalize(p.Disallowed), "\x1f"),
		p.SmallTalk,
		strings.Join(normalize(p.Examples.Allowed), "\x1f"),
		strings.Join(normalize(p.Examples.Disallowed), "\x1f"),
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1e")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
