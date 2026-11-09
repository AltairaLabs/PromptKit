package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/pipeline/stage"
	"github.com/AltairaLabs/PromptKit/runtime/v2/providers/mock"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	"github.com/AltairaLabs/PromptKit/sdk/v2"
	"github.com/AltairaLabs/PromptKit/sdk/v2/integration/probes"
)

// compactionToolPackJSON gives the probed prompt one tool, so a Send can run a
// multi-round tool loop.
const compactionToolPackJSON = `{
	"id": "compaction-contract",
	"version": "1.0.0",
	"prompts": {
		"chat": {
			"id": "chat",
			"name": "Chat",
			"system_template": "You are a helpful assistant.",
			"tools": ["lookup"]
		}
	},
	"tools": {
		"lookup": {
			"name": "lookup",
			"description": "Look something up",
			"mode": "local",
			"parameters": {"type": "object", "properties": {}}
		}
	}
}`

// toolRoundsRepo answers the first toolRounds provider calls with a call to
// lookup and every later call with text, whatever the history holds, so the
// number of rounds a Send runs does not depend on the seeded history.
type toolRoundsRepo struct {
	mu         sync.Mutex
	calls      int
	toolRounds int
}

func (r *toolRoundsRepo) GetResponse(ctx context.Context, p mock.ResponseParams) (string, error) {
	turn, err := r.GetTurn(ctx, p)
	return turn.Content, err
}

func (r *toolRoundsRepo) GetTurn(context.Context, mock.ResponseParams) (*mock.Turn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.calls <= r.toolRounds {
		return &mock.Turn{Type: "tool_calls", ToolCalls: []mock.ToolCall{{Name: "lookup"}}}, nil
	}
	return &mock.Turn{Type: "text", Content: "done"}, nil
}

// foldEveryCall is a compaction strategy that reports a fold every time it
// runs, so each run of the compactor shows up as one context.compacted event.
type foldEveryCall struct{}

func (foldEveryCall) Compact(messages []types.Message, _ int) stage.CompactResult {
	return stage.CompactResult{Messages: messages, MessagesFolded: 1}
}

func (foldEveryCall) TokenBudget() int { return 1 }

// TestContract_CompactionRunsOncePerProviderRound pins the per-Send budget
// for context compaction.
//
// Compaction is invoked by ProviderStage between tool-loop rounds: once
// before every round after the first. A Send whose model calls a tool on its
// first two rounds runs three rounds, so the compactor runs exactly twice. It
// must NEVER scale with conversation history depth — that would make
// compaction itself O(N) on top of the work it is meant to amortise.
func TestContract_CompactionRunsOncePerProviderRound(t *testing.T) {
	const toolRounds = 2
	contract := probes.StageContract{
		Stage: "compaction",
		PerSend: probes.Ops{
			"events.context.compacted": probes.Exactly(toolRounds),
		},
	}

	cases := []struct {
		name    string
		history int
	}{
		{"no-history", 0},
		{"history-50", 50},
		{"history-500", 500},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := mock.NewToolProviderWithRepository("probes-mock", "probes-model", false,
				&toolRoundsRepo{toolRounds: toolRounds})
			p, conv := probes.Run(t, probes.RunOptions{
				PackJSON:    compactionToolPackJSON,
				SeedHistory: tc.history,
				SDKOptions: []sdk.Option{
					sdk.WithProvider(provider),
					sdk.WithCompaction(true),
					sdk.WithCompactionStrategy(foldEveryCall{}),
				},
			})
			conv.OnTool("lookup", func(map[string]any) (any, error) {
				return map[string]string{"found": "yes"}, nil
			})

			resp, err := conv.Send(context.Background(), "x")
			require.NoError(t, err)
			require.Equal(t, "done", resp.Text(), "the tool loop ran to its final round")

			// Compaction events are bus-async; wait for the expected count so
			// the assertion reads the settled total, not a partial one.
			p.WaitForCount(probes.Op("events."+string(events.EventContextCompacted)), toolRounds, time.Second)
			time.Sleep(50 * time.Millisecond)

			contract.AssertHolds(t, p.Snapshot())
		})
	}
}
