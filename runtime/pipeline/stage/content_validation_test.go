package stage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/composition"
	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/claude"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/gemini"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/ollama"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/openai"
	_ "github.com/AltairaLabs/PromptKit/runtime/v2/providers/vllm"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// countingServer answers every request with a 400 and counts them. The tests
// only ask whether a request was sent, so the provider failing to read the
// reply is expected.
func countingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"test server"}}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// multimodalNonStreaming hides a provider's streaming support, so the stage
// takes its non-streaming round, while keeping its declared capabilities.
type multimodalNonStreaming struct {
	providers.Provider
	caps providers.MultimodalCapabilityProvider
}

func (p *multimodalNonStreaming) SupportsStreaming() bool { return false }

func (p *multimodalNonStreaming) GetMultimodalCapabilities() providers.MultimodalCapabilities {
	return p.caps.GetMultimodalCapabilities()
}

// SendableMultimodalCapabilities forwards the wrapped provider's sendable
// capabilities, falling back to its declared ones, as validation would.
func (p *multimodalNonStreaming) SendableMultimodalCapabilities() providers.MultimodalCapabilities {
	if sp, ok := p.caps.(providers.SendableContentProvider); ok {
		return sp.SendableMultimodalCapabilities()
	}
	return p.caps.GetMultimodalCapabilities()
}

func withoutStreaming(t *testing.T, p providers.Provider) providers.Provider {
	t.Helper()
	caps, ok := p.(providers.MultimodalCapabilityProvider)
	require.True(t, ok, "%s declares no multimodal capabilities", p.ID())
	return &multimodalNonStreaming{Provider: p, caps: caps}
}

func messageWith(part types.ContentPart) types.Message {
	return types.Message{Role: "user", Parts: []types.ContentPart{types.NewTextPart("look"), part}}
}

func runContentStage(p providers.Provider, cfg *ProviderConfig, msg types.Message) error {
	input := make(chan StreamElement, 1)
	input <- NewMessageElement(&msg)
	close(input)
	output := make(chan StreamElement, 50)
	err := NewProviderStage(p, nil, nil, cfg).Process(context.Background(), input, output)
	for range output { //nolint:revive // drain
	}
	return err
}

const b64 = "aGVsbG8="

var (
	pngPart  = types.NewImagePartFromData(b64, types.MIMETypeImagePNG, nil)
	bmpPart  = types.NewImagePartFromData(b64, "image/bmp", nil)
	wavPart  = types.NewAudioPartFromData(b64, types.MIMETypeAudioWAV)
	mp4Part  = types.NewVideoPartFromData(b64, types.MIMETypeVideoMP4)
	pdfPart  = types.NewDocumentPartFromData(b64, types.MIMETypePDF)
	docxPart = types.NewDocumentPartFromData(b64, types.MIMETypeDocx)
)

type contentCase struct {
	name        string
	spec        providers.ProviderSpec
	supported   types.ContentPart
	unsupported types.ContentPart
}

func contentCases() []contentCase {
	return []contentCase{
		{
			name:        "openai chat completions",
			spec:        providers.ProviderSpec{Type: "openai", Model: "gpt-4o", AdditionalConfig: map[string]any{"api_mode": "completions"}},
			supported:   pngPart,
			unsupported: pdfPart,
		},
		{
			name: "openai chat completions audio model",
			spec: providers.ProviderSpec{
				Type: "openai", Model: "gpt-audio", AdditionalConfig: map[string]any{"api_mode": "completions"},
			},
			supported:   wavPart,
			unsupported: mp4Part,
		},
		{
			name: "openai responses",
			spec: providers.ProviderSpec{
				Type: "openai", Model: "gpt-audio", AdditionalConfig: map[string]any{"api_mode": "responses"},
			},
			supported:   pngPart,
			unsupported: wavPart,
		},
		{
			name:        "claude",
			spec:        providers.ProviderSpec{Type: "claude", Model: "claude-sonnet-4-6"},
			supported:   pdfPart,
			unsupported: wavPart,
		},
		{
			name:        "gemini",
			spec:        providers.ProviderSpec{Type: "gemini", Model: "gemini-2.5-flash"},
			supported:   docxPart,
			unsupported: bmpPart,
		},
		{
			name:        "ollama",
			spec:        providers.ProviderSpec{Type: "ollama", Model: "llava"},
			supported:   pngPart,
			unsupported: wavPart,
		},
		{
			name:        "vllm",
			spec:        providers.ProviderSpec{Type: "vllm", Model: "qwen-vl"},
			supported:   pngPart,
			unsupported: pdfPart,
		},
	}
}

func newContentProvider(t *testing.T, spec providers.ProviderSpec, baseURL string) providers.Provider {
	t.Helper()
	spec.ID = "content-" + spec.Type
	spec.BaseURL = baseURL
	p, err := providers.CreateProviderFromSpec(spec)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func setContentProviderKeys(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "test-key")
	t.Setenv("ANTHROPIC_API_KEY", "test-key")
	t.Setenv("GEMINI_API_KEY", "test-key")
}

// Every built-in provider that declares its capabilities: a part it declares
// reaches the API, and a part it does not declare fails with
// UnsupportedContentError before any request is sent, on both the streaming
// and the non-streaming round.
func TestProviderStage_ValidatesContentBeforeCallingProvider(t *testing.T) {
	setContentProviderKeys(t)
	paths := map[string]func(*testing.T, providers.Provider) providers.Provider{
		"streaming":     func(_ *testing.T, p providers.Provider) providers.Provider { return p },
		"non-streaming": withoutStreaming,
	}
	for _, tc := range contentCases() {
		for pathName, wrap := range paths {
			t.Run(tc.name+"/"+pathName, func(t *testing.T) {
				t.Run("supported part is sent", func(t *testing.T) {
					srv, hits := countingServer(t)
					p := wrap(t, newContentProvider(t, tc.spec, srv.URL))
					err := runContentStage(p, &ProviderConfig{}, messageWith(tc.supported))
					var unsupported *providers.UnsupportedContentError
					assert.False(t, errors.As(err, &unsupported), "supported part rejected: %v", err)
					assert.Positive(t, hits.Load(), "a supported part must reach the provider API")
				})
				t.Run("unsupported part fails before the call", func(t *testing.T) {
					srv, hits := countingServer(t)
					p := wrap(t, newContentProvider(t, tc.spec, srv.URL))
					err := runContentStage(p, &ProviderConfig{}, messageWith(tc.unsupported))
					var unsupported *providers.UnsupportedContentError
					require.ErrorAs(t, err, &unsupported)
					assert.Equal(t, 1, unsupported.PartIndex)
					assert.Zero(t, hits.Load(), "no request may be sent for an unsupported part")
				})
			})
		}
	}
}

// A config's capabilities list never narrows validation. Configs often list
// capabilities for scenario selection, not to describe the model, so a part
// the adapter can send must reach the API even when the list leaves its
// modality out.
func TestProviderStage_ConfigCapabilitiesNeverNarrowValidation(t *testing.T) {
	setContentProviderKeys(t)
	cases := []struct {
		name string
		spec providers.ProviderSpec
		part types.ContentPart
	}{
		{
			name: "claude without documents sends a PDF",
			spec: providers.ProviderSpec{Type: "claude", Model: "claude-sonnet-4-6", Capabilities: []string{"text", "vision"}},
			part: pdfPart,
		},
		{
			name: "gemini without audio sends audio",
			spec: providers.ProviderSpec{Type: "gemini", Model: "gemini-2.5-flash", Capabilities: []string{"text", "vision"}},
			part: wavPart,
		},
		{
			name: "openai without vision sends an image",
			spec: providers.ProviderSpec{
				Type: "openai", Model: "gpt-4o", Capabilities: []string{"text", "tools"},
				AdditionalConfig: map[string]any{"api_mode": "completions"},
			},
			part: pngPart,
		},
	}
	paths := map[string]func(*testing.T, providers.Provider) providers.Provider{
		"streaming":     func(_ *testing.T, p providers.Provider) providers.Provider { return p },
		"non-streaming": withoutStreaming,
	}
	for _, tc := range cases {
		for pathName, wrap := range paths {
			t.Run(tc.name+"/"+pathName, func(t *testing.T) {
				srv, hits := countingServer(t)
				p := wrap(t, newContentProvider(t, tc.spec, srv.URL))
				err := runContentStage(p, &ProviderConfig{}, messageWith(tc.part))
				var unsupported *providers.UnsupportedContentError
				assert.False(t, errors.As(err, &unsupported), "config list narrowed validation: %v", err)
				assert.Positive(t, hits.Load(), "a part the adapter can send must reach the provider API")
			})
		}
	}
}

// SkipContentValidation hands the part to the provider as it is: Gemini sends
// a BMP image, which it does not declare, to the API.
func TestProviderStage_SkipContentValidationSendsUndeclaredParts(t *testing.T) {
	setContentProviderKeys(t)
	srv, hits := countingServer(t)
	p := newContentProvider(t, providers.ProviderSpec{Type: "gemini", Model: "gemini-2.5-flash"}, srv.URL)

	err := runContentStage(p, &ProviderConfig{SkipContentValidation: true}, messageWith(bmpPart))

	var unsupported *providers.UnsupportedContentError
	assert.False(t, errors.As(err, &unsupported), "validation ran despite SkipContentValidation: %v", err)
	assert.Positive(t, hits.Load(), "the undeclared part must reach the provider API")
}

// Composition steps take the opt-out from their deps, and validate by default.
func TestCompositionStepProviderConfig_CarriesSkipContentValidation(t *testing.T) {
	reg := prompt.NewRegistryWithRepository(newMockRepo())
	registerSimplePrompt(t, reg, "p")
	step := &composition.Step{ID: "s", Kind: composition.KindPrompt, PromptTask: "p"}
	prov := mock.NewProvider("m", "m", false)

	for _, skip := range []bool{true, false} {
		deps := CompositionExecutorDeps{PromptRegistry: reg, SkipContentValidation: skip}
		cfg, err := deps.stepProviderConfig(step, prov)
		require.NoError(t, err)
		assert.Equal(t, skip, cfg.SkipContentValidation)
	}
}

// A provider that declares no multimodal capabilities is not checked: the mock
// provider answers a request carrying audio, video and a document.
func TestProviderStage_UndeclaredProviderIsNotValidated(t *testing.T) {
	p := mock.NewProvider("mock", "mock-model", false)
	msg := types.Message{Role: "user", Parts: []types.ContentPart{
		types.NewTextPart("look"), wavPart, mp4Part, pdfPart,
	}}

	input := make(chan StreamElement, 1)
	input <- NewMessageElement(&msg)
	close(input)
	output := make(chan StreamElement, 50)
	err := NewProviderStage(p, nil, nil, &ProviderConfig{}).Process(context.Background(), input, output)
	var replies []string
	for e := range output {
		if e.Message != nil && e.Message.Role == "assistant" {
			replies = append(replies, e.Message.GetContent())
		}
	}
	require.NoError(t, err)
	require.Len(t, replies, 1, "the mock must have been called and answered")
	assert.Contains(t, replies[0], "Mock response")
}

// Media in an assistant reply is what a model produced, not input, so it is
// not checked: vLLM, which declares no audio, still sends a history holding
// an assistant audio reply (its adapter sends the reply's text).
func TestProviderStage_AssistantMediaIsNotValidated(t *testing.T) {
	srv, hits := countingServer(t)
	p := withoutStreaming(t, newContentProvider(t, providers.ProviderSpec{Type: "vllm", Model: "qwen-vl"}, srv.URL))
	history := []types.Message{
		{Role: "user", Content: "say hi"},
		{Role: "assistant", Parts: []types.ContentPart{types.NewTextPart("hi"), wavPart}},
		{Role: "user", Content: "again"},
	}
	input := make(chan StreamElement, len(history))
	for i := range history {
		input <- NewMessageElement(&history[i])
	}
	close(input)
	output := make(chan StreamElement, 50)
	err := NewProviderStage(p, nil, nil, &ProviderConfig{}).Process(context.Background(), input, output)
	for range output { //nolint:revive // drain
	}

	var unsupported *providers.UnsupportedContentError
	assert.False(t, errors.As(err, &unsupported), "assistant media was validated: %v", err)
	assert.Positive(t, hits.Load())
}
