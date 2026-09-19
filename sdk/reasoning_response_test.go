package sdk

import (
	"context"
	"testing"
	"time"

	rtpipeline "github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reasoningResult builds an ExecutionResult shaped like a tool loop: two
// tool-calling rounds followed by a final answer, each assistant message
// carrying its own reasoning, exactly as the provider stage assembles them.
func reasoningResult() *rtpipeline.ExecutionResult {
	return &rtpipeline.ExecutionResult{
		Messages: []types.Message{
			{Role: "user", Content: "What's the weather?"},
			{
				Role:      roleAssistant,
				Reasoning: &types.ReasoningTrace{Text: "I should call get_weather."},
			},
			{Role: "tool", Content: "sunny"},
			{
				Role:      roleAssistant,
				Content:   "It's sunny.",
				Reasoning: &types.ReasoningTrace{Text: "I have the reading; answer plainly."},
			},
		},
		Response: &rtpipeline.Response{
			Role:    roleAssistant,
			Content: "It's sunny.",
		},
	}
}

// TestBuildResponse_CarriesReasoning covers the Send path. The pipeline's
// Response type is narrower than types.Message and has no Reasoning field, so
// without an explicit recovery from ExecutionResult.Messages the SDK hands
// back a Response whose Message().Reasoning is always nil — silently, with no
// error anywhere.
func TestBuildResponse_CarriesReasoning(t *testing.T) {
	conv := newTestConversation()

	resp := conv.buildResponse(context.Background(), reasoningResult(), time.Now())

	require.NotNil(t, resp.Message())
	require.NotNil(t, resp.Message().Reasoning, "Send dropped the reasoning trace")
	assert.Equal(t, "I have the reading; answer plainly.", resp.Message().Reasoning.Text)
}

// TestBuildStreamingResponse_CarriesReasoning covers the Stream path, which
// builds its Response separately from Send — a fix on one leaves the other
// broken.
func TestBuildStreamingResponse_CarriesReasoning(t *testing.T) {
	conv := newTestConversation()
	state := &streamState{finalResult: reasoningResult()}

	resp := conv.buildStreamingResponse(state, time.Now())

	require.NotNil(t, resp.Message())
	require.NotNil(t, resp.Message().Reasoning, "Stream dropped the reasoning trace")
	assert.Equal(t, "I have the reading; answer plainly.", resp.Message().Reasoning.Text)
}

// TestBuildResponse_TakesLastAssistantReasoning pins which round's trace the
// terminal Response reports. Response means "the final response", so it must
// carry the LAST assistant round's reasoning, not the first round's and not a
// concatenation. Per-round traces travel on message.created instead.
func TestBuildResponse_TakesLastAssistantReasoning(t *testing.T) {
	conv := newTestConversation()

	resp := conv.buildResponse(context.Background(), reasoningResult(), time.Now())

	require.NotNil(t, resp.Message().Reasoning)
	assert.NotContains(t, resp.Message().Reasoning.Text, "I should call get_weather",
		"terminal response reported an earlier round's reasoning")
}

// TestBuildResponse_ReasoningPresenceIsDiscriminated pins that nil means "the
// model did not reason" rather than being the zero value we would get from
// never reading the field.
//
// Asserting nil on a no-reasoning result alone is not a test: it passes
// against a buildResponse that ignores reasoning entirely. Driving both inputs
// through the same builder is, because the present case fails the moment the
// recovery is dropped.
func TestBuildResponse_ReasoningPresenceIsDiscriminated(t *testing.T) {
	conv := newTestConversation()

	withReasoning := conv.buildResponse(context.Background(), reasoningResult(), time.Now())
	require.NotNil(t, withReasoning.Message().Reasoning,
		"an execution carrying reasoning produced none")

	noReasoning := conv.buildResponse(context.Background(), &rtpipeline.ExecutionResult{
		Messages: []types.Message{{Role: roleAssistant, Content: "No thinking."}},
		Response: &rtpipeline.Response{Role: roleAssistant, Content: "No thinking."},
	}, time.Now())
	require.NotNil(t, noReasoning.Message())
	assert.Nil(t, noReasoning.Message().Reasoning,
		"an execution with no reasoning must stay nil, not an empty trace")

	// Both came from the same builder, so the difference is the input — which
	// is what makes the nil meaningful rather than incidental.
	assert.NotEqual(t, withReasoning.Message().Reasoning, noReasoning.Message().Reasoning)
}
