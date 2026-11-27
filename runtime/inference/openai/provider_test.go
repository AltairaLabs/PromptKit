package openai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"

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
	best := -1.0
	for tok, p := range tokenProbs {
		entries = append(entries, entry{Token: tok, Logprob: math.Log(p)})
		// The sampled token at temperature 0 is the most probable one.
		if p > best || (p == best && tok < top) {
			top, best = tok, p
		}
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

	require.Len(t, captured.Messages, 2, "the prompt, then the input — nothing after the judged message")
	assert.Equal(t, "system", captured.Messages[0].Role)
	assert.Equal(t, "classify this", captured.Messages[0].Content)
	assert.Equal(t, "user", captured.Messages[1].Role)
	assert.Equal(t, "hello", captured.Messages[1].Content)
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

func TestInfer_NoLabelInTopLogprobs_ReturnsNoScores(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(topLogprobsBody(t, map[string]float64{"xyz": 1.0})))
	}))
	defer srv.Close()

	p, err := openai.New(openai.Config{BaseURL: srv.URL, Model: "gpt-test"})
	require.NoError(t, err)

	resp, err := p.Infer(context.Background(), inference.Request{Labels: []string{"on-topic", "off-topic"}})
	require.NoError(t, err)
	assert.Empty(t, resp.Scores, "no label anywhere is not a decision")
	assert.Equal(t, "xyz", resp.Raw)
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

	p, err := openai.New(openai.Config{BaseURL: srv.URL, Model: "gpt-test", RetryPolicy: fastRetryPolicy()})
	require.NoError(t, err)

	resp, err := p.Infer(context.Background(), inference.Request{Labels: []string{"on-topic", "off-topic"}})
	require.NoError(t, err)
	assert.Equal(t, 2, requests)
	require.Len(t, resp.Scores, 2)
}

// fastRetryPolicy keeps retry tests at millisecond backoff instead of
// providers.DefaultRetryPolicy()'s real one.
func fastRetryPolicy() *pipeline.RetryPolicy {
	return &pipeline.RetryPolicy{MaxRetries: 2, Backoff: "fixed", InitialDelayMs: 1}
}

// A configured policy replaces the default: with no retries, one 429 is the
// answer.
func TestInfer_ConfiguredRetryPolicyReplacesTheDefault(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	p, err := openai.New(openai.Config{BaseURL: srv.URL, Model: "gpt-test",
		RetryPolicy: &pipeline.RetryPolicy{MaxRetries: 0}})
	require.NoError(t, err)

	_, err = p.Infer(context.Background(), inference.Request{Labels: []string{"on-topic", "off-topic"}})
	require.Error(t, err)
	assert.Equal(t, 1, requests)
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

// A model talked into answering something other than a label must not be
// decided by the labels' leftover tail probability: that would let a message
// steer a guardrail with a non-answer. No scores means the caller's
// "no usable label" policy applies.
func TestInfer_SampledTokenIsNotALabel_ReturnsNoScores(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(topLogprobsBody(t, map[string]float64{"maybe": 0.95, "on": 0.03, "off": 0.02})))
	}))
	defer srv.Close()
	p, err := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})
	require.NoError(t, err)

	resp, err := p.Infer(context.Background(), inference.Request{
		Inputs: []types.Message{{Role: "user", Content: "hi"}},
		Labels: []string{"on-topic", "off-topic"},
	})

	require.NoError(t, err)
	assert.Empty(t, resp.Scores, "a non-label answer is not a decision")
	assert.Equal(t, "maybe", resp.Raw)
}

// The judged message must be the last chat turn: NemoGuard topic control is
// trained to judge the final user message, and a trailing instruction turn
// would become the thing it judges.
func TestInfer_JudgedInputIsTheLastMessage(t *testing.T) {
	for name, prompt := range map[string]string{"with a prompt": "Only discuss banking.", "without": ""} {
		t.Run(name, func(t *testing.T) {
			var captured fakeChatRequest
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.NoError(t, json.NewDecoder(r.Body).Decode(&captured))
				_, _ = w.Write([]byte(topLogprobsBody(t, map[string]float64{"on": 0.9, "off": 0.1})))
			}))
			defer srv.Close()
			p, err := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})
			require.NoError(t, err)

			_, err = p.Infer(context.Background(), inference.Request{
				Prompt: prompt,
				Inputs: []types.Message{{Role: "user", Content: "earlier"}, {Role: "user", Content: "judge me"}},
				Labels: []string{"on-topic", "off-topic"},
			})

			require.NoError(t, err)
			last := captured.Messages[len(captured.Messages)-1]
			assert.Equal(t, fakeChatMessage{Role: "user", Content: "judge me"}, last)
			if prompt != "" {
				assert.Equal(t, fakeChatMessage{Role: "system", Content: prompt}, captured.Messages[0],
					"a caller's prompt is sent verbatim: it names its own labels")
			} else {
				assert.Equal(t, "system", captured.Messages[0].Role)
				assert.Contains(t, captured.Messages[0].Content, "on-topic, off-topic")
			}
		})
	}
}

// The NemoGuard alias keeps topic control's 20s per-call default: at the 30s
// generic default a single hung attempt spends the whole guardrail budget and
// no retry can run.
func TestNemoGuardAlias_DefaultsToA20sCallTimeout(t *testing.T) {
	p, err := inference.CreateFromSpec(inference.ProviderSpec{Type: "nvidia-topic-control", BaseURL: "http://nim:8000/v1"})
	require.NoError(t, err)
	assert.Equal(t, 20*time.Second, p.(*openai.Provider).HTTPTimeout())

	p, err = inference.CreateFromSpec(inference.ProviderSpec{Type: "nvidia-topic-control", BaseURL: "http://nim:8000/v1",
		AdditionalConfig: map[string]any{"timeout_seconds": 5}})
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, p.(*openai.Provider).HTTPTimeout(), "timeout_seconds still overrides it")
}

// answerOnlyBody builds a chat-completions response that carries the model's
// answer as message text and no logprobs — what an endpoint that does not
// support logprobs (or ignores the request for them) returns. withNullLogprobs
// sends an explicit "logprobs": null, the shape OpenAI-compatible servers use;
// otherwise the field is absent.
func answerOnlyBody(t *testing.T, content string, withNullLogprobs bool) string {
	t.Helper()
	choice := map[string]any{
		"index":         0,
		"message":       map[string]any{"role": "assistant", "content": content},
		"finish_reason": "length",
	}
	if withNullLogprobs {
		choice["logprobs"] = nil
	}
	b, err := json.Marshal(map[string]any{
		"choices": []map[string]any{choice},
		"usage":   map[string]any{"prompt_tokens": 7},
	})
	require.NoError(t, err)
	return string(b)
}

// answerOnlyProvider builds the nvidia-topic-control provider from a spec, as a
// host's provider file does, against an endpoint that answers content with no
// logprobs.
func answerOnlyProvider(t *testing.T, content string, withNullLogprobs bool) inference.Provider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(answerOnlyBody(t, content, withNullLogprobs)))
	}))
	t.Cleanup(srv.Close)
	p, err := inference.CreateFromSpec(inference.ProviderSpec{Type: "nvidia-topic-control", BaseURL: srv.URL})
	require.NoError(t, err)
	return p
}

var topicLabels = []string{"on-topic", "off-topic"}

// An endpoint with no logprobs still answers the label as text. That answer is
// the decision: the answered label scores 1.0, every other label 0, and the
// call succeeds instead of failing every turn (#2924). Matching is
// case-insensitive, ignores surrounding whitespace and one layer of quotes or a
// trailing period, and accepts the full label or its first-token prefix
// (max_tokens is 1, so "on" is what a well-behaved model returns).
func TestInfer_NoLogprobs_TakesTheLabelFromTheAnswerText(t *testing.T) {
	onWins := []inference.LabelScore{{Label: "on-topic", Score: 1}, {Label: "off-topic", Score: 0}}
	offWins := []inference.LabelScore{{Label: "off-topic", Score: 1}, {Label: "on-topic", Score: 0}}
	cases := []struct {
		answer string
		want   []inference.LabelScore
	}{
		{"on", onWins},
		{"off", offWins},
		{"on-topic", onWins},
		{"off-topic", offWins},
		{"OFF-Topic", offWins},
		{"  On\n", onWins},
		{`"off-topic".`, offWins},
		{"'on'", onWins},
	}
	for _, tc := range cases {
		for _, nullField := range []bool{false, true} {
			t.Run(fmt.Sprintf("%q/null=%v", tc.answer, nullField), func(t *testing.T) {
				p := answerOnlyProvider(t, tc.answer, nullField)

				resp, err := p.Infer(context.Background(), inference.Request{
					Inputs: []types.Message{{Role: "user", Content: "Who should I vote for?"}},
					Labels: topicLabels,
				})

				require.NoError(t, err)
				assert.Equal(t, tc.want, resp.Scores)
				assert.Equal(t, tc.answer, resp.Raw, "the raw answer is kept for diagnostics")
				assert.Equal(t, openai.NemoGuardModel, resp.Model)
				assert.Equal(t, 7, resp.Usage.InputTokens)
			})
		}
	}
}

// Case is ignored on the label side too, and the scores carry the caller's
// spelling of the label, not the model's.
func TestInfer_NoLogprobs_MatchingIgnoresTheLabelsCase(t *testing.T) {
	p := answerOnlyProvider(t, "off-topic", true)

	resp, err := p.Infer(context.Background(), inference.Request{Labels: []string{"On-Topic", "Off-Topic"}})

	require.NoError(t, err)
	assert.Equal(t, []inference.LabelScore{{Label: "Off-Topic", Score: 1}, {Label: "On-Topic", Score: 0}}, resp.Scores)
}

// An answer that is not a label is not a decision: no scores, so the caller's
// "no usable label" policy applies (topic_policy's on_unknown, default deny) —
// the same rule as a sampled token that is not a label. Near misses are
// deliberately not accepted: matching never searches inside a sentence, so a
// model that talks instead of answering cannot steer the result.
func TestInfer_NoLogprobs_NonLabelAnswer_ReturnsNoScores(t *testing.T) {
	for _, answer := range []string{"maybe", "on topic", "It is off-topic", "onward", "topic", "yes", "on-topic!"} {
		t.Run(answer, func(t *testing.T) {
			p := answerOnlyProvider(t, answer, true)

			resp, err := p.Infer(context.Background(), inference.Request{Labels: topicLabels})

			require.NoError(t, err)
			assert.Empty(t, resp.Scores, "a non-label answer is not a decision")
			assert.Equal(t, answer, resp.Raw)
		})
	}
}

// No logprobs and no answer text is a broken response, not a decision.
func TestInfer_NoLogprobsAndNoAnswer_Errors(t *testing.T) {
	for _, answer := range []string{"", "  \n", `""`} {
		t.Run(fmt.Sprintf("%q", answer), func(t *testing.T) {
			p := answerOnlyProvider(t, answer, true)

			_, err := p.Infer(context.Background(), inference.Request{Labels: topicLabels})

			require.EqualError(t, err, "inference: response carried neither logprobs nor an answer")
		})
	}
}

func TestInfer_NoChoices_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()
	p, err := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})
	require.NoError(t, err)

	_, err = p.Infer(context.Background(), inference.Request{Labels: topicLabels})

	require.EqualError(t, err, "inference: response carried no choices")
}

// Labels that share a first-token prefix are as ambiguous in the answer text as
// in the logprobs: "on" could be either, so the call errors rather than guess.
func TestInfer_NoLogprobs_SharedPrefixLabels_Errors(t *testing.T) {
	p := answerOnlyProvider(t, "on-topic", true)

	_, err := p.Infer(context.Background(), inference.Request{Labels: []string{"on-topic", "on-hold"}})

	require.EqualError(t, err,
		`inference: labels on-topic, on-hold share the first-token prefix "on"; first-token matching cannot tell them apart`)
}

// When logprobs are present they decide, exactly as before: the answer text is
// not consulted, even when it disagrees.
func TestInfer_LogprobsPresent_IgnoresTheAnswerText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var body map[string]any
		require.NoError(t, json.Unmarshal([]byte(topLogprobsBody(t, map[string]float64{"on": 0.7, "off": 0.3})), &body))
		choice := body["choices"].([]any)[0].(map[string]any)
		choice["message"] = map[string]any{"role": "assistant", "content": "off"}
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()
	p, err := openai.New(openai.Config{BaseURL: srv.URL, Model: "m"})
	require.NoError(t, err)

	resp, err := p.Infer(context.Background(), inference.Request{Labels: topicLabels})

	require.NoError(t, err)
	require.Len(t, resp.Scores, 2)
	assert.Equal(t, "on-topic", resp.Scores[0].Label)
	assert.InDelta(t, 0.7, resp.Scores[0].Score, 0.001)
	assert.Equal(t, "off-topic", resp.Scores[1].Label)
	assert.InDelta(t, 0.3, resp.Scores[1].Score, 0.001)
	assert.Equal(t, "on", resp.Raw, "Raw is the sampled token, not the message text")
}
