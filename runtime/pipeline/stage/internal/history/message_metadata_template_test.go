package history

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func userWithMetadata(content string, md map[string]any) types.Message {
	return types.Message{
		Role:    "user",
		Content: content,
		Meta:    map[string]any{types.MetaMessageMetadata: md},
	}
}

func TestRenderMessageMetadata(t *testing.T) {
	tests := []struct {
		name string
		tmpl string
		md   map[string]any
		want string
	}{
		{"string inserted as is", "sel={{selection}}", map[string]any{"selection": "a"}, "sel=a"},
		{"object as compact JSON", "{{view}}", map[string]any{"view": map[string]any{"zoom": 2.0}}, `{"zoom":2}`},
		{"number as JSON", "n={{n}}", map[string]any{"n": 3.0}, "n=3"},
		{"missing key renders empty", "a={{a}} b={{b}}", map[string]any{"a": "x"}, "a=x b="},
		{"spaces inside braces", "{{ a }}", map[string]any{"a": "x"}, "x"},
		{"a value is never expanded again", "{{a}}", map[string]any{"a": "{{b}}", "b": "leak"}, "{{b}}"},
		{"null renders empty", "[{{a}}]", map[string]any{"a": nil}, "[]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, renderMessageMetadata(tt.tmpl, tt.md))
		})
	}
}

func TestWithRenderedMessageMetadata_PartsGetATextPartOnACopy(t *testing.T) {
	orig := types.Message{
		Role:  "user",
		Parts: []types.ContentPart{types.NewTextPart("look at this")},
		Meta:  map[string]any{types.MetaMessageMetadata: map[string]any{"selection": "s1"}},
	}
	in := []types.Message{orig}

	out := WithRenderedMessageMetadata(in, "<{{selection}}>")

	require.Len(t, out[0].Parts, 2)
	assert.Equal(t, "\n\n<s1>", *out[0].Parts[1].Text)
	assert.Len(t, in[0].Parts, 1, "the input message's parts must not change")
}

func TestWithRenderedMessageMetadata_EmptyRenderAddsNothing(t *testing.T) {
	in := []types.Message{userWithMetadata("hi", map[string]any{"other": "x"})}

	out := WithRenderedMessageMetadata(in, "{{selection}}")

	assert.Equal(t, "hi", out[0].Content)
}

// templater is a provider that declares a message-metadata template. Only the
// type assertion MessageMetadataTemplate makes is ever called on it.
type templater struct {
	providers.Provider
	template string
}

func (p templater) MessageMetadataTemplate() string { return p.template }

type plainProvider struct{ providers.Provider }

func TestMessageMetadataTemplate(t *testing.T) {
	assert.Equal(t, "<{{selection}}>", MessageMetadataTemplate(templater{template: "<{{selection}}>"}))
	assert.Equal(t, "", MessageMetadataTemplate(plainProvider{}))
}

// A message whose text is its Content gets the rendering appended to it, on a
// copy; a message without metadata is sent as is; and with nothing to render
// the slice handed in comes back unchanged.
func TestWithRenderedMessageMetadata_ContentAndUnchangedMessages(t *testing.T) {
	in := []types.Message{
		userWithMetadata("make it red", map[string]any{"selection": "circle-1"}),
		{Role: "assistant", Content: "Done."},
	}

	out := WithRenderedMessageMetadata(in, "[{{selection}}]")

	assert.Equal(t, "make it red\n\n[circle-1]", out[0].Content)
	assert.Equal(t, "Done.", out[1].Content)
	assert.Equal(t, "make it red", in[0].Content, "the input message is not written")

	plain := []types.Message{{Role: "user", Content: "hi"}}
	assert.Same(t, &plain[0], &WithRenderedMessageMetadata(plain, "[{{selection}}]")[0],
		"nothing rendered: the same slice")
	assert.Same(t, &in[0], &WithRenderedMessageMetadata(in, "")[0], "no template: the same slice")
}
