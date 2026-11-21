package a2a

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/testutil"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// A remote agent's artifact parts need not carry mediaType (A2A 1.0). The
// executor hands the tool caller the ones whose type it can determine and
// skips the rest, as it skips data parts.
func TestExecutor_ExecuteMultimodal_KeepsArtifactsWithoutMediaType(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	text := "done"
	task := &Task{
		ID:     "task-untyped",
		Status: TaskStatus{State: TaskStateCompleted},
		Artifacts: []Artifact{{
			ArtifactID: "a1",
			Parts: []Part{
				{Text: &text},
				{Raw: png},
				{URL: testutil.Ptr("https://example.com/chart.JPG")},
				{Raw: []byte{0, 1, 2, 3}},
				{Data: map[string]any{"k": "v"}},
			},
		}},
	}
	srv := httptest.NewServer(noCard(func(w http.ResponseWriter, r *http.Request) {
		req := decodeRPC(r)
		rpcResult(w, req.ID, task)
	}))
	defer srv.Close()

	e := NewExecutor(WithNoRetry())
	defer e.Close()
	desc := &tools.ToolDescriptor{Name: "a2a__agent__skill", A2AConfig: &tools.A2AConfig{AgentURL: srv.URL}}

	_, parts, err := e.ExecuteMultimodal(context.Background(), desc, json.RawMessage(`{"query":"chart"}`))
	if err != nil {
		t.Fatalf("ExecuteMultimodal() error = %v", err)
	}
	b64 := base64.StdEncoding.EncodeToString(png)
	want := []types.ContentPart{
		{Type: types.ContentTypeText, Text: &text},
		{Type: types.ContentTypeImage, Media: &types.MediaContent{Data: &b64, MIMEType: "image/png"}},
		{Type: types.ContentTypeImage, Media: &types.MediaContent{
			URL: testutil.Ptr("https://example.com/chart.JPG"), MIMEType: "image/jpeg",
		}},
	}
	if !reflect.DeepEqual(parts, want) {
		t.Fatalf("parts = %+v, want %+v", parts, want)
	}
}
