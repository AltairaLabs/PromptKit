package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/sdk"
	"github.com/AltairaLabs/PromptKit/sdk/integration/probes"
)

// TestContract_CompactionRunsOncePerProviderRound pins the per-Send budget
// for context compaction.
//
// Compaction is invoked by ProviderStage between tool-loop rounds. With a
// single-round (no tool calls) Send, compaction either runs once (if the
// configured threshold is exceeded) or zero times (if not). It must NEVER
// scale with conversation history depth — that would make compaction itself
// O(N) on top of the work it is meant to amortise.
//
// We assert at most 1 context.compacted event per Send across history sizes
// from 0 to 2000. The exact lower bound depends on whether the compactor
// triggers (which depends on the default budget vs. the seeded message
// content), so we only bound the upper end.
func TestContract_CompactionRunsOncePerProviderRound(t *testing.T) {
	contract := probes.StageContract{
		Stage: "compaction",
		PerSend: probes.Ops{
			"events.context.compacted": probes.AtMost(1),
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
			p, conv := probes.Run(t, probes.RunOptions{
				SeedHistory: tc.history,
				SDKOptions: []sdk.Option{
					sdk.WithCompaction(true),
				},
			})
			_, err := conv.Send(context.Background(), "x")
			require.NoError(t, err)

			// Compaction events are bus-async; settle so the upper-bound
			// assertion catches overshoot rather than racing to read 0.
			time.Sleep(50 * time.Millisecond)

			contract.AssertHolds(t, p.Snapshot())
		})
	}
}
