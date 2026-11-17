//go:build integration

package sdk_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// Live verification that a tool's image reaches a realtime model: the tool
// returns only a PNG showing a number, and the reply must name it.
//
//	go -C sdk test -tags integration -run 'TestLiveRealtimeTools_ToolImage' -count=1 -v .

// rtlBadgeNumber is drawn in the image; the model cannot guess it.
const rtlBadgeNumber = "47"

const rtlBadgePackJSON = `{
	"id": "rt-live-tool-image",
	"version": "1.0.0",
	"description": "Live realtime tool image verification",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a terse assistant. For any question about the badge you must call the get_badge tool; never guess. It returns a picture of the badge. Read the number printed on it and reply with that number only. If you received no picture, reply NONE.",
			"tools": ["get_badge"]
		}
	},
	"tools": {
		"get_badge": {
			"name": "get_badge",
			"description": "Get a picture of today's badge",
			"parameters": {"type": "object", "properties": {}}
		}
	}
}`

// rtlDigits is a 5x7 bitmap of the digits drawn.
var rtlDigits = map[rune][7]string{
	'4': {"...#.", "..##.", ".#.#.", "#..#.", "#####", "...#.", "...#."},
	'7': {"#####", "....#", "...#.", "..#..", ".#...", ".#...", ".#..."},
}

// rtlBadgePNG renders rtlBadgeNumber black on white, base64-encoded.
func rtlBadgePNG(t *testing.T) string {
	t.Helper()
	const scale, pad = 24, 24
	w := pad*2 + len(rtlBadgeNumber)*6*scale
	h := pad*2 + 7*scale
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.White)
		}
	}
	for i, r := range rtlBadgeNumber {
		for row, line := range rtlDigits[r] {
			for col, c := range line {
				if c != '#' {
					continue
				}
				x0, y0 := pad+(i*6+col)*scale, pad+row*scale
				for y := y0; y < y0+scale; y++ {
					for x := x0; x < x0+scale; x++ {
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

// rtlImageExecutor returns only an image.
type rtlImageExecutor struct{ data string }

func (e *rtlImageExecutor) Name() string { return "rtl-image" }

func (e *rtlImageExecutor) Execute(context.Context, *tools.ToolDescriptor, json.RawMessage) (json.RawMessage, error) {
	return nil, nil
}

func (e *rtlImageExecutor) ExecuteCall(
	context.Context, *tools.ToolDescriptor, tools.ToolCall,
) (json.RawMessage, []types.ContentPart, error) {
	return nil, []types.ContentPart{types.NewImagePartFromData(e.data, types.MIMETypeImagePNG, nil)}, nil
}

func TestLiveRealtimeTools_ToolImage(t *testing.T) {
	rtlEachRealtime(t, func(t *testing.T, rt rtlRealtime) {
		bus, ev := rtlBus(t)
		conv := rtlOpenDuplex(t, rt, rtlBadgePackJSON, sdk.WithEventBus(bus))
		conv.OnToolExecutor("get_badge", &rtlImageExecutor{data: rtlBadgePNG(t)})
		st := rtlWatch(t, conv)

		require.NoError(t, conv.SendText(context.Background(), "What number is on today's badge?"))
		require.Truef(t, st.waitText(0, rtlTimeout, rtlBadgeNumber),
			"the reply never read the tool's image: %q (events %s)", st.Text(), ev.summary())
		assert.NotContains(t, strings.ToUpper(st.Text()), "NONE")
		assert.Empty(t, st.Errors())
		t.Logf("reply: %q", st.Text())
	})
}
