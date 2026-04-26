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
// Cases marked with a knownViolation reference are skipped pending the bug
// fix. Removing the skip is the signal that the fix has landed; if the
// assertions then hold the contract is enforced for that fixture going
// forward.
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
		name             string
		history          int
		knownViolationOf string
	}{
		{"no-history", 0, ""},
		{"history-1", 1, "#1035"},
		{"history-5", 5, "#1035"},
		{"history-50", 50, "#1035"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.knownViolationOf != "" {
				t.Skipf("known violation: %s — see contract_known_bugs_test.go", tc.knownViolationOf)
			}
			p, conv := probes.Run(t, probes.RunOptions{SeedHistory: tc.history})
			_, err := conv.Send(context.Background(), "x")
			require.NoError(t, err)

			// EventBus is async; wait for the rendered event before snapshotting
			// so we don't false-fail with count == 0 due to in-flight delivery.
			require.True(t, p.WaitForCount("events.prompt.template.rendered", 1, 5*time.Second),
				"timed out waiting for prompt.template.rendered to land")

			contract.AssertHolds(t, p.Snapshot())
		})
	}
}
