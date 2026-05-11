package memory

import (
	"fmt"
	"strings"
)

// ContextFormatter renders a slice of retrieved memories into the string
// that gets written onto TurnState.Variables["memory_context"] by the
// retrieval pipeline stage. Hosts implement this to control how
// categories, dedup keys, confidence, or other metadata are surfaced to
// the LLM. See [DefaultContextFormatter] for the built-in behavior.
type ContextFormatter func(memories []*Memory) string

// DefaultContextFormatter is the formatter the retrieval stage uses when
// no override is supplied. It renders each memory on its own line as
// "[type] content (confidence: N.N)" — matching the historical
// hardcoded behavior. Callers should treat the output as opaque text
// suitable for direct injection into a system prompt.
func DefaultContextFormatter(memories []*Memory) string {
	var b strings.Builder
	for _, m := range memories {
		fmt.Fprintf(&b, "[%s] %s (confidence: %.1f)\n", m.Type, m.Content, m.Confidence)
	}
	return b.String()
}
