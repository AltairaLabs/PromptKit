package imagen

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/base"
)

// imagenServer records the last :predict body and answers with n predictions.
func imagenServer(t *testing.T, n int) (url string, body func() string, calls *atomic.Int32) {
	t.Helper()
	var last atomic.Value
	last.Store("")
	calls = &atomic.Int32{}
	preds := make([]string, n)
	for i := range preds {
		preds[i] = `{"bytesBase64Encoded":"SU1HLT` + string(rune('A'+i)) + `","mimeType":"image/jpeg"}`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		raw, _ := io.ReadAll(r.Body)
		last.Store(string(raw))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"predictions":[`+strings.Join(preds, ",")+`]}`)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() string { return last.Load().(string) }, calls
}

func newTestImagen(url string) *Provider {
	return NewProvider(Config{ID: "imagen", Model: "imagen-4.0-generate-001", BaseURL: url, ApiKey: "k"})
}

// Generate sends the requested size as the nearest supported aspect ratio and
// the count as sampleCount. The body is pinned whole: the prompt, the mapped
// parameters, and the fixed safety settings.
func TestGenerate_RequestBody(t *testing.T) {
	cases := []struct {
		name, size string
		count      int
		wantAspect string
		wantCount  int
	}{
		{"default", "", 0, "1:1", 1},
		{"square", "1024x1024", 1, "1:1", 1},
		{"landscape wide", "1792x1024", 2, "16:9", 2},
		{"portrait tall", "1024x1792", 3, "9:16", 3},
		{"landscape 4:3", "1024x768", 4, "4:3", 4},
		{"portrait 3:4", "768x1024", 1, "3:4", 1},
		{"between 1:1 and 4:3 nearer square", "1100x1000", 1, "1:1", 1},
		{"upper-case separator", "1536X1024", 1, "4:3", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url, body, _ := imagenServer(t, tc.wantCount)
			resp, err := newTestImagen(url).Generate(context.Background(),
				base.ImageRequest{Prompt: "a fox", Size: tc.size, Count: tc.count, Quality: "hd"})
			require.NoError(t, err)

			assert.JSONEq(t, `{"instances":[{"prompt":"a fox"}],"parameters":{"sampleCount":`+
				strconv.Itoa(tc.wantCount)+`,"aspectRatio":"`+tc.wantAspect+
				`","safetyFilterLevel":"block_some","personGeneration":"allow_adult"}}`, body())
			assert.Len(t, resp.Images, tc.wantCount, "every generated image is returned")
		})
	}
}

// Every prediction comes back, in order, as the API's base64 text, and the
// cost counts them.
func TestGenerate_ReturnsEveryImage(t *testing.T) {
	url, _, _ := imagenServer(t, 3)
	resp, err := newTestImagen(url).Generate(context.Background(),
		base.ImageRequest{Prompt: "a fox", Count: 3})
	require.NoError(t, err)

	got := make([]string, len(resp.Images))
	for i, img := range resp.Images {
		got[i] = string(img)
	}
	assert.Equal(t, []string{"SU1HLTA", "SU1HLTB", "SU1HLTC"}, got)
	assert.Equal(t, "image/jpeg", resp.MIMEType)
	require.NotNil(t, resp.Cost)
	assert.Equal(t, 3.0, resp.Cost.Quantities[unitImage])
	assert.InDelta(t, 3*costPerImage, resp.Cost.TotalCost, 1e-9)
}

// Requests the API cannot serve fail before any call: a count outside 1..4
// and a size that is not WIDTHxHEIGHT.
func TestGenerate_RefusesWhatTheAPICannotServe(t *testing.T) {
	cases := []struct {
		name string
		req  base.ImageRequest
		want string
	}{
		{"count above the maximum", base.ImageRequest{Prompt: "x", Count: 5},
			"imagen: count 5 is out of range: one call generates 1 to 4 images"},
		{"negative count", base.ImageRequest{Prompt: "x", Count: -1},
			"imagen: count -1 is out of range: one call generates 1 to 4 images"},
		{"size without separator", base.ImageRequest{Prompt: "x", Size: "large"},
			`imagen: size "large" is not WIDTHxHEIGHT, e.g. "1024x1024"`},
		{"zero dimension", base.ImageRequest{Prompt: "x", Size: "0x1024"},
			`imagen: size "0x1024" is not WIDTHxHEIGHT, e.g. "1024x1024"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url, _, calls := imagenServer(t, 1)
			_, err := newTestImagen(url).Generate(context.Background(), tc.req)
			require.EqualError(t, err, tc.want)
			assert.Equal(t, int32(0), calls.Load(), "nothing is sent")
		})
	}
}
