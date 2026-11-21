package a2a

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A status time goes on the wire as a ProtoJSON Timestamp — UTC, 0/3/6/9
// fractional digits — in both versions' shapes, whatever zone it is held in.
func TestTaskStatus_TimestampIsWrittenInUTC(t *testing.T) {
	zone := time.FixedZone("UTC-7", -7*3600)
	for _, tc := range []struct {
		ns   int
		want string
	}{
		{0, "2026-11-21T03:04:05Z"},
		{100_000_000, "2026-11-21T03:04:05.100Z"},
		{123_000_000, "2026-11-21T03:04:05.123Z"},
		{123_400_000, "2026-11-21T03:04:05.123400Z"},
		{123_456_000, "2026-11-21T03:04:05.123456Z"},
		{123_456_700, "2026-11-21T03:04:05.123456700Z"},
		{1, "2026-11-21T03:04:05.000000001Z"},
	} {
		at := time.Date(2026, 11, 20, 20, 4, 5, tc.ns, zone)
		task := &Task{ID: "t", ContextID: "c", Status: TaskStatus{State: TaskStateWorking, Timestamp: &at}}

		for _, v := range []ProtocolVersion{ProtocolVersion10, ProtocolVersion03} {
			raw, err := json.Marshal(v.WireTask(task))
			require.NoError(t, err)
			var got struct {
				Status map[string]any `json:"status"`
			}
			require.NoError(t, json.Unmarshal(raw, &got))
			assert.Equal(t, tc.want, got.Status["timestamp"], "%s, %d ns", v, tc.ns)
		}

		// The runtime reads it back as the same instant.
		raw, err := json.Marshal(task.Status)
		require.NoError(t, err)
		var back TaskStatus
		require.NoError(t, json.Unmarshal(raw, &back))
		require.NotNil(t, back.Timestamp)
		assert.True(t, back.Timestamp.Equal(at), "%s read back as %s", at, back.Timestamp)
	}

	raw, err := json.Marshal(TaskStatus{State: TaskStateWorking})
	require.NoError(t, err)
	assert.JSONEq(t, `{"state":"TASK_STATE_WORKING"}`, string(raw), "no timestamp, no key")
}
