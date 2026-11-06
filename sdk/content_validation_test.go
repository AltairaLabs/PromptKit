package sdk

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	mock "github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/statestore"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
	"github.com/AltairaLabs/PromptKit/sdk/v2/internal/pack"
	"github.com/AltairaLabs/PromptKit/sdk/v2/session"
	sdktools "github.com/AltairaLabs/PromptKit/sdk/v2/tools"
)

// textOnlyProvider is the mock provider declaring that it takes no media.
type textOnlyProvider struct{ *mock.Provider }

func (p *textOnlyProvider) GetMultimodalCapabilities() providers.MultimodalCapabilities {
	return providers.MultimodalCapabilities{}
}

func newContentValidationConversation(t *testing.T, cfg *config) *Conversation {
	t.Helper()
	cfg.provider = &textOnlyProvider{
		Provider: mock.NewProviderWithRepository("text-only", "m", false, mock.NewInMemoryMockRepository("ok")),
	}
	p := &pack.Pack{Pack: packspec.Pack{
		ID:      "test-pack",
		Prompts: map[string]*pack.Prompt{"chat": {ID: "chat", SystemTemplate: "You are helpful."}},
	}}
	store := statestore.NewMemoryStore()
	conv := &Conversation{
		pack:           p,
		prompt:         p.Prompts["chat"],
		promptName:     "chat",
		promptRegistry: pack.ToPromptRegistry(p),
		toolRegistry:   tools.NewRegistry(),
		config:         cfg,
		mode:           UnaryMode,
		handlers:       make(map[string]ToolHandler),
		asyncHandlers:  make(map[string]sdktools.AsyncToolHandler),
		pendingStore:   sdktools.NewMemoryPendingStore(),
	}
	pipe, err := conv.buildPipelineWithParams(store, "conv", nil, nil)
	require.NoError(t, err)
	conv.unarySession, err = session.NewUnarySession(session.UnarySessionConfig{
		ConversationID: "conv", StateStore: store, Pipeline: pipe,
	})
	require.NoError(t, err)
	return conv
}

// By default a Send carrying media the provider does not declare fails with
// UnsupportedContentError; WithoutContentValidation lets it through.
func TestSend_ValidatesContentUnlessDisabled(t *testing.T) {
	image := WithImageData([]byte{0x89, 0x50, 0x4E, 0x47}, "image/png")

	conv := newContentValidationConversation(t, &config{})
	_, err := conv.Send(context.Background(), "describe", image)
	var unsupported *providers.UnsupportedContentError
	require.True(t, errors.As(err, &unsupported), "want UnsupportedContentError, got %v", err)
	assert.Equal(t, "image", unsupported.ContentType)

	cfg := &config{}
	require.NoError(t, WithoutContentValidation()(cfg))
	conv = newContentValidationConversation(t, cfg)
	_, err = conv.Send(context.Background(), "describe", image)
	require.NoError(t, err)
}
