package prompt_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

// TestGovernanceSurvivesARoundTrip is the regression this file exists for.
//
// prompt.Metadata was hand-written, so when the spec added metadata.governance
// in v1.6.0 (RFC 0013) the struct simply had nowhere to put it. A pack naming
// its accountable owner, its autonomy level and whether it must disclose itself
// as AI loaded without error, validated clean, and lost all of it — the worst
// shape of failure for a governance field, because the pack still looks fine.
//
// Metadata is generated now, so this covers the accessors for the two
// extensions that are NOT spec properties and had to move into the envelope.
func TestGovernanceSurvivesARoundTrip(t *testing.T) {
	src := `{"id":"p","name":"P","version":"1.0.0","description":"d",
	  "template_engine":{"version":"v1","syntax":"{{variable}}"},
	  "prompts":{},
	  "metadata":{"domain":"support","governance":{
	     "autonomy_level":"acts_with_approval","accountable_owner":"risk@example.com",
	     "requires_ai_disclosure":true},
	     "performance":{"avg_latency_ms":42,"success_rate":0.9},
	     "changelog":[{"version":"1.0.0","date":"2026-01-01","description":"init"}]},
	  "agents":{"entry":"a","members":{"a":{"description":"d",
	     "governance":{"autonomy_level":"acts_autonomously"}}}},
	  "tools":{"t":{"name":"t","description":"d",
	     "action_scope":{"effect":"write","reversibility":"irreversible",
	        "data_classes":["pii"]}}}}`
	var p prompt.Pack
	if err := json.Unmarshal([]byte(src), &p); err != nil {
		t.Fatal(err)
	}
	// The non-spec extensions must still be reachable as typed values.
	if perf := prompt.MetadataPerformance(p.Metadata); perf == nil || perf.AvgLatencyMs != 42 {
		t.Errorf("performance not readable after load: %+v", perf)
	}
	if cl := prompt.MetadataChangelog(p.Metadata); len(cl) != 1 || cl[0].Version != "1.0.0" {
		t.Errorf("changelog not readable after load: %+v", cl)
	}
	out, err := prompt.NewPackCompiler(nil).MarshalPack(&p)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"\"domain\"", "acts_with_approval", "risk@example.com", "requires_ai_disclosure",
		"acts_autonomously", "irreversible", "pii", "avg_latency_ms", "changelog",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("DROPPED %q", want)
		}
	}
}
