package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/sdk"
	"github.com/AltairaLabs/PromptKit/sdk/integration/probes"
)

// TestContract_AutoSummarizeFiresOncePerThresholdCrossing pins the per-Send
// behavior of WithAutoSummarize.
//
// LLMSummarizer.Summarize calls Provider.Predict directly without going
// through the pipeline ProviderStage, so the regular provider.call.* events
// don't cover summarizer traffic. The probes layer wraps the dedicated
// summarizer provider and exposes the count under op "summarizer.Predict".
//
// Contract: when history exceeds the threshold, a single Send produces at
// most one Summarize call (one batch per Send). When history is below the
// threshold, no Summarize call should fire.
func TestContract_AutoSummarizeFiresOncePerThresholdCrossing(t *testing.T) {
	const threshold = 50
	const batchSize = 25

	// Note: a Send appends both the user message and the assistant response,
	// so the threshold check sees pre-Send-history + 2. "below-threshold"
	// means pre-Send + 2 stays well under the threshold.
	cases := []struct {
		name     string
		history  int
		expected probes.Bound
	}{
		{"well-below-threshold", 10, probes.Exactly(0)},
		{"just-below", threshold - 5, probes.Exactly(0)},
		{"at-threshold", threshold, probes.AtMost(1)},
		{"above-threshold", threshold * 2, probes.AtMost(1)},
		// Even at 10× threshold, summarization fires at most once per Send —
		// the IncrementalSaveStage processes one batch per turn, not many.
		{"deeply-above", threshold * 10, probes.AtMost(1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, conv := probes.Run(t, probes.RunOptions{
				SeedHistory: tc.history,
				AutoSummarize: &probes.AutoSummarizeOpts{
					Threshold: threshold,
					BatchSize: batchSize,
				},
				SDKOptions: []sdk.Option{
					sdk.WithContextWindow(20),
				},
			})

			_, err := conv.Send(context.Background(), "x")
			require.NoError(t, err)

			// Summarize runs in the IncrementalSaveStage AFTER the provider
			// call. Allow time for it to complete and for any final events
			// to land before snapshotting.
			time.Sleep(150 * time.Millisecond)

			contract := probes.StageContract{
				Stage:   "auto-summarize",
				PerSend: probes.Ops{"summarizer.Predict": tc.expected},
			}
			contract.AssertHolds(t, p.Snapshot())
		})
	}
}
