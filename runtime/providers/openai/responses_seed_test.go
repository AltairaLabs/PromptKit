package openai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func intPtr(i int) *int { return &i }

func seedRequest() providers.PredictionRequest {
	return providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
		Seed:     intPtr(42),
	}
}

// TestResponsesRequest_UnsupportedParamsCannotReintroduceSeed pins that the fix
// is unconditional.
//
// The issue proposed guarding seed behind unsupportedParams, matching
// temperature and top_p above it. That would only make the 400 SUPPRESSIBLE:
// seed is not a per-model capability on this endpoint, it is absent from it, so
// a guard leaves every caller to hit the failure and configure their way out.
// This asserts the field stays absent with nothing declared unsupported.
//
// The complementary assertion — that it is absent at all — lives beside the
// Responses field-coverage table in openai_request_field_coverage_test.go.
func TestResponsesRequest_UnsupportedParamsCannotReintroduceSeed(t *testing.T) {
	p := &Provider{unsupportedParams: nil} // nothing declared unsupported

	got := p.buildResponsesRequest(seedRequest(), nil, "")

	_, present := got["seed"]
	assert.False(t, present,
		"seed must be absent even with no unsupported_params configured")
}

// TestCompletionsRequest_StillSendsSeed guards the other direction. Chat
// Completions DOES support seed, and #1742 added it for reproducibility —
// removing it there would undo that.
func TestCompletionsRequest_StillSendsSeed(t *testing.T) {
	p := &Provider{apiMode: APIModeCompletions}

	openAIReq := map[string]interface{}{}
	req := seedRequest()
	p.enrichRequest(openAIReq, &req, "")

	require.Contains(t, openAIReq, "seed",
		"Chat Completions supports seed; reproducibility must survive")
	assert.Equal(t, 42, openAIReq["seed"])
}
