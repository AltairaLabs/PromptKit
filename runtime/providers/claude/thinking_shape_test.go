package claude

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The two 400s the Messages API returns for the wrong thinking shape, captured
// live from api.anthropic.com (2026-11-18). Bedrock wraps the message in its own
// {"message": ...} envelope; Azure passes the Anthropic body through.
const (
	adaptiveRejectedBody = `{"type":"error","error":{"type":"invalid_request_error",` +
		`"message":"adaptive thinking is not supported on this model"}}`
	enabledRejectedBody = `{"type":"error","error":{"type":"invalid_request_error",` +
		`"message":"\"thinking.type.enabled\" is not supported for this model. ` +
		`Use \"thinking.type.adaptive\" and \"output_config.effort\" to control thinking behavior."}}`
	bedrockAdaptiveRejectedBody = `{"message":"adaptive thinking is not supported on this model"}`
)

// thinkingShapeServer stands in for one model generation: it rejects the
// thinking type in rejects with rejectBody and answers every other request,
// recording each request body's "thinking" field in order.
type thinkingShapeServer struct {
	mu       sync.Mutex
	thinking []map[string]any
}

func (s *thinkingShapeServer) sent() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]any(nil), s.thinking...)
}

func newThinkingShapeServer(t *testing.T, rejects, rejectBody string) (*thinkingShapeServer, *httptest.Server) {
	t.Helper()
	rec := &thinkingShapeServer{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		thinking, _ := body["thinking"].(map[string]any)
		rec.mu.Lock()
		rec.thinking = append(rec.thinking, thinking)
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if thinking["type"] == rejects {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(rejectBody))
			return
		}
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("event: content_block_delta\n" +
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}` +
				"\n\nevent: message_stop\n" + `data: {"type":"message_stop"}` + "\n\n"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant",` +
			`"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn",` +
			`"usage":{"input_tokens":3,"output_tokens":1}}`))
	}))
	t.Cleanup(srv.Close)
	return rec, srv
}

var (
	adaptiveShape = map[string]any{"type": "adaptive"}
	budgetShape   = map[string]any{"type": "enabled", "budget_tokens": float64(2048)}
)

func thinkingSpec(model, baseURL string, extra map[string]any) providers.ProviderSpec {
	cfg := map[string]any{"thinking_budget": 2048}
	for k, v := range extra {
		cfg[k] = v
	}
	return providers.ProviderSpec{
		ID: "claude-thinking", Type: "claude", Model: model, BaseURL: baseURL,
		Defaults:         providers.ProviderDefaults{MaxTokens: 4096},
		AdditionalConfig: cfg,
	}
}

func hiRequest() providers.PredictionRequest {
	return providers.PredictionRequest{Messages: []types.Message{{Role: "user", Content: "hi"}}}
}

func buildThinkingProvider(t *testing.T, spec providers.ProviderSpec) providers.Provider {
	t.Helper()
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	p, err := providers.CreateProviderFromSpec(spec)
	require.NoError(t, err)
	return p
}

// A Bedrock inference profile does not match the name heuristic, so the first
// guess is adaptive. A 4.5 model behind it rejects that: the provider retries
// once with the budget shape, succeeds, and sends the budget shape from then on.
func TestThinkingShape_BedrockInferenceProfileLearnsBudgetShape(t *testing.T) {
	rec, srv := newThinkingShapeServer(t, thinkingTypeAdaptive, bedrockAdaptiveRejectedBody)
	spec := thinkingSpec("us.anthropic.claude-sonnet-4-5-20250929-v1:0", srv.URL, nil)
	spec.Platform = bedrockPlatform
	spec.PlatformConfig = &providers.PlatformConfig{Type: bedrockPlatform, Region: "us-east-1"}
	spec.Credential = &mockBearerCredential{}
	p := buildThinkingProvider(t, spec)

	resp, err := p.Predict(context.Background(), hiRequest())
	require.NoError(t, err)
	assert.Equal(t, "hi", resp.Content)
	_, err = p.Predict(context.Background(), hiRequest())
	require.NoError(t, err)

	assert.Equal(t, []map[string]any{adaptiveShape, budgetShape, budgetShape}, rec.sent())
}

// An Azure deployment name says nothing about the model. The tool path learns
// the shape the same way.
func TestThinkingShape_AzureDeploymentLearnsBudgetShapeOnToolPath(t *testing.T) {
	rec, srv := newThinkingShapeServer(t, thinkingTypeAdaptive, adaptiveRejectedBody)
	spec := thinkingSpec("prod-claude", "", nil)
	spec.Platform = azurePlatform
	spec.PlatformConfig = &providers.PlatformConfig{Type: azurePlatform, Endpoint: srv.URL}
	spec.Credential = &mockBearerCredential{}
	tp, ok := buildThinkingProvider(t, spec).(*ToolProvider)
	require.True(t, ok)

	_, _, err := tp.PredictWithTools(context.Background(), hiRequest(), nil, "")
	require.NoError(t, err)
	_, _, err = tp.PredictWithTools(context.Background(), hiRequest(), nil, "")
	require.NoError(t, err)

	assert.Equal(t, []map[string]any{adaptiveShape, budgetShape, budgetShape}, rec.sent())
}

// The other direction: the heuristic guesses the budget shape for a name that
// looks like 4.5, but the model behind it only takes adaptive. The streaming
// path retries with adaptive and keeps it.
func TestThinkingShape_StreamLearnsAdaptiveShape(t *testing.T) {
	for name, stream := range map[string]func(*ToolProvider) (<-chan providers.StreamChunk, error){
		"PredictStream": func(tp *ToolProvider) (<-chan providers.StreamChunk, error) {
			return tp.PredictStream(context.Background(), hiRequest())
		},
		"PredictStreamWithTools": func(tp *ToolProvider) (<-chan providers.StreamChunk, error) {
			return tp.PredictStreamWithTools(context.Background(), hiRequest(), nil, "")
		},
	} {
		t.Run(name, func(t *testing.T) {
			rec, srv := newThinkingShapeServer(t, thinkingTypeEnabled, enabledRejectedBody)
			tp, ok := buildThinkingProvider(t, thinkingSpec("claude-sonnet-4-5-alias", srv.URL, nil)).(*ToolProvider)
			require.True(t, ok)

			var text string
			for i := 0; i < 2; i++ {
				ch, err := stream(tp)
				require.NoError(t, err)
				for chunk := range ch {
					text += chunk.Delta
				}
			}

			assert.Equal(t, "hihi", text)
			assert.Equal(t, []map[string]any{budgetShape, adaptiveShape, adaptiveShape}, rec.sent())
		})
	}
}

// Existing direct-Anthropic configs send exactly what they sent before, with no
// extra round trip.
func TestThinkingShape_NameHeuristicStillFirstGuess(t *testing.T) {
	for _, tc := range []struct {
		model string
		want  map[string]any
	}{
		{"claude-sonnet-4-5", budgetShape},
		{"claude-sonnet-5", adaptiveShape},
	} {
		t.Run(tc.model, func(t *testing.T) {
			rec, srv := newThinkingShapeServer(t, "none", "")
			p := buildThinkingProvider(t, thinkingSpec(tc.model, srv.URL, nil))
			_, err := p.Predict(context.Background(), hiRequest())
			require.NoError(t, err)
			assert.Equal(t, []map[string]any{tc.want}, rec.sent())
		})
	}
}

// A declared thinking_mode wins over the name, and over what a 400 would teach:
// the provider sends the declared shape and never retries with the other one.
func TestThinkingShape_DeclaredModeWins(t *testing.T) {
	t.Run("budget on a name the heuristic calls adaptive", func(t *testing.T) {
		rec, srv := newThinkingShapeServer(t, thinkingTypeAdaptive, adaptiveRejectedBody)
		p := buildThinkingProvider(t, thinkingSpec("anthropic.claude-haiku-4-5-20251001-v1:0", srv.URL,
			map[string]any{"thinking_mode": "budget"}))
		_, err := p.Predict(context.Background(), hiRequest())
		require.NoError(t, err)
		assert.Equal(t, []map[string]any{budgetShape}, rec.sent())
	})
	t.Run("adaptive is not second-guessed by a 400", func(t *testing.T) {
		rec, srv := newThinkingShapeServer(t, thinkingTypeAdaptive, adaptiveRejectedBody)
		p := buildThinkingProvider(t, thinkingSpec("claude-sonnet-4-5", srv.URL,
			map[string]any{"thinking_mode": "adaptive"}))
		_, err := p.Predict(context.Background(), hiRequest())
		require.ErrorContains(t, err, "adaptive thinking is not supported on this model")
		assert.Equal(t, []map[string]any{adaptiveShape}, rec.sent())
	})
}

// Only the thinking-shape 400 is learned from; any other 400 is returned as is.
func TestThinkingShape_OtherBadRequestIsNotRetried(t *testing.T) {
	rec, srv := newThinkingShapeServer(t, thinkingTypeAdaptive,
		`{"type":"error","error":{"type":"invalid_request_error","message":"messages: field required"}}`)
	p := buildThinkingProvider(t, thinkingSpec("prod-claude", srv.URL, nil))
	_, err := p.Predict(context.Background(), hiRequest())
	require.ErrorContains(t, err, "messages: field required")
	assert.Equal(t, []map[string]any{adaptiveShape}, rec.sent())
}

func TestThinkingShape_InvalidModeRefused(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	_, err := providers.CreateProviderFromSpec(thinkingSpec("claude-sonnet-5", "https://example.invalid",
		map[string]any{"thinking_mode": "legacy"}))
	require.EqualError(t, err,
		`claude: additional_config.thinking_mode must be "adaptive" or "budget", got "legacy"`)
}
