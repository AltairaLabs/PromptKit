package topiccontrol_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	"github.com/AltairaLabs/PromptKit/runtime/v2/classify/backends/topiccontrol"
)

func samplePolicy() classify.TopicPolicy {
	return classify.TopicPolicy{
		Description: "Helps users evaluate and operate AltairaLabs products.",
		Allowed:     []string{"Omnia and PromptKit", "licensing and support"},
		Disallowed:  []string{"politics"},
		SmallTalk:   classify.SmallTalkAllow,
		Examples: classify.TopicExamples{
			Allowed:    []string{"Can Omnia run on OpenShift?"},
			Disallowed: []string{"Who should I vote for?"},
		},
	}
}

// TestRenderPolicy_EndsWithTheRequiredInstruction pins the one sentence the
// model card requires verbatim. Without it the model is not guaranteed to emit
// one of the two labels, and the parser degrades to unknown on every turn.
func TestRenderPolicy_EndsWithTheRequiredInstruction(t *testing.T) {
	const required = `If any of the above conditions are violated, please respond with "off-topic". ` +
		`Otherwise, respond with "on-topic". You must respond with "on-topic" or "off-topic".`

	rendered := topiccontrol.RenderPolicy(samplePolicy())

	assert.True(t, strings.HasSuffix(strings.TrimSpace(rendered), required),
		"rendered instruction must end with the required closing sentence, got:\n%s", rendered)
}

func TestRenderPolicy_IncludesEveryPolicyField(t *testing.T) {
	rendered := topiccontrol.RenderPolicy(samplePolicy())

	assert.Contains(t, rendered, "Helps users evaluate and operate AltairaLabs products.")
	assert.Contains(t, rendered, "Omnia and PromptKit")
	assert.Contains(t, rendered, "licensing and support")
	assert.Contains(t, rendered, "politics")
	assert.Contains(t, rendered, "Can Omnia run on OpenShift?")
	assert.Contains(t, rendered, "Who should I vote for?")
}

// TestRenderPolicy_SmallTalkFlipsTheSentence proves small_talk is not inert —
// the two dispositions must produce materially different instructions.
func TestRenderPolicy_SmallTalkFlipsTheSentence(t *testing.T) {
	allow := samplePolicy()
	deny := samplePolicy()
	deny.SmallTalk = classify.SmallTalkDeny

	assert.Contains(t, topiccontrol.RenderPolicy(allow), "Small talk")
	assert.NotEqual(t, topiccontrol.RenderPolicy(allow), topiccontrol.RenderPolicy(deny))
}

// TestRenderPolicy_CarriesTheNoImplicitAuthorizationRule pins the semantic rule
// the design requires: earlier in-scope turns must not license a later subject.
func TestRenderPolicy_CarriesTheNoImplicitAuthorizationRule(t *testing.T) {
	rendered := topiccontrol.RenderPolicy(samplePolicy())

	assert.Contains(t, rendered, "final user message")
	assert.Contains(t, rendered, "does not make a later message on-topic")
}

func TestRenderPolicy_OmitsEmptySections(t *testing.T) {
	minimal := classify.TopicPolicy{
		Description: "Only does one thing.",
		Allowed:     []string{"that one thing"},
		SmallTalk:   classify.SmallTalkDeny,
	}

	rendered := topiccontrol.RenderPolicy(minimal)

	assert.NotContains(t, rendered, "Disallowed topics")
	assert.NotContains(t, rendered, "Examples of")
}
