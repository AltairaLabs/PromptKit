package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AltairaLabs/PromptKit/runtime/a2a"
	"github.com/AltairaLabs/PromptKit/runtime/evals"
	"github.com/AltairaLabs/PromptKit/runtime/types"
)

func TestA2AEvalHandler_Type(t *testing.T) {
	t.Parallel()
	h := &A2AEvalHandler{}
	if h.Type() != "a2a_eval" {
		t.Errorf("got %q, want %q", h.Type(), "a2a_eval")
	}
}

func TestA2AEvalSessionHandler_Type(t *testing.T) {
	t.Parallel()
	h := &A2AEvalSessionHandler{}
	if h.Type() != "a2a_eval_session" {
		t.Errorf("got %q, want %q", h.Type(), "a2a_eval_session")
	}
}

func TestA2AEvalHandler_MissingAgentURL(t *testing.T) {
	t.Parallel()
	h := &A2AEvalHandler{}
	result, err := h.Eval(context.Background(), &evals.EvalContext{}, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected Passed=false for missing agent_url")
	}
	if result.Explanation != "a2a_eval requires an 'agent_url' param" {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}

// mockA2AServer creates a test server that responds to A2A JSON-RPC requests.
func mockA2AServer(t *testing.T, responseText string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/a2a" {
			http.NotFound(w, r)
			return
		}

		var rpcReq struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			ID     int64           `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&rpcReq); err != nil {
			t.Errorf("failed to decode rpc request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		text := responseText
		task := a2a.Task{
			ID: "test-task-1",
			Status: a2a.TaskStatus{
				State: a2a.TaskStateCompleted,
				Message: &a2a.Message{
					Role:  a2a.RoleAgent,
					Parts: []a2a.Part{{Text: &text}},
				},
			},
		}

		taskJSON, _ := json.Marshal(task)
		rpcResp := map[string]any{
			"jsonrpc": "2.0",
			"id":      rpcReq.ID,
			"result":  json.RawMessage(taskJSON),
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rpcResp)
	}))
}

func TestA2AEvalHandler_SuccessfulEval(t *testing.T) {
	t.Parallel()
	server := mockA2AServer(t, `{"passed": true, "score": 0.9, "reasoning": "Good response"}`)
	defer server.Close()

	h := &A2AEvalHandler{}
	evalCtx := &evals.EvalContext{
		CurrentOutput: "helpful answer",
		Messages: []types.Message{
			{Role: "user", Content: "help me"},
			{Role: "assistant", Content: "helpful answer"},
		},
	}
	params := map[string]any{
		"agent_url": server.URL,
		"criteria":  "Is it helpful?",
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected Passed=true, got explanation: %s", result.Explanation)
	}
	if result.Score == nil || *result.Score != 0.9 {
		t.Errorf("expected score 0.9, got %v", result.Score)
	}
	if result.Type != "a2a_eval" {
		t.Errorf("expected type a2a_eval, got %s", result.Type)
	}
}

func TestA2AEvalHandler_FailedEval(t *testing.T) {
	t.Parallel()
	server := mockA2AServer(t, `{"passed": false, "score": 0.2, "reasoning": "Poor response"}`)
	defer server.Close()

	h := &A2AEvalHandler{}
	evalCtx := &evals.EvalContext{CurrentOutput: "bad answer"}
	params := map[string]any{"agent_url": server.URL}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected Passed=false")
	}
	if result.Score == nil || *result.Score != 0.2 {
		t.Errorf("expected score 0.2, got %v", result.Score)
	}
}

func TestA2AEvalHandler_AgentError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/a2a" {
			http.NotFound(w, r)
			return
		}
		var rpcReq struct {
			ID int64 `json:"id"`
		}
		json.NewDecoder(r.Body).Decode(&rpcReq)

		rpcResp := map[string]any{
			"jsonrpc": "2.0",
			"id":      rpcReq.ID,
			"error": map[string]any{
				"code":    -32000,
				"message": "agent processing failed",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rpcResp)
	}))
	defer server.Close()

	h := &A2AEvalHandler{}
	evalCtx := &evals.EvalContext{CurrentOutput: "test"}
	params := map[string]any{"agent_url": server.URL}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected Passed=false for agent error")
	}
}

func TestA2AEvalHandler_Timeout(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte("{}"))
	}))
	defer server.Close()

	h := &A2AEvalHandler{}
	evalCtx := &evals.EvalContext{CurrentOutput: "test"}
	params := map[string]any{
		"agent_url": server.URL,
		"timeout":   "100ms",
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected Passed=false for timeout")
	}
}

func TestA2AEvalHandler_AuthToken(t *testing.T) {
	t.Setenv("A2A_EVAL_TEST_TOKEN", "my-secret-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/a2a" {
			http.NotFound(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer my-secret-token" {
			t.Errorf("expected auth 'Bearer my-secret-token', got %q", auth)
		}

		var rpcReq struct {
			ID int64 `json:"id"`
		}
		json.NewDecoder(r.Body).Decode(&rpcReq)

		text := `{"passed": true, "score": 1.0}`
		task := a2a.Task{
			ID: "test-task",
			Status: a2a.TaskStatus{
				State: a2a.TaskStateCompleted,
				Message: &a2a.Message{
					Role:  a2a.RoleAgent,
					Parts: []a2a.Part{{Text: &text}},
				},
			},
		}
		taskJSON, _ := json.Marshal(task)
		rpcResp := map[string]any{
			"jsonrpc": "2.0",
			"id":      rpcReq.ID,
			"result":  json.RawMessage(taskJSON),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rpcResp)
	}))
	defer server.Close()

	h := &A2AEvalHandler{}
	evalCtx := &evals.EvalContext{CurrentOutput: "test"}
	params := map[string]any{
		"agent_url":  server.URL,
		"auth_token": "${A2A_EVAL_TEST_TOKEN}",
	}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected Passed=true, got explanation: %s", result.Explanation)
	}
}

func TestA2AEvalSessionHandler_AggregatesContent(t *testing.T) {
	t.Parallel()
	var receivedReq ExternalEvalRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/a2a" {
			http.NotFound(w, r)
			return
		}

		var rpcReq struct {
			Params json.RawMessage `json:"params"`
			ID     int64           `json:"id"`
		}
		json.NewDecoder(r.Body).Decode(&rpcReq)

		// Parse the SendMessageRequest to extract the message text
		var smr a2a.SendMessageRequest
		json.Unmarshal(rpcReq.Params, &smr)

		// The message text contains the JSON context — parse it to verify
		if len(smr.Message.Parts) > 0 && smr.Message.Parts[0].Text != nil {
			msgText := *smr.Message.Parts[0].Text
			// Extract the JSON from the context section
			if idx := len("Context:\n"); idx > 0 {
				ctxStart := len(msgText) - 1
				for i := 0; i < len(msgText); i++ {
					if msgText[i:] == "Context:\n"[:min(9, len(msgText)-i)] {
						ctxStart = i + 9
						break
					}
				}
				if ctxStart < len(msgText) {
					json.Unmarshal([]byte(msgText[ctxStart:]), &receivedReq)
				}
			}
		}

		text := `{"passed": true, "score": 0.85, "reasoning": "good session"}`
		task := a2a.Task{
			ID: "test-task",
			Status: a2a.TaskStatus{
				State: a2a.TaskStateCompleted,
				Message: &a2a.Message{
					Role:  a2a.RoleAgent,
					Parts: []a2a.Part{{Text: &text}},
				},
			},
		}
		taskJSON, _ := json.Marshal(task)
		rpcResp := map[string]any{
			"jsonrpc": "2.0",
			"id":      rpcReq.ID,
			"result":  json.RawMessage(taskJSON),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rpcResp)
	}))
	defer server.Close()

	h := &A2AEvalSessionHandler{}
	evalCtx := &evals.EvalContext{
		Messages: []types.Message{
			{Role: "user", Content: "question 1"},
			{Role: "assistant", Content: "answer 1"},
			{Role: "user", Content: "question 2"},
			{Role: "assistant", Content: "answer 2"},
		},
	}
	params := map[string]any{"agent_url": server.URL}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Passed {
		t.Errorf("expected Passed=true, got explanation: %s", result.Explanation)
	}
	if result.Type != "a2a_eval_session" {
		t.Errorf("expected type a2a_eval_session, got %s", result.Type)
	}
}

func TestA2AEvalHandler_NoTextResponse(t *testing.T) {
	t.Parallel()
	// Server returns a task with no text parts
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/a2a" {
			http.NotFound(w, r)
			return
		}
		var rpcReq struct {
			ID int64 `json:"id"`
		}
		json.NewDecoder(r.Body).Decode(&rpcReq)

		task := a2a.Task{
			ID: "test-task",
			Status: a2a.TaskStatus{
				State: a2a.TaskStateCompleted,
			},
		}
		taskJSON, _ := json.Marshal(task)
		rpcResp := map[string]any{
			"jsonrpc": "2.0",
			"id":      rpcReq.ID,
			"result":  json.RawMessage(taskJSON),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(rpcResp)
	}))
	defer server.Close()

	h := &A2AEvalHandler{}
	evalCtx := &evals.EvalContext{CurrentOutput: "test"}
	params := map[string]any{"agent_url": server.URL}

	result, err := h.Eval(context.Background(), evalCtx, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Passed {
		t.Error("expected Passed=false for no text response")
	}
	if result.Explanation != "a2a agent returned no text response" {
		t.Errorf("unexpected explanation: %s", result.Explanation)
	}
}

func TestA2AEvalHandler_MinScore(t *testing.T) {
	t.Parallel()

	t.Run("passes with lower threshold", func(t *testing.T) {
		t.Parallel()
		server := mockA2AServer(t, `{"score": 0.7, "reasoning": "decent"}`)
		defer server.Close()

		h := &A2AEvalHandler{}
		evalCtx := &evals.EvalContext{CurrentOutput: "test"}
		params := map[string]any{
			"agent_url": server.URL,
			"min_score": 0.6,
		}
		result, err := h.Eval(context.Background(), evalCtx, params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Passed {
			t.Errorf("expected Passed=true with min_score=0.6, explanation=%s", result.Explanation)
		}
	})

	t.Run("fails with higher threshold", func(t *testing.T) {
		t.Parallel()
		server := mockA2AServer(t, `{"score": 0.7, "reasoning": "decent"}`)
		defer server.Close()

		h := &A2AEvalHandler{}
		evalCtx := &evals.EvalContext{CurrentOutput: "test"}
		params := map[string]any{
			"agent_url": server.URL,
			"min_score": 0.8,
		}
		result, err := h.Eval(context.Background(), evalCtx, params)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Passed {
			t.Error("expected Passed=false with min_score=0.8")
		}
	})
}

func TestExtractA2AResponseText(t *testing.T) {
	t.Parallel()

	t.Run("from status message", func(t *testing.T) {
		t.Parallel()
		text := "hello world"
		task := &a2a.Task{
			Status: a2a.TaskStatus{
				Message: &a2a.Message{
					Parts: []a2a.Part{{Text: &text}},
				},
			},
		}
		got := extractA2AResponseText(task)
		if got != "hello world" {
			t.Errorf("got %q, want %q", got, "hello world")
		}
	})

	t.Run("from artifacts", func(t *testing.T) {
		t.Parallel()
		text := "artifact text"
		task := &a2a.Task{
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
			Artifacts: []a2a.Artifact{
				{Parts: []a2a.Part{{Text: &text}}},
			},
		}
		got := extractA2AResponseText(task)
		if got != "artifact text" {
			t.Errorf("got %q, want %q", got, "artifact text")
		}
	})

	t.Run("empty task", func(t *testing.T) {
		t.Parallel()
		task := &a2a.Task{
			Status: a2a.TaskStatus{State: a2a.TaskStateCompleted},
		}
		got := extractA2AResponseText(task)
		if got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})
}

func TestBuildA2AMessageText(t *testing.T) {
	t.Parallel()

	t.Run("with criteria", func(t *testing.T) {
		t.Parallel()
		text := buildA2AMessageText("Be helpful", `{"current_output": "test"}`)
		if text == "" {
			t.Error("expected non-empty text")
		}
		// Should contain criteria and context
		if !containsInsensitive(text, "Be helpful") {
			t.Error("expected text to contain criteria")
		}
		if !containsInsensitive(text, "current_output") {
			t.Error("expected text to contain request JSON")
		}
	})

	t.Run("without criteria", func(t *testing.T) {
		t.Parallel()
		text := buildA2AMessageText("", `{"current_output": "test"}`)
		if containsInsensitive(text, "Criteria:") {
			t.Error("expected no criteria section when empty")
		}
	})
}
