package mediagen

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/imagen"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// onePixelJPEG is a 1x1 JPEG, base64-encoded the way the Imagen predict
// endpoint returns it in predictions[].bytesBase64Encoded. JPEG rather than
// PNG so the MIME type is the endpoint's, not the executor's PNG fallback.
const onePixelJPEG = "/9j/4AAQSkZJRgABAQEASABIAAD/2wBDAP////////////////////////////////" +
	"//////////////////////////////////////////////////////wgALCAABAAEBAREA/8QAFBABAAAAAAAAAAAAAAAAAAAAAP/aAAgBAQABPxA="

// The image__generate tool and the Imagen provider meet at base.ImageResponse:
// the provider returns each image as base64 text, and the executor puts that
// text into the image part unchanged. This runs both for real against an
// Imagen predict endpoint stand-in, so a provider that started returning
// decoded bytes (or an executor that re-encoded them) breaks here.
func TestImageGenerate_ThroughImagenProvider(t *testing.T) {
	var gotPath, gotKey string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey = r.URL.Path, r.Header.Get("x-goog-api-key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"predictions":[{"bytesBase64Encoded":"`+onePixelJPEG+`","mimeType":"image/jpeg"}]}`)
	}))
	defer server.Close()

	pool := base.NewRegistry()
	mustRegister(t, pool, imagen.NewProvider(imagen.Config{
		ID: "imagen", Model: "imagen-4.0-generate-001", BaseURL: server.URL, ApiKey: "k-1",
	}))

	args, _ := json.Marshal(imageArgs{Prompt: "a fox in snow"})
	summary, parts, err := NewExecutor(pool).ExecuteMultimodal(context.Background(), ImageGenerateDescriptor(), args)
	if err != nil {
		t.Fatalf("ExecuteMultimodal: %v", err)
	}

	if gotPath != "/models/imagen-4.0-generate-001:predict" {
		t.Errorf("request path = %q", gotPath)
	}
	if gotKey != "k-1" {
		t.Errorf("x-goog-api-key = %q, want k-1", gotKey)
	}
	instances, _ := gotBody["instances"].([]any)
	if len(instances) != 1 || instances[0].(map[string]any)["prompt"] != "a fox in snow" {
		t.Errorf("request instances = %v, want the tool's prompt", gotBody["instances"])
	}

	if len(parts) != 1 {
		t.Fatalf("parts = %d, want 1 image part", len(parts))
	}
	p := parts[0]
	if p.Type != types.ContentTypeImage || p.Media == nil || p.Media.Data == nil {
		t.Fatalf("part = %+v, want an inline image", p)
	}
	if *p.Media.Data != onePixelJPEG {
		t.Errorf("image data = %.40q..., want the endpoint's base64 unchanged", *p.Media.Data)
	}
	if p.Media.MIMEType != types.MIMETypeImageJPEG {
		t.Errorf("mime = %q", p.Media.MIMEType)
	}
	var sum map[string]any
	if err := json.Unmarshal(summary, &sum); err != nil {
		t.Fatalf("summary: %v", err)
	}
	want := map[string]any{"provider": "imagen", "mime_type": "image/jpeg", "count": float64(1)}
	for k, v := range want {
		if sum[k] != v {
			t.Errorf("summary[%s] = %v, want %v", k, sum[k], v)
		}
	}
}
