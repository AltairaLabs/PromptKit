package evals_test

import (
	"encoding/json"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// TestMetricLabelsSurviveARoundTrip is the assertion that matters for labels.
//
// RFC 0006 names labels, help, aggregation, alert_threshold and slo as runtime
// extensions and states they are NOT part of the specification — $defs/MetricDef
// is additionalProperties:true precisely so runtimes can add them. A generated
// type carrying only the named properties would load such a pack happily and
// throw the extensions away, which is the failure this covers.
func TestMetricLabelsSurviveARoundTrip(t *testing.T) {
	const src = `{
		"name": "promptpack_tone_score",
		"type": "gauge",
		"labels": {"team": "customer-success", "environment": "production"},
		"help": "Tone score",
		"slo": 0.85
	}`

	var m evals.MetricDef
	if err := json.Unmarshal([]byte(src), &m); err != nil {
		t.Fatal(err)
	}

	labels := evals.MetricLabels(&m)
	if labels["team"] != "customer-success" || labels["environment"] != "production" {
		t.Errorf("labels lost on load: %v", labels)
	}
	// The other extensions RFC 0006 names must survive too, even though nothing
	// in promptkit reads them — dropping them would corrupt someone else's pack.
	if m.Extra["help"] != "Tone score" || m.Extra["slo"] != 0.85 {
		t.Errorf("non-label extensions lost on load: %v", m.Extra)
	}

	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"name", "type", "labels", "help", "slo"} {
		if _, ok := back[k]; !ok {
			t.Errorf("%q missing after marshal; extensions must be written back as "+
				"top-level properties, not nested under an \"extra\" key: %s", k, out)
		}
	}
}

func TestSetMetricLabelsIsTheInverse(t *testing.T) {
	var m evals.MetricDef
	evals.SetMetricLabels(&m, map[string]string{"team": "cs"})
	if got := evals.MetricLabels(&m); got["team"] != "cs" {
		t.Errorf("round trip through Set/Get failed: %v", got)
	}

	// Empty removes the key rather than writing an empty object, so a pack does
	// not gain a meaningless `"labels": {}`.
	evals.SetMetricLabels(&m, nil)
	if _, present := m.Extra["labels"]; present {
		t.Errorf("empty labels must remove the key, got %v", m.Extra)
	}
	if got := evals.MetricLabels(&m); got != nil {
		t.Errorf("MetricLabels should be nil after clearing, got %v", got)
	}
}

// TestMetricLabelsRejectsAWrongShape — labels come from a pack, so the value can
// be anything. A partial map would be worse than none: it would silently drop
// the entries that did not fit while looking successful.
func TestMetricLabelsRejectsAWrongShape(t *testing.T) {
	for _, src := range []string{
		`{"name":"m","type":"gauge","labels":"not-an-object"}`,
		`{"name":"m","type":"gauge","labels":{"team":123}}`,
		`{"name":"m","type":"gauge","labels":["a","b"]}`,
	} {
		var m evals.MetricDef
		if err := json.Unmarshal([]byte(src), &m); err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if got := evals.MetricLabels(&m); got != nil {
			t.Errorf("%s: expected nil for a malformed labels value, got %v", src, got)
		}
	}
}

func TestMetricLabelsHandlesNil(t *testing.T) {
	if got := evals.MetricLabels(nil); got != nil {
		t.Errorf("MetricLabels(nil) = %v, want nil", got)
	}
	evals.SetMetricLabels(nil, map[string]string{"a": "b"}) // must not panic
}
