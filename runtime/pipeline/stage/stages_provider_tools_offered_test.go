package stage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The offered set is what makes tool availability assertable. ToolCalls record
// what the model chose; nothing else records what it could have chosen, which
// is the only evidence that a skill's allowed-tools grant took effect.

func TestRecordOffered_AccumulatesAndSorts(t *testing.T) {
	s := &ProviderStage{}
	s.recordOffered([]*providers.ToolDescriptor{{Name: "refund"}, {Name: "get_order"}})

	assert.Equal(t, []string{"get_order", "refund"}, s.offeredToolNames())
}

// A grant widens the set mid-turn, so the record has to be a union across
// rounds rather than the last round's snapshot — otherwise a tool granted in
// round 1 and dropped in round 2 would vanish from the evidence.
func TestRecordOffered_UnionsAcrossRounds(t *testing.T) {
	s := &ProviderStage{}
	s.recordOffered([]*providers.ToolDescriptor{{Name: "get_order"}})
	s.recordOffered([]*providers.ToolDescriptor{{Name: "get_order"}, {Name: "refund"}})

	assert.Equal(t, []string{"get_order", "refund"}, s.offeredToolNames())
}

// Nil for "nothing offered" is what tells the eval it has nothing to judge, so
// it must be the answer to an empty record specifically — not what the stage
// returns regardless. Recording after the empty call proves it discriminates.
func TestRecordOffered_DiscriminatesEmptyFromRecorded(t *testing.T) {
	s := &ProviderStage{}

	s.recordOffered(nil)
	assert.Nil(t, s.offeredToolNames(), "nothing recorded yet")

	s.recordOffered([]*providers.ToolDescriptor{})
	assert.Nil(t, s.offeredToolNames(), "an empty build records nothing")

	s.recordOffered([]*providers.ToolDescriptor{{Name: "refund"}})
	assert.Equal(t, []string{"refund"}, s.offeredToolNames(),
		"a real build must produce a real answer")
}

func TestRecordOffered_SkipsNilDescriptors(t *testing.T) {
	s := &ProviderStage{}
	s.recordOffered([]*providers.ToolDescriptor{nil, {Name: "refund"}, nil})
	assert.Equal(t, []string{"refund"}, s.offeredToolNames())
}

// The end-to-end property: a tool granted beyond the prompt's baseline shows up
// in the offered record, because that is the set actually handed to the
// provider. This is the check that would have caught the grant path shipping
// inert twice in PromptArena.
func TestBuildProviderTools_RecordsGrantedToolsAsOffered(t *testing.T) {
	granted := []string{}
	s := &ProviderStage{
		toolRegistry: registryWithTools(t, "get_order", "refund"),
		config:       &ProviderConfig{ToolGrants: func() []string { return granted }},
		provider:     &toolingProvider{},
	}

	_, _, err := s.buildProviderTools([]string{"get_order"}, map[string]bool{})
	require.NoError(t, err)
	assert.Equal(t, []string{"get_order"}, s.offeredToolNames(),
		"before the grant, only the baseline is offered")

	// A skill activates mid-turn and grants refund.
	granted = []string{"refund"}
	_, _, err = s.buildProviderTools([]string{"get_order"}, map[string]bool{})
	require.NoError(t, err)
	assert.Equal(t, []string{"get_order", "refund"}, s.offeredToolNames(),
		"the granted tool must appear in the offered record")
}

// toolingProvider is a minimal providers.ToolSupport: buildProviderTools
// returns early unless the provider satisfies that interface, so the embedded
// Provider plus these methods is the smallest thing that gets past the check.
type toolingProvider struct {
	providers.Provider
}

func (p *toolingProvider) BuildTooling(descriptors []*providers.ToolDescriptor) (providers.ProviderTools, error) {
	return descriptors, nil
}

func (p *toolingProvider) PredictWithTools(
	_ context.Context, _ providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	return providers.PredictionResponse{}, nil, nil
}

func (p *toolingProvider) PredictStreamWithTools(
	_ context.Context, _ providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (<-chan providers.StreamChunk, error) {
	return nil, nil
}

// Compile-time proof the fake gets past buildProviderTools' capability check —
// without this, a missing method makes the test silently record nothing.
var _ providers.ToolSupport = (*toolingProvider)(nil)

// Every provider PromptArena runs streams, so a stamp that only lands on the
// unary path is a stamp no real run ever sees (#2035). This drives the whole
// streaming loop rather than the round helper, because the bug was that the
// message built at the end of the streaming round never got the Meta key —
// asserting on offeredToolNames() alone passed throughout.
func TestProviderStage_Streaming_StampsToolsOffered(t *testing.T) {
	prov := &streamingToolingProvider{}
	ts := NewTurnState()
	ts.AllowedTools = []string{"get_order"}
	stage := NewProviderStageWithTurnState(
		prov, registryWithTools(t, "get_order", "refund"), nil,
		&ProviderConfig{Streaming: true}, nil, nil, ts,
	)

	input := make(chan StreamElement, 4)
	input <- NewMessageElement(&types.Message{Role: "user", Content: "stream turn"})
	input <- NewEndOfTurnElement()
	close(input)

	output := make(chan StreamElement, 32)
	require.NoError(t, stage.Process(context.Background(), input, output))

	var assistant []*types.Message
	for e := range output {
		if e.Message != nil && e.Message.Role == roleAssistant {
			assistant = append(assistant, e.Message)
		}
	}
	require.Len(t, assistant, 1)
	assert.Equal(t, []string{"get_order"}, assistant[0].Meta[types.MetaToolsOffered],
		"the streaming path must stamp what the turn offered, same as the unary path")
}

// streamingToolingProvider streams and declares tools: the combination every
// real provider has and the only one that reaches executeStreamingRound with a
// non-empty offered set.
type streamingToolingProvider struct {
	streamingRecordingProvider
}

func (p *streamingToolingProvider) BuildTooling(
	descriptors []*providers.ToolDescriptor,
) (providers.ProviderTools, error) {
	return descriptors, nil
}

func (p *streamingToolingProvider) PredictWithTools(
	ctx context.Context, req providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	resp, err := p.Predict(ctx, req)
	return resp, nil, err
}

func (p *streamingToolingProvider) PredictStreamWithTools(
	ctx context.Context, req providers.PredictionRequest, _ providers.ProviderTools, _ string,
) (<-chan providers.StreamChunk, error) {
	return p.PredictStream(ctx, req)
}

var _ providers.ToolSupport = (*streamingToolingProvider)(nil)
