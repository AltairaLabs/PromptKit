package sdk

import (
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

// Governance is a pack's RFC 0013 governance declaration: who is accountable
// for the agent, how autonomously it acts, what it was built for, whether it
// must disclose itself as AI.
//
// Aliased here so a host reading these facts does not have to import the
// runtime package directly.
type Governance = prompt.Governance

// Governance returns the governance declared for this conversation, or nil if
// the pack declares none.
//
// When the conversation was opened as a named agent, this is the agent's
// governance resolved against the pack's — per-field replacement, arrays and
// extensions replacing whole (see prompt.ResolveGovernance). For a plain
// single-prompt conversation, which is not an agent, it is the pack-level
// declaration.
//
// Nothing in PromptKit acts on the result. RFC 0013 is explicit that a
// governance block describes and does not gate: this is here so a host can
// SHOW what the pack claims — surface an AI disclosure in its UI, log the
// accountable owner beside a transcript, refuse to deploy a pack whose
// approved_environments do not include the one it is being deployed to.
// Those are the host's decisions to make, on its own policy, with these facts.
//
// The result is a copy; adjusting it does not change the loaded pack.
func (c *Conversation) Governance() *Governance {
	if c == nil || c.pack == nil {
		return nil
	}

	// promptName is the agent's member key when the conversation was opened as
	// an agent — openAgent calls Open(packPath, memberKey). If it names no
	// member, this is an ordinary prompt conversation and only the pack-level
	// declaration applies.
	//
	// The membership test is here for intent, not for the answer.
	// ResolveGovernance rejects an unknown agent by design, which is right for
	// a caller that named one and wrong for a conversation that never claimed
	// to be an agent — so asking "is this an agent?" up front says what is
	// meant, rather than using a rejection as normal control flow. Both paths
	// happen to land on the pack declaration, so removing either would not
	// change what is returned.
	if c.pack.Agents != nil {
		if _, isAgent := c.pack.Agents.Members[c.promptName]; isAgent {
			g, err := prompt.ResolveGovernance(c.pack, c.promptName)
			if err != nil {
				// Reachable: a member declared with no body
				// ("members":{"billing":null}) is a key that exists holding a
				// nil definition, which ResolveGovernance rejects as declared
				// but empty. An agent that declares nothing inherits the pack,
				// so this is the right answer and not just a safe one.
				return prompt.PackGovernance(c.pack)
			}
			return g
		}
	}

	return prompt.PackGovernance(c.pack)
}

// PackGovernance returns the pack-level governance declaration, ignoring any
// agent scope. Use it to show what the pack claims as a whole; use Governance
// for what applies to this conversation.
func (c *Conversation) PackGovernance() *Governance {
	if c == nil {
		return nil
	}
	return prompt.PackGovernance(c.pack)
}

// DescribeGovernance renders a declaration as a short human-readable summary,
// listing only the fields the pack actually declares.
func DescribeGovernance(g *Governance) string {
	return prompt.DescribeGovernance(g)
}
