package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/want.go.golden")

const fixtureImport = "example.com/stage/internal/core"

func runFixture(t *testing.T, pkg string) (string, string, error) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "core_aliases.go")
	var stdout bytes.Buffer
	err := run([]string{"-base", "testdata/base", "-pkg", pkg, "-import", fixtureImport, "-out", out}, &stdout)
	if err != nil {
		return "", stdout.String(), err
	}
	got, readErr := os.ReadFile(out) //nolint:gosec // the test's own temp file
	if readErr != nil {
		t.Fatal(readErr)
	}
	return string(got), stdout.String(), nil
}

// The fixture pins each shape: an alias per type, typed consts keeping their
// type (also through iota), an untyped const, a lone typed const, wrappers for
// plain, variadic, blank and unnamed parameters and for single and multiple
// results, the imports their signatures need (a /v2 path by its package name),
// single and grouped vars, each with its doc; and nothing stage did not export
// before (Helper, ErrInternal, kindHidden) nor any method.
func TestGenerate_MatchesGolden(t *testing.T) {
	got, stdout, err := runFixture(t, "testdata/core")
	if err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "want.go.golden")
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Fatalf("generated file differs from %s (rerun with -update to accept):\n%s", golden, got)
	}
	if !strings.HasSuffix(stdout, ": 13 names re-exported from "+fixtureImport+"\n") {
		t.Fatalf("report = %q, want 13 names", stdout)
	}
}

func TestGenerate_RefusesWhatItCannotWrite(t *testing.T) {
	tests := []struct {
		pkg, want string
	}{
		{"testdata/generic", "Kind: generic type; write its alias by hand"},
		{"testdata/genericfunc", "Kind: generic function; write its wrapper by hand"},
		{"testdata/noimport", "types.Message: no import named types"},
		{"testdata/missing", "testdata/missing: no Go files"},
	}
	for _, tt := range tests {
		t.Run(tt.pkg, func(t *testing.T) {
			_, _, err := runFixture(t, tt.pkg)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRun_RequiresEveryFlag(t *testing.T) {
	err := run([]string{"-base", "testdata/base"}, &bytes.Buffer{})
	if err == nil || err.Error() != "-base, -pkg, -import and -out are all required" {
		t.Fatalf("err = %v", err)
	}
	if err := run([]string{"-nope"}, &bytes.Buffer{}); err == nil {
		t.Fatal("an unknown flag was accepted")
	}
}

func TestRun_ReportsAnUnreadableBase(t *testing.T) {
	err := run([]string{"-base", "testdata/missing", "-pkg", "testdata/core", "-import", fixtureImport,
		"-out", filepath.Join(t.TempDir(), "x.go")}, &bytes.Buffer{})
	if err == nil || err.Error() != "testdata/missing: no Go files" {
		t.Fatalf("err = %v", err)
	}
}
