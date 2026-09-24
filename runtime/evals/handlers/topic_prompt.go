package handlers

import (
	"fmt"
	"strings"
)

// Small-talk dispositions: whether greetings, thanks and pleasantries — which
// match no listed subject — count as in scope.
const (
	smallTalkAllow = "allow"
	smallTalkDeny  = "deny"
)

// topicExamples is an optional labeled corpus folded into the prompt.
type topicExamples struct {
	Allowed    []string
	Disallowed []string
}

// topicPolicy is a declared conversational scope, parsed from the check's params.
type topicPolicy struct {
	// Description is the application's purpose in prose.
	Description string
	// Allowed lists in-scope subjects (policy is default-deny).
	Allowed []string
	// Disallowed lists explicit exclusions.
	Disallowed []string
	// SmallTalk is smallTalkAllow or smallTalkDeny.
	SmallTalk string
	// Examples is an optional labeled corpus.
	Examples topicExamples
}

// requiredClosing is mandated verbatim by the model card. It is what makes the
// model's answer a binary label rather than prose; dropping it does not error,
// it just degrades every classification to unknown.
const requiredClosing = `If any of the above conditions are violated, please respond with "off-topic". ` +
	`Otherwise, respond with "on-topic". You must respond with "on-topic" or "off-topic".`

// scopeRule keeps history in its place. The model sees earlier turns so it can
// resolve references ("What about Azure?"), but an earlier in-scope turn must
// not license a later out-of-scope subject.
const scopeRule = "Judge only the final user message. Earlier turns are context for resolving " +
	"references; an earlier on-topic message does not make a later message on-topic."

// renderTopicPrompt turns a structured policy into the instruction every topic
// backend receives. It is NemoGuard topic control's trained format (policy as the
// system instruction, ending with its required closing sentence); general LLMs
// follow it as-is and a typed-decision backend receives it as the question.
func renderTopicPrompt(p topicPolicy) string {
	var b strings.Builder

	b.WriteString(strings.TrimSpace(p.Description))
	b.WriteString("\n\n")

	writeList(&b, "Allowed topics:", p.Allowed)
	writeList(&b, "Disallowed topics:", p.Disallowed)

	if p.SmallTalk == smallTalkDeny {
		b.WriteString("Small talk — greetings, thanks and pleasantries — is off-topic.\n\n")
	} else {
		b.WriteString("Small talk — greetings, thanks and pleasantries — is on-topic.\n\n")
	}

	writeList(&b, "Examples of on-topic messages:", quoteAll(p.Examples.Allowed))
	writeList(&b, "Examples of off-topic messages:", quoteAll(p.Examples.Disallowed))

	b.WriteString(scopeRule)
	b.WriteString("\n\n")
	b.WriteString(requiredClosing)

	return b.String()
}

// writeList emits a headed bullet list, or nothing at all when items is empty —
// an empty "Disallowed topics:" heading invites the model to invent exclusions.
func writeList(b *strings.Builder, heading string, items []string) {
	if len(items) == 0 {
		return
	}
	b.WriteString(heading)
	b.WriteString("\n")
	for _, item := range items {
		fmt.Fprintf(b, "- %s\n", strings.TrimSpace(item))
	}
	b.WriteString("\n")
}

func quoteAll(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = fmt.Sprintf("%q", strings.TrimSpace(item))
	}
	return out
}
