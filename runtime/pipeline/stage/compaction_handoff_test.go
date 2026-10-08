package stage

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// Compaction budgets for the provider a round runs on, so a workflow handoff
// to a provider with a smaller context window compacts for it (#2208).

// windowedProvider is a callRecordingProvider with a context window, that
// records whether each round's transcript carried compacted tool results.
type windowedProvider struct {
	*callRecordingProvider
	window    int
	compacted []bool
}

func (p *windowedProvider) MaxContextTokens() int { return p.window }

func (p *windowedProvider) PredictWithTools(
	ctx context.Context, req providers.PredictionRequest, tools providers.ProviderTools, choice string,
) (providers.PredictionResponse, []types.MessageToolCall, error) {
	sawCompacted := false
	for i := range req.Messages {
		if strings.Contains(req.Messages[i].GetContent(), compactedMarker) {
			sawCompacted = true
		}
	}
	p.compacted = append(p.compacted, sawCompacted)
	return p.callRecordingProvider.PredictWithTools(ctx, req, tools, choice)
}

func TestContextCompactor_ForProvider(t *testing.T) {
	small := &windowedProvider{callRecordingProvider: newCallRecordingProvider("small", false), window: 8000}
	plain := newCallRecordingProvider("plain", false)

	fixed := &ContextCompactor{BudgetTokens: 1_000_000}
	assert.Same(t, fixed, fixed.ForProvider(small), "a fixed budget is the host's, and stays")

	following := &ContextCompactor{BudgetTokens: 1_000_000, BudgetFromProvider: true, PinRecentCount: 2}
	got, ok := following.ForProvider(small).(*ContextCompactor)
	require.True(t, ok)
	assert.Equal(t, 8000, got.BudgetTokens)
	assert.Equal(t, 2, got.PinRecentCount, "the rest of the configuration is kept")
	assert.Equal(t, 1_000_000, following.BudgetTokens, "the original is not modified")
	assert.Equal(t, DefaultBudgetTokens, following.ForProvider(plain).TokenBudget(),
		"a provider reporting no window gets the default")
}

func TestProviderStage_HandoffCompactsForTheDestinationsWindow(t *testing.T) {
	origin := &windowedProvider{callRecordingProvider: newCallRecordingProvider("origin", true), window: 10_000_000}
	dest := &windowedProvider{callRecordingProvider: newCallRecordingProvider("dest", false), window: 2_000}

	resolver := &fakeResolver{sequence: originToDestinationCall(dest)}
	stage := newCallHandoffStage(t, origin, resolver)
	stage.config.Compactor = &ContextCompactor{BudgetTokens: BudgetTokensFor(origin), BudgetFromProvider: true}

	// History with old, large tool results: tiny for the origin's window,
	// far over the destination's.
	big := strings.Repeat("result data ", 2000)
	var history []types.Message
	for i, id := range []string{"h1", "h2", "h3"} {
		history = append(history,
			types.Message{Role: "user", Content: "question"},
			types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: id, Name: "lookup", Args: []byte(`{}`)}}},
			types.Message{Role: "tool", ToolResult: &types.MessageToolResult{
				ID: id, Name: "lookup", Parts: []types.ContentPart{types.NewTextPart(big)},
			}},
			types.Message{Role: "assistant", Content: "answer " + string(rune('a'+i))},
		)
	}
	history = append(history, types.Message{Role: "user", Content: "now hand over"})

	input := make(chan StreamElement, len(history))
	for i := range history {
		input <- NewMessageElement(&history[i])
	}
	close(input)
	output := make(chan StreamElement, 64)
	require.NoError(t, stage.Process(context.Background(), input, output))
	for range output { //nolint:revive // draining
	}

	require.Equal(t, []bool{false}, origin.compacted, "the origin's window holds the history uncompacted")
	require.Equal(t, []bool{true}, dest.compacted, "the destination's first round is compacted for its window")
}
