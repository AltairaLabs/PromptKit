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

// The messages leave room for what else the round sends: a transcript that
// fits the budget on its own is compacted once the system prompt and tools
// are reserved (#2214).
func TestContextCompactor_ReservedTokensShrinkTheMessageBudget(t *testing.T) {
	history := toolHistory()
	c := &ContextCompactor{BudgetTokens: 4000}

	assert.Zero(t, c.Compact(history, 0).MessagesFolded, "fits 70% of 4000 on its own")

	reserved, ok := c.WithReserved(2500).(*ContextCompactor)
	require.True(t, ok)
	assert.Positive(t, reserved.Compact(history, 0).MessagesFolded, "does not fit once 2500 are reserved")
	assert.Zero(t, c.ReservedTokens, "WithReserved leaves the original alone")
	assert.Same(t, c, c.WithReserved(0), "an unchanged reserve is the same strategy")
}

// The provider stage reserves the round's system prompt: a large one forces
// compaction of a transcript that would otherwise be sent as is.
func TestCompactBeforeRound_ReservesTheSystemPrompt(t *testing.T) {
	run := func(systemPrompt string) int {
		stage := newCallHandoffStage(t, newCallRecordingProvider("p", false), nil)
		stage.config.Compactor = &ContextCompactor{BudgetTokens: 4000}
		loop := &toolLoop{stage: stage, acc: &providerInput{systemPrompt: systemPrompt}, messages: toolHistory()}
		loop.compactBeforeRound(2)
		folded := 0
		for i := range loop.messages {
			if strings.Contains(loop.messages[i].GetContent(), compactedMarker) {
				folded++
			}
		}
		return folded
	}
	assert.Zero(t, run("short"))
	assert.Positive(t, run(strings.Repeat("instructions ", 2500)))
}
