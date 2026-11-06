package conformance_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// providers.MaxMediaItems and providers.MaxAggregateMediaSize cap the media one
// message makes a provider load. Each provider shares one MediaLoader across a
// message's parts; a loader per part counts one item and never reaches either
// cap (#2468). These tests drive every provider, on every request path, with a
// message at each cap and one over it.

// noopCredential lets a Bedrock-platform provider send to the test server.
type noopCredential struct{}

func (noopCredential) Apply(context.Context, *http.Request) error { return nil }
func (noopCredential) Type() string                               { return "aws" }

// mediaCapSpecs are the providers whose adapters load media, with the platform
// variants that build requests through their own code.
func mediaCapSpecs() []providers.ProviderSpec {
	bedrock := &providers.PlatformConfig{Type: "bedrock", Region: "us-west-2"}
	return []providers.ProviderSpec{
		{Type: "openai", Model: "gpt-4o", AdditionalConfig: map[string]any{"api_mode": "completions"}},
		{Type: "openai", Model: "gpt-4o", AdditionalConfig: map[string]any{"api_mode": "responses"}},
		{Type: "openai", Model: "openai.gpt-oss-120b-1:0", Platform: "bedrock", PlatformConfig: bedrock,
			Credential: noopCredential{}},
		{Type: "claude", Model: "claude-sonnet-4-6"},
		{Type: "claude", Model: "anthropic.claude-sonnet-4-6", Platform: "bedrock", PlatformConfig: bedrock,
			Credential: noopCredential{}},
		{Type: "gemini", Model: "gemini-2.5-flash"},
		{Type: "ollama", Model: "llava"},
		{Type: "vllm", Model: "llava"},
	}
}

func mediaCapSpecName(spec providers.ProviderSpec) string {
	name := spec.Type
	if spec.Platform != "" {
		name += "/" + spec.Platform
	}
	if mode, ok := spec.AdditionalConfig["api_mode"].(string); ok {
		name += "/" + mode
	}
	return name
}

// countingServer counts the requests it receives and, when keep is set, keeps
// their bodies. It answers with an empty JSON object: the tests read what was
// sent, not the provider's parse of the reply.
type countingServer struct {
	mu     sync.Mutex
	keep   bool
	bodies []string
	hits   int
	srv    *httptest.Server
}

func newCountingServer(t *testing.T, keep bool) *countingServer {
	t.Helper()
	cs := &countingServer{keep: keep}
	cs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body string
		if keep {
			b, _ := io.ReadAll(r.Body)
			body = string(b)
		} else {
			_, _ = io.Copy(io.Discard, r.Body)
		}
		cs.mu.Lock()
		cs.hits++
		if keep {
			cs.bodies = append(cs.bodies, body)
		}
		cs.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(cs.srv.Close)
	return cs
}

func (c *countingServer) requests() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits
}

func (c *countingServer) lastBody() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.bodies) == 0 {
		return ""
	}
	return c.bodies[len(c.bodies)-1]
}

// buildMediaCapProvider builds spec against the server with its payload-size
// check off, so a request at the aggregate media cap is judged by the media
// caps alone.
func buildMediaCapProvider(t *testing.T, spec providers.ProviderSpec, url string) providers.Provider {
	t.Helper()
	t.Setenv("OPENAI_API_KEY", "k")
	t.Setenv("ANTHROPIC_API_KEY", "k")
	t.Setenv("GEMINI_API_KEY", "k")
	spec.ID, spec.BaseURL = "caps-"+spec.Type, url
	p, err := providers.CreateProviderFromSpec(spec)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	if s, ok := p.(interface{ SetMaxPayloadSize(int64) }); ok {
		s.SetMaxPayloadSize(0)
	}
	return p
}

var mediaCapPaths = []string{"predict", "predict_stream", "predict_with_tools", "predict_stream_with_tools"}

// sendOnPath sends req down one request path and returns the first error the
// caller sees, whether returned or carried on a stream chunk.
func sendOnPath(t *testing.T, p providers.Provider, path string, req providers.PredictionRequest) error {
	t.Helper()
	ctx := context.Background()
	drain := func(ch <-chan providers.StreamChunk, err error) error {
		if err != nil {
			return err
		}
		var first error
		for chunk := range ch {
			if chunk.Error != nil && first == nil {
				first = chunk.Error
			}
		}
		return first
	}
	tools := func() (providers.ToolSupport, providers.ProviderTools) {
		ts, ok := p.(providers.ToolSupport)
		require.Truef(t, ok, "%T has no tool support", p)
		built, err := ts.BuildTooling([]*providers.ToolDescriptor{{
			Name: "probe", Description: "a probe tool", InputSchema: json.RawMessage(`{"type":"object"}`),
		}})
		require.NoError(t, err)
		return ts, built
	}
	switch path {
	case "predict":
		_, err := p.Predict(ctx, req)
		return err
	case "predict_stream":
		return drain(p.PredictStream(ctx, req))
	case "predict_with_tools":
		ts, built := tools()
		_, _, err := ts.PredictWithTools(ctx, req, built, "auto")
		return err
	case "predict_stream_with_tools":
		ts, built := tools()
		return drain(ts.PredictStreamWithTools(ctx, req, built, "auto"))
	}
	t.Fatalf("unknown path %q", path)
	return nil
}

// inlineImage is item i's base64 data; each is distinct so the request body
// shows which items reached the wire.
func inlineImage(i int) string {
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("cap-image-%03d", i)))
}

// imageMessage is a user message with text and n inline images, numbered from
// first.
func imageMessage(first, n int) types.Message {
	msg := types.Message{Role: "user"}
	msg.AddTextPart("describe these")
	for i := first; i < first+n; i++ {
		msg.AddPart(types.NewImagePartFromData(inlineImage(i), types.MIMETypeImagePNG, nil))
	}
	return msg
}

func assertImagesOnWire(t *testing.T, body string, n int) {
	t.Helper()
	require.NotEmpty(t, body, "no request reached the server")
	for i := 0; i < n; i++ {
		require.Containsf(t, body, inlineImage(i), "image %d missing from the request", i)
	}
}

func TestProviders_MediaItemCapPerMessage(t *testing.T) {
	for _, spec := range mediaCapSpecs() {
		t.Run(mediaCapSpecName(spec), func(t *testing.T) {
			for _, path := range mediaCapPaths {
				t.Run(path, func(t *testing.T) {
					t.Run("at cap", func(t *testing.T) {
						cs := newCountingServer(t, true)
						p := buildMediaCapProvider(t, spec, cs.srv.URL)
						err := sendOnPath(t, p, path, providers.PredictionRequest{
							MaxTokens: 64, Messages: []types.Message{imageMessage(0, providers.MaxMediaItems)},
						})
						assert.NotErrorIs(t, err, providers.ErrMediaItemLimitExceeded)
						assertImagesOnWire(t, cs.lastBody(), providers.MaxMediaItems)
					})
					t.Run("over cap", func(t *testing.T) {
						cs := newCountingServer(t, true)
						p := buildMediaCapProvider(t, spec, cs.srv.URL)
						err := sendOnPath(t, p, path, providers.PredictionRequest{
							MaxTokens: 64, Messages: []types.Message{imageMessage(0, providers.MaxMediaItems+1)},
						})
						require.ErrorIs(t, err, providers.ErrMediaItemLimitExceeded)
						assert.Equal(t, 0, cs.requests(), "a request went out over the media item cap")
					})
					// The cap is per message, not per request: a conversation
					// resends every earlier message, so a per-request cap would
					// fail every turn after its media first added up to it.
					t.Run("each message at cap", func(t *testing.T) {
						cs := newCountingServer(t, true)
						p := buildMediaCapProvider(t, spec, cs.srv.URL)
						err := sendOnPath(t, p, path, providers.PredictionRequest{
							MaxTokens: 64, Messages: []types.Message{
								imageMessage(0, providers.MaxMediaItems),
								{Role: "assistant", Content: "two cats"},
								imageMessage(providers.MaxMediaItems, providers.MaxMediaItems),
							},
						})
						assert.NotErrorIs(t, err, providers.ErrMediaItemLimitExceeded)
						assertImagesOnWire(t, cs.lastBody(), 2*providers.MaxMediaItems)
					})
				})
			}
		})
	}
}

// toolResultImageRequest is a tool round whose tool result carries n images.
func toolResultImageRequest(n int) providers.PredictionRequest {
	result := &types.MessageToolResult{ID: "call-1", Name: "probe"}
	for i := 0; i < n; i++ {
		result.Parts = append(result.Parts, types.NewImagePartFromData(inlineImage(i), types.MIMETypeImagePNG, nil))
	}
	return providers.PredictionRequest{MaxTokens: 64, Messages: []types.Message{
		{Role: "user", Content: "take the pictures"},
		{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: "call-1", Name: "probe", Args: json.RawMessage(`{}`)}}},
		{Role: "tool", ToolResult: result},
	}}
}

// Tool results are messages too: a provider that sends a tool result's images
// loads them through one loader per tool result. Gemini sends only a tool
// result's first media part, and Ollama and vLLM send tool results as text, so
// neither can reach the cap there.
func TestProviders_MediaItemCapPerToolResult(t *testing.T) {
	for _, spec := range mediaCapSpecs() {
		if spec.Type != "claude" && !(spec.Type == "openai" && spec.AdditionalConfig["api_mode"] != "responses") {
			continue
		}
		t.Run(mediaCapSpecName(spec), func(t *testing.T) {
			for _, path := range []string{"predict_with_tools", "predict_stream_with_tools"} {
				t.Run(path, func(t *testing.T) {
					t.Run("at cap", func(t *testing.T) {
						cs := newCountingServer(t, true)
						p := buildMediaCapProvider(t, spec, cs.srv.URL)
						err := sendOnPath(t, p, path, toolResultImageRequest(providers.MaxMediaItems))
						assert.NotErrorIs(t, err, providers.ErrMediaItemLimitExceeded)
						assertImagesOnWire(t, cs.lastBody(), providers.MaxMediaItems)
					})
					t.Run("over cap", func(t *testing.T) {
						cs := newCountingServer(t, true)
						p := buildMediaCapProvider(t, spec, cs.srv.URL)
						err := sendOnPath(t, p, path, toolResultImageRequest(providers.MaxMediaItems+1))
						require.ErrorIs(t, err, providers.ErrMediaItemLimitExceeded)
						assert.Equal(t, 0, cs.requests(), "a request went out over the media item cap")
					})
				})
			}
		})
	}
}

// sparseImage creates a file of size bytes without writing them, so a test can
// reach the aggregate cap without filling the disk.
func sparseImage(t *testing.T, dir string, i int, size int64) string {
	t.Helper()
	path := filepath.Join(dir, fmt.Sprintf("img-%d.png", i))
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(size))
	require.NoError(t, f.Close())
	return path
}

// fileImageMessage is a user message with one image file per size.
func fileImageMessage(t *testing.T, sizes ...int64) types.Message {
	t.Helper()
	dir := t.TempDir()
	msg := types.Message{Role: "user"}
	msg.AddTextPart("describe these")
	for i, size := range sizes {
		path := sparseImage(t, dir, i, size)
		msg.AddPart(types.ContentPart{
			Type:  types.ContentTypeImage,
			Media: &types.MediaContent{FilePath: &path, MIMEType: types.MIMETypeImagePNG},
		})
	}
	return msg
}

// The aggregate cap is checked on one path per adapter: the item cap test above
// shows that every path, platform and API mode shares one loader per message,
// and the loader applies both caps the same way. Each case moves 100 MB, so the
// Bedrock and Responses variants, which convert media with the same code as
// their adapter, are left to the item cap test. Two files of half the cap reach
// it exactly, since the per-file limit is also half of it.
func TestProviders_AggregateMediaSizeCapPerMessage(t *testing.T) {
	if testing.Short() {
		t.Skip("loads 100 MB of media per provider")
	}
	half := providers.MaxAggregateMediaSize / 2
	for _, spec := range mediaCapSpecs() {
		if spec.Platform != "" || spec.AdditionalConfig["api_mode"] == "responses" {
			continue
		}
		t.Run(mediaCapSpecName(spec), func(t *testing.T) {
			t.Run("at cap", func(t *testing.T) {
				cs := newCountingServer(t, false)
				p := buildMediaCapProvider(t, spec, cs.srv.URL)
				err := sendOnPath(t, p, "predict", providers.PredictionRequest{
					MaxTokens: 64, Messages: []types.Message{fileImageMessage(t, half, half)},
				})
				assert.NotErrorIs(t, err, providers.ErrAggregateMediaSizeExceeded)
				assert.Positive(t, cs.requests(), "the request at the aggregate media cap was not sent")
			})
			t.Run("over cap", func(t *testing.T) {
				cs := newCountingServer(t, false)
				p := buildMediaCapProvider(t, spec, cs.srv.URL)
				err := sendOnPath(t, p, "predict", providers.PredictionRequest{
					MaxTokens: 64, Messages: []types.Message{fileImageMessage(t, half, half, 1)},
				})
				require.ErrorIs(t, err, providers.ErrAggregateMediaSizeExceeded)
				assert.Equal(t, 0, cs.requests(), "a request went out over the aggregate media cap")
				assert.True(t, strings.Contains(err.Error(), "split the media across messages"), err.Error())
			})
		})
	}
}
