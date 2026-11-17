package tools

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

func TestErrorResult(t *testing.T) {
	got := ErrorResult("c1", "lookup", errors.New("backend down"))
	want := types.NewTextToolResult("c1", "lookup", "Error: backend down")
	want.Error = "backend down"
	want.ErrorType = types.ToolErrorExecution
	assert.Equal(t, want, got)
}

func TestClassifyError(t *testing.T) {
	assert.Equal(t, types.ToolErrorExecution, ClassifyError(errors.New("boom")))
	wrapped := fmt.Errorf("calling: %w", &ValidationError{Type: "args_invalid", Tool: "lookup"})
	assert.Equal(t, types.ToolErrorValidation, ClassifyError(wrapped))
}

// Rewriting and capping a multimodal result's text part keeps the part's
// other fields.
func TestBuildModelResult_KeepsATextPartsOtherFields(t *testing.T) {
	text := "abcdef"
	media := &types.MediaContent{MIMEType: "text/plain"}
	got, err := BuildModelResult(nil, []types.ContentPart{{Type: types.ContentTypeText, Text: &text, Media: media}},
		ModelResultOptions{Rewrite: func(s string) (string, error) { return "<" + s + ">", nil }})
	if err != nil {
		t.Fatal(err)
	}
	want := "<abcdef>"
	assert.Equal(t, []types.ContentPart{{Type: types.ContentTypeText, Text: &want, Media: media}}, got.Parts)
	assert.Equal(t, "abcdef", text, "the caller's part is not changed")
}

// Unformatted takes a text result's content as it is: rewritten, but neither
// formatted nor, without a size limit, cut; an emptied text reads `""`.
func TestBuildModelResult_Unformatted(t *testing.T) {
	raw := []byte(`{"z": 1, "a": 2}`)
	got, err := BuildModelResult(raw, nil, ModelResultOptions{Unformatted: true})
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, `{"z": 1, "a": 2}`, got.Text)

	got, err = BuildModelResult(raw, nil, ModelResultOptions{
		Unformatted: true, Rewrite: func(string) (string, error) { return "", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	assert.Equal(t, `""`, got.Text)
}
