package tools

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
)

// The model is given a JSON tool result as text, with every value exact.
func TestFormatResultText(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"string is unquoted", `"shipped"`, "shipped"},
		{"object is indented, keys sorted", `{"b":1,"a":"x"}`, "{\n  \"a\": \"x\",\n  \"b\": 1\n}"},
		{"array is indented", `["a","b"]`, "[\n  \"a\",\n  \"b\"\n]"},
		{"small integer", `42`, "42"},
		{"round integer keeps its digits", `10000000`, "10000000"},
		{"integer beyond float precision stays exact", `9007199254740993`, "9007199254740993"},
		{"large integer inside an object stays exact",
			`{"id":9007199254740993}`, "{\n  \"id\": 9007199254740993\n}"},
		{"fraction", `1.5`, "1.5"},
		{"boolean", `true`, "true"},
		{"empty string renders as two quotes", `""`, `""`},
		{"empty raw JSON renders as two quotes", ``, `""`},
		{"null renders as null", `null`, "null"},
		{"null inside an object", `{"a":null}`, "{\n  \"a\": null\n}"},
		{"text that is not JSON passes through", `plain text`, "plain text"},
		{"JSON followed by text passes through", `{"a":1} trailing`, `{"a":1} trailing`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FormatResultText(json.RawMessage(tt.raw)))
		})
	}
}

// A result over the limit is cut at it, never inside a character, and says so;
// one within it, or with no limit, is untouched.
func TestTruncateResultText(t *testing.T) {
	assert.Equal(t, "abcdef", TruncateResultText("t", "abcdef", 0))
	assert.Equal(t, "abcdef", TruncateResultText("t", "abcdef", 6))
	assert.Equal(t, "abc\n... [truncated, 6 bytes exceeded limit of 3 bytes]", TruncateResultText("t", "abcdef", 3))
	// Each CJK character and the emoji is more than one byte.
	assert.Equal(t, "日\n... [truncated, 9 bytes exceeded limit of 4 bytes]", TruncateResultText("t", "日本語", 4))
	assert.Equal(t, "日本\n... [truncated, 9 bytes exceeded limit of 7 bytes]", TruncateResultText("t", "日本語", 7))
	assert.Equal(t, "a\n... [truncated, 5 bytes exceeded limit of 3 bytes]", TruncateResultText("t", "a😀", 3))
	// Bytes that are not UTF-8 keep their prefix: only a character's width is backed over.
	junk := "ab\x80\x80\x80\x80\x80\x80"
	assert.Equal(t, "ab\x80\x80\x80\n... [truncated, 8 bytes exceeded limit of 5 bytes]", TruncateResultText("t", junk, 5))
}

func TestBuildModelResult(t *testing.T) {
	image := types.NewImagePartFromData("aGk=", "image/png", nil)

	t.Run("raw JSON is formatted then capped", func(t *testing.T) {
		got, err := BuildModelResult(json.RawMessage(`"abcdefghij"`), nil, ModelResultOptions{MaxSize: 4})
		assert.NoError(t, err)
		assert.Equal(t, ModelResult{Text: "abcd\n... [truncated, 10 bytes exceeded limit of 4 bytes]"}, got)
	})
	t.Run("no result and empty text read as two quotes", func(t *testing.T) {
		got, _ := BuildModelResult(nil, nil, ModelResultOptions{})
		assert.Equal(t, `""`, got.Text)
		got, _ = BuildModelResult(json.RawMessage(" "), nil, ModelResultOptions{})
		assert.Equal(t, `""`, got.Text)
		got, _ = BuildModelResult(nil, []types.ContentPart{types.NewTextPart(""), image}, ModelResultOptions{})
		assert.Equal(t, `""`, got.Text)
		assert.Equal(t, `""`, *got.Parts[0].Text)
	})
	t.Run("parts keep media and cap each text part", func(t *testing.T) {
		got, err := BuildModelResult(nil, []types.ContentPart{types.NewTextPart("abcdef"), image},
			ModelResultOptions{MaxSize: 3})
		assert.NoError(t, err)
		assert.Equal(t, "abc\n... [truncated, 6 bytes exceeded limit of 3 bytes]", got.Text)
		assert.Equal(t, got.Text, *got.Parts[0].Text)
		assert.Equal(t, image, got.Parts[1])
	})
	t.Run("a text part with no text is kept as it is", func(t *testing.T) {
		empty := types.ContentPart{Type: types.ContentTypeText}
		got, err := BuildModelResult(nil, []types.ContentPart{empty}, ModelResultOptions{MaxSize: 3})
		assert.NoError(t, err)
		assert.Equal(t, []types.ContentPart{empty}, got.Parts)
	})
	t.Run("a rewrite applies before the cap, to text and to text parts", func(t *testing.T) {
		upper := func(s string) (string, error) { return s + "!", nil }
		got, _ := BuildModelResult(json.RawMessage(`"abc"`), nil, ModelResultOptions{Rewrite: upper})
		assert.Equal(t, "abc!", got.Text)
		got, _ = BuildModelResult(nil, []types.ContentPart{types.NewTextPart("x"), image}, ModelResultOptions{Rewrite: upper})
		assert.Equal(t, "x!", got.Text)
	})
	t.Run("a rewrite error is returned", func(t *testing.T) {
		boom := errors.New("boom")
		_, err := BuildModelResult(json.RawMessage(`"a"`), nil,
			ModelResultOptions{Rewrite: func(string) (string, error) { return "", boom }})
		assert.ErrorIs(t, err, boom)
	})
}
