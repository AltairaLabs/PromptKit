package sdk

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// WithExecutionTimeout bounds a turn: a provider slower than the timeout fails
// the turn with a deadline error; zero disables the bound.
func TestConv_WithExecutionTimeoutBoundsTheTurn(t *testing.T) {
	ctx := context.Background()
	slow := newConvWire(t, convWireReply{Text: "late", Delay: time.Second})
	conv := openConv(t, slow, WithExecutionTimeout(30*time.Millisecond))
	start := time.Now()
	_, err := conv.Send(ctx, "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pipeline execution timeout")
	assert.Less(t, time.Since(start), 900*time.Millisecond, "the turn must end at the timeout, not the reply")

	unbounded := newConvWire(t, convWireReply{Text: "made it", Delay: 50 * time.Millisecond})
	resp, err := openConv(t, unbounded, WithExecutionTimeout(0)).Send(ctx, "hi")
	require.NoError(t, err)
	assert.Equal(t, "made it", resp.Text())
}

// convLongTurns sends four ~50-token turns and returns the last request's
// texts and the last Send's error.
func convLongTurns(t *testing.T, opts ...Option) ([]string, error) {
	t.Helper()
	w := newConvWire(t, convWireReply{Text: "ok"})
	conv := openConv(t, w, append([]Option{WithStateStore(statestore.NewMemoryStore())}, opts...)...)
	var err error
	for i := 0; i < 4; i++ {
		_, err = conv.Send(context.Background(), fmt.Sprintf("turn %d %s", i, strings.Repeat("word ", 40)))
	}
	var short []string
	for _, x := range convTexts(t, w.lastChat(t)) {
		short = append(short, x[:min(6, len(x))])
	}
	return short, err
}

// WithTokenBudget keeps the request within the budget, counting what the user
// said: "sliding" drops the oldest whole turns, so the history after the system
// prompt opens on a user message; "fail" refuses the turn.
func TestConv_TokenBudgetTruncatesTheRequest(t *testing.T) {
	got, err := convLongTurns(t)
	require.NoError(t, err)
	assert.Equal(t, []string{"You he", "turn 0", "ok", "turn 1", "ok", "turn 2", "ok", "turn 3"}, got,
		"without a budget every turn is sent")

	// The budget fits turn 1's reply but not turn 1 itself: the reply goes too.
	got, err = convLongTurns(t, WithTokenBudget(120), WithTruncation("sliding"))
	require.NoError(t, err)
	assert.Equal(t, []string{"You he", "turn 2", "ok", "turn 3"}, got)

	_, err = convLongTurns(t, WithTokenBudget(120), WithTruncation("fail"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token budget exceeded")
}

// WithRelevanceTruncation keeps the older messages most like the current
// input when the budget forces a cut.
func TestConv_RelevanceTruncationKeepsWhatMatters(t *testing.T) {
	ctx := context.Background()
	w := newConvWire(t, convWireReply{Text: "ok"})
	conv := openConv(t, w, WithStateStore(statestore.NewMemoryStore()), WithTokenBudget(60),
		WithRelevanceTruncation(&RelevanceConfig{EmbeddingProvider: convTopicEmbedder{}, MinRecentMessages: 1,
			QuerySource: "last_user"}))
	filler := strings.Repeat("x ", 12)
	for _, u := range []string{"apples are red " + filler, "cars are fast " + filler, "weather is bad " + filler} {
		_, err := conv.Send(ctx, u)
		require.NoError(t, err)
	}
	_, err := conv.Send(ctx, "more about apples")
	require.NoError(t, err)
	texts := strings.Join(convTexts(t, w.lastChat(t)), "|")
	assert.Contains(t, texts, "apples are red", "the relevant older turn is kept")
	assert.False(t, strings.Contains(texts, "cars are fast") && strings.Contains(texts, "weather is bad"),
		"the irrelevant older turns go first: %s", texts)
}

// convPNG encodes a w×h PNG.
func convPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		img.Set(x, 0, color.RGBA{R: 255, A: 255})
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// convRequestImageSize decodes the image the request's last user message
// carries and returns its dimensions.
func convRequestImageSize(t *testing.T, r convWireRequest) (int, int) {
	t.Helper()
	for _, p := range convUserParts(t, r) {
		iu, ok := p["image_url"].(map[string]any)
		if !ok {
			continue
		}
		u := iu["url"].(string)
		data, err := base64.StdEncoding.DecodeString(u[strings.Index(u, ",")+1:])
		require.NoError(t, err)
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		require.NoError(t, err)
		return cfg.Width, cfg.Height
	}
	t.Fatal("the request carries no image")
	return 0, 0
}

// assertConvImageSize checks the dimensions of the image the request carries.
func assertConvImageSize(t *testing.T, r convWireRequest, wantW, wantH int) {
	t.Helper()
	gw, gh := convRequestImageSize(t, r)
	assert.Equal(t, []int{wantW, wantH}, []int{gw, gh})
}

// WithAutoResize and WithImagePreprocessing shrink an attached image to fit
// before it is sent; without them it is sent as attached.
func TestConv_ImagePreprocessingResizesWhatIsSent(t *testing.T) {
	big := convPNG(t, 400, 200)
	cases := []struct {
		name string
		opts []Option
		w, h int
	}{
		{"none", nil, 400, 200},
		{"auto resize", []Option{WithAutoResize(100, 100)}, 100, 50},
		{"preprocessing", []Option{WithImagePreprocessing(&stage.ImagePreprocessConfig{EnableResize: true,
			Resize: stage.ImageResizeStageConfig{MaxWidth: 200, MaxHeight: 200, Quality: 90, PreserveAspectRatio: true}})},
			200, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newConvWire(t)
			conv := openConv(t, w, tc.opts...)
			_, err := conv.Send(context.Background(), "look", WithImageData(big, "image/png"))
			require.NoError(t, err)
			assertConvImageSize(t, w.lastChat(t), tc.w, tc.h)
		})
	}
}

// Every attachment option puts its content on the user message the
// conversation records, typed and with its MIME type; an image's detail
// reaches the request.
func TestConv_SendAttachmentsAreRecordedOnTheMessage(t *testing.T) {
	dir := t.TempDir()
	file := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, data, 0o600))
		return p
	}
	pngBytes := convPNG(t, 2, 2)
	high, low := "high", "low"
	w := newConvWire(t)
	conv := openConv(t, w, WithUnsafePrivateNetworkMedia())

	_, err := conv.Send(context.Background(), "all of it",
		WithImageFile(file("a.png", pngBytes), &high),
		WithImageURL("https://example.com/b.png", &low),
		WithImageData(pngBytes, "image/png"),
		WithFile("notes.txt", []byte("file body")),
	)
	require.NoError(t, err)

	user := conv.Messages(context.Background())[0]
	var got []string
	for _, p := range user.Parts {
		desc := p.Type
		if p.Media != nil {
			desc += ":" + p.Media.MIMEType
		}
		if p.Text != nil {
			desc += ":" + *p.Text
		}
		got = append(got, desc)
	}
	assert.Equal(t, []string{
		"text:all of it",
		"image:image/png", "image:image/png", "image:image/png",
		"text:[File: notes.txt]\nfile body",
	}, got)

	var details []string
	for _, p := range convUserParts(t, w.lastChat(t)) {
		if iu, ok := p["image_url"].(map[string]any); ok {
			d, _ := iu["detail"].(string)
			details = append(details, d)
		}
	}
	assert.Equal(t, []string{"high", "low", ""}, details)
}

// Without WithSkipSchemaValidation a pack that breaks the PromptPack schema is
// refused at Open, naming the problem; with it the pack opens.
func TestConv_SchemaValidationGuardsOpen(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "k")
	w := newConvWire(t)
	pack := createTestPackFile(t, convChatPack) // no "name" or template_engine: not schema-valid
	_, err := Open(pack, "chat", WithLLMProvider(convWireSpec(w)))
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "schema")

	conv, err := Open(pack, "chat", WithLLMProvider(convWireSpec(w)), WithSkipSchemaValidation())
	require.NoError(t, err)
	_ = conv.Close()
}

// Audio, video and document attachments are recorded on the user message with their MIME
// type (the mock provider accepts every modality).
func TestConv_SendAudioAndVideoAreRecordedOnTheMessage(t *testing.T) {
	dir := t.TempDir()
	mp4 := filepath.Join(dir, "v.mp4")
	require.NoError(t, os.WriteFile(mp4, []byte("....ftypmp42"), 0o600))
	pdf := filepath.Join(dir, "d.pdf")
	require.NoError(t, os.WriteFile(pdf, []byte("%PDF-1.4"), 0o600))
	conv, err := Open(createTestPackFile(t, convChatPack), "chat", WithSkipSchemaValidation(),
		WithProvider(mock.NewProvider("mock", "mock-model", false)))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })
	_, err = conv.Send(context.Background(), "listen and watch",
		WithAudioData([]byte("RIFFxxxxWAVE"), "audio/wav"), WithVideoFile(mp4), WithVideoData([]byte("v"), "video/webm"),
		WithDocumentData([]byte("%PDF-1.4"), types.MIMETypePDF), WithDocumentFile(pdf))
	require.NoError(t, err)
	var got []string
	for _, p := range conv.Messages(context.Background())[0].Parts {
		desc := p.Type
		if p.Media != nil {
			desc += ":" + p.Media.MIMEType
		}
		got = append(got, desc)
	}
	assert.Equal(t, []string{"text", "audio:audio/wav", "video:video/mp4", "video:video/webm",
		"document:application/pdf", "document:application/pdf"}, got)
}
