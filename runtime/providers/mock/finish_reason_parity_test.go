package mock

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The mock stands in for a real provider in consumers' tests, so it must
// report finish_reason in the canonical vocabulary the real providers
// normalize to (types.FinishReason*): "stop" for text, "tool_use" when the
// turn calls tools, on the unary and the streaming paths alike. A consumer
// that branches on finish_reason, or asserts on it, otherwise passes against
// the mock and fails against every real provider.
func TestMock_FinishReasonMatchesRealProviders(t *testing.T) {
	tp := mustScriptedToolProvider(t, `
scenarios:
  s:
    turns:
      1:
        tool_calls:
          - name: get_weather
            arguments: {city: Paris}
      2: "It is sunny."
`)

	req := func(turn int) providers.PredictionRequest {
		return providers.PredictionRequest{
			Messages: []types.Message{{Role: "user", Content: "weather?"}},
			Metadata: map[string]interface{}{"mock_scenario_id": "s", "mock_turn_number": turn},
		}
	}
	finalStreamReason := func(t *testing.T, ch <-chan providers.StreamChunk) string {
		t.Helper()
		var reason string
		for c := range ch {
			if c.FinishReason != nil {
				reason = *c.FinishReason
			}
		}
		return reason
	}

	cases := []struct {
		name string
		turn int
		want string
	}{
		{"tool call turn", 1, types.FinishReasonToolUse},
		{"text turn", 2, types.FinishReasonStop},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/PredictWithTools", func(t *testing.T) {
			resp, _, err := tp.PredictWithTools(context.Background(), req(tc.turn), nil, "auto")
			require.NoError(t, err)
			assert.Equal(t, tc.want, resp.FinishReason)
		})
		t.Run(tc.name+"/PredictStreamWithTools", func(t *testing.T) {
			ch, err := tp.PredictStreamWithTools(context.Background(), req(tc.turn), nil, "auto")
			require.NoError(t, err)
			assert.Equal(t, tc.want, finalStreamReason(t, ch))
		})
	}

	t.Run("text turn/Predict", func(t *testing.T) {
		resp, err := tp.Predict(context.Background(), req(2))
		require.NoError(t, err)
		assert.Equal(t, types.FinishReasonStop, resp.FinishReason)
	})
	t.Run("text turn/PredictStream", func(t *testing.T) {
		ch, err := tp.PredictStream(context.Background(), req(2))
		require.NoError(t, err)
		assert.Equal(t, types.FinishReasonStop, finalStreamReason(t, ch))
	})
}

// mustScriptedToolProvider builds a ToolProvider over a file repository
// holding the given mock config.
func mustScriptedToolProvider(t *testing.T, yaml string) *ToolProvider {
	t.Helper()
	cfg := filepath.Join(t.TempDir(), "mock.yaml")
	require.NoError(t, os.WriteFile(cfg, []byte(yaml), 0o600))
	repo, err := NewFileMockRepository(cfg)
	require.NoError(t, err)
	return NewToolProviderWithRepository("m", "model", false, repo)
}
