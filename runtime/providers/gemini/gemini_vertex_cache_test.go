package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

// Explicit caching on Vertex uses the regional aiplatform endpoints, Bearer
// auth, and full resource paths (verified live). This pins those differences:
// the CachedContent create hits .../cachedContents under the project/location,
// the create body's model is the full publishers/google/models path, the
// returned full resource name is referenced as cachedContent, and the inline
// systemInstruction is dropped — all with the credential's Bearer auth applied.
func TestExplicitCaching_Vertex_UsesVertexEndpoints(t *testing.T) {
	var createPath, createModel string
	var genBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/cachedContents"):
			createPath = r.URL.Path
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			createModel, _ = body["model"].(string)
			_, _ = io.WriteString(w, `{"name":"projects/930678174047/locations/us-central1/cachedContents/abc123"}`)
		case strings.Contains(r.URL.Path, ":generateContent"):
			genBody, _ = io.ReadAll(r.Body)
			_, _ = io.WriteString(w, geminiGenOKBody)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	cred := &mockBearerCredential{}
	baseURL := srv.URL + "/v1/projects/test-proj/locations/us-central1/publishers/google/models"
	provider, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "v-gemini", Type: "gemini", Model: "gemini-2.5-flash", BaseURL: baseURL,
		Platform:         "vertex",
		PlatformConfig:   &providers.PlatformConfig{Type: "vertex", Region: "us-central1", Project: "test-proj"},
		Credential:       cred,
		AdditionalConfig: map[string]any{"explicit_caching": true},
	})
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}

	if _, err := provider.Predict(context.Background(), providers.PredictionRequest{
		System: bigSystem, Messages: []types.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Predict: %v", err)
	}

	// Create hit the Vertex cachedContents endpoint under project/location.
	if want := "/v1/projects/test-proj/locations/us-central1/cachedContents"; createPath != want {
		t.Errorf("create path = %q, want %q", createPath, want)
	}
	// The create-body model is the full publisher path.
	if want := "projects/test-proj/locations/us-central1/publishers/google/models/gemini-2.5-flash"; createModel != want {
		t.Errorf("create model = %q, want %q", createModel, want)
	}
	// Bearer auth was applied (Vertex requires it).
	if !cred.applied {
		t.Error("expected the Vertex credential's Bearer auth to be applied to the cache create")
	}
	// generateContent references the full returned resource name and drops system.
	var gen map[string]any
	if err := json.Unmarshal(genBody, &gen); err != nil {
		t.Fatalf("gen body not JSON: %v", err)
	}
	if gen["cachedContent"] != "projects/930678174047/locations/us-central1/cachedContents/abc123" {
		t.Errorf("cachedContent = %v, want full resource name", gen["cachedContent"])
	}
	if _, has := gen["systemInstruction"]; has {
		t.Error("systemInstruction must be dropped when cachedContent is set")
	}
}

// cachedContentDeleteURL builds the Vertex delete URL from the full resource
// name appended to the API root.
func TestCachedContentDeleteURL_Vertex(t *testing.T) {
	p := &Provider{
		platform: vertexPlatform,
		baseURL:  "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/us-central1/publishers/google/models",
	}
	got := p.cachedContentDeleteURL("projects/123/locations/us-central1/cachedContents/abc")
	want := "https://us-central1-aiplatform.googleapis.com/v1/projects/123/locations/us-central1/cachedContents/abc"
	if got != want {
		t.Errorf("delete URL = %q, want %q", got, want)
	}
}
