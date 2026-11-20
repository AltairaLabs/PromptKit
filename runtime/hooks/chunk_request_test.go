package hooks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// plainChunkHook implements only ChunkInterceptor.
type plainChunkHook struct{ chunks []string }

func (h *plainChunkHook) Name() string                                          { return "plain" }
func (h *plainChunkHook) BeforeCall(context.Context, *ProviderRequest) Decision { return Allow }
func (h *plainChunkHook) AfterCall(context.Context, *ProviderRequest, *ProviderResponse) Decision {
	return Allow
}
func (h *plainChunkHook) OnChunk(_ context.Context, c *providers.StreamChunk) Decision {
	h.chunks = append(h.chunks, c.Content)
	return Allow
}

// requestChunkHook implements both chunk interfaces.
type requestChunkHook struct {
	plainChunkHook
	callers []types.Caller
	deny    bool
}

func (h *requestChunkHook) InterceptChunk(_ context.Context, req ChunkRequest) Decision {
	h.callers = append(h.callers, req.Caller)
	h.chunks = append(h.chunks, "request:"+req.Chunk.Content)
	if h.deny {
		return Deny("tenant blocked")
	}
	return Allow
}

func TestRegistry_RunOnChunkRequest_PrefersInterceptChunk(t *testing.T) {
	caller := types.Caller{ID: "member", Claims: map[string]any{"tenant": "acme"}}
	plain := &plainChunkHook{}
	withReq := &requestChunkHook{}
	r := NewRegistry(WithProviderHook(plain), WithProviderHook(withReq))
	require.True(t, r.HasChunkInterceptors())

	d := r.RunOnChunkRequest(context.Background(),
		ChunkRequest{Chunk: &providers.StreamChunk{Content: "hi"}, Caller: caller})

	assert.True(t, d.Allow)
	assert.Equal(t, []string{"hi"}, plain.chunks)
	assert.Equal(t, []string{"request:hi"}, withReq.chunks)
	assert.Equal(t, []types.Caller{caller}, withReq.callers)
}

func TestRegistry_RunOnChunk_GivesARequestInterceptorNoCaller(t *testing.T) {
	withReq := &requestChunkHook{deny: true}
	r := NewRegistry(WithProviderHook(withReq))

	d := r.RunOnChunk(context.Background(), &providers.StreamChunk{Content: "hi"})

	assert.False(t, d.Allow)
	assert.Equal(t, "tenant blocked", d.Reason)
	assert.Equal(t, []types.Caller{{}}, withReq.callers)
}

// holdingChunkHook allows every chunk, holding back its last hold bytes.
type holdingChunkHook struct {
	plainChunkHook
	hold int
}

func (h *holdingChunkHook) OnChunk(context.Context, *providers.StreamChunk) Decision {
	return Decision{Allow: true, Hold: h.hold}
}

// The chunk is cleared only as far as every interceptor clears it: the
// registry holds back the most any of them holds.
func TestRegistry_RunOnChunk_HoldsTheMostAnyInterceptorHolds(t *testing.T) {
	r := NewRegistry(
		WithProviderHook(&holdingChunkHook{hold: 2}),
		WithProviderHook(&holdingChunkHook{hold: 5}),
		WithProviderHook(&holdingChunkHook{hold: 3}),
	)

	d := r.RunOnChunk(context.Background(), &providers.StreamChunk{Content: "hello pin"})

	assert.True(t, d.Allow)
	assert.Equal(t, 5, d.Hold)
}
