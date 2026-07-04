package pack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// packSchemaExceptions lists example packs that intentionally do NOT validate
// against the embedded PromptPack schema, keyed by a path substring with the
// reason. It is currently empty: every shipped pack is spec-compliant, which is
// the whole point of this test — the packs we ship are the spec's reference
// material.
//
// If you must add an entry, prefer fixing the pack. A legitimate exception is
// one where the pack is deliberately ahead of the published schema; note that a
// tool's execution mode (client/live/mcp/...) is a HOSTING concern bound by the
// host (e.g. conv.OnClientTool), not a pack field — do not reintroduce it here.
// The test below asserts excepted packs still FAIL, so the list can't rot.
var packSchemaExceptions = map[string]string{}

// TestExamplePacksAreSchemaCompliant validates every *.pack.json we ship (SDK
// examples + testdata) against the embedded PromptPack schema. New or edited
// example packs that drift from the spec fail here, closing the gap where every
// example smoke test passes WithSkipSchemaValidation() and nothing checks that
// the shipped packs actually conform to the schema they advertise.
func TestExamplePacksAreSchemaCompliant(t *testing.T) {
	// Cover every *.pack.json we ship, across modules. These are file reads +
	// schema validation only (no cross-module compilation), so walking sibling
	// module trees from here is safe. Paths are relative to this package dir.
	roots := []string{
		filepath.Join("..", "..", "examples"),          // sdk/examples
		filepath.Join("..", "..", "testdata", "packs"), // sdk/testdata/packs
		filepath.Join("..", "..", "..", "examples"),    // repo-root examples/
		filepath.Join("..", "..", "..", "benchmarks"),  // benchmarks/
	}

	var files []string
	for _, root := range roots {
		err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && strings.HasSuffix(p, ".pack.json") {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}

	if len(files) == 0 {
		t.Fatal("no *.pack.json files discovered — check the example/testdata roots")
	}

	for _, f := range files {
		f := f
		t.Run(f, func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}

			err = ValidateAgainstSchema(data)

			if reason, excepted := exceptionFor(f); excepted {
				// Excepted packs must still fail. If one starts passing, the
				// schema/loader has caught up — remove it from the allowlist.
				if err == nil {
					t.Fatalf("%s is on the schema-exception allowlist (%q) but now "+
						"validates cleanly — remove it from packSchemaExceptions", f, reason)
				}
				t.Logf("known exception (%s): %v", reason, err)
				return
			}

			if err != nil {
				t.Fatalf("%s does not conform to the PromptPack schema:\n%v", f, err)
			}
		})
	}
}

func exceptionFor(path string) (string, bool) {
	for substr, reason := range packSchemaExceptions {
		if strings.Contains(path, substr) {
			return reason, true
		}
	}
	return "", false
}
