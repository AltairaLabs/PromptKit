//go:build integration

package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/credentials"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TestVertex_Interactions_Live proves api_mode interactions on Vertex: a turn
// with no tools and a tool turn each reach the location's interactions
// endpoint and answer, twice each. The Interactions API on Vertex takes the
// global, us and eu locations only (any other answers HTTP 400 "Unsupported
// location"), and Gemini 3 models (gemini-2.5-flash answers "not supported by
// the Interactions API"), probed live.
//
// Run:
//
//	gcloud auth application-default login
//	GCP_PROJECT=<project> go test -tags integration ./runtime/providers/gemini/ \
//	    -run TestVertex_Interactions_Live -v
func TestVertex_Interactions_Live(t *testing.T) {
	skipIfNoVertex(t)
	location := "global"
	if l := os.Getenv("VERTEX_INTERACTIONS_LOCATION"); l != "" {
		location = l
	}
	model := "gemini-3.7-flash"
	if m := os.Getenv("VERTEX_INTERACTIONS_MODEL"); m != "" {
		model = m
	}
	ctx := context.Background()
	cred, err := credentials.NewGCPCredential(ctx, vertexProject(), location)
	if err != nil {
		t.Fatalf("NewGCPCredential: %v", err)
	}
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "vertex-interactions", Type: "gemini", Model: model,
		Platform:         vertexPlatform,
		PlatformConfig:   &providers.PlatformConfig{Project: vertexProject(), Region: location},
		Credential:       cred,
		Defaults:         providers.ProviderDefaults{MaxTokens: 2048},
		AdditionalConfig: map[string]any{"api_mode": "interactions"},
	})
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	defer func() { _ = p.Close() }()
	tp := p.(*ToolProvider)

	for run := 1; run <= 2; run++ {
		t.Run(fmt.Sprintf("tool-less/%d", run), func(t *testing.T) {
			resp, err := p.Predict(ctx, providers.PredictionRequest{
				Messages: []types.Message{{Role: roleUser, Content: "What is the capital of France? One word."}},
			})
			require.NoError(t, err)
			assert.Contains(t, strings.ToLower(resp.Content), "paris")
			assert.Equal(t, types.FinishReasonStop, resp.FinishReason)
			t.Logf("tool-less: %q", resp.Content)
		})

		t.Run(fmt.Sprintf("tool/%d", run), func(t *testing.T) {
			tools, err := tp.BuildTooling([]*providers.ToolDescriptor{{
				Name: "get_temperature", Description: "Current temperature in a city, in Celsius.",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},` +
					`"required":["city"]}`),
			}})
			require.NoError(t, err)
			resp, calls, err := tp.PredictWithTools(ctx, providers.PredictionRequest{
				Messages: []types.Message{{Role: roleUser, Content: "What is the temperature in Bristol? Use the tool."}},
			}, tools, "required")
			require.NoError(t, err)
			require.Len(t, calls, 1)
			assert.Equal(t, "get_temperature", calls[0].Name)
			assert.Contains(t, string(calls[0].Args), "Bristol")
			assert.Equal(t, types.FinishReasonToolUse, resp.FinishReason)
			t.Logf("tool: %s(%s)", calls[0].Name, calls[0].Args)
		})
	}
}
