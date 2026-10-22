package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// templatedRecordingProvider records requests and declares a message-metadata
// template, as a provider built from a spec with MessageMetadataTemplate does.
type templatedRecordingProvider struct {
	multiTurnRecordingProvider
	template string
}

func (p *templatedRecordingProvider) MessageMetadataTemplate() string { return p.template }

// templatedStreamingProvider is the streaming counterpart.
type templatedStreamingProvider struct {
	streamingRecordingProvider
	template string
}

func (p *templatedStreamingProvider) MessageMetadataTemplate() string { return p.template }

func userWithMetadata(content string, md map[string]any) types.Message {
	return types.Message{
		Role:    "user",
		Content: content,
		Meta:    map[string]any{types.MetaMessageMetadata: md},
	}
}

// runProviderStageWithHistory sends msgs as one turn and returns the elements
// the stage emitted.
func runProviderStageWithHistory(t *testing.T, stage *ProviderStage, msgs []types.Message) []StreamElement {
	t.Helper()
	input := make(chan StreamElement, len(msgs))
	for i := range msgs {
		input <- NewMessageElement(&msgs[i])
	}
	close(input)
	output := make(chan StreamElement, 50)
	require.NoError(t, stage.Process(context.Background(), input, output))
	var elems []StreamElement
	for e := range output {
		elems = append(elems, e)
	}
	return elems
}

func metadataHistory() []types.Message {
	return []types.Message{
		userWithMetadata("make it red", map[string]any{"selection": "circle-1"}),
		{Role: "assistant", Content: "Done."},
		userWithMetadata("and bigger", map[string]any{"selection": []any{"circle-1", "square-2"}}),
	}
}

func TestProviderStage_MessageMetadataTemplate_RendersEveryMessageWithMetadata(t *testing.T) {
	prov := &templatedRecordingProvider{template: "<context>Selected: {{selection}}</context>"}
	stage := NewProviderStage(prov, nil, nil, &ProviderConfig{})
	history := metadataHistory()

	runProviderStageWithHistory(t, stage, history)

	require.Len(t, prov.requests, 1)
	sent := prov.requests[0]
	require.Len(t, sent, 3)
	assert.Equal(t, "make it red\n\n<context>Selected: circle-1</context>", sent[0].GetContent())
	assert.Equal(t, "Done.", sent[1].GetContent(), "a message without metadata is sent as is")
	assert.Equal(t, `and bigger`+"\n\n"+`<context>Selected: ["circle-1","square-2"]</context>`, sent[2].GetContent())

	assert.Equal(t, "make it red", history[0].Content, "the stored message must not change")
	assert.Equal(t, "and bigger", history[2].Content, "the stored message must not change")
}

func TestProviderStage_MessageMetadataTemplate_Streaming(t *testing.T) {
	prov := &templatedStreamingProvider{template: "[{{selection}}]"}
	stage := NewProviderStage(prov, nil, nil, &ProviderConfig{Streaming: true})

	runProviderStageWithHistory(t, stage, metadataHistory())

	require.Len(t, prov.requests, 1)
	sent := prov.requests[0]
	require.Len(t, sent, 3)
	assert.Equal(t, "make it red\n\n[circle-1]", sent[0].GetContent())
	assert.Equal(t, `and bigger`+"\n\n"+`[["circle-1","square-2"]]`, sent[2].GetContent())
}

func TestProviderStage_NoMessageMetadataTemplate_SendsMessagesUnchanged(t *testing.T) {
	prov := &multiTurnRecordingProvider{}
	stage := NewProviderStage(prov, nil, nil, &ProviderConfig{})

	runProviderStageWithHistory(t, stage, metadataHistory())

	require.Len(t, prov.requests, 1)
	assert.Equal(t, "make it red", prov.requests[0][0].GetContent())
	assert.Equal(t, "and bigger", prov.requests[0][2].GetContent())
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

	out := withRenderedMessageMetadata(in, "<{{selection}}>")

	require.Len(t, out[0].Parts, 2)
	assert.Equal(t, "\n\n<s1>", *out[0].Parts[1].Text)
	assert.Len(t, in[0].Parts, 1, "the input message's parts must not change")
}

func TestWithRenderedMessageMetadata_EmptyRenderAddsNothing(t *testing.T) {
	in := []types.Message{userWithMetadata("hi", map[string]any{"other": "x"})}

	out := withRenderedMessageMetadata(in, "{{selection}}")

	assert.Equal(t, "hi", out[0].Content)
}
