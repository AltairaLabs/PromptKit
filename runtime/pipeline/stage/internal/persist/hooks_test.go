package persist

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfigOf_ReturnsTheConfigTheStageRecordsWith(t *testing.T) {
	cfg := RecordingStageConfig{
		Position:             RecordingPositionOutput,
		SessionID:            "s",
		ConversationID:       "c",
		IncludeStreamingText: true,
		IncludeVideo:         true,
	}
	rs := NewRecordingStage(&fakeEventStore{}, cfg).WithSessionID("s2")

	want := cfg
	want.SessionID = "s2"
	assert.Equal(t, want, ConfigOf(rs))
}
