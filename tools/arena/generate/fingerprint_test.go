package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/types"
	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
)

func TestFingerprint_Stability(t *testing.T) {
	session := &SessionDetail{
		SessionSummary: SessionSummary{ID: "s1"},
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi"},
		},
		EvalResults: []assertions.ConversationValidationResult{
			{Type: "content_matches", Passed: false, Details: map[string]interface{}{"pattern": "x"}},
		},
	}

	fp1 := Fingerprint(session)
	fp2 := Fingerprint(session)
	assert.Equal(t, fp1, fp2, "same input should produce same fingerprint")
	assert.Len(t, fp1, fingerprintLength)
}

func TestFingerprint_DifferentFailures(t *testing.T) {
	base := SessionDetail{
		SessionSummary: SessionSummary{ID: "s1"},
		Messages: []types.Message{
			{Role: "user", Content: "Hello"},
		},
	}

	s1 := base
	s1.EvalResults = []assertions.ConversationValidationResult{
		{Type: "content_matches", Passed: false},
	}

	s2 := base
	s2.EvalResults = []assertions.ConversationValidationResult{
		{Type: "tools_called", Passed: false},
	}

	assert.NotEqual(t, Fingerprint(&s1), Fingerprint(&s2))
}

func TestFingerprint_ContentOnly(t *testing.T) {
	s1 := &SessionDetail{
		SessionSummary: SessionSummary{ID: "s1"},
		Messages:       []types.Message{{Role: "user", Content: "Hello"}},
	}
	s2 := &SessionDetail{
		SessionSummary: SessionSummary{ID: "s2"},
		Messages:       []types.Message{{Role: "user", Content: "Goodbye"}},
	}

	assert.NotEqual(t, Fingerprint(s1), Fingerprint(s2))
}

func TestFingerprint_EmptySession(t *testing.T) {
	session := &SessionDetail{
		SessionSummary: SessionSummary{ID: "empty"},
	}
	fp := Fingerprint(session)
	assert.Len(t, fp, fingerprintLength)
}

func TestDeduplicateSessions(t *testing.T) {
	// Two sessions with identical content and failures should deduplicate.
	makeSession := func(id string) *SessionDetail {
		return &SessionDetail{
			SessionSummary: SessionSummary{ID: id},
			Messages:       []types.Message{{Role: "user", Content: "same message"}},
			EvalResults: []assertions.ConversationValidationResult{
				{Type: "content_matches", Passed: false},
			},
		}
	}

	sessions := []*SessionDetail{
		makeSession("s1"),
		makeSession("s2"),
		makeSession("s3"),
	}

	result := DeduplicateSessions(sessions)
	require.Len(t, result, 1, "duplicates should be removed")
	assert.Equal(t, "s1", result[0].ID, "first session should be kept")
}

func TestDeduplicateSessions_Unique(t *testing.T) {
	sessions := []*SessionDetail{
		{
			SessionSummary: SessionSummary{ID: "s1"},
			Messages:       []types.Message{{Role: "user", Content: "message A"}},
		},
		{
			SessionSummary: SessionSummary{ID: "s2"},
			Messages:       []types.Message{{Role: "user", Content: "message B"}},
		},
	}

	result := DeduplicateSessions(sessions)
	assert.Len(t, result, 2, "unique sessions should all be kept")
}

func TestDeduplicateSessions_Empty(t *testing.T) {
	result := DeduplicateSessions(nil)
	assert.Nil(t, result)
}

func TestDeduplicateSessions_WithTurnEvalResults(t *testing.T) {
	s1 := &SessionDetail{
		SessionSummary: SessionSummary{ID: "s1"},
		Messages:       []types.Message{{Role: "user", Content: "same"}},
		TurnEvalResults: map[int][]TurnEvalResult{
			0: {{Type: "content_includes", Passed: false}},
		},
	}
	s2 := &SessionDetail{
		SessionSummary: SessionSummary{ID: "s2"},
		Messages:       []types.Message{{Role: "user", Content: "same"}},
		TurnEvalResults: map[int][]TurnEvalResult{
			0: {{Type: "content_includes", Passed: false}},
		},
	}

	result := DeduplicateSessions([]*SessionDetail{s1, s2})
	assert.Len(t, result, 1)
}
