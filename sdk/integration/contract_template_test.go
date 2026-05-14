package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/sdk/integration/probes"
)

// TestContract_TemplateStage pins the per-Send operation budget for
// TemplateStage:
//   - render the system_template exactly once
//   - emit prompt.template.started exactly once
//   - emit prompt.template.rendered exactly once
//   - never emit prompt.template.failed
//
// Enforced for every fixture from history=0 to history=2000 — the
// per-Send TurnState render cache (see runtime/pipeline/stage/turn_state.go
// and ARCHITECTURE.md §4) ensures rendering is decoupled from element
// count. This contract test was previously t.Skipf-ed for history>0
// pending the #1035 fix; that fix landed in PR feat/turnstate-runtime.
func TestContract_TemplateStage(t *testing.T) {
	contract := probes.StageContract{
		Stage: "template",
		PerSend: probes.Ops{
			"events.prompt.template.started":  probes.Exactly(1),
			"events.prompt.template.rendered": probes.Exactly(1),
			"events.prompt.template.failed":   probes.AtMost(0),
		},
	}

	cases := []struct {
		name    string
		history int
	}{
		{"no-history", 0},
		{"history-1", 1},
		{"history-5", 5},
		{"history-50", 50},
		// Scale: a 2000-turn conversation is plausible production traffic
		// for long-running agent sessions; the contract must hold here too.
		{"history-2000", 2000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, conv := probes.Run(t, probes.RunOptions{SeedHistory: tc.history})
			_, err := conv.Send(context.Background(), "x")
			require.NoError(t, err)

			// EventBus dispatches events through a 10-worker pool, so the
			// `started` and `rendered` events the stage emits in order land
			// at the listener in any order. Waiting only for `rendered`
			// produced flakes where the snapshot saw rendered=1 but
			// started=0 because the worker for `started` hadn't reached
			// the increment yet. Wait for every event the contract demands
			// before snapshotting.
			for op, bound := range contract.PerSend {
				if bound.Min <= 0 {
					continue
				}
				require.Truef(t, p.WaitForCount(op, bound.Min, 10*time.Second),
					"timed out waiting for %s to reach %d", op, bound.Min)
			}

			contract.AssertHolds(t, p.Snapshot())
		})
	}
}
