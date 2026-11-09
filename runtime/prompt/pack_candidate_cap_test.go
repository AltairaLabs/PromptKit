package prompt

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPackCompiler_DynamicFragmentCandidateCapBoundary pins the cap on
// variable-chosen fragment candidates at its edge: exactly
// maxDynamicFragmentCandidates candidates all compile into the pack, and one
// more leaves the pack holding only the {{name}} reference.
func TestPackCompiler_DynamicFragmentCandidateCapBoundary(t *testing.T) {
	tests := []struct {
		name      string
		values    int
		wantEvery bool
	}{
		{"at the cap", maxDynamicFragmentCandidates, true},
		{"one over the cap", maxDynamicFragmentCandidates + 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newMockRepository()
			enum := make([]interface{}, tt.values)
			want := make(map[string]string, tt.values)
			for i := range enum {
				v := fmt.Sprintf("v%d", i)
				enum[i] = v
				repo.fragments["f_"+v] = &Fragment{Content: "content " + v}
				want["f_"+v] = "content " + v
			}
			cfg := fragmentConfig("chat", FragmentRef{Name: "f_{{n}}", Required: true})
			cfg.Spec.Variables = []VariableMetadata{{Name: "n", Validation: map[string]interface{}{"enum": enum}}}
			repo.prompts["chat"] = cfg

			pack, err := compileWithRepo(t, repo, "chat")
			require.NoError(t, err)
			if tt.wantEvery {
				assert.Equal(t, want, pack.Fragments)
				return
			}
			assert.Equal(t, map[string]string{"f_{{n}}": "{{f_{{n}}}}"}, pack.Fragments)
		})
	}
}
