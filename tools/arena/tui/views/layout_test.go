package views

import (
	"strings"
	"testing"
)

// TestRenderWithChrome_FillsExactArea verifies the output is exactly the
// terminal height and within its width for both an over-tall body (must not
// push the header off the top) and a short body (footer must stay pinned to the
// bottom, not float up).
func TestRenderWithChrome_FillsExactArea(t *testing.T) {
	const w, h = 40, 20
	cases := map[string]func(int) string{
		"tall body": func(int) string {
			return strings.Repeat(strings.Repeat("x", 200)+"\n", 200)
		},
		"short body": func(int) string { return "one line" },
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			out := RenderWithChrome(ChromeConfig{Width: w, Height: h, Title: "T"}, body)
			lines := strings.Split(out, "\n")
			if len(lines) != h {
				t.Fatalf("output has %d lines, want exactly %d (header/footer must stay pinned)", len(lines), h)
			}
			for i, ln := range lines {
				if n := len([]rune(ln)); n > w {
					t.Fatalf("line %d width %d exceeds terminal width %d", i, n, w)
				}
			}
		})
	}
}

// TestRenderWithChrome_EmptyUntilSized verifies nothing renders before a size
// is known (avoids the first-frame placeholder snap).
func TestRenderWithChrome_EmptyUntilSized(t *testing.T) {
	if got := RenderWithChrome(ChromeConfig{}, func(int) string { return "body" }); got != "" {
		t.Fatalf("expected empty output when unsized, got %q", got)
	}
}
