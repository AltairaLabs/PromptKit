package hf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/classify"
)

// newTestClient wires a Client at an httptest server. Each test
// constructs its own server with a custom handler so request shape
// can be asserted alongside the response decode.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := NewClient(Config{
		APIKey:     "test-token",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

func TestNewClient_RequiresAPIKey(t *testing.T) {
	if _, err := NewClient(Config{}); err == nil {
		t.Fatal("NewClient must reject empty API key")
	}
}

func TestNewClient_DefaultsBaseURL(t *testing.T) {
	c, err := NewClient(Config{APIKey: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if c.baseURL != DefaultBaseURL {
		t.Errorf("baseURL = %q, want %q", c.baseURL, DefaultBaseURL)
	}
}

func TestClient_ClassifyAudio_HappyPath(t *testing.T) {
	// Use an httptest server pointed at the default (non-dedicated)
	// path shape so the /models/{owner}/{name} prefix is exercised.
	var gotPath, gotAuth, gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		fmt.Fprintln(w, `[{"label":"angry","score":0.82},{"label":"neutral","score":0.18}]`)
	}))
	defer srv.Close()
	c, err := NewClient(Config{
		APIKey:     "test-token",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := c.ClassifyAudio(context.Background(), []byte("RIFF...WAV"), classify.AudioOptions{
		Model:    "superb/wav2vec2-base-superb-er",
		MIMEType: "audio/wav",
	})
	if err != nil {
		t.Fatalf("ClassifyAudio: %v", err)
	}
	if len(got) != 2 || got[0].Label != "angry" || got[0].Score < 0.8 {
		t.Errorf("got %v, want angry@0.82 first", got)
	}

	// Request shape pins: auth header, content-type, model in URL path,
	// raw audio body — anything missing would silently 4xx on HF
	// without these assertions.
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

func TestClient_ClassifyAudio_EmptyAudioRejected(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("server should not be called") })
	_, err := c.ClassifyAudio(context.Background(), nil, classify.AudioOptions{Model: "m"})
	if err == nil {
		t.Fatal("empty audio must be rejected without making a request")
	}
}

func TestClient_ClassifyAudio_RequiresModel(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("server should not be called") })
	_, err := c.ClassifyAudio(context.Background(), []byte("data"), classify.AudioOptions{})
	if err == nil {
		t.Fatal("missing model must be rejected without making a request")
	}
}

func TestClient_ClassifyText_HappyPath(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("text content-type = %q, want application/json", r.Header.Get("Content-Type"))
		}
		gotBody, _ = io.ReadAll(r.Body)
		// HF nested shape — one inner array per input.
		fmt.Fprintln(w, `[[{"label":"toxic","score":0.91},{"label":"clean","score":0.09}]]`)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	got, err := c.ClassifyText(context.Background(), "you suck", classify.TextOptions{
		Model:      "unitary/toxic-bert",
		MultiLabel: true,
	})
	if err != nil {
		t.Fatalf("ClassifyText: %v", err)
	}
	if len(got) != 2 || got[0].Label != "toxic" {
		t.Errorf("got %v, want toxic first", got)
	}

	// Pin the request body shape so a refactor that drops Parameters
	// (or breaks Inputs encoding) gets caught here rather than in a
	// production HF call where the failure is harder to attribute.
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
		t.Errorf("MultiLabel=true must set parameters.return_all_scores=true; got params %v", sent.Parameters)
	}
}

func TestClient_ClassifyText_FlatShape(t *testing.T) {
	// Some HF text models return flat `[{...}]` when multi-label is off.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `[{"label":"positive","score":0.99}]`)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	got, err := c.ClassifyText(context.Background(), "great product", classify.TextOptions{Model: "m"})
	if err != nil {
		t.Fatalf("ClassifyText flat shape: %v", err)
	}
	if len(got) != 1 || got[0].Label != "positive" {
		t.Errorf("got %v, want positive", got)
	}
}

func TestClient_ClassifyImage_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "image/png" {
			t.Errorf("image content-type = %q, want image/png", r.Header.Get("Content-Type"))
		}
		fmt.Fprintln(w, `[{"label":"nsfw","score":0.05},{"label":"normal","score":0.95}]`)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	got, err := c.ClassifyImage(context.Background(), []byte("\x89PNG..."), classify.ImageOptions{
		Model:    "Falconsai/nsfw_image_detection",
		MIMEType: "image/png",
	})
	if err != nil {
		t.Fatalf("ClassifyImage: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d labels, want 2", len(got))
	}
}

func TestClient_Embed_BatchShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `[[0.1, 0.2, 0.3], [0.4, 0.5, 0.6]]`)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	got, err := c.Embed(context.Background(), []string{"hello", "world"}, classify.EmbedOptions{Model: "m"})
	if err != nil {
		t.Fatalf("Embed batch: %v", err)
	}
	if len(got) != 2 || len(got[0]) != 3 {
		t.Errorf("got %v, want 2x3 vectors", got)
	}
}

func TestClient_Embed_SingleShape(t *testing.T) {
	// Single input — HF can return a flat array.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `[0.1, 0.2, 0.3]`)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	got, err := c.Embed(context.Background(), []string{"hello"}, classify.EmbedOptions{Model: "m"})
	if err != nil {
		t.Fatalf("Embed single: %v", err)
	}
	if len(got) != 1 || len(got[0]) != 3 {
		t.Errorf("got %v, want 1x3 vector", got)
	}
}

func TestClient_ModelLoadingRetry(t *testing.T) {
	// 503 twice with short estimated_time, then 200 — the client
	// should retry without surfacing ErrModelLoading.
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
	c, _ := NewClient(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	got, err := c.ClassifyAudio(context.Background(), []byte("x"), classify.AudioOptions{Model: "m"})
	if err != nil {
		t.Fatalf("after retries, ClassifyAudio: %v", err)
	}
	if calls != 3 {
		t.Errorf("server got %d calls, want 3 (2 loading + 1 success)", calls)
	}
	if len(got) != 1 {
		t.Errorf("got %v, want one label after retry", got)
	}
}

func TestClient_ModelLoadingExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintln(w, `{"estimated_time": 0.01, "error": "Model is loading"}`)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	_, err := c.ClassifyAudio(context.Background(), []byte("x"), classify.AudioOptions{Model: "m"})
	if !errors.Is(err, ErrModelLoading) {
		t.Errorf("after exhausting retries, err = %v, want ErrModelLoading", err)
	}
}

func TestClient_NonRetryableHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintln(w, `{"error":"invalid token"}`)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	_, err := c.ClassifyAudio(context.Background(), []byte("x"), classify.AudioOptions{Model: "m"})
	if err == nil {
		t.Fatal("401 must surface as error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error must include status code; got %v", err)
	}
}

func TestClient_ContextCancellationDuringRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprintln(w, `{"estimated_time": 60}`)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.ClassifyAudio(ctx, []byte("x"), classify.AudioOptions{Model: "m"})
	if err == nil {
		t.Fatal("context cancellation during retry wait must surface")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want DeadlineExceeded", err)
	}
}

func TestParseEstimatedWait_Caps(t *testing.T) {
	// 60s estimate should clamp to 15s.
	body := []byte(`{"estimated_time": 60}`)
	wait := parseEstimatedWait(body, time.Second)
	if wait > 15*time.Second {
		t.Errorf("wait = %v, want <= 15s", wait)
	}
}

func TestParseEstimatedWait_Fallback(t *testing.T) {
	// Unparseable body returns the fallback.
	wait := parseEstimatedWait([]byte("not json"), 7*time.Second)
	if wait != 7*time.Second {
		t.Errorf("wait = %v, want fallback 7s", wait)
	}
}

func TestModelURL_DedicatedEndpoint(t *testing.T) {
	// HF Inference Endpoints bake the model into the host; callers
	// pass Dedicated: true so the /models/{id} suffix is skipped.
	c, _ := NewClient(Config{
		APIKey:    "k",
		BaseURL:   "https://my-endpoint.endpoints.huggingface.cloud",
		Dedicated: true,
	})
	url, err := c.modelURL("ignored")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(url, "/models/") {
		t.Errorf("dedicated endpoint URL should not include /models/; got %q", url)
	}
}

func TestModelURL_DefaultPrefixesModels(t *testing.T) {
	// Default (Dedicated:false) shape is /models/{owner}/{name}.
	c, _ := NewClient(Config{APIKey: "k", BaseURL: "https://example.test"})
	url, err := c.modelURL("owner/model")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(url, "/models/owner/model") {
		t.Errorf("default endpoint URL should prefix /models/; got %q", url)
	}
}

func TestClient_ClassifyText_EmptyTextRejected(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("server should not be called") })
	_, err := c.ClassifyText(context.Background(), "", classify.TextOptions{Model: "m"})
	if err == nil {
		t.Fatal("empty text must be rejected without making a request")
	}
}

func TestClient_ClassifyText_RequiresModel(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("server should not be called") })
	_, err := c.ClassifyText(context.Background(), "hi", classify.TextOptions{})
	if err == nil {
		t.Fatal("missing model must be rejected")
	}
}

func TestClient_ClassifyText_GarbageResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `<!doctype html>not json`)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := c.ClassifyText(context.Background(), "hi", classify.TextOptions{Model: "m"})
	if err == nil {
		t.Fatal("non-JSON response must surface as decode error")
	}
}

func TestClient_ClassifyImage_EmptyImageRejected(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("server should not be called") })
	_, err := c.ClassifyImage(context.Background(), nil, classify.ImageOptions{Model: "m"})
	if err == nil {
		t.Fatal("empty image must be rejected without making a request")
	}
}

func TestClient_ClassifyImage_RequiresModel(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("server should not be called") })
	_, err := c.ClassifyImage(context.Background(), []byte("\x89PNG"), classify.ImageOptions{})
	if err == nil {
		t.Fatal("missing model must be rejected")
	}
}

func TestClient_ClassifyImage_DefaultsContentType(t *testing.T) {
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		fmt.Fprintln(w, `[{"label":"x","score":1}]`)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := c.ClassifyImage(context.Background(), []byte("\x89PNG"), classify.ImageOptions{Model: "m"})
	if err != nil {
		t.Fatalf("ClassifyImage: %v", err)
	}
	if gotCT != "application/octet-stream" {
		t.Errorf("Content-Type default = %q, want application/octet-stream", gotCT)
	}
}

func TestClient_Embed_EmptyInputsRejected(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("server should not be called") })
	_, err := c.Embed(context.Background(), nil, classify.EmbedOptions{Model: "m"})
	if err == nil {
		t.Fatal("empty inputs must be rejected")
	}
}

func TestClient_Embed_RequiresModel(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Fatal("server should not be called") })
	_, err := c.Embed(context.Background(), []string{"hi"}, classify.EmbedOptions{})
	if err == nil {
		t.Fatal("missing model must be rejected")
	}
}

func TestClient_Embed_UnexpectedShape(t *testing.T) {
	// HF token-level shape: [[[float]]] — not supported by MVP; surfaces
	// as a shape error so callers don't silently get wrong vectors.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `[[[0.1, 0.2], [0.3, 0.4]]]`)
	}))
	defer srv.Close()
	c, _ := NewClient(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
	_, err := c.Embed(context.Background(), []string{"hi"}, classify.EmbedOptions{Model: "m"})
	if err == nil {
		t.Fatal("per-token shape must surface as error")
	}
	if !strings.Contains(err.Error(), "unexpected embedding response shape") {
		t.Errorf("error must identify the cause; got %v", err)
	}
}
