// Command packspec-gen generates Go types from the embedded PromptPack schema.
//
// The schema is a verbatim mirror of the published spec release and is never
// hand-edited (see runtime/prompt/schema/schema.go). This generator makes the Go
// types follow it, so a spec change that the code does not handle becomes a
// compile error at the use sites rather than a test failure that can be silenced
// with an omission entry.
//
// Its defining property is that it FAILS on anything in the schema it does not
// account for — see coverage.go. A generator that silently skips what it cannot
// express would rebuild, one level up, exactly the inert-declaration bug it is
// meant to eliminate.
//
// Usage:
//
//	packspec-gen -schema PATH -out FILE [-package NAME]
//	packspec-gen -schema PATH -out FILE -check   # verify without writing
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"os"
)

// generatedFileMode is the permission for the emitted file.
const generatedFileMode = 0o600

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "packspec-gen: %v\n", err)
		os.Exit(1)
	}
}

// run parses args into runWith. It takes args rather than reading the package
// flag state so it can be exercised directly by tests.
func run(args []string) error {
	fs := flag.NewFlagSet("packspec-gen", flag.ContinueOnError)
	var (
		schemaPath = fs.String("schema", "runtime/prompt/schema/promptpack.schema.json",
			"path to the PromptPack schema")
		outPath = fs.String("out", "runtime/prompt/packspec/types.go", "file to write")
		pkgName = fs.String("package", "packspec", "generated package name")
		check   = fs.Bool("check", false, "verify the committed output is current; write nothing")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}

	return runWith(*schemaPath, *outPath, *pkgName, *check)
}

// runWith is run() with its flags resolved, so tests can drive the whole
// pipeline — including -check — without going through package-level flag state.
func runWith(schemaPath, outPath, pkgName string, check bool) error {
	schema, err := LoadSchema(schemaPath)
	if err != nil {
		return err
	}

	cov := NewCoverage()
	ex := NewExclusions()

	// Walk first: this records every keyword present, including ones the
	// emitter never reaches, so an unhandled construct anywhere in the document
	// is reported rather than missed.
	schema.WalkKeywords(cov)

	src, err := NewEmitter(schema, cov, ex, pkgName).Generate()
	if err != nil {
		return err
	}

	if covErr := cov.Reconcile(ex); covErr != nil {
		return covErr
	}

	formatted, err := format.Source([]byte(src))
	if err != nil {
		return fmt.Errorf("generated source does not parse: %w", err)
	}

	if check {
		existing, err := os.ReadFile(outPath) //nolint:gosec // build-time argument
		if err != nil {
			return fmt.Errorf("read %s: %w (run 'make packspec')", outPath, err)
		}
		if !bytes.Equal(existing, formatted) {
			return fmt.Errorf("%s is out of date — run 'make packspec' and commit", outPath)
		}
		fmt.Printf("✓ %s is current; %s\n", outPath, cov.Summary(ex))
		return nil
	}

	if err := os.WriteFile(outPath, formatted, generatedFileMode); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}
	fmt.Printf("✓ wrote %s; %s\n", outPath, cov.Summary(ex))
	return nil
}
