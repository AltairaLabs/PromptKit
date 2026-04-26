package integration

// Tests in this file pin the *current* (incorrect) behavior of known bugs.
// They pass green today; they fail loudly when the bug is fixed, prompting
// the developer to flip the assertion and move the case to the corresponding
// contract_*_test.go file.
//
// When adding a new known-bug pin:
//   1. Reference the issue number in the test name and doc comment.
//   2. Pin the exact observed counts so silent drift is caught.
//   3. Cross-link to the matching skipped case in the contract test.

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/sdk/integration/probes"
)

// TestKnownBug_Issue1035_HistoryMultipliesTemplateEvents pins the current
// over-emission of template lifecycle events.
//
// Bug: PromptAssemblyStage stamps system_template metadata onto every
// StreamElement; TemplateStage re-renders + re-emits per element; and
// StateStoreLoadStage emits one element per historical message. The
// observed count is therefore N_history + 1 instead of the contracted 1.
//
// See https://github.com/AltairaLabs/PromptKit/issues/1035 .
//
// When fixed, this test will fail. Replace it by removing the matching skip
// in TestContract_TemplateStage (contract_template_test.go).
func TestKnownBug_Issue1035_HistoryMultipliesTemplateEvents(t *testing.T) {
	const history = 4
	const expected = history + 1 // current buggy count: one render per stream element

	p, conv := probes.Run(t, probes.RunOptions{SeedHistory: history})
	_, err := conv.Send(context.Background(), "current input")
	require.NoError(t, err)

	snap := p.Snapshot()
	require.Equal(t, expected, snap.Count("events.prompt.template.started"),
		"BUG #1035: template.started should be 1 per Send; pinned at N_history+1 until fix lands")
	require.Equal(t, expected, snap.Count("events.prompt.template.rendered"),
		"BUG #1035: template.rendered should be 1 per Send; pinned at N_history+1 until fix lands")
}

// TestKnownBug_StateStoreSaveStageRedundantLoad pins the current redundant
// store.Load call performed inside StateStoreSaveStage.
//
// Bug: StateStoreSaveStage.loadOrCreateState (runtime/pipeline/stage/
// stages_core.go) calls store.Load before saving so it can preserve fields
// not present in the element stream (Summaries, TokenCount, UserID). This
// is functionally a load-modify-save and doubles per-Send Load count from
// 1 to 2.
//
// Surfaced during investigation of issue #1035.
//
// When fixed, remove the matching skip in TestContract_PipelineStateStoreBudget
// (contract_pipeline_test.go).
func TestKnownBug_StateStoreSaveStageRedundantLoad(t *testing.T) {
	p, conv := probes.Run(t, probes.RunOptions{SeedHistory: 0})
	_, err := conv.Send(context.Background(), "x")
	require.NoError(t, err)

	snap := p.Snapshot()
	require.Equal(t, 2, snap.Count("store.Load"),
		"redundant Load: 1 in StateStoreLoadStage, 1 in StateStoreSaveStage.loadOrCreateState")
	require.Equal(t, 1, snap.Count("store.Save"),
		"Save itself is correctly Send-scoped — only Load is redundant")
}
