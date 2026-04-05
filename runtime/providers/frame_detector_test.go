package providers

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// --- NDJSON ---

func TestNDJSONFrameDetector_SingleLine(t *testing.T) {
	t.Parallel()
	input := `{"response":"hello"}` + "\n"
	got, err := (NDJSONFrameDetector{}).PeekFirstFrame(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != input {
		t.Errorf("peeked %q, want %q", string(got), input)
	}
}

func TestNDJSONFrameDetector_MultipleLines(t *testing.T) {
	t.Parallel()
	// The detector should return the first complete line. The returned
	// bytes may include bufio lookahead (more lines drained into the
	// replay slice) but must begin with the first line.
	input := `{"response":"a"}` + "\n" + `{"response":"b"}` + "\n" + `{"response":"c"}` + "\n"
	got, err := (NDJSONFrameDetector{}).PeekFirstFrame(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(string(got), `{"response":"a"}`+"\n") {
		t.Fatalf("peeked %q must begin with the first line", string(got))
	}
	if !strings.Contains(input, string(got)) {
		t.Fatalf("peeked bytes must be a prefix of the input stream")
	}
}

func TestNDJSONFrameDetector_SkipsBlankLines(t *testing.T) {
	t.Parallel()
	// Some producers emit blank lines as keepalives — these are not
	// "frames" and the detector must keep reading until it finds a
	// real JSON line.
	input := "\n\n" + `{"response":"hi"}` + "\n"
	got, err := (NDJSONFrameDetector{}).PeekFirstFrame(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(got), `{"response":"hi"}`) {
		t.Errorf("peeked bytes %q must include the real content line", string(got))
	}
}

func TestNDJSONFrameDetector_EOFAfterFirstLineNoNewline(t *testing.T) {
	t.Parallel()
	// Last line without trailing newline — should still be treated as a
	// complete frame when EOF is reached.
	input := `{"response":"no newline"}`
	got, err := (NDJSONFrameDetector{}).PeekFirstFrame(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != input {
		t.Errorf("peeked %q, want %q", string(got), input)
	}
}

func TestNDJSONFrameDetector_EmptyStream(t *testing.T) {
	t.Parallel()
	_, err := (NDJSONFrameDetector{}).PeekFirstFrame(strings.NewReader(""))
	if err == nil {
		t.Fatal("empty stream should return an error")
	}
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestNDJSONFrameDetector_Name(t *testing.T) {
	t.Parallel()
	if got := (NDJSONFrameDetector{}).Name(); got != "ndjson" {
		t.Errorf("Name() = %q, want %q", got, "ndjson")
	}
}

// --- JSON array ---

func TestJSONArrayFrameDetector_SimpleObject(t *testing.T) {
	t.Parallel()
	input := `[{"candidates":[]}]`
	got, err := (JSONArrayFrameDetector{}).PeekFirstFrame(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Detector should return everything up through the closing `}` of
	// the first element. The closing `]` may or may not be in the
	// returned bytes depending on bufio lookahead.
	if !strings.HasPrefix(string(got), `[{"candidates":[]}`) {
		t.Errorf("peeked %q must include the first object", string(got))
	}
}

func TestJSONArrayFrameDetector_NestedObjects(t *testing.T) {
	t.Parallel()
	// Balanced-brace tracking must handle nested objects correctly.
	input := `[{"a":{"b":{"c":1}},"d":2},{"next":true}]`
	got, err := (JSONArrayFrameDetector{}).PeekFirstFrame(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// First element ends at the `}` matching the outermost `{` of the
	// first element, which is at position 25: `[{"a":{"b":{"c":1}},"d":2}`
	want := `[{"a":{"b":{"c":1}},"d":2}`
	if !strings.HasPrefix(string(got), want) {
		t.Errorf("peeked %q must begin with %q", string(got), want)
	}
}

func TestJSONArrayFrameDetector_StringWithEscapedQuotes(t *testing.T) {
	t.Parallel()
	// An escaped quote inside a string must NOT terminate the string —
	// the detector's string-state tracking must respect `\` escapes.
	input := `[{"text":"he said \"hi\" then {stopped}"},"next"]`
	got, err := (JSONArrayFrameDetector{}).PeekFirstFrame(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `[{"text":"he said \"hi\" then {stopped}"}`
	if !strings.HasPrefix(string(got), want) {
		t.Errorf("peeked %q must begin with %q", string(got), want)
	}
}

func TestJSONArrayFrameDetector_BracesInsideStrings(t *testing.T) {
	t.Parallel()
	// Braces inside JSON strings must not affect depth tracking.
	input := `[{"msg":"{not a brace}{also not}"},"next"]`
	got, err := (JSONArrayFrameDetector{}).PeekFirstFrame(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `[{"msg":"{not a brace}{also not}"}`
	if !strings.HasPrefix(string(got), want) {
		t.Errorf("peeked %q must begin with %q", string(got), want)
	}
}

func TestJSONArrayFrameDetector_LeadingWhitespace(t *testing.T) {
	t.Parallel()
	// Whitespace before the opening `[` and between `[` and the first
	// `{` must be tolerated — JSON producers may pretty-print or emit
	// newlines between elements.
	input := "  \n  [\n  {\"first\":1},\n  {\"second\":2}\n]"
	got, err := (JSONArrayFrameDetector{}).PeekFirstFrame(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The returned bytes must include `{"first":1}` — preserve exact
	// whitespace from the input as a prefix.
	if !strings.Contains(string(got), `{"first":1}`) {
		t.Errorf("peeked %q must include the first object", string(got))
	}
}

func TestJSONArrayFrameDetector_EmptyArray(t *testing.T) {
	t.Parallel()
	// An empty `[]` has no frames — detector should return io.EOF so
	// classifyStreamAttempt treats it as a terminal failure.
	input := `[]`
	_, err := (JSONArrayFrameDetector{}).PeekFirstFrame(strings.NewReader(input))
	if err == nil {
		t.Fatal("empty array should return an error")
	}
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestJSONArrayFrameDetector_MissingOpenBracket(t *testing.T) {
	t.Parallel()
	// A stream that doesn't start with an array is an error, not a
	// retryable transient — the caller's parser would also fail.
	input := `{"not":"an array"}`
	_, err := (JSONArrayFrameDetector{}).PeekFirstFrame(strings.NewReader(input))
	if err == nil {
		t.Fatal("missing `[` should return an error")
	}
	if !strings.Contains(err.Error(), "expected '['") {
		t.Errorf("expected '[' error, got %v", err)
	}
}

func TestJSONArrayFrameDetector_StopsAtBoundary(t *testing.T) {
	t.Parallel()
	// With a byte-at-a-time source, the detector should consume exactly
	// the bytes of the first object — no more, no less. Any over-read
	// would indicate the byte-level scanner is consuming from the
	// underlying reader past the object boundary.
	slow := &byteReader{data: []byte(`[{"x":1},{"y":2}]`)}
	got, err := (JSONArrayFrameDetector{}).PeekFirstFrame(slow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With a byte-at-a-time reader, bufio cannot pre-buffer beyond
	// our last ReadByte() call, so the returned bytes must be exactly
	// `[{"x":1}`.
	if string(got) != `[{"x":1}` {
		t.Errorf("peeked %q, want exactly `[{\"x\":1}` (no over-read)", string(got))
	}
}

func TestJSONArrayFrameDetector_Name(t *testing.T) {
	t.Parallel()
	if got := (JSONArrayFrameDetector{}).Name(); got != "json-array" {
		t.Errorf("Name() = %q, want %q", got, "json-array")
	}
}

// --- Interface compliance checks ---

func TestFrameDetector_InterfaceCompliance(t *testing.T) {
	t.Parallel()
	// Compile-time check that each concrete type implements the
	// interface. A failure here would fail the build, not the test,
	// but having them listed as assertions documents the intent.
	var _ FrameDetector = SSEFrameDetector{}
	var _ FrameDetector = NDJSONFrameDetector{}
	var _ FrameDetector = JSONArrayFrameDetector{}
}
