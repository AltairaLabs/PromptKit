package sdk

import (
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// JudgeProviderKey is the provider key a pack names when it requires an LLM
// judge, and the key the host registers its judge under:
//
//	requires:
//	  providers:
//	    - key: judge
//	      role: llm
//	      description: grades toxicity and PII checks
//
//	sdk.Open(pack, "chat",
//	    sdk.WithProvider(agent),
//	    sdk.WithLLMProvider(sdk.ProviderSpec{ID: "judge", Type: "openai", Model: "gpt-4.1-mini"}),
//	)
//
// RFC 0012 names "judge" as an example key for exactly this; nothing here
// invents a judge or borrows the agent's provider, because which model grades
// the output is the host's decision, not the runtime's. A host whose pack names
// a different key passes the provider explicitly with [WithJudgeProvider].
const JudgeProviderKey = "judge"

// resolveJudge returns the judge this conversation evaluates judge-backed
// checks through, or nil when the host supplied none.
//
// Order:
//  1. WithJudgeProvider — an explicit judge always wins, and is the escape
//     hatch for a pack that names its judge something other than "judge".
//  2. A provider registered under [JudgeProviderKey], which is what a host
//     supplies in answer to the pack's requires block.
//
// There is deliberately no third step. Falling back to the conversation's own
// provider would make the model grade its own output and quietly bill the agent
// model for it, and it would turn a missing requirement — the thing the pack
// asked the host to supply — into something that looks configured.
func resolveJudge(c *config) handlers.JudgeProvider {
	if c.judgeProvider != nil {
		return c.judgeProvider
	}
	if c.providers == nil {
		return nil
	}
	p, ok := c.providers.Get(JudgeProviderKey)
	if !ok {
		return nil
	}
	logger.Debug("resolved judge provider from the provider pool",
		"key", JudgeProviderKey, "provider", p.ID(), "model", p.Model())
	return handlers.NewProviderJudge(p)
}
