package assertions

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/pkg/config"
	"github.com/AltairaLabs/PromptKit/pkg/testutil"
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

// ToEvalDef converts an AssertionConfig to an evals.EvalDef.
// This is the bridge for unifying arena assertions with runtime evals.
func ToEvalDef(a AssertionConfig, index int) evals.EvalDef {
	def := evals.EvalDef{
		ID:        fmt.Sprintf("assertion_%d_%s", index, a.Type),
		Type:      a.Type,
		Trigger:   evals.TriggerEveryTurn,
		Params:    a.Params,
		Message:   a.Message,
		Threshold: &evals.Threshold{Passed: testutil.Ptr(true)},
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

// ToConversationEvalDef converts an AssertionConfig to an evals.EvalDef
// with TriggerOnConversationComplete. Used for conversation-level assertions.
func ToConversationEvalDef(a AssertionConfig, index int) evals.EvalDef {
	def := ToEvalDef(a, index)
	def.Trigger = evals.TriggerOnConversationComplete
	return def
}
