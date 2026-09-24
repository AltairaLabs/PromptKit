package topiccontrol_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/classify"
	"github.com/AltairaLabs/PromptKit/runtime/v2/classify/backends/topiccontrol"
)

type capturedRequest struct {
	Model    string `json:"model"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
	Temperature float64 `json:"temperature"`
}

// newFakeNIM serves one canned label and records the request it was sent.
//
// The response is built with json.Marshal rather than string concatenation:
// two of the ParseLabel cases below carry an embedded newline and embedded
// quotes, and splicing those into a JSON string literal by hand produces
// invalid JSON instead of exercising the parser.
func newFakeNIM(t *testing.T, label string, got *capturedRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(body, got))

		resp, err := json.Marshal(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": label}},
			},
		})
		require.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp)
	}))
}

func TestClient_ClassifyTopic_SendsPolicyAsSystemAndMessageLast(t *testing.T) {
	var got capturedRequest
	srv := newFakeNIM(t, "on-topic", &got)
	defer srv.Close()

	c, err := topiccontrol.New(topiccontrol.Config{BaseURL: srv.URL})
	require.NoError(t, err)

	res, err := c.ClassifyTopic(context.Background(), classify.TopicRequest{
		Policy: samplePolicy(),
		History: []classify.TopicTurn{
			{Role: "user", Text: "Can Omnia run on OpenShift?"},
			{Role: "assistant", Text: "Yes, on 4.14 and later."},
		},
		Message: "What about Azure?",
	})
	require.NoError(t, err)
	assert.Equal(t, classify.TopicAllow, res.Decision)
	assert.Nil(t, res.Confidence, "a label-emitting model must not manufacture a confidence")

	require.Len(t, got.Messages, 4)
	assert.Equal(t, "system", got.Messages[0].Role)
	assert.Contains(t, got.Messages[0].Content, "Omnia and PromptKit")
	assert.Equal(t, "user", got.Messages[1].Role)
	assert.Equal(t, "assistant", got.Messages[2].Role)
	assert.Equal(t, "user", got.Messages[3].Role)
	assert.Equal(t, "What about Azure?", got.Messages[3].Content,
		"the message under judgment must be last")
	assert.Equal(t, "nvidia/llama-3.1-nemoguard-8b-topic-control", got.Model)
	assert.Zero(t, got.Temperature, "classification must be deterministic")
}

func TestClient_ClassifyTopic_ParsesLabels(t *testing.T) {
	cases := []struct {
		name  string
		label string
		want  classify.TopicDecision
	}{
		{"on topic", "on-topic", classify.TopicAllow},
		{"off topic", "off-topic", classify.TopicDeny},
		{"padded and capitalized", "  Off-Topic\n", classify.TopicDeny},
		{"quoted", `"on-topic"`, classify.TopicAllow},
		{"trailing period", "off-topic.", classify.TopicDeny},
		{"prose the model was not supposed to emit", "I think this is fine", classify.TopicUnknown},
		{"empty", "", classify.TopicUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got capturedRequest
			srv := newFakeNIM(t, tc.label, &got)
			defer srv.Close()

			c, err := topiccontrol.New(topiccontrol.Config{BaseURL: srv.URL})
			require.NoError(t, err)

			res, err := c.ClassifyTopic(context.Background(), classify.TopicRequest{
				Policy:  samplePolicy(),
				Message: "anything",
			})
			require.NoError(t, err)
			assert.Equal(t, tc.want, res.Decision)
			assert.Equal(t, tc.label, res.Raw, "Raw must carry the label exactly as returned")
		})
	}
}

func TestClient_ClassifyTopic_ErrorsOnHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	c, err := topiccontrol.New(topiccontrol.Config{BaseURL: srv.URL})
	require.NoError(t, err)

	_, err = c.ClassifyTopic(context.Background(), classify.TopicRequest{
		Policy:  samplePolicy(),
		Message: "anything",
	})
	require.Error(t, err, "a transport failure must be an error, not a silent allow")
	assert.Contains(t, err.Error(), "500")
}

func TestNew_RequiresBaseURL(t *testing.T) {
	_, err := topiccontrol.New(topiccontrol.Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "base_url")
}

// TestFactoryRegistered proves the provider type reaches classify's factory
// registry — the gate a constructor test cannot see.
func TestFactoryRegistered(t *testing.T) {
	assert.Contains(t, classify.RegisteredTypes(), "nvidia-topic-control")

	backend, err := classify.CreateFromSpec(classify.ProviderSpec{
		ID:      "topic-control",
		Type:    "nvidia-topic-control",
		BaseURL: "http://localhost:8000/v1",
	})
	require.NoError(t, err)

	_, ok := backend.(classify.TopicClassifier)
	assert.True(t, ok, "the constructed backend must satisfy TopicClassifier")
}

// --- Timeout configuration -------------------------------------------------

// TestNew_TimeoutDefaultsAndOverrides pins that the per-call timeout is
// configurable. It matters because a timeout is an error, on_error denies, and
// a NIM answering its first request from cold can exceed a short default — so
// a too-tight timeout takes the conversation offline rather than leaking
// traffic. Observed live: a 20s default tripped on a slow shared endpoint.
func TestNew_TimeoutDefaultsAndOverrides(t *testing.T) {
	def, err := topiccontrol.New(topiccontrol.Config{BaseURL: "http://x/v1"})
	require.NoError(t, err)
	assert.Equal(t, 20*time.Second, def.HTTPTimeout(), "default must stay 20s")

	custom, err := topiccontrol.New(topiccontrol.Config{BaseURL: "http://x/v1", Timeout: 90 * time.Second})
	require.NoError(t, err)
	assert.Equal(t, 90*time.Second, custom.HTTPTimeout())

	zero, err := topiccontrol.New(topiccontrol.Config{BaseURL: "http://x/v1", Timeout: 0})
	require.NoError(t, err)
	assert.Equal(t, 20*time.Second, zero.HTTPTimeout(), "zero must fall back, not disable the timeout")
}

// TestFactory_ReadsTimeoutFromAdditionalConfig proves the knob is reachable
// from a provider file and not just from Go. A config field nothing parses is
// the inert-declaration shape this repo keeps rediscovering, so the assertion
// goes through CreateFromSpec rather than calling New directly.
func TestFactory_ReadsTimeoutFromAdditionalConfig(t *testing.T) {
	cases := []struct {
		name string
		raw  any
		want time.Duration
	}{
		{"int", 90, 90 * time.Second},
		{"float", 45.5, 45500 * time.Millisecond},
		{"absent", nil, 20 * time.Second},
		{"wrong type falls back", "sixty", 20 * time.Second},
		{"non-positive falls back", 0, 20 * time.Second},
		{"negative falls back", -5, 20 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := map[string]any{}
			if tc.raw != nil {
				cfg["timeout_seconds"] = tc.raw
			}
			backend, err := classify.CreateFromSpec(classify.ProviderSpec{
				ID:               "topic-control",
				Type:             "nvidia-topic-control",
				BaseURL:          "http://localhost:8000/v1",
				AdditionalConfig: cfg,
			})
			require.NoError(t, err)

			client, ok := backend.(*topiccontrol.Client)
			require.True(t, ok)
			assert.Equal(t, tc.want, client.HTTPTimeout())
		})
	}
}
