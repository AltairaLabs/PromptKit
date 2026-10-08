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
	p.compacted = append(p.compacted, transcriptCompacted(req.Messages))
	return p.callRecordingProvider.PredictWithTools(ctx, req, tools, choice)
}

func (p *windowedProvider) PredictStreamWithTools(
	ctx context.Context, req providers.PredictionRequest, tools providers.ProviderTools, choice string,
) (<-chan providers.StreamChunk, error) {
	p.compacted = append(p.compacted, transcriptCompacted(req.Messages))
	return p.callRecordingProvider.PredictStreamWithTools(ctx, req, tools, choice)
}

func transcriptCompacted(msgs []types.Message) bool {
	for i := range msgs {
		if strings.Contains(msgs[i].GetContent(), compactedMarker) {
			return true
		}
	}
	return false
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

	off := &ContextCompactor{BudgetTokens: 0, BudgetFromProvider: true}
	assert.Same(t, off, off.ForProvider(small), "a zero budget means compaction is off, and stays off")
}

func TestProviderStage_HandoffCompactsForTheDestinationsWindow(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "unary", true: "streaming"}[streaming], func(t *testing.T) {
			origin := &windowedProvider{callRecordingProvider: newCallRecordingProvider("origin", true), window: 10_000_000}
			dest := &windowedProvider{callRecordingProvider: newCallRecordingProvider("dest", false), window: 2_000}
			origin.streaming, dest.streaming = streaming, streaming

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
		})
	}
}

// dropOldestTwo is a compaction strategy that removes the two oldest
// messages, as a message-removing rule (CollapsePairs) can.
type dropOldestTwo struct{}

func (dropOldestTwo) Compact(msgs []types.Message, _ int) CompactResult {
	return CompactResult{Messages: append([]types.Message(nil), msgs[2:]...), MessagesFolded: 2}
}
func (dropOldestTwo) TokenBudget() int { return 1 }

func textMsgs(texts ...string) []types.Message {
	out := make([]types.Message, len(texts))
	for i, tx := range texts {
		out[i] = types.Message{Role: "user", Content: tx}
	}
	return out
}

// After a compaction removes persisted messages, the next new message is
// still appended to the log, after everything already there.
func TestToolLoop_PersistsAfterMessageRemovingCompaction(t *testing.T) {
	stage := newCallHandoffStage(t, newCallRecordingProvider("p", false), nil)
	log := &spyMessageLog{messages: textMsgs("a", "b", "c", "d")}
	stage.config.MessageLog, stage.config.MessageLogConvID = log, "conv"
	stage.config.Compactor = dropOldestTwo{}
	loop := &toolLoop{stage: stage, messages: textMsgs("a", "b", "c", "d"), lastPersistedSeq: 4, persistedIdx: 4}

	loop.compactBeforeRound(2)
	loop.messages = append(loop.messages, textMsgs("e")...)
	loop.persistMessages(context.Background(), 2)

	require.Len(t, log.messages, 5, "e is appended; nothing is lost or re-sent")
	assert.Equal(t, "e", log.messages[4].Content)
}

// When the log holds more history than this turn loaded, a new message is
// appended after all of it.
func TestToolLoop_PersistsWhenLogHoldsMoreHistoryThanLoaded(t *testing.T) {
	stage := newCallHandoffStage(t, newCallRecordingProvider("p", false), nil)
	log := &spyMessageLog{messages: textMsgs("1", "2", "3", "4", "5", "x", "y")}
	stage.config.MessageLog, stage.config.MessageLogConvID = log, "conv"
	loaded := textMsgs("x", "y") // the recent window this turn loaded
	loop := &toolLoop{stage: stage, messages: loaded, lastPersistedSeq: len(loaded), persistedIdx: len(loaded)}

	loop.preSeedLog(context.Background())
	loop.messages = append(loop.messages, textMsgs("z")...)
	loop.persistMessages(context.Background(), 1)

	require.Len(t, log.messages, 8)
	assert.Equal(t, "z", log.messages[7].Content)
}

// toolHistory is three old, large tool round-trips and a recent user turn.
func toolHistory() []types.Message {
	big := strings.Repeat("result data ", 200) // ~600 tokens each
	var history []types.Message
	for _, id := range []string{"h1", "h2", "h3"} {
		history = append(history,
			types.Message{Role: "assistant", ToolCalls: []types.MessageToolCall{{ID: id, Name: "lookup", Args: []byte(`{}`)}}},
			types.Message{Role: "tool", ToolResult: &types.MessageToolResult{
				ID: id, Name: "lookup", Parts: []types.ContentPart{types.NewTextPart(big)},
			}},
		)
	}
	return append(history, types.Message{Role: "user", Content: "next"})
}


// passThroughCompaction wraps a strategy without implementing any optional
// interface, as a host's logging wrapper does.
type passThroughCompaction struct{ inner CompactionStrategy }

func (w passThroughCompaction) Compact(m []types.Message, n int) CompactResult { return w.inner.Compact(m, n) }
func (w passThroughCompaction) TokenBudget() int                               { return w.inner.TokenBudget() }

// The provider stage compacts against the round's whole input, so a large
// system prompt or many tool definitions force compaction of a transcript that
// fits on its own — through any strategy, a wrapper included (#2214).
func TestCompactBeforeRound_ReservesTheSystemPromptAndTools(t *testing.T) {
	bigTools := []map[string]string{{"name": "t", "description": strings.Repeat("describes ", 2500)}}
	folded := func(strategy CompactionStrategy, systemPrompt string, tools any) int {
		stage := newCallHandoffStage(t, newCallRecordingProvider("p", false), nil)
		stage.config.Compactor = strategy
		loop := &toolLoop{stage: stage, acc: &providerInput{systemPrompt: systemPrompt}, messages: toolHistory(),
			providerTools: tools}
		loop.compactBeforeRound(2)
		n := 0
		for i := range loop.messages {
			if strings.Contains(loop.messages[i].GetContent(), compactedMarker) {
				n++
			}
		}
		return n
	}
	compactor := func() *ContextCompactor { return &ContextCompactor{BudgetTokens: 4000} }

	assert.Zero(t, folded(compactor(), "short", nil), "the transcript fits on its own")
	assert.Positive(t, folded(compactor(), strings.Repeat("instructions ", 2500), nil), "a large system prompt")
	assert.Positive(t, folded(compactor(), "short", bigTools), "many tool definitions")
	assert.Positive(t, folded(passThroughCompaction{compactor()}, strings.Repeat("instructions ", 2500), nil),
		"a wrapper gets the reserve through lastInputTokens")
}

// Only removals before the persisted boundary move it: a message removed
// after it was never in the log (#2214).
func TestToolLoop_MovePersistedBoundaryCountsOnlyPersistedRemovals(t *testing.T) {
	loop := &toolLoop{persistedIdx: 4}
	loop.movePersistedBoundary(6, &CompactResult{Messages: textMsgs("a", "b", "c", "d"), RemovedIndices: []int{1, 5}})
	assert.Equal(t, 3, loop.persistedIdx, "index 1 was persisted; index 5 was not")

	loop = &toolLoop{persistedIdx: 4}
	loop.movePersistedBoundary(6, &CompactResult{Messages: textMsgs("a", "b", "c", "d")})
	assert.Equal(t, 2, loop.persistedIdx, "a strategy that does not report is taken to remove persisted ones")
}

// ContextCompactor reports the positions of the messages it removed.
func TestContextCompactor_ReportsRemovedIndices(t *testing.T) {
	c := &ContextCompactor{BudgetTokens: 100, Rules: []CompactionRule{CollapsePairs()}}
	cr := c.Compact(toolHistory(), 0)
	require.NotEmpty(t, cr.RemovedIndices)
	assert.Len(t, cr.Messages, len(toolHistory())-len(cr.RemovedIndices))
	for _, i := range cr.RemovedIndices {
		assert.Equal(t, "assistant", toolHistory()[i].Role, "CollapsePairs removes the assistant half of a pair")
	}
}
