package base

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// declaredProviderTypes reads the ProviderType constants straight out of the
// source. Listing them by hand here would repeat the very omission this guards:
// the bug was a constant that existed and was left out of the enumeration, and
// a hand-written list can be left short in exactly the same way.
func declaredProviderTypes(t *testing.T) map[string]string {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "types.go", nil, 0)
	require.NoError(t, err, "parse types.go")

	declared := make(map[string]string)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) == 0 || len(vs.Values) == 0 {
				continue
			}
			ident, ok := vs.Type.(*ast.Ident)
			if !ok || ident.Name != "ProviderType" {
				continue
			}
			lit, ok := vs.Values[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			value, err := strconv.Unquote(lit.Value)
			require.NoError(t, err)
			declared[vs.Names[0].Name] = value
		}
	}

	require.NotEmpty(t, declared, "found no ProviderType constants; the parser needs updating")
	return declared
}

// TestAllProviderTypes_CoversEveryDeclaredConstant is the regression for #1999:
// ProviderTypeRerank was declared but missing from AllProviderTypes(), so
// ParseProviderType("rerank") failed and the metric collector never
// pre-registered rerank's families. A declared constant with no entry in the
// consumer that enumerates it is inert.
func TestAllProviderTypes_CoversEveryDeclaredConstant(t *testing.T) {
	declared := declaredProviderTypes(t)

	enumerated := make(map[string]bool, len(AllProviderTypes()))
	for _, pt := range AllProviderTypes() {
		enumerated[string(pt)] = true
	}

	var missing []string
	for name, value := range declared {
		if !enumerated[value] {
			missing = append(missing, name)
		}
	}
	assert.Empty(t, missing,
		"declared but absent from AllProviderTypes(), so they cannot be parsed and get no metric families: %s",
		strings.Join(missing, ", "))

	assert.Len(t, AllProviderTypes(), len(declared),
		"AllProviderTypes() and the declared constants have drifted apart")
}

// TestParseProviderType_RoundTripsEveryDeclaredConstant closes the loop the
// issue asked for: whatever is declared must parse back to itself.
func TestParseProviderType_RoundTripsEveryDeclaredConstant(t *testing.T) {
	for name, value := range declaredProviderTypes(t) {
		t.Run(name, func(t *testing.T) {
			parsed, err := ParseProviderType(value)
			require.NoError(t, err, "%s (%q) does not parse", name, value)
			assert.Equal(t, value, string(parsed))
		})
	}
}

func TestParseProviderType_RejectsUnknown(t *testing.T) {
	_, err := ParseProviderType("definitely-not-a-provider-type")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider type")
}
