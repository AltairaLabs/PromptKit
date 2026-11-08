package integration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AltairaLabs/PromptKit/runtime/v2/evals"
	"github.com/AltairaLabs/PromptKit/runtime/v2/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/v2/events"
	"github.com/AltairaLabs/PromptKit/runtime/v2/hooks/guardrails"
	"github.com/AltairaLabs/PromptKit/runtime/v2/types"
	sdk "github.com/AltairaLabs/PromptKit/sdk/v2"
)

// twoSentences is a reply sentence_count counts as 2. With max 1 the check
// scores max/count = 0.5, exactly on a 0.5 line.
const twoSentences = "One. Two."

// thresholdEvals declares the spec's {operator, value} threshold on assertion
// and guardrail wrappers whose inner score sits exactly on the line.
const thresholdEvals = `
	{"id": "gt-line", "type": "assertion", "trigger": "every_turn",
	 "params": {"eval_type": "sentence_count", "eval_params": {"max": 1}},
	 "threshold": {"operator": "gt", "value": 0.5}},
	{"id": "gte-line", "type": "assertion", "trigger": "every_turn",
	 "params": {"eval_type": "sentence_count", "eval_params": {"max": 1}},
	 "threshold": {"operator": "gte", "value": 0.5}},
	{"id": "lt-line", "type": "assertion", "trigger": "every_turn",
	 "params": {"eval_type": "sentence_count", "eval_params": {"max": 1}},
	 "threshold": {"operator": "lt", "value": 0.5}},
	{"id": "lte-line", "type": "assertion", "trigger": "every_turn",
	 "params": {"eval_type": "sentence_count", "eval_params": {"max": 1}},
	 "threshold": {"operator": "lte", "value": 0.5}},
	{"id": "eq-line", "type": "assertion", "trigger": "every_turn",
	 "params": {"eval_type": "sentence_count", "eval_params": {"max": 1}},
	 "threshold": {"operator": "eq", "value": 0.5}},
	{"id": "guard-gt-line", "type": "guardrail", "trigger": "every_turn",
	 "params": {"eval_type": "sentence_count", "eval_params": {"max": 1}},
	 "threshold": {"operator": "gt", "value": 0.5}},
	{"id": "plain-carries", "type": "sentence_count", "trigger": "every_turn",
	 "params": {"max": 1}, "threshold": {"operator": "gt", "value": 0.5}},
	{"id": "declared-twice", "type": "assertion", "trigger": "every_turn",
	 "params": {"eval_type": "sentence_count", "eval_params": {"max": 1}, "min_score": 0.1},
	 "threshold": {"operator": "gt", "value": 0.5}},
	{"id": "bad-operator", "type": "assertion", "trigger": "every_turn",
	 "params": {"eval_type": "sentence_count", "eval_params": {"max": 1}},
	 "threshold": {"operator": "approx", "value": 0.5}}`

// EvalDef.Threshold reaches the wrappers from a pack, gt and lt are strict at
// the line, a plain eval carries the threshold without a verdict, and a
// threshold the wrapper cannot apply keeps the eval from running at all.
func TestE2E_Threshold_AppliedByWrappersStrictAtTheLine(t *testing.T) {
	conv, log := openE2E(t, e2ePack("", thresholdEvals, ""), twoSentences)
	sendAndClose(t, conv, "count")

	want := map[string]string{
		"gt-line": "fail", "gte-line": "pass", "lt-line": "fail", "lte-line": "pass", "eq-line": "pass",
		"guard-gt-line": "fail", "plain-carries": "none",
	}
	for id, verdict := range want {
		r := log.only(t, id)
		assert.InDeltaf(t, 0.5, score(t, r), 1e-9, "%s: sentence_count max 1 over 2 sentences", id)
		assert.Equalf(t, verdict, passed(r), "%s verdict", id)
	}
	assert.Empty(t, log.byID("declared-twice"), "threshold with min_score must be rejected at load, not run")
	assert.Empty(t, log.byID("bad-operator"), "an unknown operator must be rejected at load, not run")
}

// structurallyBadEvals holds one def per evals.ValidateEvals finding, next to
// a good one. The prompt overrides "ok" by id, which is not a duplicate.
const structurallyBadEvals = `
	{"id": "ok", "type": "min_length", "trigger": "every_turn", "params": {"min": 1000}},
	{"id": "dup", "type": "min_length", "trigger": "every_turn", "params": {"min": 1}},
	{"id": "dup", "type": "min_length", "trigger": "every_turn", "params": {"min": 2}},
	{"id": "bad-metric", "type": "min_length", "trigger": "every_turn", "params": {"min": 1},
	 "metric": {"name": "my-score", "type": "gauge"}},
	{"id": "moon", "type": "min_length", "trigger": "on_full_moon", "params": {"min": 1}},
	{"id": "oversampled", "type": "min_length", "trigger": "sample_turns", "sample_percentage": 150,
	 "params": {"min": 1}}`

const promptOverridesOK = `,
			"evals": [{"id": "ok", "type": "min_length", "trigger": "every_turn", "params": {"min": 1}}]`

// Open skips every def evals.ValidateEvals rejects, runs the first of two
// duplicates, lets a prompt eval override a pack eval by id, and
// sdk.ValidatePack reports the same defs. The schema itself rejects the metric
// name and the sample percentage, so the pack is opened the way a host that
// skips schema validation opens it.
func TestE2E_ValidateEvals_OpenSkipsWhatValidationReports(t *testing.T) {
	packJSON := e2ePack(promptOverridesOK, structurallyBadEvals, "")
	conv, log := openE2E(t, packJSON, "hello", sdk.WithSkipSchemaValidation())
	sendAndClose(t, conv, "hi")

	assert.Equal(t, map[string]int{"ok": 1, "dup": 1}, log.ids(), "only the valid defs run, each once")
	assert.InDelta(t, 1.0, score(t, log.only(t, "ok")), 1e-9, "the prompt's override (min 1) runs, not the pack's min 1000")

	issues, err := sdk.ValidatePack(writePackFile(t, packJSON), true)
	require.NoError(t, err)
	bySeverity := map[string][]string{}
	for _, is := range issues {
		require.Equal(t, "eval", is.Kind, is.String())
		bySeverity[is.Severity] = append(bySeverity[is.Severity], is.ID)
	}
	assert.ElementsMatch(t, []string{"dup", "bad-metric", "oversampled"}, bySeverity["error"])
	assert.ElementsMatch(t, []string{"moon"}, bySeverity["warning"])
}

// sdk.Evaluate is the out-of-band route a host uses for session evals (#2518
// names it). It must run the same defs Open runs for the same pack.
func TestE2E_ValidateEvals_EvaluateSkipsWhatOpenSkips(t *testing.T) {
	packJSON := e2ePack(promptOverridesOK, structurallyBadEvals, "")
	conv, _ := openE2E(t, packJSON, "hello", sdk.WithSkipSchemaValidation())
	resp, err := conv.Send(context.Background(), "hi")
	require.NoError(t, err)
	require.NotEmpty(t, resp.Text())

	results, err := sdk.Evaluate(context.Background(), sdk.EvaluateOpts{
		PackData:   []byte(packJSON),
		PromptName: "chat",
		Messages:   conv.Messages(context.Background()),
		SessionID:  conv.ID(),
	})
	require.NoError(t, err)
	got := map[string]int{}
	for i := range results {
		got[results[i].EvalID]++
	}
	assert.Equal(t, map[string]int{"ok": 1, "dup": 1}, got, "Evaluate ran defs Open skips")
	for i := range results {
		if results[i].EvalID == "ok" {
			assert.InDelta(t, 1.0, score(t, results[i]), 1e-9, "the prompt's override (min 1) runs, not min 1000")
		}
	}
}

// unknownParamEvals misspells a key in a direct eval, in a wrapper's
// eval_params, and on a judge.
const unknownParamEvals = `
	{"id": "fine", "type": "content_excludes", "trigger": "every_turn", "params": {"patterns": ["zzz"]}},
	{"id": "typo-direct", "type": "content_excludes", "trigger": "every_turn",
	 "params": {"patterns": ["zzz"], "match_mod": "word_boundary"}},
	{"id": "typo-wrapped", "type": "assertion", "trigger": "every_turn",
	 "params": {"eval_type": "content_excludes", "eval_params": {"patterns": ["zzz"], "case_sensitiv": true}}},
	{"id": "typo-judge", "type": "llm_judge", "trigger": "every_turn", "params": {"critera": "polite"}}`

// A key a check does not read keeps the eval from running as written. Open
// skips the def, with the SDK's own runner and with a host's own
// (WithEvalRunner, the documented route); sdk.Evaluate returns an error result
// naming the key. The judge is never called.
func TestE2E_UnknownParams_EvalNeverRunsAsWritten(t *testing.T) {
	for name, extra := range map[string][]sdk.Option{
		"sdk runner":  nil,
		"host runner": {sdk.WithEvalRunner(evals.NewEvalRunner(evals.NewEvalTypeRegistry()))},
	} {
		t.Run(name, func(t *testing.T) {
			judge := &countingJudge{}
			opts := append([]sdk.Option{sdk.WithJudgeProvider(judge)}, extra...)
			conv, log := openE2E(t, e2ePack("", unknownParamEvals, ""), "hello", opts...)
			sendAndClose(t, conv, "hi")
			assert.Equal(t, map[string]int{"fine": 1}, log.ids())
			assert.Zero(t, judge.calls, "a judge with a misspelled criteria param was called")
		})
	}
	t.Run("Evaluate", func(t *testing.T) {
		judge := &countingJudge{}
		bus := events.NewEventBus()
		ec := newEventCollector(bus)
		results, err := sdk.Evaluate(context.Background(), sdk.EvaluateOpts{
			PackData:      []byte(e2ePack("", unknownParamEvals, "")),
			PromptName:    "chat",
			JudgeProvider: judge,
			EventBus:      bus,
			Messages:      []types.Message{types.NewUserMessage("hi"), types.NewAssistantMessage("hello")},
		})
		require.NoError(t, err)
		bus.Close()
		byID := map[string]evals.EvalResult{}
		var order []string
		for i := range results {
			byID[results[i].EvalID] = results[i]
			order = append(order, results[i].EvalID)
		}
		assert.Equal(t, []string{"fine", "typo-direct", "typo-wrapped", "typo-judge"}, order, "results in def order")
		assert.Equal(t, events.EvalKindAssertion, byID["typo-wrapped"].Kind, "a rejected assertion keeps its role")
		failed := map[string]bool{}
		for _, e := range ec.ofType(events.EventEvalFailed) {
			if d, ok := e.Data.(*events.EvalEventData); ok {
				failed[d.EvalID] = true
			}
		}
		assert.Equal(t, map[string]bool{"typo-direct": true, "typo-wrapped": true, "typo-judge": true}, failed,
			"each rejected def reaches the bus as eval.failed")
		assert.Empty(t, byID["fine"].Error)
		for id, key := range map[string]string{
			"typo-direct": "match_mod", "typo-wrapped": "case_sensitiv", "typo-judge": "critera",
		} {
			assert.Containsf(t, byID[id].Error, `"`+key+`"`, "%s ran as if %q were not there", id, key)
			assert.Nilf(t, byID[id].Score, "%s: a def that did not run has no score", id)
		}
		assert.Zero(t, judge.calls, "a judge with a misspelled criteria param was called")
	})

	issues, err := sdk.ValidatePack(writePackFile(t, e2ePack("", unknownParamEvals, "")), false)
	require.NoError(t, err)
	reasons := map[string]string{}
	for _, is := range issues {
		reasons[is.ID] = is.Reason
	}
	assert.Contains(t, reasons["typo-direct"], `unknown param "match_mod"`)
	assert.Contains(t, reasons["typo-direct"], "match_mode", "the error lists the valid params")
	assert.Contains(t, reasons["typo-wrapped"], `"case_sensitiv"`)
	assert.Contains(t, reasons["typo-judge"], `"critera"`)
}

// A validator with a key its check does not read fails Open, naming the key
// and the valid ones.
func TestE2E_UnknownParams_ValidatorFailsOpen(t *testing.T) {
	packJSON := e2ePack(`,
			"validators": [{"type": "banned_words", "params": {"wrods": ["acme"]}}]`, "", "")
	_, err := sdk.Open(writePackFile(t, packJSON), "chat", sdk.WithProvider(replyProvider("hi")))
	require.Error(t, err)
	assert.True(t, errors.Is(err, guardrails.ErrInvalidGuardrailParams), "got %v", err)
	assert.Contains(t, err.Error(), `"wrods"`)
	assert.Contains(t, err.Error(), "words")
}

// strictHandler is a custom check that lists its params (evals.ParamNamer)
// and declares itself long-running (evals.GroupDeclarer).
type strictHandler struct{}

func (strictHandler) Type() string { return "e2e_strict" }
func (strictHandler) Eval(context.Context, *evals.EvalContext, map[string]any) (*evals.EvalResult, error) {
	s := 1.0
	return &evals.EvalResult{Score: &s}, nil
}
func (strictHandler) ParamNames() []string { return []string{"level"} }
func (strictHandler) EvalGroups() []string { return []string{evals.GroupLongRunning} }

// looseHandler lists nothing, so any key is accepted.
type looseHandler struct{}

func (looseHandler) Type() string { return "e2e_loose" }
func (looseHandler) Eval(context.Context, *evals.EvalContext, map[string]any) (*evals.EvalResult, error) {
	s := 1.0
	return &evals.EvalResult{Score: &s}, nil
}

// A host's custom handler is checked only when it implements ParamNamer.
func TestE2E_ParamNamer_CustomHandlers(t *testing.T) {
	reg := evals.NewEvalTypeRegistry()
	reg.Register(strictHandler{})
	reg.Register(looseHandler{})
	packJSON := e2ePack("", `
	{"id": "strict-ok", "type": "e2e_strict", "trigger": "every_turn", "params": {"level": 1}},
	{"id": "strict-typo", "type": "e2e_strict", "trigger": "every_turn", "params": {"levle": 1}},
	{"id": "loose-anything", "type": "e2e_loose", "trigger": "every_turn", "params": {"whatever": 1}}`, "")
	conv, log := openE2E(t, packJSON, "hello", sdk.WithEvalRegistry(reg))
	sendAndClose(t, conv, "hi")

	assert.Equal(t, map[string]int{"strict-ok": 1, "loose-anything": 1}, log.ids())
	names, ok := reg.ParamNames("e2e_strict")
	require.True(t, ok)
	assert.Equal(t, []string{"level"}, names)
}

// sentence_count's max shapes the score, and a max_sentences guardrail blocks
// a reply over max with its message rather than cutting it to max bytes.
func TestE2E_SentenceCount_MaxScoresAndGuardrailBlocks(t *testing.T) {
	packJSON := e2ePack(`,
			"validators": [{"type": "max_sentences", "params": {"max": 2}, "message": "Too long."}]`, `
	{"id": "count", "type": "sentence_count", "trigger": "every_turn", "params": {"max": 4}},
	{"id": "uncapped", "type": "sentence_count", "trigger": "every_turn"}`, "")

	conv, log := openE2E(t, packJSON, "One. Two. Three.")
	resp := sendAndClose(t, conv, "talk")[0]
	assert.Equal(t, "Too long.", resp.Text(), "a reply over max is blocked with the validator's message")
	assert.InDelta(t, 1.0, score(t, log.only(t, "count")), 1e-9, "the blocked reply is within the eval's max 4")

	conv2, log2 := openE2E(t, e2ePack("", `
	{"id": "count", "type": "sentence_count", "trigger": "every_turn", "params": {"max": 2}},
	{"id": "uncapped", "type": "sentence_count", "trigger": "every_turn"}`, ""), "One. Two. Three. Four.")
	sendAndClose(t, conv2, "talk")
	assert.InDelta(t, 0.5, score(t, log2.only(t, "count")), 1e-9, "max 2 over 4 sentences scores max/count")
	assert.InDelta(t, 1.0, score(t, log2.only(t, "uncapped")), 1e-9, "without max the score is 1")
	assert.Equal(t, "none", passed(log2.only(t, "count")), "a plain eval states no verdict")
}

// A max that is not a positive integer is rejected at load.
func TestE2E_SentenceCount_NonIntegerMaxRejected(t *testing.T) {
	for _, bad := range []string{"0", "1.5", `"3"`} {
		t.Run(bad, func(t *testing.T) {
			packJSON := e2ePack(`,
			"validators": [{"type": "max_sentences", "params": {"max": `+bad+`}}]`, "", "")
			_, err := sdk.Open(writePackFile(t, packJSON), "chat", sdk.WithProvider(replyProvider("hi")))
			require.Error(t, err, "max %s must fail Open", bad)
			assert.Contains(t, err.Error(), "positive integer")
		})
	}
}

// case_sensitive on banned_words: a pattern blocks only in its own case, on
// Send and on Stream.
func TestE2E_CaseSensitive_BannedWords(t *testing.T) {
	packJSON := e2ePack(`,
			"validators": [{"type": "banned_words", "params": {"words": ["Acme"], "case_sensitive": true},
			                "message": "Blocked."}]`, "", "")
	cases := map[string]string{"acme is fine": "acme is fine", "Acme is here": "Blocked."}
	for reply, want := range cases {
		t.Run(reply, func(t *testing.T) {
			conv, _ := openE2E(t, packJSON, reply)
			resp, err := conv.Send(context.Background(), "hi")
			require.NoError(t, err)
			assert.Equal(t, want, resp.Text(), "Send")

			conv2, _ := openE2E(t, packJSON, reply)
			assert.Equal(t, want, streamFinalText(t, conv2), "Stream")
		})
	}
}

// streamFinalText streams one turn and returns the final response's text.
func streamFinalText(t *testing.T, conv *sdk.Conversation) string {
	t.Helper()
	var final *sdk.Response
	for chunk := range conv.Stream(context.Background(), "hi") {
		require.NoError(t, chunk.Error)
		if chunk.Type == sdk.ChunkDone {
			final = chunk.Message
		}
	}
	require.NotNil(t, final, "stream ended without a done chunk")
	return final.Text()
}

// Only the length check truncates, at a rune boundary, on Send and Stream.
func TestE2E_GuardrailTruncation_MaxLengthOnly(t *testing.T) {
	packJSON := e2ePack(`,
			"validators": [{"type": "max_length", "params": {"max": 10}}]`, "", "")
	conv, _ := openE2E(t, packJSON, "Hello world, this is long")
	resp, err := conv.Send(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "Hello worl", resp.Text())

	conv2, _ := openE2E(t, packJSON, "Hello world, this is long")
	assert.Equal(t, "Hello worl", streamFinalText(t, conv2))

	// "é" is two bytes: after the leading "a", a cut at byte 10 lands inside one.
	multi := "a" + strings.Repeat("é", 8)
	conv3, _ := openE2E(t, packJSON, multi)
	resp3, err := conv3.Send(context.Background(), "hi")
	require.NoError(t, err)
	assert.True(t, utf8.ValidString(resp3.Text()), "truncation split a rune: %q", resp3.Text())
	assert.Equal(t, "aéééé", resp3.Text(), "the cut backs off to the start of the split character")

	conv4, _ := openE2E(t, packJSON, multi)
	streamed := streamFinalText(t, conv4)
	assert.True(t, utf8.ValidString(streamed), "streamed truncation split a rune: %q", streamed)
	assert.Equal(t, "aéééé", streamed)
}

// judgeDefaultsCheck compiles: the constant the judge_defaults test relies on.
var _ = handlers.MetadataJudgeDefaults

// multiPromptBadPack has the same faults in several prompts.
const multiPromptBadPack = `{
	"id": "e2e-multi",
	"name": "E2E multi",
	"version": "1.0.0",
	"template_engine": {"version": "v1", "syntax": "{{variable}}"},
	"prompts": {
		"alpha": {"id": "alpha", "name": "Alpha", "version": "1.0.0", "system_template": "A.",
			"validators": [{"type": "banned_words", "params": {"wrods": ["x"]}}],
			"evals": [{"id": "fine", "type": "contains", "trigger": "every_turn", "params": {"patterns": ["a"]}},
			          {"id": "bad", "type": "contains", "trigger": "every_turn", "params": {"patern": ["a"]}}]},
		"bravo": {"id": "bravo", "name": "Bravo", "version": "1.0.0", "system_template": "B.",
			"validators": [{"type": "banned_words", "params": {"wrods": ["x"]}}],
			"evals": [{"id": "bad", "type": "contains", "trigger": "every_turn", "params": {"patern": ["a"]}}]},
		"charlie": {"id": "charlie", "name": "Charlie", "version": "1.0.0", "system_template": "C.",
			"validators": [{"type": "banned_words", "params": {"wrods": ["x"]}}]},
		"delta": {"id": "delta", "name": "Delta", "version": "1.0.0", "system_template": "D.",
			"validators": [{"type": "banned_words", "params": {"wrods": ["x"]}}]}
	}
}`

// sdk.ValidatePack reports a multi-prompt pack's issues in one stable order,
// prompts sorted, and each eval issue at the index of the def it names.
func TestE2E_ValidatePack_StableOrderAndIndex(t *testing.T) {
	path := writePackFile(t, multiPromptBadPack)
	first, err := sdk.ValidatePack(path, false)
	require.NoError(t, err)
	var order []string
	for _, is := range first {
		order = append(order, is.String())
	}
	for range 20 {
		again, err := sdk.ValidatePack(path, false)
		require.NoError(t, err)
		var got []string
		for _, is := range again {
			got = append(got, is.String())
		}
		require.Equal(t, order, got, "ValidatePack's order changed between runs")
	}
	var prompts []string
	for _, is := range first {
		if len(prompts) == 0 || prompts[len(prompts)-1] != is.PromptID+"/"+is.Kind {
			prompts = append(prompts, is.PromptID+"/"+is.Kind)
		}
		if is.Kind == "eval" && is.PromptID == "alpha" {
			assert.Equal(t, 1, is.Index, "alpha's bad eval is its second: %s", is.String())
		}
	}
	assert.Equal(t, []string{
		"alpha/validator", "bravo/validator", "charlie/validator", "delta/validator", "alpha/eval", "bravo/eval",
	}, prompts)
}

// sdk.Evaluate reports a def with an unusable `when` as an error result, as
// the runner always has (#1931), rather than dropping it.
func TestE2E_Evaluate_UnusableWhenIsAnErrorResult(t *testing.T) {
	packJSON := e2ePack("", `
	{"id": "gated", "type": "contains", "trigger": "every_turn", "params": {"patterns": ["x"]},
	 "when": {"tool_called": "lookup", "tool_caled": "typo"}}`, "")
	results, err := sdk.Evaluate(context.Background(), sdk.EvaluateOpts{
		PackData: []byte(packJSON), PromptName: "chat",
		Messages: []types.Message{types.NewAssistantMessage("x")},
	})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "gated", results[0].EvalID)
	assert.NotEmpty(t, results[0].Error)
}
