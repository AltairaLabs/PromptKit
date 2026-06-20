package stage

import "testing"

func TestWarnIfCachingStalled(t *testing.T) {
	const bigInput = 5000 * 4 // 4 rounds × ~5000 input tokens each

	t.Run("fires on large uncached multi-round loop", func(t *testing.T) {
		tl := &toolLoop{cachingSupported: true, cumulativeInput: bigInput, cumulativeCached: 0}
		tl.warnIfCachingStalled(cachingStallRounds)
		if !tl.warnedNoCaching {
			t.Fatal("expected a caching-stalled warning")
		}
		// Once-only: a second call must not re-arm (no double logging).
		tl.warnIfCachingStalled(cachingStallRounds + 5)
	})

	t.Run("silent when provider does not support caching", func(t *testing.T) {
		// e.g. a local model — cached is always 0, but that is expected, not a bug.
		tl := &toolLoop{cachingSupported: false, cumulativeInput: bigInput, cumulativeCached: 0}
		tl.warnIfCachingStalled(cachingStallRounds + 5)
		if tl.warnedNoCaching {
			t.Fatal("must not warn for a provider that does not support caching")
		}
	})

	t.Run("silent when caching engages", func(t *testing.T) {
		tl := &toolLoop{cachingSupported: true, cumulativeInput: bigInput, cumulativeCached: 100}
		tl.warnIfCachingStalled(cachingStallRounds)
		if tl.warnedNoCaching {
			t.Fatal("must not warn when cache reads > 0")
		}
	})

	t.Run("silent before the round threshold", func(t *testing.T) {
		tl := &toolLoop{cachingSupported: true, cumulativeInput: bigInput, cumulativeCached: 0}
		tl.warnIfCachingStalled(cachingStallRounds - 2)
		if tl.warnedNoCaching {
			t.Fatal("must not warn before cachingStallRounds")
		}
	})

	t.Run("silent for small inputs (caching would not help)", func(t *testing.T) {
		tl := &toolLoop{cachingSupported: true, cumulativeInput: 200, cumulativeCached: 0}
		tl.warnIfCachingStalled(cachingStallRounds + 3)
		if tl.warnedNoCaching {
			t.Fatal("must not warn for small inputs below the cacheable floor")
		}
	})
}
