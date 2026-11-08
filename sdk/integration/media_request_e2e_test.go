package integration

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
)

// tinyPNG returns a valid 2x2 PNG.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func imageOpts(t *testing.T, n int) []sdk.SendOption {
	t.Helper()
	data := tinyPNG(t)
	opts := make([]sdk.SendOption, n)
	for i := range opts {
		opts[i] = sdk.WithImageData(data, "image/png")
	}
	return opts
}

// TestMediaItemCap_AppliesPerMessageOnEveryProvider sends one message with one
// image over the per-message cap through every HTTP provider and both request
// paths. Nothing may reach the server, and the error names the cap. A message
// at the cap is sent.
func TestMediaItemCap_AppliesPerMessageOnEveryProvider(t *testing.T) {
	for _, vc := range vendorCases {
		for _, shape := range turnShapes {
			t.Run(vc.name+"/"+shape.name, func(t *testing.T) {
				srv := newFakeVendorServer(t)
				conv := openHTTPConvWithPack(t, shape.pack,
					providerPaths[0].opt(t, vc, srv.URL, testAPIKey, "", sdk.ProviderSpec{}))

				_, err := conv.Send(context.Background(), "describe these",
					imageOpts(t, providers.MaxMediaItems+1)...)
				require.Error(t, err)
				assert.True(t, errors.Is(err, providers.ErrMediaItemLimitExceeded),
					"error does not wrap ErrMediaItemLimitExceeded: %v", err)
				assert.Empty(t, srv.got(), "a message over the media cap reached the server")

				resp, err := conv.Send(context.Background(), "describe these",
					imageOpts(t, providers.MaxMediaItems)...)
				require.NoError(t, err, "a message at the cap must be sent")
				assert.Equal(t, "fake reply", resp.Text())
				require.Len(t, srv.got(), 1)
			})
		}
	}
}

// TestMultimodalValidation_RejectsUnsupportedPartBeforeSending sends a video
// part to providers that cannot send video. The turn fails with
// *providers.UnsupportedContentError and nothing reaches the server.
func TestMultimodalValidation_RejectsUnsupportedPartBeforeSending(t *testing.T) {
	for _, vc := range vendorCases {
		if vc.typ == "gemini" {
			continue // Gemini sends video
		}
		for _, shape := range turnShapes {
			t.Run(vc.name+"/"+shape.name, func(t *testing.T) {
				srv := newFakeVendorServer(t)
				conv := openHTTPConvWithPack(t, shape.pack,
					providerPaths[0].opt(t, vc, srv.URL, testAPIKey, "", sdk.ProviderSpec{}))

				_, err := conv.Send(context.Background(), "what happens here?",
					sdk.WithVideoData([]byte("not really a video"), "video/mp4"))
				require.Error(t, err)
				var unsupported *providers.UnsupportedContentError
				require.True(t, errors.As(err, &unsupported),
					"error is not an UnsupportedContentError: %v", err)
				assert.Equal(t, "video", unsupported.ContentType)
				assert.Empty(t, srv.got(), "an unsupported part reached the server")
			})
		}
	}
}
