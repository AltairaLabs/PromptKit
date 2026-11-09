package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/sdk/v2/integration/probes"
)

// TestContract_PipelineStateStoreBudget pins the cross-stage per-Send budget
// for state store traffic. Regardless of stage composition, a single Send
// should produce exactly one Load (history assembly) and exactly one
// persistence operation. With the consolidated IncrementalSaveStage, the
// persistence op is AppendMessages on stores that implement MessageAppender
// (the default MemoryStore does); legacy bulk Save is only used by stores
// without typed-write support.
func TestContract_PipelineStateStoreBudget(t *testing.T) {
	inv := probes.PipelineInvariants{
		Label: "state-store-budget",
		PerSend: probes.Ops{
			"store.Load":           probes.Exactly(1),
			"store.Save":           probes.Exactly(0),
			"store.AppendMessages": probes.Exactly(1),
			"store.Fork":           probes.Exactly(0),
		},
	}

	cases := []struct {
		name    string
		history int
	}{
		{"no-history", 0},
		{"history-5", 5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, conv := probes.Run(t, probes.RunOptions{SeedHistory: tc.history})
			resp, err := conv.Send(context.Background(), "x")
			require.NoError(t, err)
			inv.AssertHolds(t, p.Snapshot())

			// The one append persisted the whole turn: the seeded history,
			// then the user message and the provider's answer.
			msgs := conv.Messages(context.Background())
			require.Len(t, msgs, tc.history+2)
			assert.Equal(t, "user", msgs[tc.history].Role)
			assert.Equal(t, "x", msgs[tc.history].GetContent())
			assert.Equal(t, "assistant", msgs[tc.history+1].Role)
			assert.Equal(t, resp.Text(), msgs[tc.history+1].GetContent())
		})
	}
}
