package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

func TestSmoke(t *testing.T) {
	// Mock provider returns a schema-shaped JSON payload so the example's
	// unmarshal path is exercised without a live model.
	repo := mock.NewInMemoryMockRepository(`{"summary":"ok","questions":["q1","q2"]}`)
	provider := mock.NewProviderWithRepository("mock", "mock-model", false, repo)

	conv, err := sdk.Open("research.pack.json", "plan",
		sdk.WithProvider(provider),
		sdk.WithSkipSchemaValidation(),
		sdk.WithResponseFormat(&providers.ResponseFormat{
			Type:       providers.ResponseFormatJSONSchema,
			JSONSchema: outputSchema,
			SchemaName: "research_plan",
			Strict:     true,
		}),
	)
	require.NoError(t, err)
	defer conv.Close()

	input := PlanRequest{Topic: "battery storage", Audience: "executive"}
	resp, err := conv.Send(context.Background(), "", sdk.WithJSONInput(input))
	require.NoError(t, err)

	var plan PlanResponse
	require.NoError(t, json.Unmarshal([]byte(resp.Text()), &plan))
	assert.Equal(t, "ok", plan.Summary)
	assert.Len(t, plan.Questions, 2)
}
