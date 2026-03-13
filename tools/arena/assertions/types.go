package assertions

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// AssertionConfig is an alias for config.AssertionConfig. The canonical type
// lives in pkg/config to keep the dependency direction correct (shared library
// must not import application tools). The alias preserves backward compatibility
// so existing arena code continues to compile unchanged.
type AssertionConfig = config.AssertionConfig

// AssertionWhen is an alias for config.AssertionWhen.
type AssertionWhen = config.AssertionWhen

// AssertionResult holds the result of an assertion evaluation.
type AssertionResult struct {
	Passed  bool        `json:"passed"`
	Details interface{} `json:"details,omitempty"`
	Message string      `json:"message,omitempty"` // Human-readable description from config
}

// ToEvalDef converts an AssertionConfig to an evals.EvalDef with type "assertion".
// The original eval type and params are nested under eval_type/eval_params,
// while assertion-specific properties (min_score, max_score) stay at the top level.
func ToEvalDef(a AssertionConfig, index int) evals.EvalDef {
	params := buildAssertionParams(a.Type, a.Params)

	def := evals.EvalDef{
		ID:      fmt.Sprintf("assertion_%d_%s", index, a.Type),
		Type:    evals.WrapperTypeAssertion,
		Trigger: evals.TriggerEveryTurn,
		Params:  params,
		Message: a.Message,
	}
	if a.When != nil {
		def.When = &evals.EvalWhen{
			ToolCalled:        a.When.ToolCalled,
			ToolCalledPattern: a.When.ToolCalledPattern,
			AnyToolCalled:     a.When.AnyToolCalled,
			MinToolCalls:      a.When.MinToolCalls,
		}
	}
	return def
}

// assertionWrapperKeys are param keys that belong to the assertion wrapper,
// not the inner eval handler.
var assertionWrapperKeys = map[string]bool{
	"min_score": true,
	"max_score": true,
}

// buildAssertionParams constructs nested params for the assertion wrapper.
// Wrapper-level properties (min_score, max_score) stay at the top level.
// The inner eval type and its params are nested under eval_type/eval_params.
func buildAssertionParams(evalType string, originalParams map[string]any) map[string]any {
	params := map[string]any{
		"eval_type": evalType,
	}

	// Separate wrapper params from inner eval params
	var evalParams map[string]any
	for k, v := range originalParams {
		if assertionWrapperKeys[k] {
			params[k] = v
		} else {
			if evalParams == nil {
				evalParams = make(map[string]any)
			}
			evalParams[k] = v
		}
	}
	if evalParams != nil {
		params["eval_params"] = evalParams
	}

	return params
}

// ToConversationEvalDef converts an AssertionConfig to an evals.EvalDef
// with TriggerOnConversationComplete. Used for conversation-level assertions.
func ToConversationEvalDef(a AssertionConfig, index int) evals.EvalDef {
	def := ToEvalDef(a, index)
	def.Trigger = evals.TriggerOnConversationComplete
	return def
}
