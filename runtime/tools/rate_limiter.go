package tools

import (
	"fmt"
	"sync"
	"time"
)

// ErrRateLimitExceeded is the error reported when a tool call exceeds its
// per-minute rate limit. Registry.Execute reports its message in ToolResult.Error and
// Registry.ExecuteAsync in ToolExecutionResult.Error; neither returns it.
var ErrRateLimitExceeded = fmt.Errorf("tool rate limit exceeded")

// msPerMinute is the number of milliseconds in one minute.
const msPerMinute = 60_000

// rateLimiter tracks per-tool call timestamps and enforces a sliding-window
// rate limit (calls per minute). Safe for concurrent use.
//
// It is the registry's limit (WithRateLimit), not a conversation's: one
// limiter is shared by a registry, its clones and its children, so it counts
// every caller of the tool together, under a limit fixed when the registry is
// built. stage.CallRateWindow is the other sliding window: it enforces
// pipeline.ToolPolicy.MaxCallsPerMinute per conversation and tool, under the
// limit of the policy in force for each call, and refuses with a tool-policy
// result rather than ErrRateLimitExceeded. The two count different things,
// so they are kept apart rather than one keyed to serve both.
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
	return rl.allowAdmitted(toolName, nil)
}

// allowAdmitted is Allow, with admit (when not nil) called under the
// limiter's lock once the registry's limit allows the call. If admit refuses,
// its error is returned and the call is not recorded, so neither limit counts
// a call the other refused.
func (rl *rateLimiter) allowAdmitted(toolName string, admit func() error) error {
	if rl == nil || rl.maxPerMinute <= 0 {
		if admit != nil {
			return admit()
		}
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
	if admit != nil {
		if err := admit(); err != nil {
			return err
		}
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
