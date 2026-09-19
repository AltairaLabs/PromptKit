package evals

// ParamAliases maps legacy param names to canonical names per eval type.
// Entries exist for both the canonical type name and any aliases, so that
// NormalizeParams works regardless of which name is used.
var ParamAliases = map[string]map[string]string{
	"content_excludes": {
		"words": "patterns",
	},
	"banned_words": {
		"words": "patterns",
	},
	"max_length": {
		"max_characters": "max",
		"max_chars":      "max",
	},
	"length": {
		"max_characters": "max",
		"max_chars":      "max",
	},
	"min_length": {
		"min_characters": "min",
		"min_chars":      "min",
	},
	"sentence_count": {
		"max_sentences": "max",
	},
	"max_sentences": {
		"max_sentences": "max",
	},
	"field_presence": {
		"required_fields": "fields",
	},
	"required_fields": {
		"required_fields": "fields",
	},
}

// topicPolicyDefaultOnUncertain is topic_policy's default outcome for both
// on_unknown and on_error: default-deny, so an unresolved verdict doesn't
// silently pass a guardrail through.
const topicPolicyDefaultOnUncertain = "deny"

// topicPolicyDefaultRecentTurns mirrors handlers.defaultRecentTurns — that
// package imports this one, not the other way round, so the value is
// duplicated rather than shared.
const topicPolicyDefaultRecentTurns = 4

// ParamDefaults provides default param values for aliased eval types.
// These are applied when the param is not already present.
var ParamDefaults = map[string]map[string]any{
	"banned_words": {"match_mode": "word_boundary"},
	// direction: a topic check that only inspects the assistant's reply never
	// blocks the call it exists to prevent. The guardrail factory defaults to
	// output, so this entry is what makes the check gate input.
	"topic_policy": {
		"direction":    "input",
		"small_talk":   "allow",
		"on_deny":      "block",
		"on_unknown":   topicPolicyDefaultOnUncertain,
		"on_error":     topicPolicyDefaultOnUncertain,
		"recent_turns": topicPolicyDefaultRecentTurns,
	},
}

// ApplyDefaults merges default params for the given eval type.
// User-provided params take precedence over defaults.
func ApplyDefaults(evalType string, params map[string]any) map[string]any {
	defaults, ok := ParamDefaults[evalType]
	if !ok {
		return params
	}
	result := make(map[string]any, len(params)+len(defaults))
	for k, v := range defaults {
		result[k] = v
	}
	for k, v := range params {
		result[k] = v // user params override defaults
	}
	return result
}

// NormalizeParams rewrites legacy param names to canonical names.
// Unknown params pass through unchanged.
func NormalizeParams(evalType string, params map[string]any) map[string]any {
	aliases, ok := ParamAliases[evalType]
	if !ok {
		return params
	}
	normalized := make(map[string]any, len(params))
	for k, v := range params {
		if canonical, found := aliases[k]; found {
			// Only remap if the canonical key is not already present
			if _, exists := params[canonical]; !exists {
				normalized[canonical] = v
				continue
			}
		}
		normalized[k] = v
	}
	return normalized
}
