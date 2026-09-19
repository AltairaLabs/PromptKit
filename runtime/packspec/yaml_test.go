package packspec_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
	yaml "go.yaml.in/yaml/v3"
)

// The generated types carry custom JSON codecs for unions, shorthands and
// extensions. A YAML library does not consult MarshalJSON/UnmarshalJSON, and
// this repo decodes YAML DIRECTLY into these structs — prompt.ParseConfig runs
// yaml.Unmarshal into Config, whose Evals carry MetricDef. Generating the types
// therefore broke two things at once and neither was visible until a YAML path
// was exercised:
//
//   - metric.labels vanished from YAML prompt configs (no error, no warning)
//   - a `requires` block or a composition step failed to parse at all
//
// These tests exist so that cannot recur silently.

// TestOpenObjectsRoundTripYAML walks the generated registry, so a type that
// becomes extensible later is covered the moment it is generated.
func TestOpenObjectsRoundTripYAML(t *testing.T) {
	for name, proto := range packspec.OpenObjectPrototypes() {
		t.Run(name, func(t *testing.T) {
			target := reflect.New(reflect.TypeOf(proto).Elem()).Interface()
			if err := yaml.Unmarshal([]byte("x-promptkit-probe:\n  nested: [1, two]\n"), target); err != nil {
				t.Fatalf("YAML load: %v", err)
			}
			out, err := yaml.Marshal(target)
			if err != nil {
				t.Fatalf("YAML save: %v", err)
			}
			var back map[string]any
			if err := yaml.Unmarshal(out, &back); err != nil {
				t.Fatalf("output must be a mapping, got %s", out)
			}
			if _, ok := back["x-promptkit-probe"]; !ok {
				t.Errorf("%s dropped an extension across a YAML round trip: %s", name, out)
			}
		})
	}
}

// TestGeneratedCodecsRejectAWrongShape walks the registry and feeds each type a
// document that is valid YAML/JSON but the wrong shape. The error branches in
// the generated codecs are the difference between a clear failure and a
// silently-empty value, which is the failure mode this whole branch has been
// chasing.
func TestGeneratedCodecsRejectAWrongShape(t *testing.T) {
	for name, proto := range packspec.OpenObjectPrototypes() {
		t.Run(name, func(t *testing.T) {
			typ := reflect.TypeOf(proto).Elem()

			// A scalar where an object is required.
			target := reflect.New(typ).Interface()
			if err := yaml.Unmarshal([]byte("just-a-string"), target); err == nil {
				t.Errorf("%s accepted a scalar as an object via YAML", name)
			}
			target = reflect.New(typ).Interface()
			if err := json.Unmarshal([]byte(`"just-a-string"`), target); err == nil {
				t.Errorf("%s accepted a scalar as an object via JSON", name)
			}

			// Malformed input must error rather than yield a zero value.
			target = reflect.New(typ).Interface()
			if err := json.Unmarshal([]byte(`{"a":`), target); err == nil {
				t.Errorf("%s accepted malformed JSON", name)
			}
			target = reflect.New(typ).Interface()
			if err := yaml.Unmarshal([]byte("a: [unclosed\n"), target); err == nil {
				t.Errorf("%s accepted malformed YAML", name)
			}

			// A populated value must survive marshaling in both formats, which
			// exercises the encode side of every generated codec.
			target = reflect.New(typ).Interface()
			if err := yaml.Unmarshal([]byte("x-probe: 1\n"), target); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if _, err := yaml.Marshal(target); err != nil {
				t.Errorf("%s failed to marshal to YAML: %v", name, err)
			}
			if _, err := json.Marshal(target); err != nil {
				t.Errorf("%s failed to marshal to JSON: %v", name, err)
			}
		})
	}
}

// TestUnionsRoundTripYAML covers the union types, which are not all in the
// open-object registry. Each previously failed to parse from YAML entirely.
func TestUnionsRoundTripYAML(t *testing.T) {
	t.Run("StepInput reference", func(t *testing.T) {
		var in packspec.StepInput
		if err := yaml.Unmarshal([]byte(`"${input.order_id}"`), &in); err != nil {
			t.Fatalf("a step input written as a YAML string must parse: %v", err)
		}
		if in.String != "${input.order_id}" {
			t.Errorf("String = %q", in.String)
		}
	})

	t.Run("StepInput object", func(t *testing.T) {
		var in packspec.StepInput
		if err := yaml.Unmarshal([]byte("id: ${input.id}\nlimit: 5\n"), &in); err != nil {
			t.Fatalf("a step input written as a YAML mapping must parse: %v", err)
		}
		if in.Object["id"] != "${input.id}" {
			t.Errorf("Object = %v", in.Object)
		}
	})

	t.Run("ProviderRequirement shorthand", func(t *testing.T) {
		var r packspec.PackRequires
		if err := yaml.Unmarshal([]byte("providers:\n  - default\n  - key: judge\n    role: llm\n"), &r); err != nil {
			t.Fatalf("RFC 0012's own example must parse from YAML: %v", err)
		}
		if len(r.Providers) != 2 {
			t.Fatalf("want 2 providers, got %d", len(r.Providers))
		}
		if r.Providers[0].Shorthand != "default" {
			t.Errorf("shorthand lost: %+v", r.Providers[0])
		}
		if r.Providers[1].Key != "judge" {
			t.Errorf("object form lost: %+v", r.Providers[1])
		}
	})

	// Marshaling matters as much as parsing: capturing a union on load and
	// dropping it on save is just as lossy, and packc writes YAML back out.
	t.Run("unions marshal back to YAML", func(t *testing.T) {
		cases := []struct {
			name string
			v    any
			want string
		}{
			{"StepInput", packspec.StepInput{String: "${input.x}"}, "${input.x}"},
			{"ProviderRequirement", packspec.ProviderRequirement{Shorthand: "default"}, "default"},
			{"SkillSource", packspec.SkillSource{Shorthand: "./skills/a"}, "./skills/a"},
		}
		for _, tc := range cases {
			out, err := yaml.Marshal(tc.v)
			if err != nil {
				t.Errorf("%s: %v", tc.name, err)
				continue
			}
			if !strings.Contains(string(out), tc.want) {
				t.Errorf("%s marshaled to %q, expected it to contain %q", tc.name, out, tc.want)
			}
		}
	})

	t.Run("SkillSource shorthand", func(t *testing.T) {
		var s packspec.SkillSource
		if err := yaml.Unmarshal([]byte(`"./skills/refunds"`), &s); err != nil {
			t.Fatalf("the common skills form must parse from YAML: %v", err)
		}
		if s.Shorthand != "./skills/refunds" {
			t.Errorf("Shorthand = %q", s.Shorthand)
		}
	})
}

// TestMetricLabelsSurviveYAML pins the regression by name: this is the exact
// shape a prompt config carries, and labels were being dropped from it.
func TestMetricLabelsSurviveYAML(t *testing.T) {
	var m packspec.MetricDef
	src := "name: promptpack_tone_score\ntype: gauge\nlabels:\n  team: cs\n  environment: prod\n"
	if err := yaml.Unmarshal([]byte(src), &m); err != nil {
		t.Fatal(err)
	}
	labels, ok := m.Extra["labels"].(map[string]any)
	if !ok || labels["team"] != "cs" {
		t.Errorf("labels lost on YAML load: %v", m.Extra)
	}
	if m.Name != "promptpack_tone_score" {
		t.Errorf("named fields must still parse, got %q", m.Name)
	}
}
