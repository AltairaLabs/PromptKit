package config

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/AltairaLabs/PromptKit/runtime/v2/providers"
)

// optionalProviderBlocks are the provider config types whose every field is
// optional in a provider file. A schema reflector (PromptArena's schema-gen)
// marks a field without omitempty as required, so each field here must carry
// omitempty in both its json and yaml tags.
var optionalProviderBlocks = []any{Pricing{}, RateLimit{}, ProviderDefaults{}}

func TestOptionalProviderBlocks_EveryFieldTaggedOmitempty(t *testing.T) {
	for _, v := range optionalProviderBlocks {
		typ := reflect.TypeOf(v)
		for i := 0; i < typ.NumField(); i++ {
			f := typ.Field(i)
			for _, key := range []string{"json", "yaml"} {
				parts := strings.Split(f.Tag.Get(key), ",")
				if parts[0] == "" || parts[0] == "-" {
					t.Errorf("%s.%s: %s tag has no name", typ.Name(), f.Name, key)
					continue
				}
				if !containsString(parts[1:], "omitempty") {
					t.Errorf("%s.%s: %s tag %q lacks omitempty, so a generated schema marks %q required",
						typ.Name(), f.Name, key, f.Tag.Get(key), parts[0])
				}
			}
		}
	}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// A partial pricing, rate_limit or defaults block loads, and each key left out
// decodes to zero.
func TestLoadProvider_AcceptsPartialOptionalBlocks(t *testing.T) {
	path := writeManifest(t, providerHeader+"spec:\n  type: openai\n  model: gpt-4o\n"+
		"  pricing:\n    input_cost_per_1k: 0.5\n"+
		"  rate_limit:\n    rps: 10\n"+
		"  defaults:\n    temperature: 0.2\n    max_tokens: 256\n")

	p, err := LoadProvider(path)
	if err != nil {
		t.Fatalf("LoadProvider: %v", err)
	}
	want := Provider{
		ID: "p", Type: "openai", Model: "gpt-4o",
		Pricing:   Pricing{InputCostPer1K: 0.5},
		RateLimit: RateLimit{RPS: 10},
		Defaults:  ProviderDefaults{Temperature: 0.2, MaxTokens: 256},
	}
	if !reflect.DeepEqual(*p, want) {
		t.Errorf("loaded %+v\nwant   %+v", *p, want)
	}
}

// An explicit zero and a missing key decode to the same value, so omitempty
// dropping a zero on write cannot change what a reload sees.
func TestLoadProvider_ExplicitZeroEqualsMissing(t *testing.T) {
	zeros, err := LoadProvider(writeManifest(t, providerHeader+"spec:\n  type: openai\n"+
		"  pricing:\n    input_cost_per_1k: 0\n    output_cost_per_1k: 0\n"+
		"  rate_limit:\n    rps: 0\n    burst: 0\n"+
		"  defaults:\n    temperature: 0\n    top_p: 0\n    max_tokens: 0\n"))
	if err != nil {
		t.Fatalf("LoadProvider (explicit zeros): %v", err)
	}
	missing, err := LoadProvider(writeManifest(t, providerHeader+"spec:\n  type: openai\n"))
	if err != nil {
		t.Fatalf("LoadProvider (keys missing): %v", err)
	}
	if !reflect.DeepEqual(zeros, missing) {
		t.Errorf("explicit zeros %+v\nmissing keys   %+v", *zeros, *missing)
	}
}

// Writing a provider and reading it back keeps every optional field, zeros
// included, and a zero field is left out of the written file rather than
// written as a value a schema would then require.
func TestProvider_OptionalBlocksRoundTrip(t *testing.T) {
	cases := map[string]Provider{
		"all set": {
			ID: "p", Type: "openai",
			Pricing:   Pricing{InputCostPer1K: 0.5, OutputCostPer1K: 1.5},
			RateLimit: RateLimit{RPS: 10, Burst: 20},
			Defaults:  ProviderDefaults{Temperature: 0.7, TopP: 0.9, MaxTokens: 1024},
		},
		"meaningful zeros": {
			ID: "p", Type: "openai",
			Pricing:   Pricing{InputCostPer1K: 0.5},
			RateLimit: RateLimit{Burst: 5},
			Defaults:  ProviderDefaults{TopP: 0.9, MaxTokens: providers.MaxTokensUnlimited},
		},
		"temperature zero": {
			ID: "p", Type: "openai",
			Defaults: ProviderDefaults{Temperature: 0, MaxTokens: 256},
		},
	}
	zeroKeys := map[string][]string{
		"meaningful zeros": {"output_cost_per_1k", "rps", "temperature"},
		"temperature zero": {"temperature", "top_p", "input_cost_per_1k", "rps", "burst"},
	}
	for name, in := range cases {
		t.Run(name+"/yaml", func(t *testing.T) {
			data, err := yaml.Marshal(in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			assertKeysAbsent(t, string(data), zeroKeys[name], ":")
			var out Provider
			if err := yaml.Unmarshal(data, &out); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(out, in) {
				t.Errorf("round trip %+v\nwant       %+v", out, in)
			}
		})
		t.Run(name+"/json", func(t *testing.T) {
			data, err := json.Marshal(in)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			assertKeysAbsent(t, string(data), zeroKeys[name], `":`)
			var out Provider
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if !reflect.DeepEqual(out, in) {
				t.Errorf("round trip %+v\nwant       %+v", out, in)
			}
		})
	}
}

func assertKeysAbsent(t *testing.T, doc string, keys []string, suffix string) {
	t.Helper()
	for _, k := range keys {
		if strings.Contains(doc, k+suffix) {
			t.Errorf("zero field %q was written:\n%s", k, doc)
		}
	}
}
