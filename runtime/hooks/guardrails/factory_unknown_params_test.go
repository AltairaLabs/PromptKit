package guardrails

import (
	"errors"
	"strings"
	"testing"

	"github.com/AltairaLabs/PromptKit/runtime/v2/prompt"
)

// A pack validator whose params carry a key the handler does not read fails to
// load, naming the key and the valid ones. It used to load and run with the key
// ignored: `wrods:` on banned_words built a guardrail that blocked nothing.
func TestCompileValidators_RejectsUnknownParams(t *testing.T) {
	_, err := CompileValidators([]prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]any{"wrods": []any{"secret"}}},
	})
	if err == nil {
		t.Fatal("expected an unknown-param error")
	}
	if !errors.Is(err, ErrInvalidGuardrailParams) {
		t.Errorf("error %v does not wrap ErrInvalidGuardrailParams", err)
	}
	for _, want := range []string{
		`unknown param "wrods" for banned_words (content_excludes)`,
		"valid params: case_sensitive, match_mode, patterns",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

// direction and message are the guardrail's own params, and the thresholds are
// the wrapper's; none is reported as unknown to the handler.
func TestCompileValidators_AcceptsGuardrailParams(t *testing.T) {
	hooks, err := CompileValidators([]prompt.ValidatorConfig{
		{Type: "banned_words", Params: map[string]any{
			"words": []any{"secret"}, "direction": "input", "message": "no", "min_score": 1.0,
		}},
		{Type: "guardrail", Params: map[string]any{
			"eval_type": "contains", "eval_params": map[string]any{"patterns": []any{"x"}},
			"direction": "output", "message": "no", "action": "block", "max_score": 1.0,
		}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hooks) != 2 {
		t.Fatalf("got %d hooks, want 2", len(hooks))
	}
}
