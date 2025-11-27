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
