package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

// RAG-shaped eval handlers (faithfulness, answer_relevancy,
// contextual_precision/recall/relevancy, hallucination) all delegate to
// the same JudgeProvider as `llm_judge`. The only differences are the
// default system prompt, default criteria, and the structured input
// they assemble from the EvalContext.
//
// Default prompts are adapted from the public DeepEval and Ragas
// reference implementations (both Apache 2.0). Users can override
// either by setting `system_prompt` or `criteria` in params.

// ragInputs collects all RAG-relevant inputs that handlers may need.
type ragInputs struct {
	question string
	answer   string
	contexts []string
	// reference is the ground-truth answer, used by contextual_recall.
	reference string
}

// extractRAGContexts pulls retrieved chunks from params or metadata.
// Supports three forms in order of preference:
//
//  1. params["contexts"] — list of strings, the canonical form
//  2. params["context"]  — single string convenience form
//  3. params["context_field"] — names a key in evalCtx.Metadata to read;
//     value may be a []string, []any, or a single string
//
// Returns nil if no context is found.
func extractRAGContexts(
	evalCtx *evals.EvalContext, params map[string]any,
) []string {
	if list := extractStringSlice(params, "contexts"); len(list) > 0 {
		return list
	}
	if s, ok := params["context"].(string); ok && s != "" {
		return []string{s}
	}
	if field, ok := params["context_field"].(string); ok && field != "" {
		if evalCtx != nil && evalCtx.Metadata != nil {
			return coerceContextSlice(evalCtx.Metadata[field])
		}
	}
	return nil
}

// coerceContextSlice handles []string, []any, and string values
// from arbitrary metadata.
func coerceContextSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if t != "" {
			return []string{t}
		}
	}
	return nil
}

// extractRAGQuestion returns the last user message text, or the value
// of params["question"] if explicitly supplied. Used by handlers that
// need to evaluate answer relevance to a question.
func extractRAGQuestion(
	evalCtx *evals.EvalContext, params map[string]any,
) string {
	if q, ok := params["question"].(string); ok && q != "" {
		return q
	}
	if evalCtx == nil {
		return ""
	}
	for i := len(evalCtx.Messages) - 1; i >= 0; i-- {
		if strings.EqualFold(evalCtx.Messages[i].Role, "user") {
			return evalCtx.Messages[i].GetContent()
		}
	}
	return ""
}

// extractRAGReference returns the ground-truth answer from
// params["reference"] or params["expected_output"], or empty string.
func extractRAGReference(params map[string]any) string {
	if v, ok := params["reference"].(string); ok && v != "" {
		return v
	}
	if v, ok := params["expected_output"].(string); ok {
		return v
	}
	return ""
}

// buildRAGInputs is the unified collector used by every RAG handler.
func buildRAGInputs(
	evalCtx *evals.EvalContext, params map[string]any,
) ragInputs {
	answer := ""
	if evalCtx != nil {
		answer = evalCtx.CurrentOutput
	}
	return ragInputs{
		question:  extractRAGQuestion(evalCtx, params),
		answer:    answer,
		contexts:  extractRAGContexts(evalCtx, params),
		reference: extractRAGReference(params),
	}
}

// formatContexts joins context chunks into a numbered block for the
// judge prompt.
func formatContexts(contexts []string) string {
	if len(contexts) == 0 {
		return "(none)"
	}
	var b strings.Builder
	for i, c := range contexts {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "[chunk %d] %s", i+1, c)
	}
	return b.String()
}

// ragJudgeCall runs the LLM judge with the supplied content and default
// system prompt / criteria, falling back to user overrides when present.
// All RAG handlers funnel through here to keep their bodies trivial.
func ragJudgeCall(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	evalType string,
	params map[string]any,
	content string,
	defaultSystemPrompt string,
	defaultCriteria string,
) (*evals.EvalResult, error) {
	provider, extractErr := extractJudgeProvider(evalCtx)
	if extractErr != nil {
		return ragErrorResult(evalType, extractErr.Error()), nil
	}

	opts := buildJudgeOpts(content, params)
	opts.Emitter = emitterFromEvalCtx(evalCtx)
	if opts.SystemPrompt == "" {
		opts.SystemPrompt = defaultSystemPrompt
	}
	if opts.Criteria == "" {
		opts.Criteria = defaultCriteria
	}

	judgeResult, judgeErr := provider.Judge(ctx, opts)
	if judgeErr != nil {
		return ragErrorResult(
			evalType, fmt.Sprintf("judge error: %v", judgeErr),
		), nil
	}

	return buildEvalResult(evalType, judgeResult), nil
}

// ragErrorResult produces a standardized failure result with a score
// of zero so any pass_threshold treats the eval as failed.
func ragErrorResult(evalType, msg string) *evals.EvalResult {
	return &evals.EvalResult{
		Type:        evalType,
		Score:       boolScore(false),
		Explanation: msg,
	}
}

// ragNoContextMessage is the standard explanation when a handler that
// needs retrieved context wasn't supplied any.
const ragNoContextMessage = "no context provided: supply 'contexts' ([]string), " +
	"'context' (string), or 'context_field' (metadata key)"

// chunkContextSpec describes a "score retrieved chunks against some
// other text" handler. ContextualPrecisionHandler, ContextualRecallHandler,
// and ContextualRelevancyHandler all collapse to a single call against
// evalChunkContext with different spec values.
type chunkContextSpec struct {
	otherLabel   string // "QUESTION", "REFERENCE ANSWER", …
	otherValueFn func(ragInputs) string
	otherMissing string // explanation when the required other-field is empty
	systemPrompt string
	criteria     string
}

// evalChunkContext is the shared body for the three chunk-vs-other-text
// RAG handlers. Keeps each concrete handler trivial so dupl has no
// near-identical bodies to flag.
func evalChunkContext(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	evalType string,
	params map[string]any,
	spec chunkContextSpec,
) (*evals.EvalResult, error) {
	in := buildRAGInputs(evalCtx, params)
	if len(in.contexts) == 0 {
		return ragErrorResult(evalType, ragNoContextMessage), nil
	}
	other := spec.otherValueFn(in)
	if other == "" {
		return ragErrorResult(evalType, spec.otherMissing), nil
	}

	content := fmt.Sprintf(
		"%s:\n%s\n\nRETRIEVED CHUNKS:\n%s",
		spec.otherLabel, other, formatContexts(in.contexts),
	)
	return ragJudgeCall(
		ctx, evalCtx, evalType, params, content,
		spec.systemPrompt, spec.criteria,
	)
}

const ragNoQuestionMessage = "no question found: supply 'question' param or include a user turn in the session"
const ragNoReferenceMessage = "no ground-truth answer: supply 'reference' (or 'expected_output') in params"

// chunkLabelQuestion is the otherLabel value used by chunk-vs-question
// handlers (contextual_precision, contextual_relevancy). Hoisted to a
// const so goconst stops complaining about the repeated literal.
const chunkLabelQuestion = "QUESTION"
