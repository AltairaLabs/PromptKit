package openai_test

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/inference/openai"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeChatMessage/fakeChatRequest decode the wire body the Provider sends,
// so tests can assert on it without importing the package's unexported
// wire types.
type fakeChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type fakeChatRequest struct {
	Model       string            `json:"model"`
	Messages    []fakeChatMessage `json:"messages"`
	MaxTokens   int               `json:"max_tokens"`
	Temperature float64           `json:"temperature"`
	Logprobs    bool              `json:"logprobs"`
	TopLogprobs int               `json:"top_logprobs"`
}

// topLogprobsBody builds a chat-completions response carrying the given
// token->probability map as the single sampled position's top_logprobs.
func topLogprobsBody(t *testing.T, tokenProbs map[string]float64) string {
	t.Helper()
	type entry struct {
		Token   string  `json:"token"`
		Logprob float64 `json:"logprob"`
	}
	entries := make([]entry, 0, len(tokenProbs))
	var top string
	for tok, p := range tokenProbs {
		entries = append(entries, entry{Token: tok, Logprob: math.Log(p)})
		top = tok
	}
	body := map[string]any{
		"choices": []map[string]any{
			{
				"logprobs": map[string]any{
					"content": []map[string]any{
						{"token": top, "logprob": 0, "top_logprobs": entries},
					},
				},
			},
		},
		"usage": map[string]any{"prompt_tokens": 7},
	}
	b, err := json.Marshal(body)
	require.NoError(t, err)
	return string(b)
}

func TestInfer_TwoLabels_RenormalizesAndSortsHighestFirst(t *testing.T) {
	var captured fakeChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		_, _ = w.Write([]byte(topLogprobsBody(t, map[string]float64{"on": 0.7, "off": 0.3})))
	}))
	defer srv.Close()

	p, err := openai.New(openai.Config{BaseURL: srv.URL, Model: "gpt-test"})
	require.NoError(t, err)

	resp, err := p.Infer(context.Background(), inference.Request{
		Prompt: "classify this",
		Inputs: []types.Message{{Role: "user", Content: "hello"}},
		Labels: []string{"on-topic", "off-topic"},
	})
	require.NoError(t, err)

	require.Len(t, resp.Scores, 2)
	assert.Equal(t, "on-topic", resp.Scores[0].Label)
	assert.InDelta(t, 0.7, resp.Scores[0].Score, 0.001)
	assert.Equal(t, "off-topic", resp.Scores[1].Label)
	assert.InDelta(t, 0.3, resp.Scores[1].Score, 0.001)

	require.Len(t, captured.Messages, 3)
	assert.Equal(t, "system", captured.Messages[0].Role)
	assert.Equal(t, "classify this", captured.Messages[0].Content)
	assert.Equal(t, "user", captured.Messages[1].Role)
	assert.Equal(t, "hello", captured.Messages[1].Content)
	assert.Equal(t, "user", captured.Messages[2].Role)
	assert.Contains(t, captured.Messages[2].Content, "on-topic")
	assert.Contains(t, captured.Messages[2].Content, "off-topic")
	assert.Equal(t, 1, captured.MaxTokens)
	assert.InDelta(t, 0, captured.Temperature, 0.0001)
	assert.True(t, captured.Logprobs)
	assert.Equal(t, 20, captured.TopLogprobs)
	assert.Equal(t, "gpt-test", captured.Model)
}

func TestInfer_TooFewLabels_ErrLabelsRequired(t *testing.T) {
	p, err := openai.New(openai.Config{Model: "gpt-test"})
	require.NoError(t, err)

	for _, labels := range [][]string{nil, {"only-one"}} {
		_, err := p.Infer(context.Background(), inference.Request{Labels: labels})
		require.Error(t, err)
		assert.ErrorIs(t, err, inference.ErrLabelsRequired)
	}
}

func TestInfer_TooManyLabels_Errors(t *testing.T) {
	p, err := openai.New(openai.Config{Model: "gpt-test"})
	require.NoError(t, err)

	labels := make([]string, 11)
	for i := range labels {
		labels[i] = "label"
	}
	_, err = p.Infer(context.Background(), inference.Request{Labels: labels})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "10")
}

func TestInfer_SharedPrefixLabels_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(topLogprobsBody(t, map[string]float64{"on": 1.0})))
	}))
	defer srv.Close()

	p, err := openai.New(openai.Config{BaseURL: srv.URL, Model: "gpt-test"})
	require.NoError(t, err)

	_, err = p.Infer(context.Background(), inference.Request{Labels: []string{"on-topic", "on-hold"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "on-topic")
	assert.Contains(t, err.Error(), "on-hold")
}

func TestInfer_NoLabelInTopLogprobs_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(topLogprobsBody(t, map[string]float64{"xyz": 1.0})))
	}))
	defer srv.Close()

	p, err := openai.New(openai.Config{BaseURL: srv.URL, Model: "gpt-test"})
	require.NoError(t, err)

	_, err = p.Infer(context.Background(), inference.Request{Labels: []string{"on-topic", "off-topic"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "none of the labels")
}

func TestInfer_RetriesOn429ThenSucceeds(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(topLogprobsBody(t, map[string]float64{"on": 0.6, "off": 0.4})))
	}))
	defer srv.Close()

	p, err := openai.New(openai.Config{BaseURL: srv.URL, Model: "gpt-test"})
	require.NoError(t, err)

	resp, err := p.Infer(context.Background(), inference.Request{Labels: []string{"on-topic", "off-topic"}})
	require.NoError(t, err)
	assert.Equal(t, 2, requests)
	require.Len(t, resp.Scores, 2)
}

func TestInfer_NonRetryableStatus_ReturnsProviderHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()

	p, err := openai.New(openai.Config{BaseURL: srv.URL, Model: "gpt-test"})
	require.NoError(t, err)

	_, err = p.Infer(context.Background(), inference.Request{Labels: []string{"on-topic", "off-topic"}})
	require.Error(t, err)
	var httpErr *providers.ProviderHTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusBadRequest, httpErr.StatusCode)
}

func TestCreateFromSpec_NvidiaTopicControl_DefaultsModel(t *testing.T) {
	var captured fakeChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		_, _ = w.Write([]byte(topLogprobsBody(t, map[string]float64{"on": 1.0})))
	}))
	defer srv.Close()

	p, err := inference.CreateFromSpec(inference.ProviderSpec{Type: "nvidia-topic-control", BaseURL: srv.URL})
	require.NoError(t, err)

	_, err = p.Infer(context.Background(), inference.Request{Labels: []string{"on-topic", "off-topic"}})
	require.NoError(t, err)
	assert.Equal(t, openai.NemoGuardModel, captured.Model)
}

func TestCreateFromSpec_NvidiaTopicControl_RequiresBaseURL(t *testing.T) {
	_, err := inference.CreateFromSpec(inference.ProviderSpec{Type: "nvidia-topic-control"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "base_url")
}

func TestCreateFromSpec_OpenAI_RequiresModel(t *testing.T) {
	_, err := inference.CreateFromSpec(inference.ProviderSpec{Type: "openai"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model")
}

func TestCreateFromSpec_NvidiaTopicControl_HonorsExplicitModel(t *testing.T) {
	var captured fakeChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
		_, _ = w.Write([]byte(topLogprobsBody(t, map[string]float64{"on": 1.0})))
	}))
	defer srv.Close()

	p, err := inference.CreateFromSpec(inference.ProviderSpec{
		Type: "nvidia-topic-control", BaseURL: srv.URL, Model: "custom-model",
	})
	require.NoError(t, err)

	_, err = p.Infer(context.Background(), inference.Request{Labels: []string{"on-topic", "off-topic"}})
	require.NoError(t, err)
	assert.Equal(t, "custom-model", captured.Model)
}

// TestFactory_ReadsTimeoutFromAdditionalConfig proves the timeout_seconds
// knob is reachable from a provider file and not just from Go — the same
// shape of check as topiccontrol's TestFactory_ReadsTimeoutFromAdditionalConfig.
func TestFactory_ReadsTimeoutFromAdditionalConfig(t *testing.T) {
	cases := []struct {
		name string
		raw  any
		want time.Duration
	}{
		{"int", 10, 10 * time.Second},
		{"int64", int64(15), 15 * time.Second},
		{"float", 2.5, 2500 * time.Millisecond},
		{"absent", nil, 30 * time.Second},
		{"wrong type falls back", "sixty", 30 * time.Second},
		{"non-positive falls back", 0, 30 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := map[string]any{}
			if tc.raw != nil {
				cfg["timeout_seconds"] = tc.raw
			}
			p, err := inference.CreateFromSpec(inference.ProviderSpec{
				Type: "openai", Model: "gpt-test", AdditionalConfig: cfg,
			})
			require.NoError(t, err)

			provider, ok := p.(*openai.Provider)
			require.True(t, ok)
			assert.Equal(t, tc.want, provider.HTTPTimeout())
		})
	}
}
