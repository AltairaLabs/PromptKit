package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

type cannedTopicClassifier struct{ decision classify.TopicDecision }

func (c *cannedTopicClassifier) ClassifyTopic(
	_ context.Context, _ classify.TopicRequest,
) (classify.TopicResult, error) {
	return classify.TopicResult{Decision: c.decision, Raw: string(c.decision)}, nil
}

// runProviderStageWithCtx mirrors runProviderStage but lets the caller supply
// the context, which is how the classify registry reaches the handler.
func runProviderStageWithCtx(
	t *testing.T, ctx context.Context, stage *ProviderStage, userContent string,
) ([]StreamElement, error) {
	t.Helper()
	input := make(chan StreamElement, 1)
	msg := types.Message{Role: "user", Content: userContent}
	input <- NewMessageElement(&msg)
	close(input)

	output := make(chan StreamElement, 50)
	err := stage.Process(ctx, input, output)

	var elems []StreamElement
	for e := range output {
		elems = append(elems, e)
	}
	return elems, err
}

func topicPolicyValidator() prompt.ValidatorConfig {
	return prompt.ValidatorConfig{
		Type:    "topic_policy",
		Message: "I can only help with AltairaLabs products.",
		Params: map[string]any{
			"description": "Helps users operate AltairaLabs products.",
			"allowed":     []any{"Omnia and PromptKit"},
		},
	}
}

func topicStage(t *testing.T, provider *redactionRecordingProvider) *ProviderStage {
	t.Helper()
	compiled, err := guardrails.CompileValidators([]prompt.ValidatorConfig{topicPolicyValidator()})
	require.NoError(t, err)
	require.Len(t, compiled, 1)

	reg := hooks.NewRegistry(hooks.WithProviderHook(compiled[0]))
	return NewProviderStageWithHooks(provider, nil, nil, &ProviderConfig{MaxTokens: 100}, nil, reg)
}

func topicCtx(t *testing.T, decision classify.TopicDecision) context.Context {
	t.Helper()
	reg := classify.NewRegistry()
	classify.RegisterBackendDefaulting(reg, "topic-control", &cannedTopicClassifier{decision: decision})
	return classify.WithRegistry(context.Background(), reg)
}

// TestProviderStage_TopicPolicy_DeniedTurnNeverCallsProvider is the assertion
// the whole feature exists for: the off-topic answer is not generated and then
// suppressed, it is never generated.
func TestProviderStage_TopicPolicy_DeniedTurnNeverCallsProvider(t *testing.T) {
	provider := &redactionRecordingProvider{Provider: mock.NewProvider("p", "m", false)}
	stage := topicStage(t, provider)

	elems, err := runProviderStageWithCtx(t, topicCtx(t, classify.TopicDeny), stage, "Who should I vote for?")

	require.NoError(t, err, "an enforced guardrail continues the pipeline rather than erroring")
	assert.Equal(t, 0, provider.callCount(), "a denied turn must not reach the agent provider")

	msgs := assistantMessages(elems)
	require.Len(t, msgs, 1)
	assert.Equal(t, "I can only help with AltairaLabs products.", msgs[0].Content)
}

func TestProviderStage_TopicPolicy_AllowedTurnCallsProvider(t *testing.T) {
	provider := &redactionRecordingProvider{Provider: mock.NewProvider("p", "m", false)}
	stage := topicStage(t, provider)

	_, err := runProviderStageWithCtx(t, topicCtx(t, classify.TopicAllow), stage, "Can Omnia run on OpenShift?")

	require.NoError(t, err)
	assert.Equal(t, 1, provider.callCount(), "an in-scope turn must reach the agent provider")
}

// TestProviderStage_TopicPolicy_UnboundClassifierBlocks proves the guardrail
// fails closed end to end, not just in the handler unit test.
func TestProviderStage_TopicPolicy_UnboundClassifierBlocks(t *testing.T) {
	provider := &redactionRecordingProvider{Provider: mock.NewProvider("p", "m", false)}
	stage := topicStage(t, provider)

	_, err := runProviderStageWithCtx(t, context.Background(), stage, "Can Omnia run on OpenShift?")

	require.NoError(t, err)
	assert.Equal(t, 0, provider.callCount(),
		"with no classifier bound the guardrail must block rather than silently pass")
}

// TestProviderStage_TopicPolicy_GatesInputByDefault is the direction-default
// assertion, made through a real compiled validator rather than by reading the
// defaults table. With the factory reading direction from the raw params this
// check would run output-only and the provider would be called.
func TestProviderStage_TopicPolicy_GatesInputByDefault(t *testing.T) {
	provider := &redactionRecordingProvider{Provider: mock.NewProvider("p", "m", false)}
	stage := topicStage(t, provider)

	_, err := runProviderStageWithCtx(t, topicCtx(t, classify.TopicDeny), stage, "off topic please")

	require.NoError(t, err)
	assert.Equal(t, 0, provider.callCount(),
		"topic_policy must default to direction: input; an output-only check calls the provider first")
}
