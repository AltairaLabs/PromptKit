package bedrock_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/providers/bedrock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capture records what the fake Bedrock endpoint received, so tests can assert
// on the wire format rather than on a mock's call count.
type capture struct {
	paths  []string
	bodies []map[string]any
}

func fakeBedrock(t *testing.T, respond func(i int) string) (*httptest.Server, *capture) {
	t.Helper()
	c := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		c.paths = append(c.paths, r.URL.Path)
		c.bodies = append(c.bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respond(len(c.bodies) - 1)))
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

func titanProvider(t *testing.T, url string) *bedrock.EmbeddingProvider {
	t.Helper()
	p, err := bedrock.NewEmbeddingProvider(
		bedrock.WithModel("amazon.titan-embed-text-v2:0"),
		bedrock.WithBaseURL(url),
		bedrock.WithPlatformAuth(),
	)
	require.NoError(t, err)
	return p
}

func cohereProvider(t *testing.T, url string) *bedrock.EmbeddingProvider {
	t.Helper()
	p, err := bedrock.NewEmbeddingProvider(
		bedrock.WithModel("cohere.embed-english-v3"),
		bedrock.WithBaseURL(url),
		bedrock.WithPlatformAuth(),
	)
	require.NoError(t, err)
	return p
}

// Titan takes a single string under "inputText" — not OpenAI's {"model","input"}
// and not Cohere's {"texts"}. Sending the wrong shape is the failure this whole
// provider exists to avoid, so it is asserted on the decoded request body.
func TestTitan_RequestUsesInputTextShape(t *testing.T) {
	srv, c := fakeBedrock(t, func(int) string { return `{"embedding":[0.5,0.25],"inputTextTokenCount":3}` })

	_, err := titanProvider(t, srv.URL).Embed(context.Background(),
		providers.EmbeddingRequest{Texts: []string{"hello"}})

	require.NoError(t, err)
	require.Len(t, c.bodies, 1)
	assert.Equal(t, "hello", c.bodies[0]["inputText"])
	assert.NotContains(t, c.bodies[0], "input", "must not send the OpenAI field")
	assert.NotContains(t, c.bodies[0], "texts", "must not send the Cohere field")
}

func TestTitan_ParsesEmbeddingAndTokenCount(t *testing.T) {
	srv, _ := fakeBedrock(t, func(int) string { return `{"embedding":[0.5,0.25],"inputTextTokenCount":3}` })

	got, err := titanProvider(t, srv.URL).Embed(context.Background(),
		providers.EmbeddingRequest{Texts: []string{"hello"}})

	require.NoError(t, err)
	assert.Equal(t, [][]float32{{0.5, 0.25}}, got.Embeddings)
	require.NotNil(t, got.Usage)
	assert.Equal(t, 3, got.Usage.TotalTokens)
}

// Titan embeds one text per call, so a multi-text request must fan out and
// reassemble in order. A provider that sent only the first text, or that
// collected results out of order, fails here.
func TestTitan_FansOutOneCallPerTextPreservingOrder(t *testing.T) {
	vectors := []string{
		`{"embedding":[1],"inputTextTokenCount":1}`,
		`{"embedding":[2],"inputTextTokenCount":2}`,
		`{"embedding":[3],"inputTextTokenCount":4}`,
	}
	srv, c := fakeBedrock(t, func(i int) string { return vectors[i] })

	got, err := titanProvider(t, srv.URL).Embed(context.Background(),
		providers.EmbeddingRequest{Texts: []string{"a", "b", "c"}})

	require.NoError(t, err)
	assert.Equal(t, [][]float32{{1}, {2}, {3}}, got.Embeddings)
	require.Len(t, c.bodies, 3, "one call per text")
	assert.Equal(t, []any{"a", "b", "c"},
		[]any{c.bodies[0]["inputText"], c.bodies[1]["inputText"], c.bodies[2]["inputText"]})
	assert.Equal(t, 7, got.Usage.TotalTokens, "token counts must sum across calls")
}

// The Bedrock invoke path carries the model id, including its ":" version
// suffix. A provider that posted to a bare host, or dropped the model, fails.
func TestTitan_PostsToModelInvokePath(t *testing.T) {
	srv, c := fakeBedrock(t, func(int) string { return `{"embedding":[1],"inputTextTokenCount":1}` })

	_, err := titanProvider(t, srv.URL).Embed(context.Background(),
		providers.EmbeddingRequest{Texts: []string{"hello"}})

	require.NoError(t, err)
	assert.Equal(t, "/model/amazon.titan-embed-text-v2:0/invoke", c.paths[0])
}

// A per-request model override must retarget the invoke path, since the model
// lives in the URL rather than the body.
func TestTitan_RequestModelOverrideRetargetsPath(t *testing.T) {
	srv, c := fakeBedrock(t, func(int) string { return `{"embedding":[1],"inputTextTokenCount":1}` })

	_, err := titanProvider(t, srv.URL).Embed(context.Background(),
		providers.EmbeddingRequest{Texts: []string{"hello"}, Model: "amazon.titan-embed-text-v1"})

	require.NoError(t, err)
	assert.Equal(t, "/model/amazon.titan-embed-text-v1/invoke", c.paths[0])
}

// Cohere is natively batched and requires input_type; it must not be driven
// through the Titan path.
func TestCohere_RequestUsesTextsShapeWithInputType(t *testing.T) {
	srv, c := fakeBedrock(t, func(int) string { return `{"embeddings":[[1,2],[3,4]]}` })

	_, err := cohereProvider(t, srv.URL).Embed(context.Background(),
		providers.EmbeddingRequest{Texts: []string{"a", "b"}})

	require.NoError(t, err)
	require.Len(t, c.bodies, 1, "cohere batches — one call for both texts")
	assert.Equal(t, []any{"a", "b"}, c.bodies[0]["texts"])
	assert.NotEmpty(t, c.bodies[0]["input_type"], "cohere rejects a request without input_type")
	assert.NotContains(t, c.bodies[0], "inputText", "must not send the Titan field")
}

func TestCohere_ParsesEmbeddingsInOrder(t *testing.T) {
	srv, _ := fakeBedrock(t, func(int) string { return `{"embeddings":[[1,2],[3,4]]}` })

	got, err := cohereProvider(t, srv.URL).Embed(context.Background(),
		providers.EmbeddingRequest{Texts: []string{"a", "b"}})

	require.NoError(t, err)
	assert.Equal(t, [][]float32{{1, 2}, {3, 4}}, got.Embeddings)
}

func TestCohere_InputTypeIsConfigurable(t *testing.T) {
	srv, c := fakeBedrock(t, func(int) string { return `{"embeddings":[[1]]}` })
	p, err := bedrock.NewEmbeddingProvider(
		bedrock.WithModel("cohere.embed-english-v3"),
		bedrock.WithBaseURL(srv.URL),
		bedrock.WithPlatformAuth(),
		bedrock.WithInputType("search_query"),
	)
	require.NoError(t, err)

	_, err = p.Embed(context.Background(), providers.EmbeddingRequest{Texts: []string{"a"}})

	require.NoError(t, err)
	assert.Equal(t, "search_query", c.bodies[0]["input_type"])
}

// Batch sizes differ by family; a single shared constant would be wrong for one
// of them and would drive callers into malformed requests.
func TestMaxBatchSizeDiffersByModelFamily(t *testing.T) {
	assert.Equal(t, 1, titanProvider(t, "http://unused").MaxBatchSize(),
		"titan embeds one text per call")
	assert.Greater(t, cohereProvider(t, "http://unused").MaxBatchSize(), 1,
		"cohere batches natively")
}

func TestUnknownModelFamilyIsRejectedAtConstruction(t *testing.T) {
	_, err := bedrock.NewEmbeddingProvider(
		bedrock.WithModel("meta.llama3-70b-instruct-v1:0"),
		bedrock.WithPlatformAuth(),
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "meta.llama3-70b-instruct-v1:0",
		"error must name the model so a misconfiguration is diagnosable")
}

func TestEmptyRequestMakesNoCall(t *testing.T) {
	srv, c := fakeBedrock(t, func(int) string { return `{"embedding":[1]}` })

	got, err := titanProvider(t, srv.URL).Embed(context.Background(),
		providers.EmbeddingRequest{Texts: nil})

	require.NoError(t, err)
	assert.Empty(t, got.Embeddings)
	assert.Empty(t, c.bodies, "no HTTP call for an empty request")
}

func TestHTTPErrorIsSurfaced(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"not authorized"}`))
	}))
	t.Cleanup(srv.Close)

	_, err := titanProvider(t, srv.URL).Embed(context.Background(),
		providers.EmbeddingRequest{Texts: []string{"hello"}})

	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "not authorized")
}

// A partial failure must not be reported as success with a short vector set.
func TestTitan_FanOutStopsOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	got, err := titanProvider(t, srv.URL).Embed(context.Background(),
		providers.EmbeddingRequest{Texts: []string{"a", "b"}})

	require.Error(t, err)
	assert.Empty(t, got.Embeddings, "a failed fan-out must not return partial results")
}

// Titan v1 emits 1536, so its default must not be the v2 default.
func TestDimensionsFollowModelVersion(t *testing.T) {
	v1, err := bedrock.NewEmbeddingProvider(
		bedrock.WithModel("amazon.titan-embed-text-v1"), bedrock.WithPlatformAuth())
	require.NoError(t, err)
	v2, err := bedrock.NewEmbeddingProvider(
		bedrock.WithModel("amazon.titan-embed-text-v2:0"), bedrock.WithPlatformAuth())
	require.NoError(t, err)

	assert.NotEqual(t, v1.EmbeddingDimensions(), v2.EmbeddingDimensions())
}

// An explicit override must survive even when it equals another family's
// default — otherwise "did the caller set this?" is confused with "is this the
// default value?", and the override is silently discarded.
func TestExplicitDimensionsOverrideFamilyDefault(t *testing.T) {
	p, err := bedrock.NewEmbeddingProvider(
		bedrock.WithModel("amazon.titan-embed-text-v1"),
		bedrock.WithDimensions(1024),
		bedrock.WithPlatformAuth(),
	)
	require.NoError(t, err)

	assert.Equal(t, 1024, p.EmbeddingDimensions())
}

func TestDimensionsDefaultPerFamily(t *testing.T) {
	assert.Positive(t, titanProvider(t, "http://unused").EmbeddingDimensions())
	assert.Positive(t, cohereProvider(t, "http://unused").EmbeddingDimensions())
}

// Interface compliance is asserted at compile time in embedding.go
// (var _ providers.EmbeddingProvider = (*EmbeddingProvider)(nil)); a runtime
// test of the same thing can never fail, so it is not repeated here.
