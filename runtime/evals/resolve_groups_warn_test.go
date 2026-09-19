package evals

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/logger"
)

// These cover #1950: a WithEvalGroups name that matches nothing silently
// disabled every eval. An explicitly requested group with no match is a
// misconfiguration and must be reported at Warn, naming both sides. The
// benign cases — no groups requested, or a pack with no evals at all — stay
// quiet.

func captureEvalWarnLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	logger.SetLogger(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { logger.SetLogger(nil) })
	return &buf
}

func TestFilterByGroups_RequestedGroupWithNoMatchWarns(t *testing.T) {
	logs := captureEvalWarnLogs(t)

	defs := []EvalDef{
		{ID: "a", Groups: []string{"safety"}},
		{ID: "b", Groups: []string{"latency", "cost"}},
	}
	got := FilterByGroups(defs, []string{"saftey"})

	if len(got) != 0 {
		t.Fatalf("expected no defs for a group that matches nothing, got %d", len(got))
	}
	out := logs.String()
	for _, want := range []string{"requested group matches no eval", "saftey", "safety", "latency", "cost"} {
		if !strings.Contains(out, want) {
			t.Errorf("warning must contain %q; got:\n%s", want, out)
		}
	}
}

// TestFilterByGroups_WarnsPerUnmatchedGroupOnly pins that a mix of matched
// and unmatched groups reports only the unmatched ones.
func TestFilterByGroups_WarnsPerUnmatchedGroupOnly(t *testing.T) {
	logs := captureEvalWarnLogs(t)

	defs := []EvalDef{{ID: "a", Groups: []string{"safety"}}}
	got := FilterByGroups(defs, []string{"safety", "quality"})

	if len(got) != 1 {
		t.Fatalf("expected the matched def to survive, got %d", len(got))
	}
	out := logs.String()
	if !strings.Contains(out, "group=quality") {
		t.Errorf("the unmatched group must be named; got:\n%s", out)
	}
	if strings.Contains(out, "group=safety") {
		t.Errorf("a matched group must not be reported as unmatched; got:\n%s", out)
	}
}

func TestFilterByGroups_NoGroupsRequestedIsQuiet(t *testing.T) {
	logs := captureEvalWarnLogs(t)

	defs := []EvalDef{{ID: "a", Groups: []string{"safety"}}}
	if got := FilterByGroups(defs, nil); len(got) != 1 {
		t.Fatalf("nil groups must return all defs, got %d", len(got))
	}
	if logs.Len() != 0 {
		t.Errorf("no groups requested must not warn; got:\n%s", logs.String())
	}
}

func TestFilterByGroups_NoEvalsAtAllIsQuiet(t *testing.T) {
	logs := captureEvalWarnLogs(t)

	if got := FilterByGroups(nil, []string{"safety"}); len(got) != 0 {
		t.Fatalf("expected no defs, got %d", len(got))
	}
	if logs.Len() != 0 {
		t.Errorf("a pack with no evals must not warn about groups; got:\n%s", logs.String())
	}
}
