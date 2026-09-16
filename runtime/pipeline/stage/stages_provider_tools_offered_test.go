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

func TestRecordOffered_NothingOfferedIsNil(t *testing.T) {
	s := &ProviderStage{}
	s.recordOffered(nil)
	assert.Nil(t, s.offeredToolNames())
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
