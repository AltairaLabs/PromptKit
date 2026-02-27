package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
)

const defaultA2AEvalTimeout = 60 * time.Second

// A2AEvalHandler evaluates a single assistant turn by sending
// conversation context to an A2A agent and interpreting the agent's
// response as a structured eval result.
//
// Params:
//   - agent_url (string, required): A2A agent endpoint URL
//   - auth_token (string, optional): auth token, supports ${ENV_VAR}
//   - timeout (string, optional): request timeout, default 60s
//   - criteria (string, optional): evaluation criteria
//   - include_messages (bool, optional): include conversation history, default true
//   - include_tool_calls (bool, optional): include tool call records, default false
//   - min_score (float64, optional): minimum score threshold
//   - extra (map[string]any, optional): arbitrary data forwarded in request
type A2AEvalHandler struct{}

// Type returns the eval type identifier.
func (h *A2AEvalHandler) Type() string { return "a2a_eval" }

// Eval sends the current assistant output to the configured A2A agent.
func (h *A2AEvalHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	return executeA2AEval(ctx, evalCtx, params, h.Type(), evalCtx.CurrentOutput)
}

// A2AEvalSessionHandler evaluates an entire conversation by sending
// all assistant messages to an A2A agent.
//
// Params: same as A2AEvalHandler.
type A2AEvalSessionHandler struct{}

// Type returns the eval type identifier.
func (h *A2AEvalSessionHandler) Type() string { return "a2a_eval_session" }

// Eval sends all assistant messages to the configured A2A agent.
func (h *A2AEvalSessionHandler) Eval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
) (*evals.EvalResult, error) {
	content := collectAssistantContent(evalCtx)
	return executeA2AEval(ctx, evalCtx, params, h.Type(), content)
}

// executeA2AEval is the shared implementation for both A2A eval handlers.
func executeA2AEval(
	ctx context.Context,
	evalCtx *evals.EvalContext,
	params map[string]any,
	evalType string,
	content string,
) (*evals.EvalResult, error) {
	agentURL, ok := params["agent_url"].(string)
	if !ok || agentURL == "" {
		return &evals.EvalResult{
			Type:        evalType,
			Passed:      false,
			Explanation: "a2a_eval requires an 'agent_url' param",
		}, nil
	}

	timeout := parseDuration(params, "timeout", defaultA2AEvalTimeout)
	minScore := extractFloat64Ptr(params, "min_score")

	// Build A2A client with optional auth.
	var clientOpts []a2a.ClientOption
	if token, ok := params["auth_token"].(string); ok && token != "" {
		expanded := expandEnvVars(token)
		if expanded != "" {
			clientOpts = append(clientOpts, a2a.WithAuth("Bearer", expanded))
		}
	}
	client := a2a.NewClient(agentURL, clientOpts...)

	// Build the message content: criteria as text + request context as JSON.
	reqBody := buildExternalRequest(evalCtx, params, content)
	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		return &evals.EvalResult{
			Type:        evalType,
			Passed:      false,
			Explanation: fmt.Sprintf("failed to marshal request: %v", err),
		}, nil
	}

	messageText := buildA2AMessageText(reqBody.Criteria, string(reqJSON))
	textPtr := messageText

	// Set timeout on context.
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Send synchronous message to the A2A agent.
	task, err := client.SendMessage(ctx, &a2a.SendMessageRequest{
		Message: a2a.Message{
			Role:  a2a.RoleUser,
			Parts: []a2a.Part{{Text: &textPtr}},
		},
		Configuration: &a2a.SendMessageConfiguration{
			Blocking: true,
		},
	})
	if err != nil {
		return &evals.EvalResult{
			Type:        evalType,
			Passed:      false,
			Explanation: fmt.Sprintf("a2a agent error: %v", err),
		}, nil
	}

	// Extract text response from the task.
	responseText := extractA2AResponseText(task)
	if responseText == "" {
		return &evals.EvalResult{
			Type:        evalType,
			Passed:      false,
			Explanation: "a2a agent returned no text response",
		}, nil
	}

	result := parseExternalResponse([]byte(responseText), minScore)
	result.Type = evalType
	return result, nil
}

// buildA2AMessageText creates the user message text sent to the eval agent.
func buildA2AMessageText(criteria, requestJSON string) string {
	var sb strings.Builder
	sb.WriteString("You are an evaluation judge. Evaluate the following content and respond with a JSON object ")
	sb.WriteString("containing: \"passed\" (boolean), \"score\" (float 0-1), and \"reasoning\" (string).\n\n")

	if criteria != "" {
		sb.WriteString("Criteria: ")
		sb.WriteString(criteria)
		sb.WriteString("\n\n")
	}

	sb.WriteString("Context:\n")
	sb.WriteString(requestJSON)
	return sb.String()
}

// extractA2AResponseText extracts text content from an A2A task response.
// It checks the status message first, then artifacts.
func extractA2AResponseText(task *a2a.Task) string {
	// Check status message first.
	if task.Status.Message != nil {
		if text := extractPartsText(task.Status.Message.Parts); text != "" {
			return text
		}
	}

	// Check artifacts.
	for i := range task.Artifacts {
		if text := extractPartsText(task.Artifacts[i].Parts); text != "" {
			return text
		}
	}

	return ""
}

// extractPartsText concatenates text parts from A2A message parts.
func extractPartsText(parts []a2a.Part) string {
	var texts []string
	for i := range parts {
		if parts[i].Text != nil {
			texts = append(texts, *parts[i].Text)
		}
	}
	return strings.Join(texts, "\n")
}
