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
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/sdk/integration/probes"
)

// eventDrainTimeout caps how long known-bug tests will wait for the async
// EventBus to deliver the events the bug emits. Pinned counts also re-check
// after a brief settle to catch overshoot.
const (
	eventDrainTimeout = 5 * time.Second
	eventSettleDelay  = 50 * time.Millisecond
)

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
