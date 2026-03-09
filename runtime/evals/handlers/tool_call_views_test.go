package handlers

import (
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestStringifyResult_String(t *testing.T) {
	got := stringifyResult("hello world")
	if got != "hello world" {
		t.Errorf("got %q, want %q", got, "hello world")
	}
}

func TestStringifyResult_ContentPartsText(t *testing.T) {
	text := "some text"
	parts := []types.ContentPart{
		{Type: "text", Text: &text},
	}
	got := stringifyResult(parts)
	if got != "some text" {
		t.Errorf("got %q, want %q", got, "some text")
	}
}

func TestStringifyResult_ContentPartsMedia(t *testing.T) {
	parts := []types.ContentPart{
		{
			Type: "image",
			Media: &types.MediaContent{
				MIMEType: "image/png",
			},
		},
	}
	got := stringifyResult(parts)
	want := "[image:image/png]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStringifyResult_ContentPartsMixed(t *testing.T) {
	text := "a caption"
	parts := []types.ContentPart{
		{Type: "text", Text: &text},
		{
			Type: "image",
			Media: &types.MediaContent{
				MIMEType: "image/jpeg",
			},
		},
	}
	got := stringifyResult(parts)
	want := "a caption[image:image/jpeg]"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStringifyResult_Map(t *testing.T) {
	got := stringifyResult(map[string]any{"key": "value"})
	if got != `{"key":"value"}` {
		t.Errorf("got %q", got)
	}
}

func TestStringifyResult_Number(t *testing.T) {
	got := stringifyResult(float64(42))
	if got != "42" {
		t.Errorf("got %q, want %q", got, "42")
	}
}
