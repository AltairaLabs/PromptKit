package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A caller who asks for a strict response_format asked for strict mode on
// purpose. A schema strict mode cannot express is refused before the request
// is sent, naming the keyword and where it sits, rather than 400ing at OpenAI
// (a refused keyword) or being rewritten into a different contract (a
// map-typed object closed to accept no keys). A schema strict mode can express
// is still sent strict, with the standard strict rewrite.

// strictRefusalCall is one way a turn reaches the wire.
type strictRefusalCall struct {
	name string
	call func(ctx context.Context, tp *ToolProvider, req providers.PredictionRequest) error
}

// strictRefusalCalls is every public entry point that can send a response
// format: plain and tool turns, buffered and streamed.
var strictRefusalCalls = []strictRefusalCall{
	{"Predict", func(ctx context.Context, tp *ToolProvider, req providers.PredictionRequest) error {
		_, err := tp.Predict(ctx, req)
		return err
	}},
	{"PredictStream", func(ctx context.Context, tp *ToolProvider, req providers.PredictionRequest) error {
		ch, err := tp.PredictStream(ctx, req)
		drainStrictRefusal(ch)
		return err
	}},
	{"PredictWithTools", func(ctx context.Context, tp *ToolProvider, req providers.PredictionRequest) error {
		tools, err := tp.BuildTooling([]*providers.ToolDescriptor{
			desc("get_weather", `{"type":"object","properties":{"city":{"type":"string"}}}`),
		})
		if err != nil {
			return err
		}
		_, _, err = tp.PredictWithTools(ctx, req, tools, "auto")
		return err
	}},
	{"PredictStreamWithTools", func(ctx context.Context, tp *ToolProvider, req providers.PredictionRequest) error {
		tools, err := tp.BuildTooling([]*providers.ToolDescriptor{
			desc("get_weather", `{"type":"object","properties":{"city":{"type":"string"}}}`),
		})
		if err != nil {
			return err
		}
		ch, err := tp.PredictStreamWithTools(ctx, req, tools, "auto")
		drainStrictRefusal(ch)
		return err
	}},
}

func drainStrictRefusal(ch <-chan providers.StreamChunk) {
	if ch == nil {
		return
	}
	for range ch { //nolint:revive // draining the stream so the server handler finishes
	}
}

// strictRefusalSpecs are the serializers the openai provider has: Chat
// Completions (OpenAI and Azure OpenAI) and the Responses API.
func strictRefusalSpecs() []struct {
	name string
	spec providers.ProviderSpec
} {
	return []struct {
		name string
		spec providers.ProviderSpec
	}{
		{"chat completions", openAISpec("completions")},
		{"responses", openAISpec("responses")},
		{"azure openai", azureSpec()},
	}
}

// strictRefusalServer records every request body it receives and answers the
// buffered or streamed shape of either API.
type strictRefusalServer struct {
	mu     sync.Mutex
	bodies [][]byte
}

func (s *strictRefusalServer) handle(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	s.mu.Lock()
	s.bodies = append(s.bodies, b)
	s.mu.Unlock()
	var req struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(b, &req)
	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		return
	}
	strictFallbackReply(w, r)
}

func (s *strictRefusalServer) requests() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.bodies...)
}

// newStrictRefusalProvider builds the provider spec describes against a
// recording server.
func newStrictRefusalProvider(t *testing.T, spec providers.ProviderSpec) (*ToolProvider, *strictRefusalServer) {
	t.Helper()
	rec := &strictRefusalServer{}
	srv := httptest.NewServer(http.HandlerFunc(rec.handle))
	t.Cleanup(srv.Close)
	spec.BaseURL = srv.URL
	if spec.PlatformConfig != nil {
		pc := *spec.PlatformConfig
		pc.Endpoint = srv.URL
		spec.PlatformConfig = &pc
	}
	p, err := providers.CreateProviderFromSpec(spec)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	tp, ok := p.(*ToolProvider)
	require.True(t, ok, "openai spec must build a ToolProvider, got %T", p)
	return tp, rec
}

func strictResponseRequest(schema string) providers.PredictionRequest {
	return providers.PredictionRequest{
		Messages: []types.Message{{Role: "user", Content: "hi"}},
		ResponseFormat: &providers.ResponseFormat{
			Type:       providers.ResponseFormatJSONSchema,
			JSONSchema: json.RawMessage(schema),
			SchemaName: "answer",
			Strict:     true,
		},
	}
}

func TestStrictResponseFormat_IncompatibleSchemaIsRefusedBeforeTheCall(t *testing.T) {
	cases := []struct {
		name, schema, wantErr string
	}{
		{
			// A keyword strict mode refuses: OpenAI would 400 the request.
			name: "minProperties",
			schema: `{"type":"object","properties":{"tags":{"type":"object","minProperties":1,` +
				`"properties":{"a":{"type":"string"}}}}}`,
			wantErr: `openai: response_format "answer" asks for strict mode, but strict mode cannot express ` +
				`its schema: "minProperties" at $.properties.tags is not supported in strict mode; ` +
				`change the schema or send the response format with strict: false`,
		},
		{
			// A map-typed object: the strict rewrite would close it to accept no keys.
			name: "map-typed object",
			schema: `{"type":"object","properties":{"labels":{"type":"object",` +
				`"additionalProperties":{"type":"string"}}}}`,
			wantErr: `openai: response_format "answer" asks for strict mode, but strict mode cannot express ` +
				`its schema: "additionalProperties" at $.properties.labels is not false (strict mode requires ` +
				`every object to be closed); change the schema or send the response format with strict: false`,
		},
		{
			// The path reaches into array lists by index.
			name: "oneOf inside an anyOf branch",
			schema: `{"type":"object","properties":{"v":{"anyOf":[{"type":"string"},` +
				`{"oneOf":[{"type":"string"},{"type":"number"}]}]}}}`,
			wantErr: `openai: response_format "answer" asks for strict mode, but strict mode cannot express ` +
				`its schema: "oneOf" at $.properties.v.anyOf[1] is not supported in strict mode; ` +
				`change the schema or send the response format with strict: false`,
		},
	}
	for _, tc := range cases {
		for _, sp := range strictRefusalSpecs() {
			for _, c := range strictRefusalCalls {
				t.Run(tc.name+"/"+sp.name+"/"+c.name, func(t *testing.T) {
					tp, rec := newStrictRefusalProvider(t, sp.spec)
					err := c.call(context.Background(), tp, strictResponseRequest(tc.schema))
					require.Error(t, err)
					assert.Equal(t, tc.wantErr, err.Error())
					assert.Empty(t, rec.requests(), "the refusal must come before any request is sent")
				})
			}
		}
	}
}

// strictCompatibleResponseSchema needs the strict rewrite (a nested object
// without additionalProperties) but nothing strict mode cannot express.
const strictCompatibleResponseSchema = `{"type":"object","properties":{` +
	`"items":{"type":"array","items":{"type":"object","properties":{"name":{"type":"string"}}}},` +
	`"when":{"type":"string","format":"date"}},"required":["items"]}`

const strictCompatibleResponseSchemaRewritten = `{"type":"object","additionalProperties":false,` +
	`"required":["items","when"],"properties":{` +
	`"items":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["name"],` +
	`"properties":{"name":{"type":"string"}}}},` +
	`"when":{"type":"string","format":"date"}}}`

func TestStrictResponseFormat_CompatibleSchemaIsSentStrictAndRewritten(t *testing.T) {
	for _, sp := range strictRefusalSpecs() {
		for _, c := range strictRefusalCalls {
			t.Run(sp.name+"/"+c.name, func(t *testing.T) {
				tp, rec := newStrictRefusalProvider(t, sp.spec)
				_ = c.call(context.Background(), tp, strictResponseRequest(strictCompatibleResponseSchema))
				reqs := rec.requests()
				require.Len(t, reqs, 1)

				var body struct {
					ResponseFormat json.RawMessage `json:"response_format"`
					Text           struct {
						Format json.RawMessage `json:"format"`
					} `json:"text"`
				}
				require.NoError(t, json.Unmarshal(reqs[0], &body))
				if sp.name == "responses" {
					assert.JSONEq(t, `{"type":"json_schema","json_schema":{"name":"answer","strict":true,`+
						`"schema":`+strictCompatibleResponseSchemaRewritten+`}}`, string(body.Text.Format))
					return
				}
				assert.JSONEq(t, `{"type":"json_schema","json_schema":{"name":"answer","strict":true,`+
					`"schema":`+strictCompatibleResponseSchemaRewritten+`}}`, string(body.ResponseFormat))
			})
		}
	}
}

// Without strict, the schema is guidance: nothing is refused and the schema
// goes out exactly as written, whatever it contains.
func TestStrictResponseFormat_NonStrictSchemaIsSentVerbatim(t *testing.T) {
	schema := `{"type":"object","properties":{"labels":{"type":"object","minProperties":1,` +
		`"additionalProperties":{"type":"string"}}}}`
	tp, rec := newStrictRefusalProvider(t, openAISpec("completions"))
	req := strictResponseRequest(schema)
	req.ResponseFormat.Strict = false
	_, err := tp.Predict(context.Background(), req)
	require.NoError(t, err)
	reqs := rec.requests()
	require.Len(t, reqs, 1)
	var body struct {
		ResponseFormat json.RawMessage `json:"response_format"`
	}
	require.NoError(t, json.Unmarshal(reqs[0], &body))
	assert.JSONEq(t, `{"type":"json_schema","json_schema":{"name":"answer","schema":`+schema+`}}`,
		string(body.ResponseFormat))
}
