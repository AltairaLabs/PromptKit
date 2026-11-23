//go:build integration

package gemini

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// TestGemini_Interactions_ToolLessTurns_Live proves #2914 end to end: with
// api_mode interactions, turns with no tools (Predict, PredictStream) reach
// the Interactions API and come back right: a plain multi-turn chat that
// remembers the model's own reply, a stream, a PDF and an image input, and a
// JSON-schema answer. Each case runs twice, since the model is stochastic.
//
// Run:
//
//	GEMINI_API_KEY=... go test -tags integration ./runtime/providers/gemini/ \
//	    -run TestGemini_Interactions_ToolLessTurns_Live -v
func TestGemini_Interactions_ToolLessTurns_Live(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	model := "gemini-3.7-flash"
	if m := os.Getenv("GEMINI_INTERACTIONS_MODELS"); m != "" {
		model = m
	}
	p, err := providers.CreateProviderFromSpec(providers.ProviderSpec{
		ID: "gemini-int-all-turns", Type: "gemini", Model: model,
		BaseURL:          "https://generativelanguage.googleapis.com/v1beta",
		Defaults:         providers.ProviderDefaults{MaxTokens: 2048},
		AdditionalConfig: map[string]any{"api_mode": "interactions"},
	})
	if err != nil {
		t.Fatalf("CreateProviderFromSpec: %v", err)
	}
	defer func() { _ = p.Close() }()
	ctx := context.Background()

	for run := 1; run <= 2; run++ {
		t.Run(fmt.Sprintf("chat/%d", run), func(t *testing.T) {
			resp, err := p.Predict(ctx, providers.PredictionRequest{
				Messages: []types.Message{
					{Role: roleUser, Content: "Hi, I'm Zed."},
					{Role: roleAssistant, Content: "Hello Zed! The magic word is BANANA."},
					// Probed live: with the reply replayed as the user's (bare
					// content items) the model answered NONE; as model_output it
					// quotes the reply.
					{Role: roleUser, Content: "Quote, verbatim, the exact text of the most recent message " +
						"YOU (the assistant) sent in this conversation, or say NONE if you have sent none."},
				},
			})
			require.NoError(t, err)
			assert.Contains(t, strings.ToUpper(resp.Content), "BANANA")
			assert.Equal(t, types.FinishReasonStop, resp.FinishReason)
			require.NotNil(t, resp.CostInfo)
			assert.Positive(t, resp.CostInfo.InputTokens)
			t.Logf("chat: %q, %d in / %d out", resp.Content, resp.CostInfo.InputTokens, resp.CostInfo.OutputTokens)
		})

		t.Run(fmt.Sprintf("stream/%d", run), func(t *testing.T) {
			ch, err := p.PredictStream(ctx, providers.PredictionRequest{
				Messages: []types.Message{{Role: roleUser, Content: "Count from 1 to 5, digits separated by spaces."}},
			})
			require.NoError(t, err)
			var content, finish string
			var cost *types.CostInfo
			for c := range ch {
				require.NoError(t, c.Error)
				content = c.Content
				if c.FinishReason != nil {
					finish = *c.FinishReason
				}
				if c.CostInfo != nil {
					cost = c.CostInfo
				}
			}
			assert.Contains(t, content, "1 2 3 4 5")
			assert.Equal(t, types.FinishReasonStop, finish)
			require.NotNil(t, cost, "the stream's final chunk carries the cost")
			t.Logf("stream: %q", content)
		})

		t.Run(fmt.Sprintf("pdf/%d", run), func(t *testing.T) {
			msg := types.Message{Role: roleUser}
			msg.AddTextPart("What single word is written in this PDF? Reply with the word only.")
			msg.AddPart(types.ContentPart{Type: types.ContentTypeDocument, Media: &types.MediaContent{
				Data: strPtr(base64.StdEncoding.EncodeToString(onePagePDF("PINEAPPLE"))), MIMEType: types.MIMETypePDF}})
			resp, err := p.Predict(ctx, providers.PredictionRequest{Messages: []types.Message{msg}})
			require.NoError(t, err)
			assert.Contains(t, strings.ToUpper(resp.Content), "PINEAPPLE")
			t.Logf("pdf: %q", resp.Content)
		})

		t.Run(fmt.Sprintf("image/%d", run), func(t *testing.T) {
			msg := types.Message{Role: roleUser}
			msg.AddTextPart("What color is this image? One word.")
			msg.AddPart(types.NewImagePartFromData(
				base64.StdEncoding.EncodeToString(solidPNG(t, color.RGBA{R: 255, A: 255})), types.MIMETypeImagePNG, nil))
			resp, err := p.Predict(ctx, providers.PredictionRequest{Messages: []types.Message{msg}})
			require.NoError(t, err)
			assert.Contains(t, strings.ToLower(resp.Content), "red")
			t.Logf("image: %q", resp.Content)
		})

		t.Run(fmt.Sprintf("json/%d", run), func(t *testing.T) {
			resp, err := p.Predict(ctx, providers.PredictionRequest{
				Messages: []types.Message{{Role: roleUser, Content: "What is the capital of France?"}},
				ResponseFormat: &providers.ResponseFormat{
					Type: providers.ResponseFormatJSONSchema,
					JSONSchema: json.RawMessage(`{"type":"object","properties":{"capital":{"type":"string"}},` +
						`"required":["capital"],"additionalProperties":false}`),
				},
			})
			require.NoError(t, err)
			var out struct {
				Capital string `json:"capital"`
			}
			require.NoError(t, json.Unmarshal([]byte(resp.Content), &out), "not JSON: %q", resp.Content)
			assert.Equal(t, "Paris", out.Capital)
			t.Logf("json: %s", resp.Content)
		})
	}
}

func strPtr(s string) *string { return &s }

// solidPNG is a 32x32 PNG of one color.
func solidPNG(t *testing.T, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for x := 0; x < 32; x++ {
		for y := 0; y < 32; y++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// onePagePDF is a minimal one-page PDF that shows word in Helvetica.
func onePagePDF(word string) []byte {
	content := "BT /F1 48 Tf 72 700 Td (" + word + ") Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, 0, len(objs))
	for i, o := range objs {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return []byte(b.String())
}
