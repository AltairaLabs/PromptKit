package duplex

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// withCacheWrites is cost with n tokens written to the provider's prompt
// cache, as a priced call's Quantities carry them.
func withCacheWrites(cost *types.CostInfo, n int) *types.CostInfo {
	cost.Quantities = map[string]float64{"cache_write_token": float64(n)}
	return cost
}

// A realtime call's completion reports the cache writes its usage carried.
func TestDuplexCost_CompletionReportsCacheWrites(t *testing.T) {
	h, ev := costHarness(t, 0)
	h.emit(gen(1, textTurn("It is 21 degrees", withCacheWrites(usd(20, 5), 311))))
	done, _ := ev.await(t, 1, 0)
	h.finish()

	require.Len(t, done, 1)
	assert.Equal(t, 311, done[0].CacheWriteTokens)
	assertCall(t, done[0], 20, 5)
}

// Usage that arrives after the call's completion reports its cache writes on
// provider.call.usage.
func TestDuplexCost_LateUsageReportsCacheWrites(t *testing.T) {
	h, ev := costHarnessWaiting(t, 20*time.Millisecond)
	h.emit(gen(1, toolCallTurn(weatherCall("c1"))))
	done, _ := ev.await(t, 1, 0)
	h.emit(gen(1, []providers.StreamChunk{{CostInfo: withCacheWrites(usd(600, 12), 42)}}))
	done, usage := ev.await(t, 1, 1)
	h.finish()

	require.Len(t, usage, 1)
	assert.Equal(t, 0, done[0].CacheWriteTokens, "the completion went out before the usage")
	assert.Equal(t, 42, usage[0].CacheWriteTokens)
	assertUsage(t, usage[0], done[0].CallID, 600, 12)
}
