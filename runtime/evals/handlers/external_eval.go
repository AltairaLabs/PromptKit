package handlers

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

const defaultExternalTimeout = 30 * time.Second

// envVarPattern matches ${VAR_NAME} for environment variable interpolation.
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// ExternalEvalRequest is the standard request body sent to external
// eval endpoints (REST) and formatted as context for A2A eval agents.
type ExternalEvalRequest struct {
	CurrentOutput string         `json:"current_output"`
	Messages      []messageView  `json:"messages,omitempty"`
	ToolCalls     []toolCallView `json:"tool_calls,omitempty"`
	Criteria      string         `json:"criteria,omitempty"`
	Variables     map[string]any `json:"variables,omitempty"`
	Extra         map[string]any `json:"extra,omitempty"`
}

// messageView is a simplified message representation for external eval requests.
type messageView struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// externalEvalResponse is the standard response expected from external
// eval endpoints and A2A eval agents.
type externalEvalResponse struct {
	Passed    *bool   `json:"passed"`
	Score     float64 `json:"score"`
	Reasoning string  `json:"reasoning"`
}

// expandEnvVars replaces ${VAR} patterns with their environment variable values.
func expandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		varName := match[2 : len(match)-1]
		return os.Getenv(varName)
	})
}

// buildExternalRequest constructs an ExternalEvalRequest from the eval context and params.
func buildExternalRequest(
	evalCtx *evals.EvalContext,
	params map[string]any,
	content string,
) *ExternalEvalRequest {
	req := &ExternalEvalRequest{
		CurrentOutput: content,
		Variables:     evalCtx.Variables,
	}

	if v, ok := params["criteria"].(string); ok {
		req.Criteria = v
	}

	req.Extra = extractMapAny(params, "extra")

	includeMessages := true
	if v, ok := params["include_messages"].(bool); ok {
		includeMessages = v
	}
	if includeMessages {
		req.Messages = buildMessageViews(evalCtx)
	}

	if extractBool(params, "include_tool_calls") {
		req.ToolCalls = buildToolCallViews(evalCtx)
	}

	return req
}

// buildMessageViews converts eval context messages to simplified message views.
func buildMessageViews(evalCtx *evals.EvalContext) []messageView {
	views := make([]messageView, 0, len(evalCtx.Messages))
	for i := range evalCtx.Messages {
		msg := &evalCtx.Messages[i]
		views = append(views, messageView{
			Role:    msg.Role,
			Content: msg.GetContent(),
		})
	}
	return views
}

// buildToolCallViews converts eval context tool calls to simplified views.
func buildToolCallViews(evalCtx *evals.EvalContext) []toolCallView {
	views := make([]toolCallView, 0, len(evalCtx.ToolCalls))
	for i := range evalCtx.ToolCalls {
		tc := &evalCtx.ToolCalls[i]
		views = append(views, toolCallView{
			Index:  tc.TurnIndex,
			Name:   tc.ToolName,
			Args:   tc.Arguments,
			Result: fmt.Sprintf("%v", tc.Result),
			Error:  tc.Error,
		})
	}
	return views
}

// parseExternalResponse parses the standard {passed, score, reasoning}
// response from an external eval endpoint. Uses the same pass-determination
// logic as parseJudgeResponse.
func parseExternalResponse(body []byte, minScore *float64) *evals.EvalResult {
	var resp externalEvalResponse

	// Extract JSON from response (might be wrapped in text/markdown)
	jsonStr := string(body)
	if idx := strings.Index(jsonStr, "{"); idx >= 0 {
		if end := strings.LastIndex(jsonStr, "}"); end >= idx {
			jsonStr = jsonStr[idx : end+1]
		}
	}

	if err := json.Unmarshal([]byte(jsonStr), &resp); err != nil {
		return &evals.EvalResult{
			Passed:      false,
			Explanation: fmt.Sprintf("failed to parse response: %v", err),
		}
	}

	score := resp.Score
	var passed bool
	if resp.Passed != nil {
		passed = *resp.Passed
	} else if minScore != nil {
		passed = score >= *minScore
	} else {
		passed = score >= defaultPassThreshold
	}

	return &evals.EvalResult{
		Passed:      passed,
		Score:       &score,
		Explanation: resp.Reasoning,
	}
}

// parseDuration extracts a duration param with a default value.
func parseDuration(params map[string]any, key string, defaultVal time.Duration) time.Duration {
	v, ok := params[key].(string)
	if !ok || v == "" {
		return defaultVal
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return defaultVal
	}
	return d
}
