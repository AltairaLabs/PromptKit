package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/sdk/integration/probes"
)

// TestContract_PipelineStateStoreBudget pins the cross-stage per-Send budget
// for state store traffic. Regardless of stage composition, a single Send
// should produce exactly one Load (history assembly) and exactly one Save
// (state persistence).
//
// Currently violated by StateStoreSaveStage.loadOrCreateState, which performs
// an additional Load inside the save path. See contract_known_bugs_test.go
// for the pinned violation count.
func TestContract_PipelineStateStoreBudget(t *testing.T) {
	inv := probes.PipelineInvariants{
		Label: "state-store-budget",
		PerSend: probes.Ops{
			"store.Load": probes.Exactly(1),
			"store.Save": probes.Exactly(1),
			"store.Fork": probes.Exactly(0),
		},
	}

	cases := []struct {
		name             string
		history          int
		knownViolationOf string
	}{
		{"no-history", 0, "#1035: redundant Load in StateStoreSaveStage"},
		{"history-5", 5, "#1035: redundant Load in StateStoreSaveStage"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.knownViolationOf != "" {
				t.Skipf("known violation: %s — see contract_known_bugs_test.go", tc.knownViolationOf)
			}
			p, conv := probes.Run(t, probes.RunOptions{SeedHistory: tc.history})
			_, err := conv.Send(context.Background(), "x")
			require.NoError(t, err)
			inv.AssertHolds(t, p.Snapshot())
		})
	}
}
