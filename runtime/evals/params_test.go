package evals

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// namedParamsHandler declares the params it reads.
type namedParamsHandler struct {
	typeName string
	params   []string
	validate error
}

func (h *namedParamsHandler) Type() string { return h.typeName }
func (h *namedParamsHandler) Eval(context.Context, *EvalContext, map[string]any) (*EvalResult, error) {
	score := 1.0
	return &EvalResult{Type: h.typeName, Score: &score}, nil
}
func (h *namedParamsHandler) ParamNames() []string { return h.params }
func (h *namedParamsHandler) ValidateParams(map[string]any) error {
	return h.validate
}

// undeclaredParamsHandler reads params it does not declare.
type undeclaredParamsHandler struct{}

func (undeclaredParamsHandler) Type() string { return "undeclared" }
func (undeclaredParamsHandler) Eval(context.Context, *EvalContext, map[string]any) (*EvalResult, error) {
	return &EvalResult{}, nil
}

func TestValidateHandlerParams_RejectsUnknownKeys(t *testing.T) {
	h := &namedParamsHandler{typeName: "judge", params: []string{"rubric", "criteria"}}

	err := ValidateHandlerParams("judge", h, map[string]any{"critera": "x", "rubric": "y", "zeta": 1})
	if err == nil {
		t.Fatal("expected an unknown-param error")
	}
	want := `unknown params "critera", "zeta" for judge; valid params: criteria, rubric`
	if err.Error() != want {
		t.Fatalf("error = %q\nwant  %q", err, want)
	}
}

func TestValidateHandlerParams_AcceptsKnownThresholdAndAllowedKeys(t *testing.T) {
	h := &namedParamsHandler{typeName: "judge", params: []string{"criteria"}}
	params := map[string]any{"criteria": "x", "min_score": 0.5, "max_score": 1.0, "direction": "input"}
	if err := ValidateHandlerParams("judge", h, params, "direction"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := ValidateHandlerParams("judge", h, params); err == nil {
		t.Fatal("direction is not the handler's and was not allowed, so it must be rejected")
	}
}

func TestValidateHandlerParams_NamesTheAliasAndCanonicalType(t *testing.T) {
	h := &namedParamsHandler{typeName: "content_excludes", params: []string{"patterns"}}
	err := ValidateHandlerParams("banned_words", h, map[string]any{"wrods": []any{"x"}})
	if err == nil || !strings.Contains(err.Error(), "for banned_words (content_excludes)") {
		t.Fatalf("error = %v, want it to name the alias and the canonical type", err)
	}
}

func TestValidateHandlerParams_TakesNoParams(t *testing.T) {
	h := &namedParamsHandler{typeName: "done"}
	err := ValidateHandlerParams("done", h, map[string]any{"state": "x"})
	if err == nil || !strings.Contains(err.Error(), `unknown param "state" for done, which takes no params`) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateHandlerParams_HandlerValidatorRunsFirst(t *testing.T) {
	own := errors.New("own message")
	h := &namedParamsHandler{typeName: "x", validate: own}
	if err := ValidateHandlerParams("x", h, map[string]any{"unknown": 1}); !errors.Is(err, own) {
		t.Fatalf("error = %v, want the handler's own", err)
	}
}

func TestValidateHandlerParams_UndeclaredHandlerIsNotChecked(t *testing.T) {
	if err := ValidateHandlerParams("undeclared", undeclaredParamsHandler{}, map[string]any{"anything": 1}); err != nil {
		t.Fatalf("a handler without ParamNames must not be checked: %v", err)
	}
}

func TestRegistryParamNames(t *testing.T) {
	r := NewEmptyEvalTypeRegistry()
	r.Register(&namedParamsHandler{typeName: "judge", params: []string{"rubric", "criteria"}})
	r.Register(undeclaredParamsHandler{})
	if err := r.RegisterAlias("judge_alias", "judge"); err != nil {
		t.Fatal(err)
	}

	for _, typ := range []string{"judge", "judge_alias"} {
		got, ok := r.ParamNames(typ)
		if !ok || !reflect.DeepEqual(got, []string{"criteria", "rubric"}) {
			t.Errorf("ParamNames(%q) = %v, %v; want sorted [criteria rubric], true", typ, got, ok)
		}
	}
	// The returned slice is a copy.
	got, _ := r.ParamNames("judge")
	got[0] = "mutated"
	if again, _ := r.ParamNames("judge"); again[0] != "criteria" {
		t.Errorf("ParamNames returned the handler's own slice")
	}
	if got, ok := r.ParamNames("undeclared"); ok || got != nil {
		t.Errorf("undeclared handler: got %v, %v; want nil, false", got, ok)
	}
	if _, ok := r.ParamNames("missing"); ok {
		t.Error("unregistered type reported params")
	}
}

func TestValidateEvalTypes_RejectsUnknownParams(t *testing.T) {
	r := NewEvalTypeRegistry()
	r.Register(&namedParamsHandler{typeName: "judge", params: []string{"criteria"}})

	errs := ValidateEvalTypes([]EvalDef{
		{ID: "ok", Type: "judge", Params: map[string]any{"criteria": "x"}},
		{ID: "typo", Type: "judge", Params: map[string]any{"critera": "x"}},
		{ID: "wrapped-typo", Type: WrapperTypeAssertion, Params: map[string]any{
			"eval_type": "judge", "eval_params": map[string]any{"critera": "x"}, "min_score": 0.5,
		}},
		{ID: "wrapper-typo", Type: WrapperTypeGuardrail, Params: map[string]any{
			"eval_type": "judge", "eval_params": map[string]any{"criteria": "x"}, "acton": "block",
		}},
	}, r)
	if len(errs) != 3 {
		t.Fatalf("got %d errors, want 3: %v", len(errs), errs)
	}
	for i, want := range []string{
		`eval "typo": unknown param "critera" for judge`,
		`eval "wrapped-typo": assertion inner eval "judge": unknown param "critera" for judge`,
		`eval "wrapper-typo": unknown param "acton" for guardrail; valid params: action, eval_params, eval_type`,
	} {
		if !strings.Contains(errs[i], want) {
			t.Errorf("errs[%d] = %q, want it to contain %q", i, errs[i], want)
		}
	}
}

// Eval does not run the unknown-param check: a host that adds its own keys to a
// wrapper's inner params at run time keeps working. The check is a load-time one.
func TestAssertionWrapper_EvalDoesNotCheckUnknownParams(t *testing.T) {
	r := NewEvalTypeRegistry()
	r.Register(&namedParamsHandler{typeName: "judge", params: []string{"criteria"}})
	h, _ := r.Get(WrapperTypeAssertion)
	result, err := h.Eval(context.Background(), &EvalContext{}, map[string]any{
		"eval_type": "judge", "eval_params": map[string]any{"host_key": "x"},
	})
	if err != nil || result.Passed == nil || !*result.Passed {
		t.Fatalf("Eval = %+v, %v; want a passing result", result, err)
	}
}
