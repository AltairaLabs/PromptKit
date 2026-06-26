package views

import (
	"strings"
	"testing"
)

// TestRenderWithChrome_CapsOversizedBody verifies an over-tall / over-wide body
// is clipped to its allotment so it can't push the header off-screen or wrap.
func TestRenderWithChrome_CapsOversizedBody(t *testing.T) {
	const w, h = 40, 20
	out := RenderWithChrome(ChromeConfig{Width: w, Height: h, Title: "T"}, func(int) string {
		// A body far taller and wider than the area.
		var b strings.Builder
		for i := 0; i < 200; i++ {
			b.WriteString(strings.Repeat("x", 200) + "\n")
		}
		return b.String()
	})

	lines := strings.Split(out, "\n")
	if len(lines) > h {
		t.Fatalf("output has %d lines, exceeds terminal height %d (header pushed off)", len(lines), h)
	}
	for i, ln := range lines {
		if n := len([]rune(ln)); n > w {
			t.Fatalf("line %d width %d exceeds terminal width %d", i, n, w)
		}
	}
}

// TestRenderWithChrome_EmptyUntilSized verifies nothing renders before a size
// is known (avoids the first-frame placeholder snap).
func TestRenderWithChrome_EmptyUntilSized(t *testing.T) {
	if got := RenderWithChrome(ChromeConfig{}, func(int) string { return "body" }); got != "" {
		t.Fatalf("expected empty output when unsized, got %q", got)
	}
}
