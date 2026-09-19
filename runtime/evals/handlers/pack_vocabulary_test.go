package handlers

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// PromptKit READS the PromptPack spec. It does not extend it.
//
// Every string in this list is vocabulary a pack author has to type, so adding
// one is a decision about someone else's file format — and the spec that owns
// that format lives in another repo. The two ways it has gone wrong here are
// worth naming, because both looked like ordinary code at the time:
//
//   - the runtime AUTHORING a name. `JudgeProviderKey = "judge"` made the
//     runtime decide what a pack must call its grading provider, which is the
//     pack author's choice alone (#1996, corrected in #2027);
//   - a pack naming something HOST-side. `classifier_id` took a provider id
//     that only meant anything in one deployment, so the host could not swap
//     providers without editing the pack (removed in #2027).
//
// This test does not judge intent — it cannot. It makes the decision visible:
// a new param means editing this list, in the diff, with a reason. If you are
// adding one, say who owns the name and why the existing `provider` param does
// not already cover it.
//
// Scope, stated honestly: it sees params a handler reads out of the map it is
// given. It cannot see a key invented elsewhere in the codebase, which is what
// JudgeProviderKey was — for that, the rule in runtime/CLAUDE.md is the control,
// and this list is the example of what following it looks like.
var packFacingParams = map[string]string{
	// The one way a check names an ancillary provider. Value is a LOGICAL key
	// from the pack's own requires block.
	"provider": "names a provider the pack declares in requires; the host binds it",

	// Wrapper-level: consumed by the guardrail factory and the assertion
	// wrapper rather than by a handler.
	"direction": "which side of the provider call a guardrail gates",
	"message":   "user-facing text when a guardrail blocks",
	"min_score": "assertion threshold; handlers reject it",
	"max_score": "assertion threshold; handlers reject it",

	// Removed vocabulary, listed so the rejection path stays deliberate.
	"classifier_id": "REMOVED: named a host-side provider id inside a pack (#2027)",
}

// baselineParams is the vocabulary that already existed when this guard was
// added. It is a ratchet, not an endorsement: these names have NOT been
// reviewed against the ownership rule one by one, and listing them here says
// only that they predate the guard. Adding to this list is not the way to add
// a param — add it to packFacingParams, with a reason.
var baselineParams = []string{
	"actual", "agent", "agent_role", "agent_url", "allowed", "args", "auth_token",
	"baseline_content", "baseline_state", "body", "branch", "case_sensitive",
	"check", "command", "contains", "context", "context_field", "criteria",
	"cwd", "description", "disallowed", "embedding", "env", "eval_type",
	"examples", "excluded_args", "expect_match", "expected", "expected_args",
	"expected_content", "expected_label", "expected_output", "expected_state",
	"expression", "failure_pattern", "forbidden_args", "headers", "id",
	"ignore_case", "include_messages", "index", "jmespath_expression",
	"judge_prompt", "labels", "match_mode", "max_cost_usd", "max_latency_ms",
	"max_tokens", "media_type", "message_index", "message_role", "method",
	"metric", "mime_type", "min_calls", "min_similarity", "model", "name",
	"on_deny", "on_error", "on_unknown", "parallel", "path", "pattern",
	"patterns", "policy", "question", "recent_turns", "reference",
	"result_matches", "role", "rubric", "rules", "schema", "should_trigger",
	"skill", "small_talk", "state", "steps", "strict", "success_pattern",
	"system_prompt", "text", "threshold", "timeout_ms", "tool", "tool_name",
	"tools", "top_k", "transition", "turns", "type", "url", "validator",
	"validator_type", "value", "weight", "when",
}

// declaredParams is every name this guard accepts.
func declaredParams() map[string]bool {
	out := make(map[string]bool, len(packFacingParams)+len(baselineParams))
	for name := range packFacingParams {
		out[name] = true
	}
	for _, name := range baselineParams {
		out[name] = true
	}
	return out
}

// TestPackFacingParams_AreDeclared fails when a handler reads a param that is
// not in the list above. The fix is to add it WITH a reason — or, far more
// often, to use `provider` and the existing vocabulary instead of inventing
// something.
func TestPackFacingParams_AreDeclared(t *testing.T) {
	found, err := paramLiteralsInPackage(".")
	require.NoError(t, err)
	require.NotEmpty(t, found, "found no param reads at all; the scanner needs updating")

	declared := declaredParams()
	var undeclared []string
	for name := range found {
		if !declared[name] {
			undeclared = append(undeclared, name)
		}
	}
	sort.Strings(undeclared)

	assert.Empty(t, undeclared,
		"these params are read from pack-authored config but are not declared in "+
			"packFacingParams: %s\n\nAdding pack vocabulary is a decision about a file "+
			"format PromptKit reads and does not own. Declare it with a reason, and check "+
			"first whether `provider` already covers it — see runtime/CLAUDE.md.",
		strings.Join(undeclared, ", "))
}

// paramLiteralsInPackage collects string literals used to index a map named
// `params` — how every handler reads its configuration.
func paramLiteralsInPackage(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	fset := token.NewFileSet()
	found := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, parseErr := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if parseErr != nil {
			return nil, parseErr
		}
		{
			ast.Inspect(file, func(n ast.Node) bool {
				idx, ok := n.(*ast.IndexExpr)
				if !ok {
					return true
				}
				ident, ok := idx.X.(*ast.Ident)
				if !ok || ident.Name != "params" {
					return true
				}
				lit, ok := idx.Index.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				if value, err := strconv.Unquote(lit.Value); err == nil {
					found[value] = true
				}
				return true
			})
		}
	}
	return found, nil
}
