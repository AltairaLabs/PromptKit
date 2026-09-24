package systemone

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// newTestProvider wires a Provider at an httptest server. Each test builds
// its own server with a custom handler so request shape can be asserted
// alongside the response decode.
func newTestProvider(t *testing.T, handler http.HandlerFunc) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p, err := New(Config{APIKey: "test-token", BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return p
}

// fastRetryPolicy is a transient-retry policy tests can inject via
// Provider.retryPolicy so exhausted-retry cases don't pay
// providers.DefaultRetryPolicy()'s real (500ms+) backoff. Production always
// uses providers.DefaultRetryPolicy(), set by New().
func fastRetryPolicy() pipeline.RetryPolicy {
	return pipeline.RetryPolicy{MaxRetries: 2, Backoff: "exponential", InitialDelayMs: 5}
}

func TestNew_RequiresBaseURL(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New must reject empty base_url")
	}
}

func TestNew_DefaultsModel(t *testing.T) {
	p, err := New(Config{BaseURL: "https://example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if p.model != DefaultModel {
		t.Errorf("model = %q, want %q", p.model, DefaultModel)
	}
}

func TestNew_UsesConfigModel(t *testing.T) {
	p, err := New(Config{BaseURL: "https://example.test", Model: "  jev-2  "})
	if err != nil {
		t.Fatal(err)
	}
	if p.model != "jev-2" {
		t.Errorf("model = %q, want %q (trimmed)", p.model, "jev-2")
	}
}

func TestNew_DefaultsToProvidersDefaultRetryPolicy(t *testing.T) {
	p, err := New(Config{BaseURL: "https://example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if p.retryPolicy != providers.DefaultRetryPolicy() {
		t.Errorf("retryPolicy = %+v, want providers.DefaultRetryPolicy() %+v",
			p.retryPolicy, providers.DefaultRetryPolicy())
	}
}

func TestInfer_RequestShape(t *testing.T) {
	var gotBody string
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		fmt.Fprintln(w, `{"model":"jev-latest","answers":{"q":{"type":"choice","choice":"on-topic",
			"probabilities":{"on-topic":0.9,"off-topic":0.1}}},"usage":{"input_tokens":42}}`)
	})

	_, err := p.Infer(context.Background(), inference.Request{
		Prompt: "Is this on topic?",
		Inputs: []types.Message{
			{Role: "user", Content: "hi there"},
			{Role: "assistant", Content: "hello"},
		},
		Labels: []string{"on-topic", "off-topic"},
	})
	if err != nil {
		t.Fatalf("Infer: %v", err)
	}

	if !strings.Contains(gotBody, `"model":"jev-latest"`) {
		t.Errorf("body missing model: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"role":"user"`) || !strings.Contains(gotBody, `"role":"assistant"`) {
		t.Errorf("body missing state roles: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"type":"choice"`) {
		t.Errorf("body missing questions.q.type=choice: %s", gotBody)
	}
	if !strings.Contains(gotBody, `"criteria":{"on-topic":null,"off-topic":null}`) {
		t.Errorf("body missing ordered criteria: %s", gotBody)
	}
}

func TestInfer_ProbabilitiesBecomeScoresHighestFirst(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"model":"jev-latest","answers":{"q":{"type":"choice","choice":"off-topic",
			"probabilities":{"on-topic":0.2,"off-topic":0.8}}},"usage":{"input_tokens":10}}`)
	})

	resp, err := p.Infer(context.Background(), inference.Request{
		Prompt: "topic?", Labels: []string{"on-topic", "off-topic"},
	})
	if err != nil {
		t.Fatalf("Infer: %v", err)
	}
	if len(resp.Scores) != 2 {
		t.Fatalf("got %d scores, want 2", len(resp.Scores))
	}
	if resp.Scores[0].Label != "off-topic" || resp.Scores[0].Score != 0.8 {
		t.Errorf("Scores[0] = %+v, want off-topic 0.8 (highest first)", resp.Scores[0])
	}
	if resp.Scores[1].Label != "on-topic" || resp.Scores[1].Score != 0.2 {
		t.Errorf("Scores[1] = %+v, want on-topic 0.2", resp.Scores[1])
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", resp.Usage.InputTokens)
	}
}

func TestInfer_GatewayCostParsed(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"model":"jev-latest","answers":{"q":{"type":"choice","choice":"on-topic",
			"probabilities":{"on-topic":0.9,"off-topic":0.1}}},"usage":{"input_tokens":10},
			"provider_metadata":{"gateway":{"cost":"0.000123","marketCost":"0.000456"}}}`)
	})

	resp, err := p.Infer(context.Background(), inference.Request{
		Prompt: "topic?", Labels: []string{"on-topic", "off-topic"},
	})
	if err != nil {
		t.Fatalf("Infer: %v", err)
	}
	if resp.Usage.Cost != 0.000123 {
		t.Errorf("Cost = %v, want 0.000123 (cost preferred over marketCost)", resp.Usage.Cost)
	}
}

func TestInfer_GatewayCostFallsBackToMarketCost(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"model":"jev-latest","answers":{"q":{"type":"choice","choice":"on-topic",
			"probabilities":{"on-topic":0.9,"off-topic":0.1}}},"usage":{"input_tokens":10},
			"provider_metadata":{"gateway":{"marketCost":"0.000456"}}}`)
	})

	resp, err := p.Infer(context.Background(), inference.Request{
		Prompt: "topic?", Labels: []string{"on-topic", "off-topic"},
	})
	if err != nil {
		t.Fatalf("Infer: %v", err)
	}
	if resp.Usage.Cost != 0.000456 {
		t.Errorf("Cost = %v, want 0.000456 (fallback to marketCost)", resp.Usage.Cost)
	}
}

func TestInfer_MissingCostLeavesZero(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"model":"jev-latest","answers":{"q":{"type":"choice","choice":"on-topic",
			"probabilities":{"on-topic":0.9,"off-topic":0.1}}},"usage":{"input_tokens":10}}`)
	})

	resp, err := p.Infer(context.Background(), inference.Request{
		Prompt: "topic?", Labels: []string{"on-topic", "off-topic"},
	})
	if err != nil {
		t.Fatalf("Infer: %v", err)
	}
	if resp.Usage.Cost != 0 {
		t.Errorf("Cost = %v, want 0", resp.Usage.Cost)
	}
}

func TestInfer_RequiresAtLeastTwoLabels(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no request should have been sent")
	})
	_, err := p.Infer(context.Background(), inference.Request{Prompt: "x", Labels: []string{"only-one"}})
	if !errors.Is(err, inference.ErrLabelsRequired) {
		t.Errorf("err = %v, want ErrLabelsRequired", err)
	}
	_, err = p.Infer(context.Background(), inference.Request{Prompt: "x"})
	if !errors.Is(err, inference.ErrLabelsRequired) {
		t.Errorf("err (no labels) = %v, want ErrLabelsRequired", err)
	}
}

func TestInfer_MissingAnswerIsError(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"model":"jev-latest","answers":{}}`)
	})
	_, err := p.Infer(context.Background(), inference.Request{
		Prompt: "topic?", Labels: []string{"on-topic", "off-topic"},
	})
	if err == nil {
		t.Fatal("missing answer must be an error")
	}
}

func TestInfer_WrongAnswerTypeIsError(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"model":"jev-latest","answers":{"q":{"type":"score","score":0.5}}}`)
	})
	_, err := p.Infer(context.Background(), inference.Request{
		Prompt: "topic?", Labels: []string{"on-topic", "off-topic"},
	})
	if err == nil {
		t.Fatal("wrong answer type must be an error")
	}
}

func TestInfer_ChoiceOutsideLabelsIsError(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"model":"jev-latest","answers":{"q":{"type":"choice","choice":"maybe-topic",
			"probabilities":{"on-topic":0.5,"off-topic":0.5}}}}`)
	})
	_, err := p.Infer(context.Background(), inference.Request{
		Prompt: "topic?", Labels: []string{"on-topic", "off-topic"},
	})
	if err == nil {
		t.Fatal("choice outside labels must be an error")
	}
}

func TestInfer_TransientRetry_429ThenSucceeds(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprintln(w, `{"model":"jev-latest","answers":{"q":{"type":"choice","choice":"on-topic",
			"probabilities":{"on-topic":0.9,"off-topic":0.1}}}}`)
	}))
	defer srv.Close()
	p, _ := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	p.retryPolicy = fastRetryPolicy()

	resp, err := p.Infer(context.Background(), inference.Request{
		Prompt: "topic?", Labels: []string{"on-topic", "off-topic"},
	})
	if err != nil {
		t.Fatalf("after retrying 429, Infer: %v", err)
	}
	if calls != 2 {
		t.Errorf("server got %d calls, want 2 (1 rate-limited + 1 success)", calls)
	}
	if len(resp.Scores) != 2 {
		t.Errorf("got %v, want two scores after retry", resp.Scores)
	}
}

func TestInfer_TransientRetry_502Exhausted_ReturnsProviderHTTPError(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintln(w, `bad gateway`)
	}))
	defer srv.Close()
	p, _ := New(Config{BaseURL: srv.URL, HTTPClient: srv.Client()})
	p.retryPolicy = fastRetryPolicy()

	_, err := p.Infer(context.Background(), inference.Request{
		Prompt: "topic?", Labels: []string{"on-topic", "off-topic"},
	})
	if err == nil {
		t.Fatal("502 exhausting retries must surface as an error")
	}
	var httpErr *providers.ProviderHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want *providers.ProviderHTTPError", err)
	}
	if httpErr.StatusCode != http.StatusBadGateway {
		t.Errorf("StatusCode = %d, want 502", httpErr.StatusCode)
	}
	// fastRetryPolicy: 1 initial + 2 retries.
	if calls != 3 {
		t.Errorf("server got %d calls, want 3 (1 initial + 2 retries)", calls)
	}
}

func TestInfer_NonRetryableHTTPError_ReturnsProviderHTTPError(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintln(w, `{"error":"invalid token"}`)
	})
	_, err := p.Infer(context.Background(), inference.Request{
		Prompt: "topic?", Labels: []string{"on-topic", "off-topic"},
	})
	var httpErr *providers.ProviderHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want *providers.ProviderHTTPError", err)
	}
	if httpErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", httpErr.StatusCode)
	}
}
