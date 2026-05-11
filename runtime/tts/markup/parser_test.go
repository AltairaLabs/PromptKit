package markup

import (
	"reflect"
	"testing"
)

func TestParseTags_FlagTag(t *testing.T) {
	tags := ParseTags("[whispers]Come here")
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
	got := tags[0]
	if got.Name != "whispers" || got.Value != "" {
		t.Errorf("Name=%q Value=%q", got.Name, got.Value)
	}
	if got.Start != 0 || got.End != 10 {
		t.Errorf("Start=%d End=%d", got.Start, got.End)
	}
}

func TestParseTags_ValuedTag(t *testing.T) {
	tags := ParseTags("hello [pause:500ms]world")
	if len(tags) != 1 {
		t.Fatalf("expected 1 tag, got %d", len(tags))
	}
	if tags[0].Name != "pause" || tags[0].Value != "500ms" {
		t.Errorf("got %+v", tags[0])
	}
}

func TestParseTags_CloseTag(t *testing.T) {
	tags := ParseTags("[excited]Hi[/]")
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
	if !tags[1].IsClose() {
		t.Errorf("expected second tag to be close marker; got %+v", tags[1])
	}
}

func TestParseTags_MultipleAndOrder(t *testing.T) {
	tags := ParseTags("[a]one [b:val]two [c]three")
	names := make([]string, 0, len(tags))
	for _, t := range tags {
		names = append(names, t.Name)
	}
	if !reflect.DeepEqual(names, []string{"a", "b", "c"}) {
		t.Errorf("got names %v", names)
	}
}

func TestParseTags_CaseInsensitiveAndTrimmed(t *testing.T) {
	tags := ParseTags("[ Whispers ]")
	if len(tags) != 1 || tags[0].Name != "whispers" {
		t.Errorf("got %+v", tags)
	}
}

func TestParseTags_MalformedAreSkipped(t *testing.T) {
	// Empty body
	if got := ParseTags("[]"); len(got) != 0 {
		t.Errorf("empty body should be skipped, got %+v", got)
	}
	// Unbalanced — should stop scanning, no tags.
	if got := ParseTags("[unclosed text"); len(got) != 0 {
		t.Errorf("unbalanced should be ignored, got %+v", got)
	}
}

func TestParseTags_EscapedBracketsAreNotTags(t *testing.T) {
	tags := ParseTags(`stage direction \[smile\] inline`)
	if len(tags) != 0 {
		t.Errorf("escaped brackets should not produce tags; got %+v", tags)
	}
}

func TestStripTags_NoTags(t *testing.T) {
	if got := StripTags("plain text"); got != "plain text" {
		t.Errorf("got %q", got)
	}
}

func TestStripTags_RemovesAll(t *testing.T) {
	got := StripTags("[whispers]Come here[/]Did you hear that?")
	want := "Come hereDid you hear that?"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStripTags_UnescapesLiteralBrackets(t *testing.T) {
	got := StripTags(`stage direction \[smile\] inline`)
	want := `stage direction [smile] inline`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestExtractInstructions_FlagTag(t *testing.T) {
	ins, stripped := ExtractInstructions("[whispers]Come here")
	if ins != "whisper" {
		t.Errorf("instructions = %q", ins)
	}
	if stripped != "Come here" {
		t.Errorf("stripped = %q", stripped)
	}
}

func TestExtractInstructions_MultipleTags(t *testing.T) {
	ins, stripped := ExtractInstructions("[excited][pause:500ms]Surprise!")
	want := "excited; pause for 500ms"
	if ins != want {
		t.Errorf("instructions = %q, want %q", ins, want)
	}
	if stripped != "Surprise!" {
		t.Errorf("stripped = %q", stripped)
	}
}

func TestExtractInstructions_DedupesRepeats(t *testing.T) {
	ins, _ := ExtractInstructions("[excited][excited]Hello")
	if ins != "excited" {
		t.Errorf("repeats should de-dupe; got %q", ins)
	}
}

func TestExtractInstructions_CloseTagsIgnored(t *testing.T) {
	ins, _ := ExtractInstructions("[whispers]hi[/]bye")
	if ins != "whisper" {
		t.Errorf("close marker should be filtered; got %q", ins)
	}
}

func TestExtractInstructions_UnknownTagPassesThrough(t *testing.T) {
	ins, _ := ExtractInstructions("[mysterious]Hello")
	if ins != "mysterious" {
		t.Errorf("unknown tag should appear verbatim; got %q", ins)
	}
}

func TestExtractInstructions_NoTags(t *testing.T) {
	ins, stripped := ExtractInstructions("just plain text")
	if ins != "" {
		t.Errorf("expected empty instructions; got %q", ins)
	}
	if stripped != "just plain text" {
		t.Errorf("stripped = %q", stripped)
	}
}

func TestToSSML_NoTags(t *testing.T) {
	if got := ToSSML("hello"); got != "hello" {
		t.Errorf("got %q", got)
	}
}

func TestToSSML_WrapsWithProsody(t *testing.T) {
	got := ToSSML("[whispers]Come here[/]Plain")
	// The whisper span runs from the tag to "[/]"; "Plain" follows un-wrapped.
	want := `<prosody volume="x-soft" rate="slow">Come here</prosody>Plain`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestToSSML_PauseEmitsBreak(t *testing.T) {
	got := ToSSML("hello [pause:500ms]world")
	want := `hello <break time="500ms"/>world`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestToSSML_PauseDefaultDuration(t *testing.T) {
	got := ToSSML("[pause]hello")
	want := `<break time="500ms"/>hello`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestToSSML_UnknownTagDropsWrapper(t *testing.T) {
	got := ToSSML("[mysterious]hello")
	if got != "hello" {
		t.Errorf("got %q (expected 'hello' — unknown tag should drop wrapper)", got)
	}
}

func TestToSSML_EscapesUnsafeChars(t *testing.T) {
	got := ToSSML(`a & b < c > "d"`)
	want := `a &amp; b &lt; c &gt; &quot;d&quot;`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestToSSML_ConsecutiveTagsImplicitlyClosePrior(t *testing.T) {
	// New tag implicitly closes the previous span.
	got := ToSSML("[whispers]soft[shouts]loud")
	want := `<prosody volume="x-soft" rate="slow">soft</prosody>` +
		`<prosody volume="x-loud">loud</prosody>`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
