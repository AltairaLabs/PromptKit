package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/codes"

	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/tools"
)

// A tool that ran but failed completes (tool.call.completed) with status
// "failed" rather than emitting tool.call.failed. Its span ended OK, so traces
// showed a failing tool as healthy (#2148).
func TestOTelEventListener_ToolCompletedStatusSetsSpanStatus(t *testing.T) {
	cases := []struct {
		status string
		want   codes.Code
	}{
		{string(tools.ToolStatusFailed), codes.Error},
		{"error", codes.Error},
		{string(tools.ToolStatusComplete), codes.Ok},
		{"success", codes.Ok},
	}
	for _, tc := range cases {
		t.Run(tc.status, func(t *testing.T) {
			listener, exp, tp := newTestListener(t)
			now := time.Now()
			listener.StartSession(context.Background(), "sess-1")

			listener.OnEvent(&events.Event{
				Type: events.EventToolCallStarted, Timestamp: now,
				SessionID: "sess-1", ExecutionID: "run-1",
				Data: &events.ToolCallStartedData{ToolName: "http", CallID: "call-1"},
			})
			listener.OnEvent(&events.Event{
				Type: events.EventToolCallCompleted, Timestamp: now.Add(10 * time.Millisecond),
				SessionID: "sess-1", ExecutionID: "run-1",
				Data: &events.ToolCallCompletedData{
					ToolName: "http", CallID: "call-1", Duration: 10 * time.Millisecond, Status: tc.status,
				},
			})

			listener.EndSession("sess-1")
			toolSpan := findSpan(t, flushAndGetSpans(t, tp, exp), "execute_tool")
			assert.Equal(t, tc.want, toolSpan.Status.Code, "status %q", tc.status)
		})
	}
}
