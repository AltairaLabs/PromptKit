package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// A tool that ran but failed completes with the executor's failure status,
// tools.ToolStatusFailed ("failed"), not "error". The counter compared against
// "error" alone, so every failed call landed in status="success" (#2148).
func TestMetricContext_ToolCallCompletedStatusMapsToLabel(t *testing.T) {
	cases := []struct {
		status    string
		wantLabel string
	}{
		{string(tools.ToolStatusFailed), statusError},
		{statusError, statusError},
		{string(tools.ToolStatusComplete), statusSuccess},
		{"success", statusSuccess},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			c, _ := newTestCollector()
			ctx := c.Bind(nil)

			ctx.OnEvent(&events.Event{
				Type: events.EventToolCallCompleted,
				Data: &events.ToolCallCompletedData{
					ToolName: "http", Duration: 10 * time.Millisecond, Status: tc.status,
				},
			})

			other := statusSuccess
			if tc.wantLabel == statusSuccess {
				other = statusError
			}
			assert.Equal(t, 1.0, testutil.ToFloat64(c.toolCallsTotal.WithLabelValues("http", tc.wantLabel)),
				"status %q must count under status=%q", tc.status, tc.wantLabel)
			assert.Equal(t, 0.0, testutil.ToFloat64(c.toolCallsTotal.WithLabelValues("http", other)),
				"status %q must not count under status=%q", tc.status, other)
		})
	}
}

// An executor error emits tool.call.failed only, so it must count once, as an
// error: the completed handler's change must not make it count twice.
func TestMetricContext_ToolCallFailedEventCountsOnce(t *testing.T) {
	c, _ := newTestCollector()
	ctx := c.Bind(nil)

	ctx.OnEvent(&events.Event{
		Type: events.EventToolCallFailed,
		Data: &events.ToolCallFailedData{ToolName: "http", Duration: 10 * time.Millisecond},
	})

	assert.Equal(t, 1.0, testutil.ToFloat64(c.toolCallsTotal.WithLabelValues("http", statusError)))
	assert.Equal(t, 0.0, testutil.ToFloat64(c.toolCallsTotal.WithLabelValues("http", statusSuccess)))
}
