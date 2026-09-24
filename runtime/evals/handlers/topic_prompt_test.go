package handlers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func sampleTopicPolicy() topicPolicy {
	return topicPolicy{
		Description: "Helps users evaluate and operate AltairaLabs products.",
		Allowed:     []string{"Omnia and PromptKit", "licensing and support"},
		Disallowed:  []string{"politics"},
		SmallTalk:   smallTalkAllow,
		Examples: topicExamples{
			Allowed:    []string{"Can Omnia run on OpenShift?"},
			Disallowed: []string{"Who should I vote for?"},
		},
	}
}

// NemoGuard topic control was trained on this closing sentence verbatim, and
// every backend receives the same prompt, so it is pinned exactly.
func TestRenderTopicPrompt_EndsWithTheRequiredInstruction(t *testing.T) {
	const required = `If any of the above conditions are violated, please respond with "off-topic". ` +
		`Otherwise, respond with "on-topic". You must respond with "on-topic" or "off-topic".`

	rendered := renderTopicPrompt(sampleTopicPolicy())

	assert.True(t, strings.HasSuffix(strings.TrimSpace(rendered), required),
		"rendered instruction must end with the required closing sentence, got:\n%s", rendered)
}

func TestRenderTopicPrompt_IncludesEveryPolicyField(t *testing.T) {
	rendered := renderTopicPrompt(sampleTopicPolicy())

	assert.Contains(t, rendered, "Helps users evaluate and operate AltairaLabs products.")
	assert.Contains(t, rendered, "Omnia and PromptKit")
	assert.Contains(t, rendered, "licensing and support")
	assert.Contains(t, rendered, "politics")
	assert.Contains(t, rendered, "Can Omnia run on OpenShift?")
	assert.Contains(t, rendered, "Who should I vote for?")
}

func TestRenderTopicPrompt_SmallTalkFlipsTheSentence(t *testing.T) {
	allow := sampleTopicPolicy()
	deny := sampleTopicPolicy()
	deny.SmallTalk = smallTalkDeny

	assert.Contains(t, renderTopicPrompt(allow), "is on-topic")
	assert.Contains(t, renderTopicPrompt(deny), "is off-topic")
}

// Earlier in-scope turns must not license a later subject.
func TestRenderTopicPrompt_CarriesTheNoImplicitAuthorizationRule(t *testing.T) {
	rendered := renderTopicPrompt(sampleTopicPolicy())

	assert.Contains(t, rendered, "final user message")
	assert.Contains(t, rendered, "does not make a later message on-topic")
}

func TestRenderTopicPrompt_OmitsEmptySections(t *testing.T) {
	minimal := topicPolicy{
		Description: "Only does one thing.",
		Allowed:     []string{"that one thing"},
		SmallTalk:   smallTalkDeny,
	}

	rendered := renderTopicPrompt(minimal)

	assert.NotContains(t, rendered, "Disallowed topics")
	assert.NotContains(t, rendered, "Examples of")
}
