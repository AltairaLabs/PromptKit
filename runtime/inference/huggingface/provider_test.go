package huggingface

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/v2/inference"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// newTestProvider wires a Provider at an httptest server. Each test
// constructs its own server with a custom handler so request shape can be
// asserted alongside the response decode.
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

func audioPart(data []byte, mimeType string) types.Message {
	b64 := base64.StdEncoding.EncodeToString(data)
	return types.Message{Role: "user", Parts: []types.ContentPart{{
		Type:  types.ContentTypeAudio,
		Media: &types.MediaContent{Data: &b64, MIMEType: mimeType},
	}}}
}

func imagePart(data []byte, mimeType string) types.Message {
	b64 := base64.StdEncoding.EncodeToString(data)
	return types.Message{Role: "assistant", Parts: []types.ContentPart{{
		Type:  types.ContentTypeImage,
		Media: &types.MediaContent{Data: &b64, MIMEType: mimeType},
	}}}
}

func TestNew_RequiresAPIKey(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New must reject empty API key")
	}
}

func TestNew_DefaultsBaseURL(t *testing.T) {
	p, err := New(Config{APIKey: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if p.baseURL != DefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", p.baseURL, DefaultBaseURL)
	}
}

func TestNew_UsesConfigModelAsDefault(t *testing.T) {
	p, err := New(Config{APIKey: "x", Model: "  configured-model  "})
	if err != nil {
		t.Fatal(err)
	}
	if p.model != "configured-model" {
		t.Errorf("model = %q, want %q (trimmed)", p.model, "configured-model")
	}
}

func TestNew_DefaultsToProvidersDefaultRetryPolicy(t *testing.T) {
	p, err := New(Config{APIKey: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if p.retryPolicy != providers.DefaultRetryPolicy() {
		t.Errorf("retryPolicy = %+v, want providers.DefaultRetryPolicy() %+v",
			p.retryPolicy, providers.DefaultRetryPolicy())
	}
}

// fastRetryPolicy is a transient-retry policy tests can inject via
// Provider.retryPolicy so exhausted-retry cases don't pay
// providers.DefaultRetryPolicy()'s real (500ms+) backoff. Production always
// uses providers.DefaultRetryPolicy(), set by New().
func fastRetryPolicy() pipeline.RetryPolicy {
	return pipeline.RetryPolicy{
		MaxRetries:     2,
		Backoff:        "exponential",
		InitialDelayMs: 5,
	}
}

func TestInfer_Audio_HappyPath(t *testing.T) {
	var gotPath, gotAuth, gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		// Deliberately unsorted so the sort-highest-first behavior is exercised.
		fmt.Fprintln(w, `[{"label":"neutral","score":0.18},{"label":"angry","score":0.82}]`)
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "test-token", BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}

	resp, err := p.Infer(context.Background(), inference.Request{
		Model:  "superb/wav2vec2-base-superb-er",
		Inputs: []types.Message{audioPart([]byte("RIFF...WAV"), "audio/wav")},
	})
	if err != nil {
		t.Fatalf("Infer: %v", err)
	}
	if len(resp.Scores) != 2 || resp.Scores[0].Label != "angry" || resp.Scores[0].Score < 0.8 {
		t.Errorf("got %v, want angry@0.82 first (sorted)", resp.Scores)
	}

	if !strings.HasPrefix(gotPath, "/models/superb/") {
		t.Errorf("URL path = %q, want /models/superb/...", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q, want Bearer test-token", gotAuth)
	}
	if gotContentType != "audio/wav" {
		t.Errorf("Content-Type = %q, want audio/wav", gotContentType)
	}
	if string(gotBody) != "RIFF...WAV" {
		t.Errorf("body = %q, want raw audio bytes", string(gotBody))
	}
}

func TestInfer_Audio_EmptyBytesRejected(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("server should not be called") })
	empty := ""
	_, err := p.Infer(context.Background(), inference.Request{
		Model: "m",
		Inputs: []types.Message{{Parts: []types.ContentPart{{
			Type:  types.ContentTypeAudio,
			Media: &types.MediaContent{Data: &empty},
		}}}},
	})
	if err == nil {
		t.Fatal("empty audio bytes must be rejected without making a request")
	}
}

func TestInfer_RequiresModel(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("server should not be called") })
	_, err := p.Infer(context.Background(), inference.Request{
		Inputs: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("missing model must be rejected without making a request")
	}
}

func TestInfer_Text_HappyPath(t *testing.T) {
	var gotBody []byte
	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		fmt.Fprintln(w, `[[{"label":"toxic","score":0.91},{"label":"clean","score":0.09}]]`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	resp, err := p.Infer(context.Background(), inference.Request{
		Model:  "unitary/toxic-bert",
		Inputs: []types.Message{{Role: "user", Content: "you suck"}},
		Params: map[string]any{"multi_label": true},
	})
	if err != nil {
		t.Fatalf("Infer: %v", err)
	}
	if len(resp.Scores) != 2 || resp.Scores[0].Label != "toxic" {
		t.Errorf("got %v, want toxic first", resp.Scores)
	}
	if gotContentType != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotContentType)
	}

	var sent struct {
		Inputs     string         `json:"inputs"`
		Parameters map[string]any `json:"parameters"`
	}
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("request body must be valid JSON: %v (raw: %s)", err, gotBody)
	}
	if sent.Inputs != "you suck" {
		t.Errorf("request inputs = %q, want %q", sent.Inputs, "you suck")
	}
	if v, ok := sent.Parameters["return_all_scores"].(bool); !ok || !v {
		t.Errorf("multi_label=true must set parameters.return_all_scores=true; got params %v", sent.Parameters)
	}
}

func TestInfer_Text_FlatShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `[{"label":"positive","score":0.99}]`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	resp, err := p.Infer(context.Background(), inference.Request{
		Model:  "m",
		Inputs: []types.Message{{Role: "user", Content: "great product"}},
	})
	if err != nil {
		t.Fatalf("Infer flat shape: %v", err)
	}
	if len(resp.Scores) != 1 || resp.Scores[0].Label != "positive" {
		t.Errorf("got %v, want positive", resp.Scores)
	}
}

func TestInfer_Text_EmptyTextRejected(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("server should not be called") })
	_, err := p.Infer(context.Background(), inference.Request{
		Model:  "m",
		Inputs: []types.Message{{Role: "user", Content: ""}},
	})
	if err == nil {
		t.Fatal("empty text must be rejected without making a request")
	}
}

func TestInfer_Text_GarbageResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `<!doctype html>not json`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := p.Infer(context.Background(), inference.Request{
		Model:  "m",
		Inputs: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("non-JSON response must surface as decode error")
	}
}

func TestInfer_Image_HappyPath(t *testing.T) {
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		fmt.Fprintln(w, `[{"label":"nsfw","score":0.05},{"label":"normal","score":0.95}]`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	resp, err := p.Infer(context.Background(), inference.Request{
		Model:  "Falconsai/nsfw_image_detection",
		Inputs: []types.Message{imagePart([]byte("\x89PNG..."), "image/png")},
	})
	if err != nil {
		t.Fatalf("Infer image: %v", err)
	}
	if len(resp.Scores) != 2 {
		t.Errorf("got %d labels, want 2", len(resp.Scores))
	}
	if gotCT != "image/png" {
		t.Errorf("content-type = %q, want image/png", gotCT)
	}
}

func TestInfer_Image_EmptyBytesRejected(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("server should not be called") })
	empty := ""
	_, err := p.Infer(context.Background(), inference.Request{
		Model: "m",
		Inputs: []types.Message{{Parts: []types.ContentPart{{
			Type:  types.ContentTypeImage,
			Media: &types.MediaContent{Data: &empty},
		}}}},
	})
	if err == nil {
		t.Fatal("empty image bytes must be rejected without making a request")
	}
}

func TestInfer_Image_DefaultsContentType(t *testing.T) {
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		fmt.Fprintln(w, `[{"label":"x","score":1}]`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := p.Infer(context.Background(), inference.Request{
		Model:  "m",
		Inputs: []types.Message{imagePart([]byte("\x89PNG"), "")},
	})
	if err != nil {
		t.Fatalf("Infer: %v", err)
	}
	if gotCT != defaultContentType {
		t.Errorf("Content-Type default = %q, want %q", gotCT, defaultContentType)
	}
}

func TestInfer_ZeroShot_LabelsPathAndBody(t *testing.T) {
	var gotPath string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotBody, _ = io.ReadAll(r.Body)
		// Deliberately unsorted, to exercise the sort-highest-first behavior.
		fmt.Fprintln(w, `{"sequence":"hello","labels":["off-topic","on-topic"],"scores":[0.2,0.8]}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	resp, err := p.Infer(context.Background(), inference.Request{
		Model:  "facebook/bart-large-mnli",
		Inputs: []types.Message{{Role: "user", Content: "hello"}},
		Labels: []string{"on-topic", "off-topic"},
	})
	if err != nil {
		t.Fatalf("Infer zero-shot: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/pipeline/zero-shot-classification") {
		t.Errorf("path = %q, want suffix /pipeline/zero-shot-classification", gotPath)
	}
	var sent struct {
		Inputs     string         `json:"inputs"`
		Parameters map[string]any `json:"parameters"`
	}
	if err := json.Unmarshal(gotBody, &sent); err != nil {
		t.Fatalf("request body must be valid JSON: %v (raw: %s)", err, gotBody)
	}
	labels, ok := sent.Parameters["candidate_labels"].([]any)
	if !ok || len(labels) != 2 {
		t.Fatalf("parameters.candidate_labels = %v, want 2 labels", sent.Parameters["candidate_labels"])
	}

	if len(resp.Scores) != 2 || resp.Scores[0].Label != "on-topic" || resp.Scores[0].Score < 0.7 {
		t.Errorf("got %v, want on-topic@0.8 first (sorted)", resp.Scores)
	}
}

func TestInfer_ZeroShot_LengthMismatch_Errors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"sequence":"hello","labels":["on-topic","off-topic"],"scores":[0.8]}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	_, err := p.Infer(context.Background(), inference.Request{
		Model:  "m",
		Inputs: []types.Message{{Role: "user", Content: "hello"}},
		Labels: []string{"on-topic", "off-topic"},
	})
	if err == nil {
		t.Fatal("mismatched labels/scores lengths must error")
	}
}

func TestInfer_Prompt_Errors(t *testing.T) {
	p := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("server should not be called") })
	_, err := p.Infer(context.Background(), inference.Request{
		Model:  "m",
		Prompt: "classify this",
		Inputs: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("a non-empty Prompt must be rejected; HF classification takes none")
	}
}

func TestInfer_Model_OverridesConfiguredModel(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		fmt.Fprintln(w, `[{"label":"x","score":1}]`)
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL, Model: "configured-model", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}

	_, err = p.Infer(context.Background(), inference.Request{
		Model:  "request-model",
		Inputs: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Infer: %v", err)
	}
	if !strings.Contains(gotPath, "request-model") {
		t.Errorf("path = %q, want it to use Request.Model over the configured model", gotPath)
	}
}

func TestInfer_UsesConfiguredModel_WhenRequestModelEmpty(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		fmt.Fprintln(w, `[{"label":"x","score":1}]`)
	}))
	defer srv.Close()
	p, err := New(Config{APIKey: "k", BaseURL: srv.URL, Model: "configured-model", HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}

	_, err = p.Infer(context.Background(), inference.Request{
		Inputs: []types.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Infer: %v", err)
	}
	if !strings.Contains(gotPath, "configured-model") {
		t.Errorf("path = %q, want it to fall back to the configured model", gotPath)
	}
}

func TestInfer_ModelLoadingRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintln(w, `{"estimated_time": 0.01, "error": "Model is loading"}`)
			return
		}
		fmt.Fprintln(w, `[{"label":"angry","score":0.8}]`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	resp, err := p.Infer(context.Background(), inference.Request{
		Model:  "m",
		Inputs: []types.Message{audioPart([]byte("x"), "")},
	})
	if err != nil {
		t.Fatalf("after retries, Infer: %v", err)
	}
	if calls != 3 {
		t.Errorf("server got %d calls, want 3 (2 loading + 1 success)", calls)
	}
	if len(resp.Scores) != 1 {
		t.Errorf("got %v, want one label after retry", resp.Scores)
	}
}

func TestInfer_ModelLoadingExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintln(w, `{"estimated_time": 0.01, "error": "Model is loading"}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	_, err := p.Infer(context.Background(), inference.Request{
		Model:  "m",
		Inputs: []types.Message{audioPart([]byte("x"), "")},
	})
	if !errors.Is(err, inference.ErrModelLoading) {
		t.Errorf("after exhausting retries, err = %v, want ErrModelLoading", err)
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
		fmt.Fprintln(w, `[{"label":"angry","score":0.8}]`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
	p.retryPolicy = fastRetryPolicy()

	resp, err := p.Infer(context.Background(), inference.Request{
		Model:  "m",
		Inputs: []types.Message{audioPart([]byte("x"), "")},
	})
	if err != nil {
		t.Fatalf("after retrying 429, Infer: %v", err)
	}
	if calls != 2 {
		t.Errorf("server got %d calls, want 2 (1 rate-limited + 1 success)", calls)
	}
	if len(resp.Scores) != 1 {
		t.Errorf("got %v, want one label after retry", resp.Scores)
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
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
	p.retryPolicy = fastRetryPolicy()

	_, err := p.Infer(context.Background(), inference.Request{
		Model:  "m",
		Inputs: []types.Message{audioPart([]byte("x"), "")},
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintln(w, `{"error":"invalid token"}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	_, err := p.Infer(context.Background(), inference.Request{
		Model:  "m",
		Inputs: []types.Message{audioPart([]byte("x"), "")},
	})
	if err == nil {
		t.Fatal("401 must surface as error")
	}
	var httpErr *providers.ProviderHTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("err = %v, want *providers.ProviderHTTPError", err)
	}
	if httpErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", httpErr.StatusCode)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error must include status code; got %v", err)
	}
}

func TestInfer_ModelNotSupported_JSONErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintln(w, `{"error":"Model superb/wav2vec2-base-superb-er is not supported for task audio-classification on provider hf-inference"}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	_, err := p.Infer(context.Background(), inference.Request{
		Model:  "superb/wav2vec2-base-superb-er",
		Inputs: []types.Message{audioPart([]byte("x"), "")},
	})
	if !errors.Is(err, inference.ErrModelNotSupported) {
		t.Fatalf("err = %v, want ErrModelNotSupported", err)
	}
}

func TestInfer_ModelNotSupported_PlaintextBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, "Model not supported")
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	_, err := p.Infer(context.Background(), inference.Request{
		Model:  "m",
		Inputs: []types.Message{audioPart([]byte("x"), "")},
	})
	if !errors.Is(err, inference.ErrModelNotSupported) {
		t.Fatalf("err = %v, want ErrModelNotSupported", err)
	}
}

func TestInfer_GenericHTTPError_NotModelNotSupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintln(w, `{"error":"invalid token"}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	_, err := p.Infer(context.Background(), inference.Request{
		Model:  "m",
		Inputs: []types.Message{audioPart([]byte("x"), "")},
	})
	if err == nil {
		t.Fatal("401 must surface as error")
	}
	if errors.Is(err, inference.ErrModelNotSupported) {
		t.Errorf("401 'invalid token' must NOT be classified as ErrModelNotSupported: %v", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error must include status code; got %v", err)
	}
}

func TestInfer_ContextCancellationDuringRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintln(w, `{"estimated_time": 60}`)
	}))
	defer srv.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := p.Infer(ctx, inference.Request{
		Model:  "m",
		Inputs: []types.Message{audioPart([]byte("x"), "")},
	})
	if err == nil {
		t.Fatal("context cancellation during retry wait must surface")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
}

func TestParseEstimatedWait_Caps(t *testing.T) {
	body := []byte(`{"estimated_time": 60}`)
	wait := parseEstimatedWait(body, time.Second)
	if wait > maxLoadingWaitCap {
		t.Errorf("wait = %v, want <= %v", wait, maxLoadingWaitCap)
	}
}

func TestParseEstimatedWait_Fallback(t *testing.T) {
	wait := parseEstimatedWait([]byte("not json"), 7*time.Second)
	if wait != 7*time.Second {
		t.Errorf("wait = %v, want fallback 7s", wait)
	}
}

func TestEndpointURL_DedicatedEndpoint(t *testing.T) {
	p, _ := New(Config{
		APIKey:    "k",
		BaseURL:   "https://my-endpoint.endpoints.huggingface.cloud",
		Dedicated: true,
	})
	url, err := p.endpointURL("ignored", "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(url, "/models/") {
		t.Errorf("dedicated endpoint URL should not include /models/; got %q", url)
	}
}

func TestEndpointURL_DefaultPrefixesModels(t *testing.T) {
	p, _ := New(Config{APIKey: "k", BaseURL: "https://example.test"})
	url, err := p.endpointURL("owner/model", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(url, "/models/owner/model") {
		t.Errorf("default endpoint URL should prefix /models/; got %q", url)
	}
}

func TestEndpointURL_RequiresModel(t *testing.T) {
	p, _ := New(Config{APIKey: "k", BaseURL: "https://example.test"})
	if _, err := p.endpointURL("", ""); err == nil {
		t.Fatal("empty model must be rejected, even for dedicated endpoints")
	}
}

func TestIsUnsupportedModelResponse(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"json not supported", `{"error":"Model x is not supported for task y"}`, true},
		{"plaintext not supported", "Model not supported", true},
		{"unrelated json error", `{"error":"invalid token"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isUnsupportedModelResponse([]byte(tc.body)); got != tc.want {
				t.Errorf("isUnsupportedModelResponse(%q) = %v, want %v", tc.body, got, tc.want)
			}
		})
	}
}
