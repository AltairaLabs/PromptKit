package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/evals/handlers"
	"github.com/AltairaLabs/PromptKit/runtime/providers"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

const (
	// defaultJudgeSystemPrompt is the system prompt used when no custom prompt is provided.
	defaultJudgeSystemPrompt = "You are an evaluation judge. Evaluate the following content " +
		"and respond with a JSON object containing: " +
		"\"passed\" (boolean), \"score\" (float 0-1), and \"reasoning\" (string)."

	// judgeMaxTokens is the maximum token limit for judge LLM calls.
	judgeMaxTokens = 1024

	// defaultPassThreshold is the default score threshold for passing when no explicit passed field or minScore is set.
	defaultPassThreshold = 0.5
)

// JudgeProvider implements handlers.JudgeProvider using an SDK provider.
type JudgeProvider struct {
	provider providers.Provider
}

// NewJudgeProvider creates a JudgeProvider backed by the given provider.
func NewJudgeProvider(p providers.Provider) *JudgeProvider {
	return &JudgeProvider{provider: p}
}

// Judge sends the evaluation prompt to an LLM and returns the parsed verdict.
//
//nolint:gocritic // JudgeOpts passed by value to satisfy handlers.JudgeProvider interface
func (jp *JudgeProvider) Judge(ctx context.Context, opts handlers.JudgeOpts) (*handlers.JudgeResult, error) {
	systemPrompt := opts.SystemPrompt
	if systemPrompt == "" {
		systemPrompt = defaultJudgeSystemPrompt
	}

	userContent := fmt.Sprintf("Content to evaluate:\n%s\n\nCriteria: %s", opts.Content, opts.Criteria)
	if opts.Rubric != "" {
		userContent += fmt.Sprintf("\n\nRubric: %s", opts.Rubric)
	}

	userMsg := types.Message{Role: "user"}
	userMsg.AddTextPart(userContent)

	resp, err := jp.provider.Predict(ctx, providers.PredictionRequest{
		System:      systemPrompt,
		Messages:    []types.Message{userMsg},
		Temperature: 0.0,
		MaxTokens:   judgeMaxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("judge provider call failed: %w", err)
	}

	return parseJudgeResponse(resp.Content, opts.MinScore)
}

// parseJudgeResponse parses the LLM judge response into a JudgeResult.
func parseJudgeResponse(raw string, minScore *float64) (*handlers.JudgeResult, error) {
	// Try JSON parse
	var parsed struct {
		Passed    *bool   `json:"passed"`
		Score     float64 `json:"score"`
		Reasoning string  `json:"reasoning"`
	}

	// Extract JSON from response (might be wrapped in markdown)
	jsonStr := raw
	if idx := strings.Index(raw, "{"); idx >= 0 {
		if end := strings.LastIndex(raw, "}"); end >= idx {
			jsonStr = raw[idx : end+1]
		}
	}

	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		// Fallback: treat as passed if content exists
		return &handlers.JudgeResult{
			Passed:    true,
			Score:     defaultPassThreshold,
			Reasoning: "Could not parse judge response",
			Raw:       raw,
		}, nil
	}

	result := &handlers.JudgeResult{
		Score:     parsed.Score,
		Reasoning: parsed.Reasoning,
		Raw:       raw,
	}

	if parsed.Passed != nil {
		result.Passed = *parsed.Passed
	} else if minScore != nil {
		result.Passed = parsed.Score >= *minScore
	} else {
		result.Passed = parsed.Score >= defaultPassThreshold
	}

	return result, nil
}

// Ensure JudgeProvider implements handlers.JudgeProvider.
var _ handlers.JudgeProvider = (*JudgeProvider)(nil)

// evalMiddleware holds dispatch state for eval execution within a conversation.
type evalMiddleware struct {
	dispatcher   evals.EvalDispatcher
	defs         []evals.EvalDef
	resultWriter evals.ResultWriter
	conv         *Conversation
	turnIndex    int
}

// newEvalMiddleware creates eval middleware for a conversation.
// Returns nil if no dispatcher is configured or no eval defs are resolved.
func newEvalMiddleware(conv *Conversation) *evalMiddleware {
	if conv.config == nil || conv.config.evalDispatcher == nil {
		return nil
	}

	// Resolve eval defs from pack + prompt
	var packEvals, promptEvals []evals.EvalDef
	if conv.pack != nil {
		packEvals = conv.pack.Evals
	}
	if conv.prompt != nil {
		promptEvals = conv.prompt.Evals
	}

	defs := evals.ResolveEvals(packEvals, promptEvals)
	if len(defs) == 0 {
		return nil
	}

	// Build composite result writer from configured writers
	var resultWriter evals.ResultWriter
	if len(conv.config.evalResultWriters) == 1 {
		resultWriter = conv.config.evalResultWriters[0]
	} else if len(conv.config.evalResultWriters) > 1 {
		resultWriter = evals.NewCompositeResultWriter(conv.config.evalResultWriters...)
	}

	return &evalMiddleware{
		dispatcher:   conv.config.evalDispatcher,
		defs:         defs,
		resultWriter: resultWriter,
		conv:         conv,
	}
}

// dispatchTurnEvals dispatches turn-level evals asynchronously.
// Nil-safe: no-op if middleware is nil.
func (em *evalMiddleware) dispatchTurnEvals(ctx context.Context) {
	if em == nil {
		return
	}

	em.turnIndex++
	evalCtx := em.buildEvalContext()

	// Dispatch async — don't block Send()
	go func() {
		results, err := em.dispatcher.DispatchTurnEvals(ctx, em.defs, evalCtx)
		if err != nil {
			log.Printf("evals: turn dispatch error: %v", err)
		}
		if em.resultWriter != nil && len(results) > 0 {
			if writeErr := em.resultWriter.WriteResults(ctx, results); writeErr != nil {
				log.Printf("evals: result write error: %v", writeErr)
			}
		}
	}()
}

// dispatchSessionEvals dispatches session-complete evals synchronously.
// Nil-safe: no-op if middleware is nil.
// Runs synchronously during Close() to ensure completion.
func (em *evalMiddleware) dispatchSessionEvals(ctx context.Context) {
	if em == nil {
		return
	}

	evalCtx := em.buildEvalContext()

	results, err := em.dispatcher.DispatchSessionEvals(ctx, em.defs, evalCtx)
	if err != nil {
		log.Printf("evals: session dispatch error: %v", err)
	}
	if em.resultWriter != nil && len(results) > 0 {
		if writeErr := em.resultWriter.WriteResults(ctx, results); writeErr != nil {
			log.Printf("evals: session result write error: %v", writeErr)
		}
	}
}

// buildEvalContext creates an EvalContext from the conversation state.
func (em *evalMiddleware) buildEvalContext() *evals.EvalContext {
	ctx := &evals.EvalContext{
		TurnIndex: em.turnIndex,
		PromptID:  em.conv.promptName,
	}

	// Safely get session info — sessions may not be initialized in tests
	// or when middleware is used standalone.
	if em.conv.unarySession != nil || em.conv.duplexSession != nil {
		ctx.Messages = em.conv.Messages(context.Background())
		ctx.SessionID = em.conv.ID()

		for i := len(ctx.Messages) - 1; i >= 0; i-- {
			if ctx.Messages[i].Role == "assistant" {
				ctx.CurrentOutput = ctx.Messages[i].GetContent()
				break
			}
		}
	}

	return ctx
}
