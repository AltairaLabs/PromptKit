package sdk

import (
	"fmt"

	"github.com/AltairaLabs/PromptKit/runtime/logger"
	"github.com/AltairaLabs/PromptKit/runtime/prompt"
)

// checkProviderRequirements enforces the one behavioral rule RFC 0012 places
// on a runtime:
//
//	"A consuming runtime/deployer SHOULD treat an unsatisfied `required`
//	 requirement as an error and an unsatisfied optional requirement as a
//	 warning."
//
// Before this, `requires` was accepted by the schema, dropped on load, and read
// by nothing — so a pack that declared its dependencies failed exactly as late
// as one that did not, at the first request rather than at Open. The point of
// declaring them is to move that failure earlier.
//
// What this deliberately does NOT do is resolve anything. Which concrete model
// satisfies a requirement, from which endpoint, with which credentials, are
// explicit non-goals of the RFC and remain the host's business. This only
// answers "did the host supply something for every key the pack said it needs".
func checkProviderRequirements(p *prompt.Pack, cfg *config) error {
	reqs, err := prompt.ResolveRequirements(p)
	if err != nil {
		// A malformed requires block is the pack's error, not the host's, and
		// it is cheap to surface here rather than let a duplicate key silently
		// collapse two providers into one.
		return fmt.Errorf("pack requires: %w", err)
	}
	if len(reqs) == 0 {
		return nil
	}

	required, optional := prompt.Unsatisfied(reqs, cfg.providerInventory())

	if len(optional) > 0 {
		logger.Warn("pack declares optional providers that are not configured; "+
			"features depending on them will not run",
			"unsatisfied", prompt.DescribeUnsatisfied(optional))
	}
	if len(required) > 0 {
		return fmt.Errorf(
			"pack requires providers that are not configured: %s. "+
				"Supply them with WithProvider/WithLLMProvider (llm), WithEmbeddingProvider, "+
				"WithTTSProvider or WithSTTProvider, using the key the pack names",
			prompt.DescribeUnsatisfied(required))
	}
	return nil
}

// providerInventory reports what this conversation can supply, as role -> keys,
// in the vocabulary RFC 0012 uses for `role`.
//
// The reserved key "default" means "the primary LLM", so the agent provider
// answers to both its own ID and to "default" — a pack declaring `- default`,
// the RFC's own first example, must be satisfied by an ordinary WithProvider
// call rather than requiring the host to know the reserved name.
func (c *config) providerInventory() prompt.ProviderInventory {
	inv := prompt.ProviderInventory{}

	var llm []string
	if c.agentSet {
		llm = append(llm, prompt.RequirementKeyDefault)
		if c.agentProviderID != "" && c.agentProviderID != prompt.RequirementKeyDefault {
			llm = append(llm, c.agentProviderID)
		}
	}
	if c.providers != nil {
		llm = append(llm, c.providers.List()...)
	}
	if len(llm) > 0 {
		inv[prompt.RequirementRoleLLM] = llm
	}

	if len(c.embeddingProviderIDs) > 0 {
		inv["embedding"] = c.embeddingProviderIDs
	}
	if len(c.ttsProviderIDs) > 0 {
		inv["tts"] = c.ttsProviderIDs
	}
	if len(c.sttProviderIDs) > 0 {
		inv["stt"] = c.sttProviderIDs
	}
	return inv
}
