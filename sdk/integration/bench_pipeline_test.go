package integration

import (
	"context"
	"fmt"
	"testing"

	"github.com/AltairaLabs/PromptKit/sdk/integration/probes"
)

// BenchmarkPipeline_PerSend measures wall time and allocations of a single
// Send across a range of conversation history sizes.
//
// The benchmark is parameterised so future-you can spot O(N²) regressions
// (where doubling history more than doubles per-Send cost). It does not
// assert anything by itself — its purpose is to surface performance cliffs
// that contract tests cannot see.
//
//   - bench=0: control. Per-Send cost should be roughly equal across runs.
//   - bench=50: typical short conversation.
//   - bench=500: long-running agent session.
//   - bench=2000: stress; if anything is O(N²), it shows here.
//
// Run with:
//
//	go test -bench=BenchmarkPipeline_PerSend ./sdk/integration -benchmem
func BenchmarkPipeline_PerSend(b *testing.B) {
	for _, history := range []int{0, 50, 500, 2000} {
		b.Run(fmt.Sprintf("history=%d", history), func(b *testing.B) {
			b.ReportAllocs()
			ctx := context.Background()

			for i := 0; i < b.N; i++ {
				b.StopTimer()
				_, conv := probes.Run(b, probes.RunOptions{
					SeedHistory:    history,
					ConversationID: fmt.Sprintf("bench-%d-%d", history, i),
				})
				b.StartTimer()

				if _, err := conv.Send(ctx, "x"); err != nil {
					b.Fatalf("Send failed: %v", err)
				}
			}
		})
	}
}
