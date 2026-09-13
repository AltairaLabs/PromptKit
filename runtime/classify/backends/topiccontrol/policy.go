// Package topiccontrol implements classify.TopicClassifier against NVIDIA's
// Llama-3.1-NemoGuard-8B TopicControl model, served by NIM over an
// OpenAI-compatible chat-completions endpoint.
//
// The model takes a topical instruction as the system message and a
// conversation whose final entry is the user message to judge, and answers
// with exactly one of two labels: "on-topic" or "off-topic". Rendering a
// classify.TopicPolicy into that instruction lives here rather than in the
// eval handler, so a future non-chat backend can consume the same policy
// without the pack changing.
package topiccontrol

import (
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
)

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

// RenderPolicy turns a structured policy into the system instruction the
// TopicControl model expects.
func RenderPolicy(p classify.TopicPolicy) string {
	var b strings.Builder

	b.WriteString(strings.TrimSpace(p.Description))
	b.WriteString("\n\n")

	writeList(&b, "Allowed topics:", p.Allowed)
	writeList(&b, "Disallowed topics:", p.Disallowed)

	if p.SmallTalk == classify.SmallTalkDeny {
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
