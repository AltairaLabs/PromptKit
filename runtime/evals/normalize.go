package evals

// ParamAliases maps legacy param names to canonical names per eval type.
var ParamAliases = map[string]map[string]string{
	"content_excludes": {
		"words": "patterns",
	},
	"max_length": {
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
	"field_presence": {
		"required_fields": "fields",
	},
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
