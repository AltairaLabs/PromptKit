package handlers

import (
	"fmt"
	"testing"
)

// TestTopicPolicyHandler_MarkWarnedFirstOccurrenceOnly pins "loud once per
// conversation, not once per turn": the first call for a given session id
// reports true (log it), every later call for the same id reports false.
func TestTopicPolicyHandler_MarkWarnedFirstOccurrenceOnly(t *testing.T) {
	h := &TopicPolicyHandler{}

	if !h.markWarned("s1") {
		t.Fatal("first call for a session id must report true")
	}
	if h.markWarned("s1") {
		t.Fatal("second call for the same session id must report false")
	}
	if !h.markWarned("s2") {
		t.Fatal("first call for a different session id must report true")
	}
}

// TestTopicPolicyHandler_MarkWarnedBoundsMemory is the #1996-follow-up
// regression test: TopicPolicyHandler is registered once as a process-wide
// singleton, so without a cap warnedSessions would accumulate one entry per
// distinct SessionID for the life of the process. Driving more than
// maxWarnedSessions distinct ids through it must never let the tracker grow
// past the cap. This asserts on the tracker's size directly rather than on
// log output — a test that only counted log lines would pass against the old
// unbounded sync.Map too.
func TestTopicPolicyHandler_MarkWarnedBoundsMemory(t *testing.T) {
	h := &TopicPolicyHandler{}

	for i := 0; i < maxWarnedSessions*3; i++ {
		h.markWarned(fmt.Sprintf("session-%d", i))
		if got := len(h.warnedSessions); got > maxWarnedSessions {
			t.Fatalf("warnedSessions has %d entries after %d distinct ids, want <= %d (cap)",
				got, i+1, maxWarnedSessions)
		}
	}
}
