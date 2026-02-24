package junit_test

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/tools/arena/assertions"
	"github.com/AltairaLabs/PromptKit/tools/arena/engine"
	"github.com/AltairaLabs/PromptKit/tools/arena/results/junit"
)

func TestJUnit_ConversationAssertions_PropertiesAndFailure(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "junit.xml")
	repo := junit.NewJUnitResultRepository(out)

	rr := engine.RunResult{
		RunID:      "r1",
		ScenarioID: "sc1",
		ProviderID: "prov",
		Region:     "us",
		Duration:   time.Second,
		ConversationAssertions: engine.AssertionsSummary{
			Failed: 1,
			Passed: false,
			Total:  2,
			Results: []assertions.ConversationValidationResult{
				{Type: "tools_called", Passed: false, Message: "failed A"},
				{Type: "tools_called", Passed: true, Message: "ok"},
			},
		},
	}

	require.NoError(t, repo.SaveResults([]engine.RunResult{rr}))

	data, err := os.ReadFile(out)
	require.NoError(t, err)

	var suites junit.JUnitTestSuites
	require.NoError(t, xml.Unmarshal(data, &suites))

	require.Len(t, suites.TestSuites, 1)
	require.Len(t, suites.TestSuites[0].TestCases, 1)
	tc := suites.TestSuites[0].TestCases[0]

	// Conversation assertion properties should be attached
	props := map[string]string{}
	for _, p := range tc.Properties {
		props[p.Name] = p.Value
	}
	if props["conversation_assertions.total"] != "2" || props["conversation_assertions.failed"] != "1" || props["conversation_assertions.passed"] != "false" {
		t.Fatalf("unexpected conversation assertion properties: %#v", props)
	}

	// And an injected failure since no other failures/errors present but conv assertions failed
	if tc.Failure == nil || tc.Failure.Type != "ConversationAssertionFailure" {
		t.Fatalf("expected ConversationAssertionFailure, got: %+v", tc.Failure)
	}
}

func TestJUnit_ConversationAssertions_EvalProperties(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "junit.xml")
	repo := junit.NewJUnitResultRepository(out)

	rr := engine.RunResult{
		RunID:      "r2",
		ScenarioID: "sc2",
		ProviderID: "prov",
		Region:     "us",
		Duration:   time.Second,
		ConversationAssertions: engine.AssertionsSummary{
			Failed: 0,
			Passed: true,
			Total:  2,
			Results: []assertions.ConversationValidationResult{
				{
					Type: "pack_eval:llm_judge", Passed: true, Message: "good",
					Details: map[string]interface{}{
						"score":       0.85,
						"eval_id":     "eval-abc",
						"duration_ms": int64(42),
					},
				},
				{
					Type: "tools_called", Passed: true, Message: "ok",
				},
			},
		},
	}

	require.NoError(t, repo.SaveResults([]engine.RunResult{rr}))

	data, err := os.ReadFile(out)
	require.NoError(t, err)

	var suites junit.JUnitTestSuites
	require.NoError(t, xml.Unmarshal(data, &suites))

	require.Len(t, suites.TestSuites, 1)
	require.Len(t, suites.TestSuites[0].TestCases, 1)
	tc := suites.TestSuites[0].TestCases[0]

	props := map[string]string{}
	for _, p := range tc.Properties {
		props[p.Name] = p.Value
	}

	// Verify per-result properties for the first result (has eval data)
	require.Equal(t, "0.8500", props["conversation_assertions.0.score"])
	require.Equal(t, "eval-abc", props["conversation_assertions.0.eval_id"])
	require.Equal(t, "42", props["conversation_assertions.0.duration_ms"])

	// Second result should not have eval properties
	_, hasScore1 := props["conversation_assertions.1.score"]
	require.False(t, hasScore1, "second result should not have score property")
}
