package integration

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/mock"
	"github.com/AltairaLabs/PromptKit/sdk"
)

// recordingProvider wraps a mock provider and records the last prediction
// request so tests can assert on the rendered system prompt and user turn.
type recordingProvider struct {
	*mock.Provider
	mu      sync.Mutex
	lastReq providers.PredictionRequest
}

func newRecordingProvider() *recordingProvider {
	return &recordingProvider{Provider: mock.NewProvider("rec", "rec-model", false)}
}

func (r *recordingProvider) Predict(
	ctx context.Context, req providers.PredictionRequest,
) (providers.PredictionResponse, error) {
	r.mu.Lock()
	r.lastReq = req
	r.mu.Unlock()
	return r.Provider.Predict(ctx, req)
}

func (r *recordingProvider) PredictStream(
	ctx context.Context, req providers.PredictionRequest,
) (<-chan providers.StreamChunk, error) {
	r.mu.Lock()
	r.lastReq = req
	r.mu.Unlock()
	return r.Provider.PredictStream(ctx, req)
}

func (r *recordingProvider) system() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastReq.System
}

// userText returns the concatenated text of the last user-role message.
func (r *recordingProvider) userText() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var sb strings.Builder
	for i := range r.lastReq.Messages {
		m := r.lastReq.Messages[i]
		if m.Role != "user" {
			continue
		}
		if m.Content != "" {
			sb.WriteString(m.Content)
		}
		for _, p := range m.Parts {
			if p.Type == "text" && p.Text != nil {
				sb.WriteString(*p.Text)
			}
		}
	}
	return sb.String()
}

func jsonInputPack(systemTemplate string) string {
	return `{
		"id": "json-input-test",
		"version": "1.0.0",
		"description": "Pack for WithJSONInput integration tests",
		"prompts": {
			"fn": {
				"id": "fn",
				"name": "Function",
				"system_template": ` + quote(systemTemplate) + `
			}
		}
	}`
}

// quote returns a JSON-safe quoted string.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

func openRecordingConv(t *testing.T, systemTemplate string, opts ...sdk.Option) (*sdk.Conversation, *recordingProvider) {
	t.Helper()
	rec := newRecordingProvider()
	allOpts := append([]sdk.Option{sdk.WithProvider(rec), sdk.WithSkipSchemaValidation()}, opts...)
	conv := openTestConvWithPack(t, jsonInputPack(systemTemplate), "fn", allOpts...)
	return conv, rec
}

func TestJSONInput_BindsFieldToSystemPrompt(t *testing.T) {
	conv, rec := openRecordingConv(t, "topic={{topic}}")
	_, err := conv.Send(context.Background(), "", sdk.WithJSONInput(map[string]any{"topic": "batteries"}))
	require.NoError(t, err)
	assert.Contains(t, rec.system(), "topic=batteries")
}

func TestJSONInput_WholeObjectBoundToInputVar(t *testing.T) {
	conv, rec := openRecordingConv(t, "payload={{input}}")
	_, err := conv.Send(context.Background(), "", sdk.WithJSONInput(map[string]any{"a": 1}))
	require.NoError(t, err)
	assert.Contains(t, rec.system(), `payload={"a":1}`)
}

func TestJSONInput_OverridesWithVariablesDefault(t *testing.T) {
	conv, rec := openRecordingConv(t, "topic={{topic}}",
		sdk.WithVariables(map[string]string{"topic": "default"}))
	_, err := conv.Send(context.Background(), "", sdk.WithJSONInput(map[string]any{"topic": "override"}))
	require.NoError(t, err)
	assert.Contains(t, rec.system(), "topic=override")
	assert.NotContains(t, rec.system(), "topic=default")
}

func TestJSONInput_EmptyMessageFallsBackToJSONUserTurn(t *testing.T) {
	conv, rec := openRecordingConv(t, "static prompt")
	_, err := conv.Send(context.Background(), "", sdk.WithJSONInput(map[string]any{"topic": "x"}))
	require.NoError(t, err)
	assert.JSONEq(t, `{"topic":"x"}`, rec.userText())
}

func TestJSONInput_NonEmptyMessagePreservedWithBinding(t *testing.T) {
	conv, rec := openRecordingConv(t, "topic={{topic}}")
	_, err := conv.Send(context.Background(), "hello there", sdk.WithJSONInput(map[string]any{"topic": "x"}))
	require.NoError(t, err)
	assert.Equal(t, "hello there", rec.userText())
	assert.Contains(t, rec.system(), "topic=x")
}

func TestJSONInput_StreamBindsField(t *testing.T) {
	conv, rec := openRecordingConv(t, "topic={{topic}}")
	for chunk := range conv.Stream(context.Background(), "", sdk.WithJSONInput(map[string]any{"topic": "wind"})) {
		require.NoError(t, chunk.Error)
	}
	assert.Contains(t, rec.system(), "topic=wind")
}
