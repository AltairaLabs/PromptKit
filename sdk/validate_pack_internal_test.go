package sdk

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

func TestCleanGuardrailError(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "strips standard guardrail prefix",
			in:   `guardrail "max_length": max_length requires positive int`,
			want: "max_length requires positive int",
		},
		{
			name: "no prefix is returned unchanged",
			in:   `unknown guardrail type: "foo"`,
			want: `unknown guardrail type: "foo"`,
		},
		{
			name: "prefix present but without closing `\": ` is returned unchanged",
			in:   `guardrail mid-sentence text`,
			want: `guardrail mid-sentence text`,
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, cleanGuardrailError(tc.in))
		})
	}
}

func TestFindEvalTypeByID(t *testing.T) {
	defs := []evals.EvalDef{
		{ID: "one", Type: "max_length"},
		{ID: "two", Type: "contains"},
	}
	assert.Equal(t, "max_length", findEvalTypeByID(defs, "one"))
	assert.Equal(t, "contains", findEvalTypeByID(defs, "two"))
	assert.Equal(t, "", findEvalTypeByID(defs, "missing"), "missing id returns empty type")
	assert.Equal(t, "", findEvalTypeByID(nil, "anything"), "nil slice returns empty type")
}
