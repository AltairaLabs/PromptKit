//go:build integration

package gemini

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Live: a tool result carrying two images reaches the model on a Gemini 2.x
// model (media after the function responses) and a Gemini 3 model (media in
// the function response's parts). The numbers drawn cannot be guessed.
//
//	go -C runtime test -tags integration -run TestLiveToolResultMedia -count=1 -v ./providers/gemini/

var liveDigits = map[rune][7]string{
	'4': {"...#.", "..##.", ".#.#.", "#..#.", "#####", "...#.", "...#."},
	'7': {"#####", "....#", "...#.", "..#..", ".#...", ".#...", ".#..."},
	'2': {".###.", "#...#", "....#", "...#.", "..#..", ".#...", "#####"},
	'9': {".###.", "#...#", "#...#", ".####", "....#", "...#.", ".##.."},
}

func liveNumberPNG(t *testing.T, number string) string {
	t.Helper()
	const scale, pad = 16, 16
	w, h := pad*2+len(number)*6*scale, pad*2+7*scale
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.White)
		}
	}
	for i, r := range number {
		for row, line := range liveDigits[r] {
			for col, c := range line {
				if c != '#' {
					continue
				}
				for y := pad + row*scale; y < pad+(row+1)*scale; y++ {
					for x := pad + (i*6+col)*scale; x < pad+(i*6+col+1)*scale; x++ {
						img.Set(x, y, color.Black)
					}
				}
			}
		}
	}
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

func TestLiveToolResultMedia_TwoImages(t *testing.T) {
	if os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	for _, model := range []string{"gemini-2.5-flash", "gemini-3.8-flash"} {
		t.Run(model, func(t *testing.T) {
			p := NewProvider("live", model, "https://generativelanguage.googleapis.com/v1beta",
				providers.ProviderDefaults{MaxTokens: 2048}, false)
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			resp, err := p.Predict(ctx, providers.PredictionRequest{
				System: "Read the numbers printed on the badge pictures the tool returned and reply with them only, " +
					"comma separated, in order. If you received no pictures, reply NONE.",
				MaxTokens: 2048,
				Messages: []types.Message{
					{Role: "user", Content: "What numbers are on today's two badges?"},
					{Role: "assistant", ToolCalls: []types.MessageToolCall{
						{ID: "call_1", Name: "get_badges", Args: json.RawMessage(`{}`)}}},
					{Role: "tool", ToolResult: &types.MessageToolResult{ID: "call_1", Name: "get_badges",
						Parts: []types.ContentPart{
							types.NewTextPart("Two badge pictures"),
							types.NewImagePartFromData(liveNumberPNG(t, "47"), types.MIMETypeImagePNG, nil),
							types.NewImagePartFromData(liveNumberPNG(t, "92"), types.MIMETypeImagePNG, nil),
						}}},
				},
			})
			require.NoError(t, err)
			t.Logf("reply: %q", resp.Content)
			assert.Contains(t, resp.Content, "47")
			assert.Contains(t, resp.Content, "92")
			assert.NotContains(t, strings.ToUpper(resp.Content), "NONE")
		})
	}
}
