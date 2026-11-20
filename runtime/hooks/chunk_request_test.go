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

// enforcingChunkHook replaces the chunk's Content with its own text.
type enforcingChunkHook struct {
	plainChunkHook
	replacement string
}

func (h *enforcingChunkHook) OnChunk(_ context.Context, c *providers.StreamChunk) Decision {
	h.chunks = append(h.chunks, c.Content)
	c.Content = h.replacement
	return Enforced("rewrote "+h.replacement, nil)
}

// An interceptor after one that enforces judges the rewritten chunk, and the
// decision whose rewrite stands is returned; a deny after it short-circuits.
func TestRegistry_RunOnChunk_LaterInterceptorsJudgeAnEnforcedRewrite(t *testing.T) {
	first := &enforcingChunkHook{replacement: "cut"}
	second := &enforcingChunkHook{replacement: "replaced"}
	after := &plainChunkHook{}
	r := NewRegistry(WithProviderHook(first), WithProviderHook(second), WithProviderHook(after))
	chunk := &providers.StreamChunk{Content: "original text"}

	d := r.RunOnChunk(context.Background(), chunk)

	assert.Equal(t, []string{"original text"}, first.chunks)
	assert.Equal(t, []string{"cut"}, second.chunks)
	assert.Equal(t, []string{"replaced"}, after.chunks)
	assert.Equal(t, "replaced", chunk.Content)
	assert.Equal(t, []any{false, true, "rewrote replaced"}, []any{d.Allow, d.Enforced, d.Reason})

	denying := NewRegistry(WithProviderHook(&enforcingChunkHook{replacement: "cut"}),
		WithProviderHook(&requestChunkHook{deny: true}), WithProviderHook(after))
	d = denying.RunOnChunk(context.Background(), &providers.StreamChunk{Content: "x"})
	assert.Equal(t, []any{false, false, "tenant blocked"}, []any{d.Allow, d.Enforced, d.Reason})
	assert.Equal(t, []string{"replaced"}, after.chunks, "nothing runs after a deny")
}

// contentChunkHook enforces by giving its text in Decision.Content, leaving
// the chunk it was handed as it was.
type contentChunkHook struct {
	plainChunkHook
	text string
}

func (h *contentChunkHook) OnChunk(_ context.Context, c *providers.StreamChunk) Decision {
	h.chunks = append(h.chunks, c.Content)
	d := Enforced("left "+h.text, map[string]any{"validator_type": h.text})
	d.Content = &h.text
	return d
}

// JudgeChunk reports every firing, in order, each with the text it left, and
// leaves the caller's chunk as it was; the next interceptor judges the text
// the last one left, whichever way it gave it.
func TestRegistry_JudgeChunk_ReportsEveryFiringWithItsText(t *testing.T) {
	first := &enforcingChunkHook{replacement: "cut"}
	second := &contentChunkHook{text: "replaced"}
	after := &plainChunkHook{}
	r := NewRegistry(WithProviderHook(first), WithProviderHook(second), WithProviderHook(after))
	chunk := &providers.StreamChunk{Content: "original text"}

	j := r.JudgeChunk(context.Background(), ChunkRequest{Chunk: chunk})

	require.Len(t, j.Firings, 2)
	assert.Equal(t, "cut", *j.Firings[0].Content)
	assert.Equal(t, "replaced", *j.Firings[1].Content)
	assert.Equal(t, []string{"cut"}, second.chunks)
	assert.Equal(t, []string{"replaced"}, after.chunks)
	assert.Nil(t, j.Holds, "a chunk that fired holds nothing")
	assert.Equal(t, "original text", chunk.Content, "the caller's chunk is not rewritten")

	d := r.RunOnChunk(context.Background(), chunk)
	assert.Equal(t, "replaced", chunk.Content, "RunOnChunk rewrites the chunk to the text that stands")
	assert.Equal(t, "left replaced", d.Reason)
}

// Each interceptor's hold is reported in registration order.
func TestRegistry_JudgeChunk_ReportsEachHold(t *testing.T) {
	r := NewRegistry(
		WithProviderHook(&holdingChunkHook{hold: 2}),
		WithProviderHook(&holdingChunkHook{hold: 5}),
	)

	j := r.JudgeChunk(context.Background(), ChunkRequest{Chunk: &providers.StreamChunk{Content: "hello pin"}})

	assert.Equal(t, []int{2, 5}, j.Holds)
	assert.Equal(t, 5, j.MaxHold())
	assert.Empty(t, j.Firings)
}

// afterCallEnforcer enforces after the call, either by rewriting the response
// in place or by giving its text in Decision.Content.
type afterCallEnforcer struct {
	plainChunkHook
	inPlace bool
	text    string
}

func (h *afterCallEnforcer) AfterCall(_ context.Context, _ *ProviderRequest, resp *ProviderResponse) Decision {
	d := Enforced("enforced", nil)
	if h.inPlace {
		resp.Message.Content = h.text
	} else {
		d.Content = &h.text
	}
	return d
}

// An enforcing AfterCall hook's text reaches the caller both ways: as
// Decision.Content and as the response's content, whichever the hook set.
func TestRegistry_RunAfterProviderCall_ReconcilesTheEnforcedText(t *testing.T) {
	for name, inPlace := range map[string]bool{"in place": true, "as Content": false} {
		t.Run(name, func(t *testing.T) {
			r := NewRegistry(WithProviderHook(&afterCallEnforcer{inPlace: inPlace, text: "redacted"}))
			resp := &ProviderResponse{Message: types.Message{Content: "secret"}}

			d := r.RunAfterProviderCall(context.Background(), &ProviderRequest{}, resp)

			require.NotNil(t, d.Content)
			assert.Equal(t, "redacted", *d.Content)
			assert.Equal(t, "redacted", resp.Message.Content)
		})
	}
}

// afterCallDenier denies after the call without enforcing.
type afterCallDenier struct{ plainChunkHook }

func (afterCallDenier) AfterCall(context.Context, *ProviderRequest, *ProviderResponse) Decision {
	return Deny("no")
}

// A deny leaves no text: nothing replaces the response it judged.
func TestRegistry_RunAfterProviderCall_DenyCarriesNoText(t *testing.T) {
	r := NewRegistry(WithProviderHook(&afterCallDenier{}))

	d := r.RunAfterProviderCall(context.Background(), &ProviderRequest{},
		&ProviderResponse{Message: types.Message{Content: "secret"}})

	assert.Equal(t, []any{false, false, (*string)(nil)}, []any{d.Allow, d.Enforced, d.Content})
}
