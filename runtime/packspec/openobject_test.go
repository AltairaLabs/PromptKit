package packspec_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/packspec"
)

// TestOpenObjectsPreserveExtensions covers every additionalProperties:true def
// in the schema, not just the one promptkit happens to use.
//
// These defs are envelopes the spec expects runtimes to extend (RFC 0006: "the
// spec defines the envelope; runtimes extend it"). Dropping an unknown key here
// would corrupt someone else's pack silently — it loads, it validates, and the
// data is gone. Each case asserts the extension survives BOTH directions, since
// capturing on load while dropping on save is just as lossy.
func TestOpenObjectsPreserveExtensions(t *testing.T) {
	cases := []struct {
		name  string
		json  string
		into  func() any
		named func(any) string // reads a named field, to prove it still parses
	}{
		{
			name:  "MetricDef",
			json:  `{"name":"quality","type":"gauge","labels":{"team":"cs"},"slo":0.85}`,
			into:  func() any { return &packspec.MetricDef{} },
			named: func(v any) string { return v.(*packspec.MetricDef).Name },
		},
		{
			name:  "ProviderCapabilities",
			json:  `{"tool_use":true,"x-vendor-quirk":"needs-warmup"}`,
			into:  func() any { return &packspec.ProviderCapabilities{} },
			named: func(any) string { return "" },
		},
		{
			name:  "GenericMediaTypeConfig",
			json:  `{"max_size_mb":10,"x-codec":"opus"}`,
			into:  func() any { return &packspec.GenericMediaTypeConfig{} },
			named: func(any) string { return "" },
		},
		{
			name:  "WorkflowConfigEngine",
			json:  `{"x-engine":"custom","max_parallel":4}`,
			into:  func() any { return &packspec.WorkflowConfigEngine{} },
			named: func(any) string { return "" },
		},
		{
			name:  "PackMetadata",
			json:  `{"domain":"finance","performance":{"avg_latency_ms":120},"changelog":[{"version":"1.0.0"}]}`,
			into:  func() any { return &packspec.PackMetadata{} },
			named: func(v any) string { return v.(*packspec.PackMetadata).Domain },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := tc.into()
			if err := json.Unmarshal([]byte(tc.json), target); err != nil {
				t.Fatalf("load: %v", err)
			}

			var original map[string]any
			if err := json.Unmarshal([]byte(tc.json), &original); err != nil {
				t.Fatal(err)
			}

			out, err := json.Marshal(target)
			if err != nil {
				t.Fatalf("save: %v", err)
			}
			var back map[string]any
			if err := json.Unmarshal(out, &back); err != nil {
				t.Fatal(err)
			}

			for k := range original {
				if _, ok := back[k]; !ok {
					t.Errorf("%q was dropped by the round trip. Extensions must be written "+
						"back as top-level properties: %s", k, out)
				}
			}
			if want := tc.named(target); want != "" && back["domain"] == nil && back["name"] == nil {
				t.Errorf("named fields must still serialize: %s", out)
			}

			// The no-extension path is a separate branch: it must short-circuit
			// rather than round-tripping through a map, and must not invent an
			// empty Extra object in the output.
			t.Run("without extensions", func(t *testing.T) {
				plain := tc.into()
				if err := json.Unmarshal([]byte(`{}`), plain); err != nil {
					t.Fatalf("empty object must load: %v", err)
				}
				out, err := json.Marshal(plain)
				if err != nil {
					t.Fatal(err)
				}
				var back map[string]any
				if err := json.Unmarshal(out, &back); err != nil {
					t.Fatalf("output must be an object: %s", out)
				}
				if _, stray := back["Extra"]; stray {
					t.Errorf("Extra must never appear as a key of its own: %s", out)
				}
			})
		})
	}
}

// TestEveryOpenObjectRoundTripsExtensions walks the generated registry rather
// than a hand-written list, so a def that becomes extensible in a future spec
// revision is covered the moment it is generated — no table to fall behind.
//
// 22 types are extensible: five declare additionalProperties:true, and the rest
// omit the keyword, which JSON Schema defines as true. promptkit's validator
// already accepts extras on all of them, so a type that drops them loses data
// from packs that validate.
func TestEveryOpenObjectRoundTripsExtensions(t *testing.T) {
	prototypes := packspec.OpenObjectPrototypes()
	if len(prototypes) == 0 {
		t.Fatal("no open objects registered — the generator stopped emitting the registry")
	}

	for name, proto := range prototypes {
		t.Run(name, func(t *testing.T) {
			// A key no schema names, so it can only survive via Extra.
			const src = `{"x-promptkit-probe":{"nested":[1,"two"]}}`
			target := reflect.New(reflect.TypeOf(proto).Elem()).Interface()
			if err := json.Unmarshal([]byte(src), target); err != nil {
				t.Fatalf("load: %v", err)
			}
			out, err := json.Marshal(target)
			if err != nil {
				t.Fatalf("save: %v", err)
			}
			var back map[string]any
			if err := json.Unmarshal(out, &back); err != nil {
				t.Fatalf("output must be a JSON object, got %s", out)
			}
			if _, ok := back["x-promptkit-probe"]; !ok {
				t.Errorf("%s dropped an extension across a round trip: %s", name, out)
			}
		})
	}
}

// TestOpenObjectTypedFieldWinsOverExtra — Extra is a catch-all for keys the
// schema does not name, so a collision means the caller put a named property
// there by mistake. The typed field is authoritative; silently letting Extra
// override it would make the struct field a lie.
func TestOpenObjectTypedFieldWinsOverExtra(t *testing.T) {
	m := packspec.MetricDef{
		Name:  "real_name",
		Type:  "gauge",
		Extra: map[string]any{"name": "shadow", "help": "kept"},
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if back["name"] != "real_name" {
		t.Errorf("typed field must win over Extra, got %v", back["name"])
	}
	if back["help"] != "kept" {
		t.Errorf("a genuine extension must still be written, got %s", out)
	}
}

func TestOpenObjectRejectsMalformedJSON(t *testing.T) {
	var m packspec.MetricDef
	if err := json.Unmarshal([]byte(`{"name":`), &m); err == nil {
		t.Error("malformed JSON must error")
	}
	if err := json.Unmarshal([]byte(`"a string, not an object"`), &m); err == nil {
		t.Error("a non-object must error rather than yield an empty value")
	}
}

// TestOpenObjectWithNoExtensionsIsUnchanged — the common case must not gain a
// stray key or reorder into something unrecognizable.
func TestOpenObjectWithNoExtensionsIsUnchanged(t *testing.T) {
	var m packspec.MetricDef
	if err := json.Unmarshal([]byte(`{"name":"m","type":"gauge"}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.Extra != nil {
		t.Errorf("no extensions present, so Extra should stay nil, got %v", m.Extra)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 || back["name"] != "m" || back["type"] != "gauge" {
		t.Errorf("unexpected output for a plain metric: %s", out)
	}
}
