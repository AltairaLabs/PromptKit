package tools

import (
	"fmt"
	"sync"
	"time"
)

// ErrRateLimitExceeded is returned when a tool call exceeds its per-minute rate limit.
var ErrRateLimitExceeded = fmt.Errorf("tool rate limit exceeded")

// msPerMinute is the number of milliseconds in one minute.
const msPerMinute = 60_000

// rateLimiter tracks per-tool call timestamps and enforces a sliding-window
// rate limit (calls per minute). Safe for concurrent use.
type rateLimiter struct {
	mu               sync.Mutex
	maxPerMinute     int                // 0 means unlimited
	callsByTool      map[string][]int64 // tool name -> unix-millis timestamps
	nowFunc          func() int64       // injectable clock for testing
	windowDurationMs int64
}

// newRateLimiter creates a rate limiter with the given per-minute limit.
// Pass 0 to disable rate limiting.
func newRateLimiter(maxPerMinute int) *rateLimiter {
	return &rateLimiter{
		maxPerMinute:     maxPerMinute,
		callsByTool:      make(map[string][]int64),
		nowFunc:          func() int64 { return time.Now().UnixMilli() },
		windowDurationMs: msPerMinute,
	}
}

// Allow checks whether a call to the named tool is permitted under the rate
// limit. If allowed, it records the call and returns nil. If the limit is
// exceeded, it returns ErrRateLimitExceeded (wrapped with the tool name).
func (rl *rateLimiter) Allow(toolName string) error {
	if rl == nil || rl.maxPerMinute <= 0 {
		return nil
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.nowFunc()
	cutoff := now - rl.windowDurationMs

	// Evict expired timestamps
	timestamps := rl.callsByTool[toolName]
	start := 0
	for start < len(timestamps) && timestamps[start] < cutoff {
		start++
	}
	timestamps = timestamps[start:]

	if len(timestamps) >= rl.maxPerMinute {
		return fmt.Errorf("%w: tool %q limited to %d calls/minute",
			ErrRateLimitExceeded, toolName, rl.maxPerMinute)
	}

	rl.callsByTool[toolName] = append(timestamps, now)
	return nil
}

// SetLimit updates the per-minute limit. Pass 0 to disable.
func (rl *rateLimiter) SetLimit(maxPerMinute int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.maxPerMinute = maxPerMinute
}
