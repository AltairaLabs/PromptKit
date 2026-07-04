package pack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// packSchemaExceptions lists example packs that intentionally do NOT validate
// against the embedded PromptPack schema, with the reason. Anything not listed
// here MUST validate cleanly — that is the whole point of this test: the packs
// we ship are the spec's reference material, so they have to be spec-compliant.
//
// The key is a path substring (matched against the discovered file path).
var packSchemaExceptions = map[string]string{
	// client-tools declares tool-level `mode: "client"` and a `client: {consent}`
	// block. These are real runtime concepts (runtime/tools.ToolDescriptor.Mode +
	// ClientConfig, documented as a built-in mode in runtime/CLAUDE.md), but they
	// are ahead of the published spec on two fronts: (1) the upstream
	// promptpack.org schema's Tool definition is additionalProperties:false and
	// does not yet model them, and (2) the SDK loader (pack.ToToolRepository)
	// currently drops them and hardcodes Mode:"local". Until the schema models
	// client tools and the loader reads them, this example is knowingly ahead of
	// spec. See https://github.com/AltairaLabs/promptpack-spec.
	"client-tools": "declares tool mode/client (client-side execution) not yet in the published schema",
}

// TestExamplePacksAreSchemaCompliant validates every *.pack.json we ship (SDK
// examples + testdata) against the embedded PromptPack schema. New or edited
// example packs that drift from the spec fail here, closing the gap where every
// example smoke test passes WithSkipSchemaValidation() and nothing checks that
// the shipped packs actually conform to the schema they advertise.
func TestExamplePacksAreSchemaCompliant(t *testing.T) {
	roots := []string{
		filepath.Join("..", "..", "examples"),
		filepath.Join("..", "..", "testdata", "packs"),
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
