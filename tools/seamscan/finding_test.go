package main

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmitText_WritesFileLineKindSubject(t *testing.T) {
	var buf bytes.Buffer
	EmitText(&buf, []Finding{{Kind: "weak-assertion", File: "x_test.go", Line: 12, Subject: "TestFoo"}})
	assert.Equal(t, "x_test.go:12\tweak-assertion\tTestFoo\n", buf.String())
}

func TestEmitText_IncludesDetailWhenPresent(t *testing.T) {
	var buf bytes.Buffer
	EmitText(&buf, []Finding{{Kind: "no-assertion", File: "y_test.go", Line: 3, Subject: "TestBar", Detail: "no calls found"}})
	assert.Equal(t, "y_test.go:3\tno-assertion\tTestBar\tno calls found\n", buf.String())
}

func TestEmitText_MultipleFindingsOnePerLine(t *testing.T) {
	var buf bytes.Buffer
	EmitText(&buf, []Finding{
		{Kind: "no-assertion", File: "a_test.go", Line: 1, Subject: "TestA"},
		{Kind: "weak-assertion", File: "b_test.go", Line: 2, Subject: "TestB"},
	})
	assert.Equal(t, "a_test.go:1\tno-assertion\tTestA\nb_test.go:2\tweak-assertion\tTestB\n", buf.String())
}

func TestEmitJSON_EmptySliceIsJSONArrayNotNull(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, EmitJSON(&buf, nil))
	assert.Equal(t, "[]\n", buf.String())
}

func TestEmitJSON_EncodesFindingFields(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, EmitJSON(&buf, []Finding{{Kind: "weak-assertion", File: "x_test.go", Line: 1, Subject: "TestFoo"}}))

	var got []Finding
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, Finding{Kind: "weak-assertion", File: "x_test.go", Line: 1, Subject: "TestFoo"}, got[0])
}
