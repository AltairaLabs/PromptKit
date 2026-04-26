package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/events"
	"github.com/AltairaLabs/PromptKit/sdk"
	"github.com/AltairaLabs/PromptKit/sdk/integration/probes"
)

// TestContract_TokenBudgetCapsProviderInput verifies that when WithTokenBudget
// is set, the message volume entering the provider stays bounded by the
// budget regardless of how long the conversation grows.
//
// We assert via provider.call.completed event payloads (which carry actual
// InputTokens reported by the provider) rather than counting messages — this
// is what the budget is actually meant to bound.
//
// The mock provider's token estimates are coarse; we therefore assert a
// generous tolerance (≤ 2× budget) so the test isn't flaky on estimation
// noise but still catches order-of-magnitude regressions where the budget
// is silently ignored and the entire history flows to the provider.
func TestContract_TokenBudgetCapsProviderInput(t *testing.T) {
	const budget = 4000
	const tolerance = 2 // mock provider estimates are coarse

	cases := []struct {
		name    string
		history int
	}{
		{"no-history", 0},
		{"history-100", 100},
		{"history-1000", 1000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, conv := probes.Run(t, probes.RunOptions{
				SeedHistory: tc.history,
				SDKOptions: []sdk.Option{
					sdk.WithTokenBudget(budget),
					sdk.WithCompaction(true),
				},
			})
			_, err := conv.Send(context.Background(), "x")
			require.NoError(t, err)

			require.True(t,
				p.WaitForCount("events.provider.call.completed", 1, 5*time.Second),
				"timed out waiting for provider.call.completed")
			time.Sleep(50 * time.Millisecond)

			calls := p.Events(events.EventProviderCallCompleted)
			require.NotEmpty(t, calls, "expected at least one provider call")

			maxInput := 0
			for _, e := range calls {
				data, ok := e.Data.(*events.ProviderCallCompletedData)
				require.True(t, ok, "unexpected payload type for provider.call.completed")
				if data.InputTokens > maxInput {
					maxInput = data.InputTokens
				}
			}

			require.LessOrEqual(t, maxInput, budget*tolerance,
				"provider received %d input tokens, budget is %d (tolerance %dx); "+
					"history=%d", maxInput, budget, tolerance, tc.history)
		})
	}
}
