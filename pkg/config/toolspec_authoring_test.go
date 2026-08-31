package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// A spec property nobody can WRITE is the same as no property at all, and it
// fails silently: the schema accepts it, the runtime carries it, and no author
// ever produces one. $defs/Tool.action_scope shipped in PromptPack v1.6.0 and
// sat in exactly that state — carriable, unwritable — until someone noticed in
// conversation a week later.
//
// ToolSpec is the authoring type arena and packc write tools through, so it is
// where that gap shows. This pins it to the spec's $defs/Tool.

// notAuthorable records a $defs/Tool property ToolSpec deliberately does not
// carry, and why. Adding an entry is a decision, not a way to quiet the test.
var notAuthorable = map[string]string{
	"parameters": "authored as `input_schema`, a full JSON Schema document; " +
		"prompt.ConvertToolToPackTool converts it to $defs/Tool.parameters at compile",
}

func TestEveryToolPropertyIsAuthorable(t *testing.T) {
	schemaPath := filepath.Join("..", "..", "runtime", "prompt", "schema", "promptpack.schema.json")
	raw, err := os.ReadFile(schemaPath) //nolint:gosec // fixed in-repo path
	if err != nil {
		t.Skipf("embedded schema not readable from here: %v", err)
	}

	var doc struct {
		Defs map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse schema: %v", err)
	}

	toolDef, ok := doc.Defs["Tool"]
	if !ok {
		t.Fatal("schema has no $defs/Tool")
	}

	authorable := map[string]bool{}
	rt := reflect.TypeOf(ToolSpec{})
	for i := 0; i < rt.NumField(); i++ {
		tag := strings.Split(rt.Field(i).Tag.Get("yaml"), ",")[0]
		if tag != "" && tag != "-" {
			authorable[tag] = true
		}
	}

	var missing []string
	for prop := range toolDef.Properties {
		if authorable[prop] {
			continue
		}
		if reason, deliberate := notAuthorable[prop]; deliberate {
			t.Logf("%q is deliberately not a ToolSpec field: %s", prop, reason)
			continue
		}
		missing = append(missing, prop)
	}
	sort.Strings(missing)

	if len(missing) > 0 {
		t.Errorf("$defs/Tool properties with no authoring route: %s\n\n"+
			"A property an author cannot write is the same as no property — the "+
			"schema accepts it and nobody ever produces one.\n\n"+
			"Add the field to ToolSpec, or record it in notAuthorable with the "+
			"route it takes instead.", strings.Join(missing, ", "))
	}

	// A stale exemption is its own failure: it claims a property is handled
	// elsewhere when the schema may no longer define it at all.
	for prop, reason := range notAuthorable {
		if _, defined := toolDef.Properties[prop]; !defined {
			t.Errorf("stale exemption: %q is not a $defs/Tool property any more "+
				"(reason was: %s)", prop, reason)
		}
		if authorable[prop] {
			t.Errorf("stale exemption: ToolSpec now carries %q directly; drop the "+
				"entry (reason was: %s)", prop, reason)
		}
	}
}
